package main

import (
	"errors"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
)

// shortcutAction is one command a keyboard shortcut can run.
type shortcutAction struct {
	ID    string
	Label string
	// Chord is the default binding in the canonical chord grammar.
	Chord string
}

// shortcutActions lists every action whose keyboard shortcut can be
// customized, in the order the settings page shows them.
var shortcutActions = []shortcutAction{
	{ID: "new", Label: "新请求", Chord: "Mod+N"},
	{ID: "save", Label: "收藏请求", Chord: "Mod+S"},
	{ID: "send", Label: "发送请求", Chord: "Mod+Enter"},
	{ID: "focusURL", Label: "聚焦地址栏", Chord: "Mod+L"},
	{ID: "stop", Label: "停止请求", Chord: "Escape"},
	{ID: "settings", Label: "打开设置", Chord: "Mod+Comma"},
}

// shortcutPlatform is the platform shortcuts resolve for. Tests parse and
// format both darwin and windows chords on one host, so neither path relies
// on the ui package's platform variables.
func shortcutPlatform() string { return runtime.GOOS }

var (
	errShortcutUnsupported = errors.New("不支持的快捷键")
	errShortcutModifier    = errors.New("快捷键需要包含 Ctrl/Cmd 或 Alt")
)

// chordTokens is a chord parsed into tokens, before "Mod" resolves to the
// platform's shortcut modifier. Keeping the tokens separate lets one
// grammar describe macOS (Mod is Command, Ctrl is Control) and Windows and
// Linux (Mod is Ctrl) and still round-trip stored values.
type chordTokens struct {
	mod, ctrl, alt, shift bool
	key                   ui.Key
}

// shortcutNamedKeys are the key tokens of the chord grammar that name a key
// rather than one of its single characters.
var shortcutNamedKeys = []struct {
	Key   ui.Key
	Token string
}{
	{ui.KeyEnter, "Enter"},
	{ui.KeyEscape, "Escape"},
	{ui.KeyTab, "Tab"},
	{ui.KeySpace, "Space"},
	{ui.KeyBackspace, "Backspace"},
	{ui.KeyDelete, "Delete"},
	{ui.KeyInsert, "Insert"},
	{ui.KeyHome, "Home"},
	{ui.KeyEnd, "End"},
	{ui.KeyPageUp, "PageUp"},
	{ui.KeyPageDown, "PageDown"},
	{ui.KeyLeft, "Left"},
	{ui.KeyRight, "Right"},
	{ui.KeyUp, "Up"},
	{ui.KeyDown, "Down"},
	{ui.KeyF1, "F1"},
	{ui.KeyF2, "F2"},
	{ui.KeyF3, "F3"},
	{ui.KeyF4, "F4"},
	{ui.KeyF5, "F5"},
	{ui.KeyF6, "F6"},
	{ui.KeyF7, "F7"},
	{ui.KeyF8, "F8"},
	{ui.KeyF9, "F9"},
	{ui.KeyF10, "F10"},
	{ui.KeyF11, "F11"},
	{ui.KeyF12, "F12"},
	{ui.KeyMinus, "Minus"},
	{ui.KeyEqual, "Equal"},
	{ui.KeyComma, "Comma"},
	{ui.KeyPeriod, "Period"},
	{ui.KeySlash, "Slash"},
	{ui.KeySemicolon, "Semicolon"},
	{ui.KeyQuote, "Quote"},
	{ui.KeyBracketLeft, "BracketLeft"},
	{ui.KeyBracketRight, "BracketRight"},
	{ui.KeyBackslash, "Backslash"},
}

// shortcutKeyCharacters shows the punctuation keys as the characters they
// type, in labels, displays and accelerators.
var shortcutKeyCharacters = map[ui.Key]string{
	ui.KeyMinus:        "-",
	ui.KeyEqual:        "=",
	ui.KeyComma:        ",",
	ui.KeyPeriod:       ".",
	ui.KeySlash:        "/",
	ui.KeySemicolon:    ";",
	ui.KeyQuote:        "'",
	ui.KeyBracketLeft:  "[",
	ui.KeyBracketRight: "]",
	ui.KeyBackslash:    `\`,
}

var shortcutKeyTokens = func() map[ui.Key]string {
	tokens := make(map[ui.Key]string, len(shortcutNamedKeys)+36)
	for _, k := range shortcutNamedKeys {
		tokens[k.Key] = k.Token
	}
	for i := 0; i < 26; i++ {
		tokens[ui.KeyA+ui.Key(i)] = string(rune('A' + i))
	}
	for i := 0; i < 10; i++ {
		tokens[ui.Key0+ui.Key(i)] = string(rune('0' + i))
	}
	return tokens
}()

var shortcutKeyByToken = func() map[string]ui.Key {
	keys := make(map[string]ui.Key, len(shortcutKeyTokens))
	for key, token := range shortcutKeyTokens {
		keys[strings.ToLower(token)] = key
	}
	return keys
}()

// shortcutKeyToken returns the grammar token of key, and whether the key
// can be bound at all.
func shortcutKeyToken(key ui.Key) (string, bool) {
	token, ok := shortcutKeyTokens[key]
	return token, ok
}

// shortcutKeyDisplay shows key as the platform labels it.
func shortcutKeyDisplay(key ui.Key, platform string) string {
	if platform == "darwin" {
		switch key {
		case ui.KeyEnter:
			return "↵"
		case ui.KeyEscape:
			return "Esc"
		case ui.KeyBackspace:
			return "⌫"
		case ui.KeyDelete:
			return "Del"
		case ui.KeyPageUp:
			return "PgUp"
		case ui.KeyPageDown:
			return "PgDn"
		case ui.KeyLeft:
			return "←"
		case ui.KeyRight:
			return "→"
		case ui.KeyUp:
			return "↑"
		case ui.KeyDown:
			return "↓"
		}
	} else if key == ui.KeyEscape {
		return "Esc"
	}
	if character, ok := shortcutKeyCharacters[key]; ok {
		return character
	}
	token, ok := shortcutKeyToken(key)
	if !ok {
		return "?"
	}
	return token
}

// shortcutModifierDisplay shows mods in the canonical order Mod, Ctrl, Alt,
// Shift: symbols on macOS, names elsewhere.
func shortcutModifierDisplay(mods ui.Modifiers, platform string) string {
	if platform == "darwin" {
		var b strings.Builder
		if mods&ui.Super != 0 {
			b.WriteString("⌘")
		}
		if mods&ui.Ctrl != 0 {
			b.WriteString("⌃")
		}
		if mods&ui.Alt != 0 {
			b.WriteString("⌥")
		}
		if mods&ui.Shift != 0 {
			b.WriteString("⇧")
		}
		return b.String()
	}
	var parts []string
	if mods&ui.Super != 0 {
		// The chord grammar cannot describe the Windows key.
		parts = append(parts, "Super")
	}
	if mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	return strings.Join(parts, "+")
}

// formatChord shows a resolved binding as the platform labels it: "⌘ ↵" and
// "Esc" on macOS, "Ctrl+Enter" and "Esc" elsewhere.
func formatChord(mods ui.Modifiers, key ui.Key, platform string) string {
	keyText := shortcutKeyDisplay(key, platform)
	if platform == "darwin" {
		if modsText := shortcutModifierDisplay(mods, platform); modsText != "" {
			return modsText + " " + keyText
		}
		return keyText
	}
	if modsText := shortcutModifierDisplay(mods, platform); modsText != "" {
		return modsText + "+" + keyText
	}
	return keyText
}

// String formats the tokens in the canonical chord grammar.
func (t chordTokens) String() string {
	parts := make([]string, 0, 5)
	if t.mod {
		parts = append(parts, "Mod")
	}
	if t.ctrl {
		parts = append(parts, "Ctrl")
	}
	if t.alt {
		parts = append(parts, "Alt")
	}
	if t.shift {
		parts = append(parts, "Shift")
	}
	token, ok := shortcutKeyToken(t.key)
	if !ok {
		return ""
	}
	return strings.Join(append(parts, token), "+")
}

// resolve turns the tokens into the modifiers and key the runtime registers
// for this platform.
func (t chordTokens) resolve(platform string) (ui.Modifiers, error) {
	if t.mod && t.ctrl && platform != "darwin" {
		// Ctrl is both the Mod token and the explicit Ctrl token there, so
		// a chord naming both is ambiguous.
		return 0, errShortcutUnsupported
	}
	var mods ui.Modifiers
	if t.mod {
		if platform == "darwin" {
			mods |= ui.Super
		} else {
			mods |= ui.Ctrl
		}
	}
	if t.ctrl {
		mods |= ui.Ctrl
	}
	if t.alt {
		mods |= ui.Alt
	}
	if t.shift {
		mods |= ui.Shift
	}
	return mods, nil
}

// validate reports whether the tokens are a binding the app accepts: one
// with Ctrl, Cmd (Mod) or Alt, or a bare Escape and function key.
func (t chordTokens) validate() error {
	if t.mod || t.ctrl || t.alt {
		return nil
	}
	if t.key == ui.KeyEscape || t.key >= ui.KeyF1 && t.key <= ui.KeyF12 {
		return nil
	}
	return errShortcutModifier
}

// chordTokensFromMods parses a key press back into the chord grammar.
func chordTokensFromMods(mods ui.Modifiers, key ui.Key, platform string) (chordTokens, error) {
	t := chordTokens{key: key}
	if _, ok := shortcutKeyToken(key); !ok {
		return t, errShortcutUnsupported
	}
	if mods&ui.Super != 0 {
		if platform != "darwin" {
			if mods&(ui.Ctrl|ui.Alt) == 0 {
				return t, errShortcutModifier // the Windows key alone names no allowed modifier
			}
			return t, errShortcutUnsupported // no chord token names the Windows key
		}
		t.mod = true
	}
	if mods&ui.Ctrl != 0 {
		if platform == "darwin" {
			t.ctrl = true
		} else {
			t.mod = true
		}
	}
	t.alt = mods&ui.Alt != 0
	t.shift = mods&ui.Shift != 0
	if mods&^(ui.Super|ui.Ctrl|ui.Alt|ui.Shift) != 0 {
		return t, errShortcutUnsupported
	}
	return t, nil
}

// parseChordTokens parses a chord in the canonical grammar: modifiers
// Mod, Ctrl, Alt and Shift joined by "+", then a key token.
func parseChordTokens(s string) (chordTokens, error) {
	var t chordTokens
	s = strings.TrimSpace(s)
	if s == "" {
		return t, errShortcutUnsupported
	}
	parts := strings.Split(s, "+")
	for i, raw := range parts {
		token := strings.TrimSpace(raw)
		if token == "" {
			return t, errShortcutUnsupported
		}
		last := i == len(parts)-1
		switch strings.ToLower(token) {
		case "mod", "ctrl", "alt", "shift":
			if last {
				return t, errShortcutUnsupported
			}
			switch strings.ToLower(token) {
			case "mod":
				if t.mod {
					return t, errShortcutUnsupported
				}
				t.mod = true
			case "ctrl":
				if t.ctrl {
					return t, errShortcutUnsupported
				}
				t.ctrl = true
			case "alt":
				if t.alt {
					return t, errShortcutUnsupported
				}
				t.alt = true
			case "shift":
				if t.shift {
					return t, errShortcutUnsupported
				}
				t.shift = true
			}
			continue
		}
		if !last {
			return t, errShortcutUnsupported
		}
		key, ok := shortcutKeyByToken[strings.ToLower(token)]
		if !ok {
			return t, errShortcutUnsupported
		}
		t.key = key
	}
	if t.key == ui.KeyUnknown {
		return t, errShortcutUnsupported
	}
	return t, nil
}

// parseChord resolves a chord for the given platform, which may differ from
// the platform the app runs on so that both grammars are testable.
func parseChord(s, platform string) (ui.Modifiers, ui.Key, error) {
	tokens, err := parseChordTokens(s)
	if err != nil {
		return 0, ui.KeyUnknown, err
	}
	mods, err := tokens.resolve(platform)
	if err != nil {
		return 0, ui.KeyUnknown, err
	}
	return mods, tokens.key, nil
}

// validateChord reports whether mods and key pressed on platform may be
// bound: a chord with Ctrl, Cmd (Mod) or Alt, or a bare Escape or function
// key.
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

// acceleratorFor translates a chord in the canonical grammar to MyGo's menu
// accelerator grammar: Mod is CmdOrCtrl, Ctrl is Ctrl, then the key.
func acceleratorFor(chord string) string {
	tokens, err := parseChordTokens(chord)
	if err != nil {
		return ""
	}
	parts := make([]string, 0, 5)
	if tokens.mod {
		parts = append(parts, "CmdOrCtrl")
	}
	if tokens.ctrl {
		parts = append(parts, "Ctrl")
	}
	if tokens.alt {
		parts = append(parts, "Alt")
	}
	if tokens.shift {
		parts = append(parts, "Shift")
	}
	key, ok := acceleratorKey(tokens.key)
	if !ok {
		return ""
	}
	return strings.Join(append(parts, key), "+")
}

// acceleratorKey names a key in MyGo's accelerator grammar: single lower
// case characters for letters, digits and punctuation, and named keys.
func acceleratorKey(key ui.Key) (string, bool) {
	if key >= ui.KeyA && key <= ui.KeyZ {
		return strings.ToLower(string(rune('a' + key - ui.KeyA))), true
	}
	if key >= ui.Key0 && key <= ui.Key9 {
		return string(rune('0' + key - ui.Key0)), true
	}
	if character, ok := shortcutKeyCharacters[key]; ok {
		return character, true
	}
	return shortcutKeyToken(key)
}

// shortcutBinding is a resolved binding, comparable, as collision checks
// need it.
type shortcutBinding struct {
	Mods ui.Modifiers
	Key  ui.Key
}

// effectiveShortcut is what an action's shortcut is after stored overrides
// are applied: the canonical chord plus the binding the runtime registers.
type effectiveShortcut struct {
	Action string
	Label  string
	Chord  string
	Mods   ui.Modifiers
	Key    ui.Key
}

func (s effectiveShortcut) binding() shortcutBinding {
	return shortcutBinding{Mods: s.Mods, Key: s.Key}
}

// defaultShortcuts resolves the built-in shortcuts for platform.
func defaultShortcuts(platform string) []effectiveShortcut {
	shortcuts := make([]effectiveShortcut, 0, len(shortcutActions))
	for _, action := range shortcutActions {
		mods, key, err := parseChord(action.Chord, platform)
		if err != nil {
			continue // the defaults are constants and always parse
		}
		shortcuts = append(shortcuts, effectiveShortcut{Action: action.ID, Label: action.Label, Chord: action.Chord, Mods: mods, Key: key})
	}
	return shortcuts
}

// resolveShortcuts applies stored overrides to the defaults. Invalid or
// colliding overrides are ignored so a hand-edited settings file never
// panics nor registers one chord for two actions: every action keeps a
// unique binding.
func resolveShortcuts(overrides map[string]string, platform string) []effectiveShortcut {
	shortcuts := defaultShortcuts(platform)
	type candidate struct {
		chord string
		mods  ui.Modifiers
		key   ui.Key
	}
	candidates := make(map[string]candidate, len(overrides))
	for _, action := range shortcutActions {
		raw, ok := overrides[action.ID]
		if !ok {
			continue
		}
		tokens, err := parseChordTokens(raw)
		if err != nil {
			continue
		}
		if err := tokens.validate(); err != nil {
			continue
		}
		mods, err := tokens.resolve(platform)
		if err != nil {
			continue
		}
		candidates[action.ID] = candidate{chord: tokens.String(), mods: mods, key: tokens.key}
	}
	// Apply overrides over a few passes: an override may take a default
	// that another override frees later in the same map, as when two
	// actions trade bindings. Each pass keeps every binding unique, and a
	// pass that changes nothing is the last.
	for pass := 0; pass <= len(shortcutActions); pass++ {
		changed := false
		for i := range shortcuts {
			next, ok := candidates[shortcuts[i].Action]
			if !ok {
				continue
			}
			if shortcuts[i].Mods == next.mods && shortcuts[i].Key == next.key && shortcuts[i].Chord == next.chord {
				continue
			}
			taken := false
			for j := range shortcuts {
				if j != i && shortcuts[j].Mods == next.mods && shortcuts[j].Key == next.key {
					taken = true
					break
				}
			}
			if taken {
				continue
			}
			shortcuts[i].Chord, shortcuts[i].Mods, shortcuts[i].Key = next.chord, next.mods, next.key
			changed = true
		}
		if !changed {
			break
		}
	}
	return shortcuts
}

func shortcutActionExists(id string) bool {
	for _, action := range shortcutActions {
		if action.ID == id {
			return true
		}
	}
	return false
}

// shortcuts resolves the app's current bindings.
func (a *application) shortcuts() []effectiveShortcut {
	return resolveShortcuts(a.workspace.Settings.Shortcuts, shortcutPlatform())
}

// shortcutBindingFor returns the binding of one action.
func (a *application) shortcutBindingFor(id string) (effectiveShortcut, bool) {
	for _, shortcut := range a.shortcuts() {
		if shortcut.Action == id {
			return shortcut, true
		}
	}
	return effectiveShortcut{}, false
}

// shortcutDisplay shows one action's binding as the platform labels it.
func (a *application) shortcutDisplay(id string) string {
	shortcut, ok := a.shortcutBindingFor(id)
	if !ok {
		return ""
	}
	return formatChord(shortcut.Mods, shortcut.Key, shortcutPlatform())
}

// defaultShortcutFor returns one action's built-in binding.
func (a *application) defaultShortcutFor(id string) (effectiveShortcut, bool) {
	for _, shortcut := range defaultShortcuts(shortcutPlatform()) {
		if shortcut.Action == id {
			return shortcut, true
		}
	}
	return effectiveShortcut{}, false
}

// shortcutIsDefault reports whether the action still uses its built-in
// binding and stores no override. A colliding stored override is ignored by
// resolution but stays resettable, so it counts as non-default.
func (a *application) shortcutIsDefault(id string) bool {
	if _, ok := a.workspace.Settings.Shortcuts[id]; ok {
		return false
	}
	current, ok := a.shortcutBindingFor(id)
	if !ok {
		return false
	}
	fallback, ok := a.defaultShortcutFor(id)
	return ok && current.binding() == fallback.binding()
}

// sendButtonWidth returns the width the primary send button needs so its
// shortcut label is not clipped. macOS default labels fit the historic 112
// DIP button; Windows/Linux labels such as "发送  Ctrl+Enter" need more, and
// a customized chord can widen them further. Widths are rough DIP guesses:
// CJK runes take about 13, other runes about 7.4, plus 28 of button padding.
func sendButtonWidth(label string) float32 {
	width := float32(28)
	for _, r := range label {
		if r > 0x2e80 {
			width += 13
		} else {
			width += 7.4
		}
	}
	if width < 112 {
		return 112
	}
	return float32(int(width + 1))
}

// footerShortcutText is the hint shown along the bottom of the window. It
// takes the bindings and platform so both platforms' text is testable.
func footerShortcutText(shortcuts []effectiveShortcut, platform string) string {
	items := []struct {
		action string
		label  string
	}{
		{"new", "新请求"},
		{"focusURL", "地址"},
		{"send", "发送"},
		{"stop", "停止"},
	}
	display := func(action string) string {
		for _, shortcut := range shortcuts {
			if shortcut.Action == action {
				return formatChord(shortcut.Mods, shortcut.Key, platform)
			}
		}
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, display(item.action)+" "+item.label)
	}
	return strings.Join(parts, "     ")
}

// footerShortcutHint is the hint shown along the bottom of the window.
func (a *application) footerShortcutHint() string {
	return footerShortcutText(a.shortcuts(), shortcutPlatform())
}

// saveShortcutLabel labels the save button with its current binding.
func (a *application) saveShortcutLabel() string {
	return "收藏  " + a.shortcutDisplay("save")
}

// sendShortcutLabel labels the send button with its current binding.
func (a *application) sendShortcutLabel() string {
	return "发送  " + a.shortcutDisplay("send")
}

// runShortcutAction runs one action from the keyboard.
func (a *application) runShortcutAction(id string) {
	switch id {
	case "new":
		a.newRequest()
	case "save":
		a.saveRequest()
	case "send":
		a.send()
	case "focusURL":
		a.page = 0
		a.urlHandle.Focus()
	case "stop":
		a.stop()
	case "settings":
		a.page = 7
		a.notice = ""
	}
}

// textShortcut runs a configured action from a TextInput's OnTextKey, as the
// URL field does: the focused input sees the chord first, and a key it does
// not take carries on to the input. It reports whether k was a binding.
func (a *application) textShortcut(k ui.TextKey) bool {
	for _, shortcut := range a.shortcuts() {
		if shortcut.Mods == k.Mods && shortcut.Key == k.Key {
			a.runShortcutAction(shortcut.Action)
			return true
		}
	}
	return false
}

// conflictingShortcut reports the label of the action already using a
// binding other than the one being recorded.
func (a *application) conflictingShortcut(actionID string, mods ui.Modifiers, key ui.Key) (string, bool) {
	for _, shortcut := range a.shortcuts() {
		if shortcut.Action != actionID && shortcut.Mods == mods && shortcut.Key == key {
			return shortcut.Label, true
		}
	}
	return "", false
}

// startShortcutRecording starts recording a new chord for the action. The
// menu is rebuilt with the settings accelerator removed, so that a chord the
// native menu would otherwise take, as Cmd+, on macOS, reaches the recorder.
func (a *application) startShortcutRecording(id string) {
	if !shortcutActionExists(id) {
		return
	}
	a.recordingShortcut = id
	a.shortcutCapture = ""
	a.shortcutMessage = ""
	a.installMenu()
}

// cancelShortcutRecording leaves recording without changing anything.
func (a *application) cancelShortcutRecording() {
	a.recordingShortcut = ""
	a.shortcutCapture = ""
	a.shortcutMessage = ""
	a.installMenu()
}

// resetShortcut restores one action's built-in binding.
func (a *application) resetShortcut(id string) {
	if _, ok := a.workspace.Settings.Shortcuts[id]; ok {
		delete(a.workspace.Settings.Shortcuts, id)
		if len(a.workspace.Settings.Shortcuts) == 0 {
			a.workspace.Settings.Shortcuts = nil
		}
		a.persist()
		a.toast = "已恢复默认快捷键"
	}
	if a.recordingShortcut == id {
		a.recordingShortcut = ""
		a.shortcutCapture = ""
	}
	a.shortcutMessage = ""
	a.installMenu()
}

// resetAllShortcuts restores every action's built-in binding.
func (a *application) resetAllShortcuts() {
	if len(a.workspace.Settings.Shortcuts) > 0 {
		a.workspace.Settings.Shortcuts = nil
		a.persist()
		a.toast = "已全部恢复默认快捷键"
	}
	a.recordingShortcut = ""
	a.shortcutCapture = ""
	a.shortcutMessage = ""
	a.installMenu()
}

// captureShortcut handles a key pressed while recording: Escape cancels,
// Backspace restores the default, and any other key becomes the action's new
// binding when it is valid and unused.
func (a *application) captureShortcut(k ui.TextKey) bool {
	id := a.recordingShortcut
	if id == "" {
		return false
	}
	if k.Mods == 0 && k.Key == ui.KeyEscape {
		a.cancelShortcutRecording()
		return true
	}
	if k.Mods == 0 && k.Key == ui.KeyBackspace {
		a.resetShortcut(id)
		return true
	}
	platform := shortcutPlatform()
	tokens, err := chordTokensFromMods(k.Mods, k.Key, platform)
	if err != nil {
		a.shortcutMessage = err.Error()
		return true
	}
	if err := tokens.validate(); err != nil {
		a.shortcutMessage = err.Error()
		return true
	}
	mods, err := tokens.resolve(platform)
	if err != nil {
		a.shortcutMessage = err.Error()
		return true
	}
	if label, ok := a.conflictingShortcut(id, mods, k.Key); ok {
		a.shortcutMessage = "该快捷键已被「" + label + "」使用"
		return true
	}
	// Store only overrides: binding an action to its default drops the
	// stored entry.
	defaults, _ := a.defaultShortcutFor(id)
	if defaults.binding() == (shortcutBinding{Mods: mods, Key: k.Key}) {
		if _, ok := a.workspace.Settings.Shortcuts[id]; ok {
			delete(a.workspace.Settings.Shortcuts, id)
			if len(a.workspace.Settings.Shortcuts) == 0 {
				a.workspace.Settings.Shortcuts = nil
			}
		}
	} else {
		if a.workspace.Settings.Shortcuts == nil {
			a.workspace.Settings.Shortcuts = make(map[string]string, 1)
		}
		a.workspace.Settings.Shortcuts[id] = tokens.String()
	}
	a.persist()
	a.toast = "快捷键已更新"
	a.recordingShortcut = ""
	a.shortcutCapture = ""
	a.shortcutMessage = ""
	a.installMenu()
	return true
}
