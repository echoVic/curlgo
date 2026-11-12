package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo/ui"
)

// textKey builds a key press as a text input's OnTextKey would see it.
func textKey(mods ui.Modifiers, key ui.Key) ui.TextKey {
	return ui.TextKey{Mods: mods, Key: key}
}

func shortcutOf(t *testing.T, shortcuts []effectiveShortcut, action string) effectiveShortcut {
	t.Helper()
	for _, shortcut := range shortcuts {
		if shortcut.Action == action {
			return shortcut
		}
	}
	t.Fatalf("缺少动作 %q 的快捷键", action)
	return effectiveShortcut{}
}

func TestShortcutParse(t *testing.T) {
	valid := []struct {
		chord    string
		platform string
		mods     ui.Modifiers
		key      ui.Key
	}{
		{"Mod+N", "darwin", ui.Super, ui.KeyN},
		{"Mod+N", "windows", ui.Ctrl, ui.KeyN},
		{"Ctrl+Enter", "darwin", ui.Ctrl, ui.KeyEnter},
		{"Mod+Comma", "windows", ui.Ctrl, ui.KeyComma},
		{"Escape", "darwin", 0, ui.KeyEscape},
		{"Mod+Shift+F5", "windows", ui.Ctrl | ui.Shift, ui.KeyF5},
		{"Alt+0", "darwin", ui.Alt, ui.Key0},
		{"Mod+Ctrl+Alt+Shift+Z", "darwin", ui.Super | ui.Ctrl | ui.Alt | ui.Shift, ui.KeyZ},
	}
	for _, test := range valid {
		mods, key, err := parseChord(test.chord, test.platform)
		if err != nil {
			t.Fatalf("parseChord(%q, %q) 失败：%v", test.chord, test.platform, err)
		}
		if mods != test.mods || key != test.key {
			t.Fatalf("parseChord(%q, %q) = %v/%v，期望 %v/%v", test.chord, test.platform, mods, key, test.mods, test.key)
		}
	}
	invalid := []struct {
		chord    string
		platform string
	}{
		{"", "darwin"},
		{"Mod", "darwin"},
		{"Mod+", "darwin"},
		{"+N", "darwin"},
		{"Hyper+N", "darwin"},
		{"Mod+Bogus", "darwin"},
		{"Mod+N+J", "darwin"},
		{"N+Mod", "darwin"},
		{"Mod+Mod+N", "darwin"},
		// Mod and Ctrl are the same modifier outside macOS.
		{"Mod+Ctrl+Enter", "windows"},
		{"Mod+Ctrl+Enter", "linux"},
	}
	for _, test := range invalid {
		if _, _, err := parseChord(test.chord, test.platform); err == nil {
			t.Fatalf("parseChord(%q, %q) 应失败", test.chord, test.platform)
		}
	}
}

func TestShortcutFormat(t *testing.T) {
	formats := []struct {
		mods     ui.Modifiers
		key      ui.Key
		platform string
		want     string
	}{
		// The macOS defaults must stay byte identical to what the app showed.
		{ui.Super, ui.KeyN, "darwin", "⌘ N"},
		{ui.Super, ui.KeyS, "darwin", "⌘ S"},
		{ui.Super, ui.KeyEnter, "darwin", "⌘ ↵"},
		{ui.Super, ui.KeyL, "darwin", "⌘ L"},
		{0, ui.KeyEscape, "darwin", "Esc"},
		{ui.Super | ui.Shift, ui.KeyK, "darwin", "⌘⇧ K"},
		{ui.Super, ui.KeyComma, "darwin", "⌘ ,"},
		{ui.Super | ui.Ctrl, ui.KeyJ, "darwin", "⌘⌃ J"},
		// Windows and Linux show Ctrl and named keys.
		{ui.Ctrl, ui.KeyN, "windows", "Ctrl+N"},
		{ui.Ctrl, ui.KeyEnter, "windows", "Ctrl+Enter"},
		{ui.Ctrl, ui.KeyComma, "windows", "Ctrl+,"},
		{ui.Ctrl | ui.Shift, ui.KeyF5, "windows", "Ctrl+Shift+F5"},
		{ui.Ctrl | ui.Alt, ui.KeySemicolon, "windows", "Ctrl+Alt+;"},
		{0, ui.KeyEscape, "windows", "Esc"},
	}
	for _, test := range formats {
		if got := formatChord(test.mods, test.key, test.platform); got != test.want {
			t.Fatalf("formatChord(%v, %v, %q) = %q，期望 %q", test.mods, test.key, test.platform, got, test.want)
		}
	}
}

func TestShortcutRoundTrip(t *testing.T) {
	cases := []struct {
		mods     ui.Modifiers
		key      ui.Key
		platform string
		chord    string
	}{
		{ui.Super | ui.Shift, ui.KeyK, "darwin", "Mod+Shift+K"},
		{ui.Ctrl, ui.KeyEnter, "windows", "Mod+Enter"},
		{ui.Ctrl, ui.KeyEnter, "darwin", "Ctrl+Enter"},
		{ui.Super, ui.KeyComma, "darwin", "Mod+Comma"},
		{0, ui.KeyEscape, "windows", "Escape"},
		{ui.Ctrl | ui.Alt, ui.KeySlash, "linux", "Mod+Alt+Slash"},
	}
	for _, test := range cases {
		tokens, err := chordTokensFromMods(test.mods, test.key, test.platform)
		if err != nil {
			t.Fatalf("chordTokensFromMods(%v, %v, %q) 失败：%v", test.mods, test.key, test.platform, err)
		}
		if got := tokens.String(); got != test.chord {
			t.Fatalf("chordTokensFromMods(%v, %v, %q) = %q，期望 %q", test.mods, test.key, test.platform, got, test.chord)
		}
		mods, key, err := parseChord(test.chord, test.platform)
		if err != nil || mods != test.mods || key != test.key {
			t.Fatalf("往返 %q/%q 失败：%v %v/%v", test.chord, test.platform, err, mods, key)
		}
	}
}

func TestShortcutValidate(t *testing.T) {
	cases := []struct {
		mods     ui.Modifiers
		key      ui.Key
		platform string
		valid    bool
	}{
		// Plain typing keys need a modifier.
		{0, ui.KeyEnter, "darwin", false},
		{0, ui.KeyTab, "windows", false},
		{0, ui.KeyA, "darwin", false},
		{0, ui.Key0, "windows", false},
		{0, ui.KeySpace, "darwin", false},
		{0, ui.KeyBackspace, "windows", false},
		{0, ui.KeyDelete, "darwin", false},
		{0, ui.KeyLeft, "windows", false},
		{ui.Shift, ui.KeyN, "darwin", false},
		// Escape and the function keys stand alone.
		{0, ui.KeyEscape, "darwin", true},
		{0, ui.KeyF5, "windows", true},
		{ui.Shift, ui.KeyF5, "darwin", true},
		// Cmd on macOS, Ctrl elsewhere, Alt everywhere.
		{ui.Super, ui.KeyN, "darwin", true},
		{ui.Ctrl, ui.KeyN, "windows", true},
		{ui.Alt, ui.KeyEnter, "windows", true},
		{ui.Super | ui.Ctrl, ui.KeyN, "darwin", true},
		// The Windows key is not part of the chord grammar.
		{ui.Super, ui.KeyN, "windows", false},
		{ui.Super | ui.Ctrl, ui.KeyN, "windows", false},
		// Unsupported keys are rejected.
		{0, ui.KeyBackquote, "darwin", false},
		{ui.Ctrl, ui.KeyContextMenu, "windows", false},
	}
	for _, test := range cases {
		err := validateChord(test.mods, test.key, test.platform)
		if test.valid && err != nil {
			t.Fatalf("validateChord(%v, %v, %q) 应有效：%v", test.mods, test.key, test.platform, err)
		}
		if !test.valid && err == nil {
			t.Fatalf("validateChord(%v, %v, %q) 应无效", test.mods, test.key, test.platform)
		}
	}
}

func TestShortcutDefaults(t *testing.T) {
	darwin := resolveShortcuts(nil, "darwin")
	if got := shortcutOf(t, darwin, "new"); got.Mods != ui.Super || got.Key != ui.KeyN {
		t.Fatalf("macOS 新请求默认绑定错误：%+v", got)
	}
	if got := shortcutOf(t, darwin, "send"); got.Mods != ui.Super || got.Key != ui.KeyEnter {
		t.Fatalf("macOS 发送默认绑定错误：%+v", got)
	}
	if got := shortcutOf(t, darwin, "stop"); got.Mods != 0 || got.Key != ui.KeyEscape {
		t.Fatalf("macOS 停止默认绑定错误：%+v", got)
	}
	if got := shortcutOf(t, darwin, "settings"); got.Mods != ui.Super || got.Key != ui.KeyComma {
		t.Fatalf("macOS 设置默认绑定错误：%+v", got)
	}
	windows := resolveShortcuts(nil, "windows")
	if got := shortcutOf(t, windows, "new"); got.Mods != ui.Ctrl || got.Key != ui.KeyN {
		t.Fatalf("Windows 新请求默认绑定错误：%+v", got)
	}
	if got := shortcutOf(t, windows, "send"); got.Mods != ui.Ctrl || got.Key != ui.KeyEnter {
		t.Fatalf("Windows 发送默认绑定错误：%+v", got)
	}
	if got := footerShortcutText(darwin, "darwin"); got != "⌘ N 新请求     ⌘ L 地址     ⌘ ↵ 发送     Esc 停止" {
		t.Fatalf("macOS 页脚默认文本被改变：%q", got)
	}
	if got := footerShortcutText(windows, "windows"); got != "Ctrl+N 新请求     Ctrl+L 地址     Ctrl+Enter 发送     Esc 停止" {
		t.Fatalf("Windows 页脚默认文本错误：%q", got)
	}
	if got := acceleratorFor(shortcutOf(t, windows, "settings").Chord); got != "CmdOrCtrl+," {
		t.Fatalf("设置菜单默认加速键错误：%q", got)
	}
	if got := acceleratorFor(shortcutOf(t, darwin, "settings").Chord); got != "CmdOrCtrl+," {
		t.Fatalf("设置菜单默认加速键错误：%q", got)
	}
}

func TestShortcutRenderedLabels(t *testing.T) {
	a, _ := testApp(t)
	platform := shortcutPlatform()
	wantFooter := footerShortcutText(a.shortcuts(), platform)
	if a.footerShortcutHint() != wantFooter {
		t.Fatalf("页脚与解析结果不一致：%q", a.footerShortcutHint())
	}
	wantSave := "收藏  " + a.shortcutDisplay("save")
	if a.saveShortcutLabel() != wantSave {
		t.Fatalf("收藏按钮标签错误：%q", a.saveShortcutLabel())
	}
	wantSend := "发送  " + a.shortcutDisplay("send")
	if a.sendShortcutLabel() != wantSend {
		t.Fatalf("发送按钮标签错误：%q", a.sendShortcutLabel())
	}
	tt := ui.NewTester(a.view, 1200, 820)
	texts := strings.Join(tt.Texts(), "\n")
	if !strings.Contains(texts, wantFooter) {
		t.Fatalf("界面没有渲染默认页脚：%q", wantFooter)
	}
	if !tt.HasText(wantSave) || !tt.HasText(wantSend) {
		t.Fatalf("界面没有渲染默认按钮标签：%q / %q", wantSave, wantSend)
	}
	if runtime.GOOS == "darwin" {
		// The macOS defaults are kept byte identical.
		if got := a.footerShortcutHint(); got != "⌘ N 新请求     ⌘ L 地址     ⌘ ↵ 发送     Esc 停止" {
			t.Fatalf("macOS 页脚默认文本被改变：%q", got)
		}
		if got := a.saveShortcutLabel(); got != "收藏  ⌘ S" {
			t.Fatalf("macOS 收藏按钮默认文本被改变：%q", got)
		}
		if got := a.sendShortcutLabel(); got != "发送  ⌘ ↵" {
			t.Fatalf("macOS 发送按钮默认文本被改变：%q", got)
		}
	}
}

func TestShortcutLabelsFollowOverrides(t *testing.T) {
	a, _ := testApp(t)
	a.workspace.Settings.Shortcuts = map[string]string{"save": "Mod+K", "send": "Mod+J", "focusURL": "Mod+U"}
	tt := ui.NewTester(a.view, 1200, 820)
	platform := shortcutPlatform()
	save, _ := a.shortcutBindingFor("save")
	if !tt.HasText("收藏  " + formatChord(save.Mods, save.Key, platform)) {
		t.Fatalf("收藏按钮没有跟随自定义绑定：%q", a.saveShortcutLabel())
	}
	send, _ := a.shortcutBindingFor("send")
	if !tt.HasText("发送  " + formatChord(send.Mods, send.Key, platform)) {
		t.Fatalf("发送按钮没有跟随自定义绑定：%q", a.sendShortcutLabel())
	}
	footer := a.footerShortcutHint()
	for _, want := range []string{a.shortcutDisplay("new") + " 新请求", a.shortcutDisplay("focusURL") + " 地址", a.shortcutDisplay("send") + " 发送", a.shortcutDisplay("stop") + " 停止"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("页脚没有跟随自定义绑定：%q 缺少 %q", footer, want)
		}
	}
	if !strings.Contains(strings.Join(tt.Texts(), "\n"), a.shortcutDisplay("focusURL")+" 地址") {
		t.Fatal("界面页脚没有跟随自定义绑定")
	}
}

func TestShortcutResolveOverrides(t *testing.T) {
	// Overrides round-trip through the canonical grammar.
	darwin := resolveShortcuts(map[string]string{"new": "Mod+Shift+K"}, "darwin")
	got := shortcutOf(t, darwin, "new")
	if got.Mods != ui.Super|ui.Shift || got.Key != ui.KeyK || got.Chord != "Mod+Shift+K" {
		t.Fatalf("macOS 自定义绑定解析错误：%+v", got)
	}
	windows := resolveShortcuts(map[string]string{"new": "Mod+Shift+K"}, "windows")
	got = shortcutOf(t, windows, "new")
	if got.Mods != ui.Ctrl|ui.Shift || got.Key != ui.KeyK {
		t.Fatalf("Windows 自定义绑定解析错误：%+v", got)
	}
	// Unknown and invalid entries are ignored, never panicking.
	invalid := resolveShortcuts(map[string]string{
		"new":    "Bogus",
		"save":   "Q",
		"send":   "Mod+Ctrl+Enter",
		"stop":   "Mod+Backquote",
		"absent": "Mod+Z",
	}, "windows")
	for action, wantKey := range map[string]ui.Key{"new": ui.KeyN, "save": ui.KeyS, "send": ui.KeyEnter, "stop": ui.KeyEscape} {
		if got := shortcutOf(t, invalid, action); got.Key != wantKey {
			t.Fatalf("无效存储项没有回退默认：%s=%+v", action, got)
		}
	}
	// An override that collides with another action's default is ignored.
	conflict := resolveShortcuts(map[string]string{"send": "Mod+N"}, "windows")
	if got := shortcutOf(t, conflict, "new"); got.Mods != ui.Ctrl || got.Key != ui.KeyN {
		t.Fatalf("冲突的存储项抢走了默认绑定：%+v", got)
	}
	if got := shortcutOf(t, conflict, "send"); got.Mods != ui.Ctrl || got.Key != ui.KeyEnter {
		t.Fatalf("冲突的存储项没有回退默认：%+v", got)
	}
	// An override may take a default another override frees.
	moved := resolveShortcuts(map[string]string{"focusURL": "Mod+K", "new": "Mod+L"}, "darwin")
	if got := shortcutOf(t, moved, "new"); got.Mods != ui.Super || got.Key != ui.KeyL {
		t.Fatalf("移动后的默认绑定无法被接管：%+v", got)
	}
	if got := shortcutOf(t, moved, "focusURL"); got.Mods != ui.Super || got.Key != ui.KeyK {
		t.Fatalf("让位的动作没有应用自定义绑定：%+v", got)
	}
	seen := map[shortcutBinding]string{}
	for _, shortcut := range moved {
		if other, ok := seen[shortcut.binding()]; ok {
			t.Fatalf("动作 %q 与 %q 冲突于 %v", shortcut.Action, other, shortcut.binding())
		}
		seen[shortcut.binding()] = shortcut.Action
	}
	// Two overrides for the same chord: only the first action keeps it.
	duplicate := resolveShortcuts(map[string]string{"new": "Mod+K", "save": "Mod+K"}, "darwin")
	if got := shortcutOf(t, duplicate, "new"); got.Key != ui.KeyK {
		t.Fatalf("第一个动作没有拿到绑定：%+v", got)
	}
	if got := shortcutOf(t, duplicate, "save"); got.Key != ui.KeyS {
		t.Fatalf("重复绑定没有被忽略：%+v", got)
	}
}

func TestShortcutAccelerator(t *testing.T) {
	cases := []struct{ chord, want string }{
		{"Mod+Comma", "CmdOrCtrl+,"},
		{"Mod+Enter", "CmdOrCtrl+Enter"},
		{"Mod+Shift+N", "CmdOrCtrl+Shift+n"},
		{"Ctrl+Alt+F5", "Ctrl+Alt+F5"},
		{"Escape", "Escape"},
		{"Alt+Tab", "Alt+Tab"},
		{"Mod+Semicolon", "CmdOrCtrl+;"},
		{"Mod+Slash", "CmdOrCtrl+/"},
		{"Mod+Space", "CmdOrCtrl+Space"},
		{"Mod", ""},
		{"", ""},
		{"Bogus", ""},
		{"Mod+Backquote", ""},
	}
	for _, test := range cases {
		if got := acceleratorFor(test.chord); got != test.want {
			t.Fatalf("acceleratorFor(%q) = %q，期望 %q", test.chord, got, test.want)
		}
	}
}

func TestShortcutSettingsPersist(t *testing.T) {
	store, err := core.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := core.NewWorkspace()
	w.Settings.Shortcuts = map[string]string{"send": "Mod+J"}
	if err := store.Save(w); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Settings.Shortcuts["send"] != "Mod+J" {
		t.Fatalf("快捷键覆盖项未持久化：%+v", loaded.Settings.Shortcuts)
	}
}

func TestShortcutRecording(t *testing.T) {
	a, _ := testApp(t)
	a.saves = make(chan saveJob, 1)
	platform := shortcutPlatform()

	// A chord is stored as an override and persisted.
	a.startShortcutRecording("send")
	if a.recordingShortcut != "send" || a.shortcutCapture != "" {
		t.Fatalf("开始录制状态错误：%q/%q", a.recordingShortcut, a.shortcutCapture)
	}
	if !a.captureShortcut(textKey(ui.Cmd, ui.KeyJ)) {
		t.Fatal("录制没有接受新组合")
	}
	tokens, err := chordTokensFromMods(ui.Cmd, ui.KeyJ, platform)
	if err != nil {
		t.Fatal(err)
	}
	if a.recordingShortcut != "" || a.shortcutMessage != "" || a.toast != "快捷键已更新" {
		t.Fatalf("录制结束状态错误：%q/%q/%q", a.recordingShortcut, a.shortcutMessage, a.toast)
	}
	if got := a.workspace.Settings.Shortcuts["send"]; got != tokens.String() {
		t.Fatalf("覆盖值 %q，期望 %q", got, tokens.String())
	}
	send, _ := a.shortcutBindingFor("send")
	if send.Key != ui.KeyJ || send.Mods != ui.Cmd {
		t.Fatalf("发送绑定未更新：%+v", send)
	}
	job := <-a.saves
	if job.workspace.Settings.Shortcuts["send"] != tokens.String() {
		t.Fatalf("保存快照缺少快捷键覆盖：%+v", job.workspace.Settings.Shortcuts)
	}

	// Recording the default chord drops the override.
	a.workspace.Settings.Shortcuts["new"] = "Mod+K"
	a.startShortcutRecording("new")
	a.captureShortcut(textKey(ui.Cmd, ui.KeyN))
	if _, ok := a.workspace.Settings.Shortcuts["new"]; ok {
		t.Fatal("录制默认组合仍保存了覆盖")
	}

	// Plain keys are invalid, Escape cancels, Backspace restores.
	a.startShortcutRecording("save")
	a.captureShortcut(textKey(0, ui.KeyEnter))
	if !strings.Contains(a.shortcutMessage, "需要包含") || a.recordingShortcut != "save" {
		t.Fatalf("无效按键处理错误：%q/%q", a.shortcutMessage, a.recordingShortcut)
	}
	a.captureShortcut(textKey(0, ui.KeyEscape))
	if a.recordingShortcut != "" || a.shortcutMessage != "" {
		t.Fatalf("Esc 没有取消录制：%q/%q", a.recordingShortcut, a.shortcutMessage)
	}
	a.workspace.Settings.Shortcuts["save"] = "Mod+J"
	a.startShortcutRecording("save")
	a.captureShortcut(textKey(0, ui.KeyBackspace))
	if _, ok := a.workspace.Settings.Shortcuts["save"]; ok || !a.shortcutIsDefault("save") || a.recordingShortcut != "" {
		t.Fatal("Backspace 没有恢复默认")
	}

	// A conflict with another action is reported and not stored.
	a.startShortcutRecording("save")
	a.captureShortcut(textKey(ui.Cmd, ui.KeyN))
	if !strings.Contains(a.shortcutMessage, "已被") || !strings.Contains(a.shortcutMessage, "「新请求」") {
		t.Fatalf("冲突提示错误：%q", a.shortcutMessage)
	}
	if _, ok := a.workspace.Settings.Shortcuts["save"]; ok {
		t.Fatal("冲突的快捷键被保存")
	}
	a.captureShortcut(textKey(0, ui.KeyEscape))

	// Reset restores every default.
	a.workspace.Settings.Shortcuts = map[string]string{"new": "Mod+K", "save": "Mod+J"}
	a.resetShortcut("save")
	if _, ok := a.workspace.Settings.Shortcuts["save"]; ok {
		t.Fatal("恢复默认没有删除覆盖")
	}
	a.resetAllShortcuts()
	if a.workspace.Settings.Shortcuts != nil {
		t.Fatalf("全部恢复默认后仍有覆盖：%+v", a.workspace.Settings.Shortcuts)
	}
}

func TestShortcutViewDispatch(t *testing.T) {
	t.Run("默认绑定仍然可用", func(t *testing.T) {
		a, _ := testApp(t)
		tt := ui.NewTester(a.view, 1200, 820)
		old := a.workspace.Draft.ID
		tt.Key(ui.Cmd, ui.KeyN)
		if a.workspace.Draft.ID == old {
			t.Fatal("默认 Mod+N 没有新建请求")
		}
		tt.Key(ui.Cmd, ui.KeyL)
		if !tt.Focused("请求地址") {
			t.Fatal("默认 Mod+L 没有聚焦地址栏")
		}
	})
	t.Run("自定义绑定替换默认", func(t *testing.T) {
		a, _ := testApp(t)
		a.workspace.Settings.Shortcuts = map[string]string{"new": "Mod+K"}
		tt := ui.NewTester(a.view, 1200, 820)
		old := a.workspace.Draft.ID
		tt.Key(ui.Cmd, ui.KeyK)
		if a.workspace.Draft.ID == old {
			t.Fatal("自定义 Mod+K 没有新建请求")
		}
		old = a.workspace.Draft.ID
		tt.Key(ui.Cmd, ui.KeyN)
		if a.workspace.Draft.ID != old {
			t.Fatal("旧默认 Mod+N 仍被注册")
		}
	})
	t.Run("地址栏聚焦时发送绑定仍触发", func(t *testing.T) {
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
			fmt.Fprint(w, "done")
		}))
		defer srv.Close()
		defer close(release)
		a, _ := testApp(t)
		a.workspace.Settings.Shortcuts = map[string]string{"send": "Mod+J"}
		tt := ui.NewTester(a.view, 1200, 820)
		clickUI(t, tt, "请求地址")
		if !tt.Focused("请求地址") {
			t.Fatal("地址栏没有获得焦点")
		}
		tt.Type(srv.URL)
		if a.workspace.Draft.URL != srv.URL {
			t.Fatalf("地址栏输入未生效：%q", a.workspace.Draft.URL)
		}
		tt.Key(ui.Cmd, ui.KeyJ)
		if !a.busy {
			t.Fatal("地址栏聚焦时自定义发送快捷键没有发送")
		}
		tt.Key(0, ui.KeyEscape)
		if a.busy {
			t.Fatal("Esc 没有停止请求")
		}
		tt.Frame()
	})
	t.Run("输入框自己处理的组合也触发绑定", func(t *testing.T) {
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
			fmt.Fprint(w, "done")
		}))
		defer srv.Close()
		defer close(release)
		a, _ := testApp(t)
		// Mod+A is select all in a text input, so the window shortcut is not
		// consulted; the input's OnTextKey matcher must send instead.
		a.workspace.Settings.Shortcuts = map[string]string{"send": "Mod+A"}
		tt := ui.NewTester(a.view, 1200, 820)
		clickUI(t, tt, "请求地址")
		tt.Type(srv.URL)
		tt.Key(ui.Cmd, ui.KeyA)
		if !a.busy {
			t.Fatal("地址栏内按自定义发送组合没有发送")
		}
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
	})
	t.Run("地址栏聚焦时默认发送绑定仍触发", func(t *testing.T) {
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
			fmt.Fprint(w, "done")
		}))
		defer srv.Close()
		defer close(release)
		a, _ := testApp(t)
		tt := ui.NewTester(a.view, 1200, 820)
		clickUI(t, tt, "请求地址")
		tt.Type(srv.URL)
		tt.Key(ui.Cmd, ui.KeyEnter)
		if !a.busy {
			t.Fatal("地址栏聚焦时 Mod+Enter 没有发送")
		}
		tt.Key(0, ui.KeyEscape)
		tt.Frame()
	})
}

func TestShortcutTextKeyMatcher(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
		fmt.Fprint(w, "done")
	}))
	defer srv.Close()
	defer close(release)
	a, _ := testApp(t)
	a.workspace.Draft = appDraft(srv.URL)
	a.syncDraft()
	if a.textShortcut(textKey(ui.Cmd, ui.KeyA)) {
		t.Fatal("未绑定的组合被输入框匹配")
	}
	if !a.textShortcut(textKey(ui.Cmd, ui.KeyEnter)) {
		t.Fatal("默认发送绑定没有被输入框匹配")
	}
	if !a.busy {
		t.Fatal("输入框匹配后没有发送")
	}
	a.stop()
	if !a.textShortcut(textKey(0, ui.KeyEscape)) {
		t.Fatal("默认停止绑定没有被输入框匹配")
	}
}

func TestShortcutSettingsUI(t *testing.T) {
	a, _ := testApp(t)
	a.saves = make(chan saveJob, 1)
	platform := shortcutPlatform()
	tt := ui.NewTester(a.view, 1200, 820)
	clickUI(t, tt, "设置")
	if !tt.HasText("快捷键") {
		t.Fatal("应用设置页没有快捷键卡片")
	}
	if !tt.HasText("点击「更改」后按下新组合") {
		t.Fatal("快捷键卡片缺少说明文字")
	}
	// The shortcuts card sits below the update card: scroll it into view.
	tt.Scroll(600, 400, 0, 1000)
	tt.Frame()
	clickUI(t, tt, "更改 发送请求")
	if a.recordingShortcut != "send" {
		t.Fatalf("点击更改没有开始录制：%q", a.recordingShortcut)
	}
	if !tt.Focused("新的快捷键") {
		t.Fatal("录制输入框没有获得焦点")
	}
	tt.Key(ui.Cmd, ui.KeyJ)
	if a.recordingShortcut != "" {
		t.Fatalf("录制成功后仍在录制：%q", a.recordingShortcut)
	}
	tokens, err := chordTokensFromMods(ui.Cmd, ui.KeyJ, platform)
	if err != nil {
		t.Fatal(err)
	}
	send, ok := a.shortcutBindingFor("send")
	if !ok || send.Key != ui.KeyJ || send.Mods != ui.Cmd {
		t.Fatalf("界面录制没有更新发送绑定：%+v", send)
	}
	if got := a.workspace.Settings.Shortcuts["send"]; got != tokens.String() {
		t.Fatalf("界面录制的覆盖值 %q，期望 %q", got, tokens.String())
	}
	select {
	case job := <-a.saves:
		if job.workspace.Settings.Shortcuts["send"] != tokens.String() {
			t.Fatalf("界面录制没有持久化：%+v", job.workspace.Settings.Shortcuts)
		}
	default:
		t.Fatal("界面录制没有提交保存")
	}

	// A conflicting chord shows the other action's name and is not stored.
	clickUI(t, tt, "更改 收藏请求")
	tt.Key(ui.Cmd, ui.KeyN)
	if !strings.Contains(a.shortcutMessage, "已被") || !strings.Contains(a.shortcutMessage, "新请求") {
		t.Fatalf("冲突提示错误：%q", a.shortcutMessage)
	}
	if _, ok := a.workspace.Settings.Shortcuts["save"]; ok {
		t.Fatal("冲突的快捷键被保存")
	}
	tt.Key(0, ui.KeyEscape)
	if a.recordingShortcut != "" {
		t.Fatal("Esc 没有取消录制")
	}

	// 恢复默认 clears one override.
	clickUI(t, tt, "恢复默认 发送请求")
	if _, ok := a.workspace.Settings.Shortcuts["send"]; ok {
		t.Fatal("恢复默认没有清除覆盖")
	}
	if got, _ := a.shortcutBindingFor("send"); got.Key != ui.KeyEnter {
		t.Fatalf("恢复默认后的发送绑定错误：%+v", got)
	}

	// 全部恢复默认 clears every override.
	a.workspace.Settings.Shortcuts = map[string]string{"new": "Mod+K", "save": "Mod+J"}
	tt.Frame()
	clickUI(t, tt, "全部恢复默认")
	if a.workspace.Settings.Shortcuts != nil {
		t.Fatalf("全部恢复默认后仍有覆盖：%+v", a.workspace.Settings.Shortcuts)
	}
}

func TestShortcutSendButtonWidth(t *testing.T) {
	// macOS defaults keep the historic width so existing renders stay put.
	if got := sendButtonWidth("发送  ⌘ ↵"); got != 112 {
		t.Fatalf("macOS send button width = %v", got)
	}
	for _, label := range []string{"发送  Ctrl+Enter", "发送  Ctrl+Shift+Enter", "发送  Ctrl+Alt+F12"} {
		if got := sendButtonWidth(label); got <= 112 {
			t.Fatalf("send button width for %q = %v, want > 112", label, got)
		}
	}
}

func TestShortcutCollidedOverrideStaysResettable(t *testing.T) {
	a, _ := testApp(t)
	// A hand-edited file can leave an override that resolution ignores
	// because another action already owns the chord. The row must still be
	// resettable, otherwise the user cannot remove it.
	a.workspace.Settings.Shortcuts = map[string]string{"save": "Mod+N"}
	if a.shortcutIsDefault("save") {
		t.Fatal("collided override reported as default")
	}
	a.resetShortcut("save")
	if _, ok := a.workspace.Settings.Shortcuts["save"]; ok {
		t.Fatalf("reset left collided override: %+v", a.workspace.Settings.Shortcuts)
	}
	if got, _ := a.shortcutBindingFor("save"); got.Key != ui.KeyS {
		t.Fatalf("reset binding = %+v", got)
	}
}

// The stop shortcut with nothing running must leave a finished response
// usable: stop() bumps the generation, which would disable validating and
// extracting from the response already on screen.
func TestStopShortcutIdleKeepsCapturedResponse(t *testing.T) {
	a, _ := testApp(t)
	a.capturedResponse = responseCapture{requestID: a.workspace.Draft.ID, generation: a.generation, received: true, complete: true, status: 200}
	if !a.canValidateResponse() {
		t.Fatal("测试前提不成立：响应应可验证")
	}
	a.runShortcutAction("stop")
	if !a.canValidateResponse() {
		t.Fatal("空闲时按停止快捷键不应使当前响应失效")
	}
	a.busy = true
	a.runShortcutAction("stop")
	if a.busy || a.status != "已停止" {
		t.Fatalf("请求进行中按停止快捷键应停止请求：busy=%v status=%q", a.busy, a.status)
	}
}

// validateChord reports whether mods and key pressed on platform may be
// bound, running the same three checks captureShortcut applies to a key press:
// a chord with Ctrl, Cmd (Mod) or Alt, or a bare Escape or function key.
func validateChord(mods ui.Modifiers, key ui.Key, platform string) error {
	tokens, err := chordTokensFromMods(mods, key, platform)
	if err != nil {
		return err
	}
	if err := tokens.validate(); err != nil {
		return err
	}
	_, err = tokens.resolve(platform)
	return err
}
