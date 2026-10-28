package main

import (
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo/ui"
)

func clickUI(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
}
func inputUI(t *testing.T, tt *ui.Tester, label, value string) {
	t.Helper()
	clickUI(t, tt, label)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type(value)
}

func TestApplicationView(t *testing.T) {
	t.Run("CA设置输入保存清除及最小窗口布局", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		defer srv.Close()
		ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
		a, _ := testApp(t)
		tt := ui.NewTester(a.view, 940, 680)
		clickUI(t, tt, "环境与全局请求头")
		r, ok := tt.Find("CA 证书内容")
		if !ok || r.H < 70 || r.Y+r.H > 680 {
			t.Fatalf("最小窗口内 CA 编辑区不可用：%+v", r)
		}
		inputUI(t, tt, "CA 证书内容", ca)
		clickUI(t, tt, "保存配置")
		if a.workspace.Settings.CACertPEM != ca || a.notice != "" {
			t.Fatalf("UI CA 输入没有保存：%q", a.notice)
		}
		clickUI(t, tt, "清除证书")
		if a.caCertPEM != "" || a.workspace.Settings.CACertPEM != ca {
			t.Fatal("清除证书应在保存配置后生效")
		}
		clickUI(t, tt, "保存配置")
		if a.workspace.Settings.CACertPEM != "" {
			t.Fatal("清除 CA 后未保存")
		}
		clickUI(t, tt, "返回请求")
		if !tt.HasText("请求地址") {
			t.Fatal("保存 CA 后不能返回请求界面")
		}
	})
	t.Run("响应高亮跟随内容类型", func(t *testing.T) {
		a, _ := testApp(t)
		tt := ui.NewTester(a.view, 1200, 820)
		language := func() string {
			if l := a.responseEditor.Buffer.Language(); l != nil {
				return l.Name
			}
			return ""
		}
		a.applyEvent(core.Event{Kind: "body", Text: "<a/>", Language: "xml"})
		tt.Frame()
		if language() != "XML" {
			t.Fatalf("XML 响应应使用 XML 高亮：%q", language())
		}
		a.applyEvent(core.Event{Kind: "body", Text: "plain", Language: ""})
		tt.Frame()
		if language() != "" {
			t.Fatalf("纯文本响应不应高亮：%q", language())
		}
		a.applyEvent(core.Event{Kind: "body", Text: `{"a":1}`, Language: "json"})
		a.responseTab = 1
		a.responseHeaders = http.Header{"X": {"1"}}
		tt.Frame()
		if language() != "" {
			t.Fatalf("响应头页不应使用正文高亮：%q", language())
		}
	})
	t.Run("流响应刷新不保留撤销历史", func(t *testing.T) {
		a, _ := testApp(t)
		a.workspace.Draft.Protocol = "SSE"
		tt := ui.NewTester(a.view, 1200, 820)
		for i := 0; i < 8; i++ {
			a.stream = fmt.Sprintf("snapshot-%d\n", i) + strings.Repeat("x", 16384)
			tt.Frame()
		}
		if a.responseEditor.Buffer.CanUndo() {
			t.Fatal("只读流响应的每次全文刷新保留了旧全文撤销历史，256KiB展示裁剪不能限制实际保留内容")
		}
	})
	t.Run("设置界面保存后恢复请求", func(t *testing.T) {
		a, _ := testApp(t)
		tt := ui.NewTester(a.view, 1200, 820)
		clickUI(t, tt, "环境与全局请求头")
		// 静态标签与输入框拥有相同文本，Find先命中标签；点击它右侧的真实输入框。
		label, ok := tt.Find("环境名称")
		if !ok {
			t.Fatal("缺少环境名称标签")
		}
		tt.ClickAt(label.X+label.W+10+110, label.Y+label.H/2)
		tt.Key(ui.Cmd, ui.KeyA)
		tt.Type("UI环境")
		inputUI(t, tt, "环境变量", `{"host":"https://example.test"}`)
		inputUI(t, tt, "全局请求头", "X-Global: value")
		clickUI(t, tt, "保存配置")
		if a.workspace.Settings.Environment != "UI环境" || a.workspace.Settings.Environments["UI环境"]["host"] != "https://example.test" || len(a.workspace.Settings.GlobalHeaders) != 1 {
			t.Fatalf("设置输入未保存：envName=%q variables=%q globalHeaders=%q notice=%q settings=%+v", a.envName, a.variables, a.globalHeaders, a.notice, a.workspace.Settings)
		}
		clickUI(t, tt, "返回请求")
		tt.Frame()
		if !tt.HasText("请求地址") {
			t.Fatal("设置返回未恢复请求页面")
		}
	})
	t.Run("搜索名称地址忽略大小写且中文安全截断", func(t *testing.T) {
		a, _ := testApp(t)
		x, y := appDraft("https://example.test/alpha"), appDraft("https://example.test/beta")
		x.ID, x.Name = "x", "中文请求名称很长用于确认多字节字符不会被错误截断成乱码测试"
		y.ID, y.Name = "y", "Other"
		a.workspace.Requests = []core.Request{x, y}
		tt := ui.NewTester(a.view, 1200, 820)
		inputUI(t, tt, "搜索请求", "ALPHA")
		if !tt.HasText("打开 "+x.Name) || tt.HasText("打开 Other") {
			t.Fatal("搜索过滤与大小写处理错误")
		}
		inputUI(t, tt, "搜索请求", "other")
		if !tt.HasText("打开 Other") || tt.HasText("打开 "+x.Name) {
			t.Fatal("搜索名称失败")
		}
		tt.Frame()
	})
	t.Run("关闭等待中快捷键不修改最终快照", func(t *testing.T) {
		a, _ := testApp(t)
		a.saves = make(chan saveJob, 1)
		tt := ui.NewTester(a.view, 1200, 820)
		a.closing = true
		tt.Frame()
		before := appSnapshot(t, a.workspace)
		tt.Key(ui.Cmd, ui.KeyN)
		tt.Key(ui.Cmd, ui.KeyS)
		tt.Key(ui.Cmd, ui.KeyEnter)
		tt.Frame()
		if appSnapshot(t, a.workspace) != before || len(a.saves) != 0 || a.busy {
			t.Fatal("最终保存等待中快捷键仍修改工作区")
		}
	})
	t.Run("初始布局名称地址输入与新建", func(t *testing.T) {
		a, _ := testApp(t)
		tt := ui.NewTester(a.view, 1200, 820)
		for _, text := range []string{"CurlGo", ">", "贴入请求，马上开始"} {
			if !tt.HasText(text) {
				t.Fatalf("初始界面未显示品牌文本 %q", text)
			}
		}
		r, ok := tt.Find("请求头内容")
		if !ok || r.H < 60 {
			t.Fatalf("请求头编辑区不可用：%+v", r)
		}
		inputUI(t, tt, "请求名称", "UI请求")
		inputUI(t, tt, "请求地址", "https://example.test/ui")
		if a.workspace.Draft.Name != "UI请求" || a.workspace.Draft.URL != "https://example.test/ui" {
			t.Fatal("真实输入未更新模型")
		}
		old := a.workspace.Draft.ID
		clickUI(t, tt, "＋  新请求")
		if a.workspace.Draft.ID == old || a.workspace.Draft.URL != "" {
			t.Fatal("新建按钮未生效")
		}
		tt.Frame()
		tt.Frame()
	})
	t.Run("tab切换和多帧生命周期", func(t *testing.T) {
		a, _ := testApp(t)
		tt := ui.NewTester(a.view, 1200, 820)
		clickUI(t, tt, "正文")
		inputUI(t, tt, "请求正文", `{"hello":"world"}`)
		clickUI(t, tt, "选项")
		inputUI(t, tt, "超时秒数", "15")
		clickUI(t, tt, "Cookie")
		if !tt.HasText("还没有 Cookie") {
			t.Fatal("Cookie标签未展示")
		}
		clickUI(t, tt, "请求头")
		tt.Frame()
		clickUI(t, tt, "正文")
		tt.Frame()
		if a.workspace.Draft.Body != `{"hello":"world"}` || a.workspace.Draft.TimeoutSeconds != 15 {
			t.Fatal("切页丢失编辑内容")
		}
	})
	t.Run("浅深主题实际选择", func(t *testing.T) {
		a, _ := testApp(t)
		tt := ui.NewTester(a.view, 1200, 820)
		before := tt.Image().RGBAAt(500, 100)
		clickUI(t, tt, "主题")
		clickUI(t, tt, "深色")
		tt.Frame()
		if a.workspace.Settings.Theme != "深色" {
			t.Fatal("主题选择未更新")
		}
		after := tt.Image().RGBAAt(500, 100)
		if before == after {
			t.Fatal("切换主题没有改变渲染")
		}
		clickUI(t, tt, "主题")
		clickUI(t, tt, "浅色")
		tt.Frame()
		if a.workspace.Settings.Theme != "浅色" {
			t.Fatal("不能切回浅色")
		}
	})
	t.Run("导入收藏历史恢复与页面返回", func(t *testing.T) {
		a, _ := testApp(t)
		tt := ui.NewTester(a.view, 1200, 820)
		clickUI(t, tt, "导入 curl / 集合")
		inputUI(t, tt, "curl 命令", `curl https://example.test/imported -H 'X-Test: yes'`)
		clickUI(t, tt, "导入请求")
		if a.workspace.Draft.URL != "https://example.test/imported" {
			t.Fatal("UI导入失败")
		}
		inputUI(t, tt, "请求名称", "已收藏")
		clickUI(t, tt, "收藏  ⌘ S")
		clickUI(t, tt, "＋  新请求")
		clickUI(t, tt, "打开 已收藏")
		if a.workspace.Draft.URL != "https://example.test/imported" {
			t.Fatal("收藏恢复失败")
		}
		a.workspace.History = []core.History{{Request: appDraft("https://example.test/history"), At: time.Date(2024, 1, 1, 12, 30, 0, 0, time.UTC)}}
		clickUI(t, tt, "历史")
		tt.Frame()
		var historyLabel string
		for _, label := range tt.Texts() {
			if strings.HasPrefix(label, "12:30  GET") {
				historyLabel = label
				break
			}
		}
		if historyLabel == "" {
			t.Fatal("未显示历史项")
		}
		clickUI(t, tt, historyLabel)
		if a.workspace.Draft.URL != "https://example.test/history" {
			t.Fatal("历史未恢复草稿")
		}
		clickUI(t, tt, "环境与全局请求头")
		tt.Frame()
		clickUI(t, tt, "返回请求")
		tt.Frame()
	})
	t.Run("快捷键新建聚焦发送取消", func(t *testing.T) {
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
		old := a.workspace.Draft.ID
		tt.Key(ui.Cmd, ui.KeyN)
		if a.workspace.Draft.ID == old {
			t.Fatal("Cmd+N未新建")
		}
		tt.Key(ui.Cmd, ui.KeyL)
		if !tt.Focused("请求地址") {
			t.Fatal("Cmd+L未聚焦地址")
		}
		tt.Type(srv.URL)
		tt.Key(ui.Cmd, ui.KeyEnter)
		if !a.busy {
			t.Fatal("Cmd+Enter未发送")
		}
		tt.Key(0, ui.KeyEscape)
		if a.busy || a.cancel != nil {
			t.Fatal("Esc未取消")
		}
		tt.Frame()
	})
	t.Run("非法头阻止复制旧curl", func(t *testing.T) {
		a, _ := testApp(t)
		a.workspace.Draft = appDraft("https://example.test")
		a.syncDraft()
		tt := ui.NewTester(a.view, 1200, 820)
		tt.SetClipboard("before")
		inputUI(t, tt, "请求头内容", "invalid header")
		clickUI(t, tt, "复制 curl")
		if tt.Clipboard() != "before" || a.notice == "" {
			t.Fatal("非法输入后仍复制旧请求")
		}
	})
}
