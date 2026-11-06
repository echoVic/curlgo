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

func TestAuthView(t *testing.T) {
	for _, theme := range []string{"浅色", "深色"} {
		t.Run(theme, func(t *testing.T) {
			seen := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen <- r.Header.Get("Authorization")
				fmt.Fprint(w, "authenticated")
			}))
			defer server.Close()
			a, q := testApp(t)
			a.workspace.Settings.Theme = theme
			a.workspace.Settings.Environments[a.workspace.Settings.Environment]["token"] = "test-token"
			a.selectRequest(appDraft(server.URL))
			tt := ui.NewTester(a.view, 940, 680)
			clickUI(t, tt, "鉴权")
			clickUI(t, tt, "鉴权方式")
			clickUI(t, tt, "Bearer Token")
			inputUI(t, tt, "Bearer Token 值", "{{token}}")
			for _, label := range []string{"鉴权方式", "Bearer Token 值", "显示凭证"} {
				rect, ok := tt.Find(label)
				if !ok || rect.W < 30 || rect.X+rect.W > 940 || rect.Y+rect.H > 680 {
					t.Fatalf("auth control outside minimum window %s: %+v", label, rect)
				}
			}
			clickUI(t, tt, "复制 curl")
			if !strings.Contains(tt.Clipboard(), "Bearer test-token") {
				t.Fatal("copied curl omitted auth")
			}
			clickUI(t, tt, a.saveShortcutLabel())
			clickUI(t, tt, a.sendShortcutLabel())
			pumpApp(t, q, func() bool { return !a.busy })
			if got := <-seen; got != "Bearer test-token" || a.response != "authenticated" || a.notice != "" {
				t.Fatalf("UI authentication failed: header=%q notice=%q", got, a.notice)
			}
			if a.workspace.History[0].Request.Auth.Token != "{{token}}" || a.workspace.Requests[0].Auth.Token != "{{token}}" {
				t.Fatal("history/favorite saved expanded credential")
			}
			clickUI(t, tt, "鉴权方式")
			clickUI(t, tt, "Basic Auth")
			inputUI(t, tt, "Basic 用户名", "{{user}}")
			inputUI(t, tt, "Basic 密码", "{{pass}}")
			clickUI(t, tt, "鉴权方式")
			clickUI(t, tt, "API Key")
			inputUI(t, tt, "API Key 名称", "access_key")
			inputUI(t, tt, "API Key 值", "a&+ 中文")
			clickUI(t, tt, "Query")
			cmd, err := core.ExportCURL(a.workspace.Draft, a.workspace.Settings)
			if err != nil || !strings.Contains(cmd, "access_key=a%26%2B+%E4%B8%AD%E6%96%87") {
				t.Fatalf("API Key query UI failed: %v", err)
			}
			clickUI(t, tt, "鉴权方式")
			clickUI(t, tt, "Bearer Token")
			if a.workspace.Draft.Auth.Token != "{{token}}" || a.workspace.Draft.Auth.Username != "{{user}}" {
				t.Fatal("switching mode lost editable fields")
			}
			a.selectRequest(a.workspace.Requests[0])
			if a.workspace.Draft.Auth.Mode != core.AuthBearer || a.workspace.Draft.Auth.Password != "" {
				t.Fatal("editing draft contaminated favorite")
			}
		})
	}
}

func TestAuthSaveValidation(t *testing.T) {
	a, _ := testApp(t)
	a.selectRequest(appDraft("https://example.test"))
	a.workspace.Draft.Auth = core.Authentication{Mode: core.AuthBasic, Username: "bad:name"}
	a.saveRequest()
	if a.notice == "" || len(a.workspace.Requests) != 0 {
		t.Fatal("invalid auth saved into favorites")
	}
	a.workspace.Draft.Auth.Username = "{{not_yet_configured}}"
	a.notice = ""
	a.saveRequest()
	if a.notice != "" || len(a.workspace.Requests) != 1 {
		t.Fatal("saving a template tried resolving its variables")
	}
}

func TestAuthSnapshotsAndRestart(t *testing.T) {
	a, _ := testApp(t)
	a.selectRequest(appDraft("https://example.test"))
	config := core.Authentication{
		Mode: core.AuthBasic, Token: "{{inactive}}", Username: "{{user}}", Password: "{{pass}}",
		Key: "X-Key", Value: "{{api_key}}", Location: core.APIKeyHeader,
	}
	a.workspace.Draft.Auth = config
	a.saveRequest()
	a.addHistory(a.workspace.Draft, "sent")
	a.copySavedRequest(a.workspace.Draft.ID)
	if len(a.workspace.Requests) != 2 || a.workspace.Requests[1].Auth != config {
		t.Fatal("copy favorite lost auth")
	}
	a.showCredentials = true
	a.selectRequest(a.workspace.History[0].Request)
	if a.showCredentials {
		t.Fatal("credential visibility persisted across request selection")
	}
	snapshot := cloneWorkspace(a.workspace)
	a.workspace.Draft.Auth.Password = "changed"
	a.workspace.Requests[0].Auth.Password = "changed"
	a.workspace.History[0].Request.Auth.Password = "changed"
	if snapshot.Draft.Auth != config || snapshot.Requests[0].Auth != config || snapshot.History[0].Request.Auth != config {
		t.Fatal("auth shares mutable snapshot state")
	}
	store, err := core.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	restarted := newApplication(loaded)
	defer restarted.engine.Close()
	if appSnapshot(t, loaded) != appSnapshot(t, snapshot) || restarted.workspace.Draft.Auth != config || restarted.showCredentials {
		t.Fatalf("restart lost config: loaded=%s snapshot=%s", appSnapshot(t, loaded), appSnapshot(t, snapshot))
	}
}
