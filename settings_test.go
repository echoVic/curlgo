package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// These catch navigation swallowing the existing environment page, settings
// controls being clipped at the minimum size, and theme edits losing drafts.
func TestAppSettingsNavigationAndRender(t *testing.T) {
	for _, size := range [][2]int{{1200, 820}, {940, 680}} {
		a, _ := testApp(t)
		a.workspace.Draft.URL = "https://example.test/unsaved"
		tt := ui.NewTester(a.view, size[0], size[1])
		clickUI(t, tt, "设置")
		if a.page != 7 {
			t.Fatalf("settings navigation selected page %d", a.page)
		}
		for _, label := range []string{"应用设置", "当前版本", "检查更新", "后台自动检查", "自动下载并安装"} {
			r, ok := tt.Find(label)
			if !ok || r.W <= 0 || r.H <= 0 || r.Y < 58 || r.Y+r.H > float32(size[1]-28) {
				t.Fatalf("%dx%d: settings control %q not visible: %+v", size[0], size[1], label, r)
			}
		}
		clickUI(t, tt, "应用主题")
		clickUI(t, tt, "深色")
		if a.workspace.Settings.Theme != "深色" || a.workspace.Draft.URL != "https://example.test/unsaved" {
			t.Fatal("settings theme did not preserve draft")
		}
		clickUI(t, tt, "环境与全局请求头")
		if a.page != 1 || !tt.HasText("保存配置") {
			t.Fatal("environment settings no longer usable")
		}
		clickUI(t, tt, "返回请求")
		if a.page != 0 || a.workspace.Draft.URL != "https://example.test/unsaved" {
			t.Fatal("navigation lost current request")
		}
	}
}

func TestAppSettingsDevelopmentExplainsUnavailableUpdates(t *testing.T) {
	a, _ := testApp(t)
	a.page = 7
	tt := ui.NewTester(a.view, 940, 680)
	if !tt.HasText("开发构建") || !tt.HasText("安装目录") {
		t.Fatal("settings did not explain development/installation restrictions")
	}
	if strings.Contains(strings.Join(tt.Texts(), "\n"), "离线") {
		t.Fatal("app incorrectly claims offline operation despite update networking")
	}
}
