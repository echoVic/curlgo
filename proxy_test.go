package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo/ui"
)

func TestApplicationProxySettings(t *testing.T) {
	proxied := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "via proxy "+r.Host)
	}))
	defer proxied.Close()
	t.Run("保存后请求经过代理", func(t *testing.T) {
		a, q := testApp(t)
		a.proxyURL = proxied.URL
		a.applySettings()
		if a.workspace.Settings.Proxy.URL != proxied.URL || a.notice != "" {
			t.Fatalf("代理没有保存：%q", a.notice)
		}
		a.workspace.Draft = appDraft("http://api.localhost/x")
		a.send()
		pumpApp(t, q, func() bool { return !a.busy })
		if a.response != "via proxy api.localhost" {
			t.Fatalf("请求没有经过代理：%q %q", a.response, a.notice)
		}
	})
	t.Run("无效代理不造成部分保存", func(t *testing.T) {
		a, _ := testApp(t)
		before := appSnapshot(t, a.workspace.Settings)
		a.proxyURL = "ftp://bad"
		a.envName, a.globalHeaders = "another", "X-New: value"
		a.applySettings()
		if appSnapshot(t, a.workspace.Settings) != before || !strings.Contains(a.notice, "代理") {
			t.Fatalf("无效代理改变了设置：%q", a.notice)
		}
	})
	t.Run("重启从工作区恢复代理", func(t *testing.T) {
		w := core.NewWorkspace()
		w.Settings.Proxy = core.ProxySettings{URL: proxied.URL, Bypass: "internal.localhost"}
		a := newApplication(w)
		defer a.engine.Close()
		if a.proxyURL != proxied.URL || a.proxyBypass != "internal.localhost" {
			t.Fatal("代理编辑字段没有恢复")
		}
		var body string
		err := a.engine.Run(t.Context(), appDraft("http://api.localhost/"), func(e core.Event) {
			if e.Kind == "body" {
				body = e.Text
			}
		})
		if err != nil || body != "via proxy api.localhost" {
			t.Fatalf("重启后代理没有生效：%v %q", err, body)
		}
	})
	t.Run("损坏的已存代理阻止请求直到修正", func(t *testing.T) {
		w := core.NewWorkspace()
		w.Settings.Proxy = core.ProxySettings{URL: "ftp://bad"}
		w.Draft = appDraft("http://api.localhost/")
		a := newApplication(w)
		defer a.engine.Close()
		a.send()
		if a.busy || len(a.workspace.History) != 0 || !strings.Contains(a.notice, "代理") {
			t.Fatalf("损坏的代理配置被静默忽略：%q", a.notice)
		}
		a.proxyURL = ""
		a.applySettings()
		if a.notice != "" || a.proxyError != nil {
			t.Fatalf("修正代理后旧错误未清除：%q", a.notice)
		}
	})
}

func TestProxySettingsView(t *testing.T) {
	a, _ := testApp(t)
	tt := ui.NewTester(a.view, 940, 680)
	clickUI(t, tt, "环境与全局请求头")
	for _, label := range []string{"代理地址", "不使用代理的地址", "保存配置"} {
		if r, ok := tt.Find(label); !ok || r.Y+r.H > 680 {
			t.Fatalf("最小窗口内看不到「%s」：%+v", label, r)
		}
	}
	inputUI(t, tt, "代理地址", "127.0.0.1:8888")
	inputUI(t, tt, "不使用代理的地址", "internal.localhost")
	clickUI(t, tt, "保存配置")
	if p := a.workspace.Settings.Proxy; p.URL != "127.0.0.1:8888" || p.Bypass != "internal.localhost" || a.notice != "" {
		t.Fatalf("界面输入的代理没有保存：%+v %q", p, a.notice)
	}
}
