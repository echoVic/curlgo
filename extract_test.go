package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo/ui"
)

func extractionDraft(address string) core.Request {
	r := appDraft(address)
	r.Extractors = []core.ResponseExtractor{
		{ID: "token-rule", Variable: "token", Source: core.ExtractJSON, Selector: "/data/access_token"},
		{ID: "id-rule", Variable: "id", Source: core.ExtractJSON, Selector: "/data/id"},
	}
	return r
}

func TestResponseExtractionLoginChain(t *testing.T) {
	seen := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{"access_token":"private-token","id":9007199254740993}}`)
			return
		}
		seen <- r.Header.Get("Authorization") + " " + r.URL.Path
		fmt.Fprint(w, "authenticated")
	}))
	defer server.Close()
	a, q := testApp(t)
	a.selectRequest(extractionDraft(server.URL + "/login"))
	oldVars := a.workspace.Settings.Environments[a.envName]
	a.send()
	update := nextAppUpdate(t, q)
	if oldVars["token"] != "" {
		t.Fatal("background goroutine updated the environment")
	}
	update()
	pumpApp(t, q, func() bool { return !a.busy })
	vars := a.workspace.Settings.Environments[a.envName]
	if vars["token"] != "private-token" || vars["id"] != "9007199254740993" || oldVars["token"] != "" {
		t.Fatal("extraction was not an atomic replacement")
	}
	if !strings.Contains(a.response, "access_token") || a.notice != "" || strings.Contains(a.extractionNotice, "private-token") {
		t.Fatalf("extraction lost response or leaked feedback: %q", a.notice)
	}
	a.applySettings()
	if a.workspace.Settings.Environments[a.envName]["token"] != "private-token" {
		t.Fatal("stale settings editor reverted extracted variables")
	}
	r := appDraft(server.URL + "/users/{{id}}")
	r.ID = "protected"
	r.Auth = core.Authentication{Mode: core.AuthBearer, Token: "{{token}}"}
	a.selectRequest(r)
	a.send()
	pumpApp(t, q, func() bool { return !a.busy })
	if got := <-seen; got != "Bearer private-token /users/9007199254740993" {
		t.Fatalf("next request did not reuse variables: %q", got)
	}
}

func TestResponseExtractionFailuresDoNotWrite(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		shortRead  bool
	}{
		{"missing", `{"data":{"access_token":"new"}}`, 200, false},
		{"null", `{"data":{"access_token":"new","id":null}}`, 200, false},
		{"invalid", `{"data":`, 200, false},
		{"non2xx", `{"data":{"access_token":"new","id":1}}`, 401, false},
		{"truncated", strings.Repeat("x", core.MaxBody+1), 200, false},
		{"body read", `{"data":{"access_token":"new","id":1}}`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.shortRead {
					w.Header().Set("Content-Length", "10000")
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			a, q := testApp(t)
			a.selectRequest(extractionDraft(server.URL))
			a.workspace.Settings.Environments[a.envName]["token"] = "old"
			before := appSnapshot(t, a.workspace.Settings)
			a.send()
			pumpApp(t, q, func() bool { return !a.busy })
			if appSnapshot(t, a.workspace.Settings) != before {
				t.Fatal("failed response extraction wrote partial values")
			}
			if tc.name != "non2xx" && a.notice == "" {
				t.Fatal("extraction failure was not visible")
			}
			if !tc.shortRead && (a.response == "" || !strings.HasPrefix(a.status, fmt.Sprint(tc.status))) {
				t.Fatal("extraction failure discarded HTTP response")
			}
			if tc.shortRead && a.canExtractResponse() {
				t.Fatal("manual extraction enabled for failed transport")
			}
		})
	}
}

func TestResponseExtractionUsesSendTimeEnvironmentAndRules(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"data":{"access_token":"sent-token","id":1}}`)
	}))
	defer server.Close()
	a, q := testApp(t)
	a.selectRequest(extractionDraft(server.URL))
	original := a.envName
	a.send()
	update := nextAppUpdate(t, q)
	a.workspace.Settings.Environment, a.envName = "other", "other"
	a.workspace.Settings.Environments["other"] = map[string]string{"token": "other-token"}
	a.variables = `{"token":"other-token"}`
	a.workspace.Draft.Extractors[0].Selector = "/missing"
	update()
	pumpApp(t, q, func() bool { return !a.busy })
	if a.workspace.Settings.Environments[original]["token"] != "sent-token" || a.workspace.Settings.Environments["other"]["token"] != "other-token" ||
		a.variables != `{"token":"other-token"}` {
		t.Fatal("send-time settings or frozen extraction rules changed")
	}
	a.workspace.Draft.Extractors[0].Selector = "/data/access_token"
	a.extractCurrentResponse()
	if a.workspace.Settings.Environments["other"]["token"] != "sent-token" {
		t.Fatal("manual extraction did not use currently selected environment")
	}
}

func TestResponseExtractionIgnoresCancelledAndStaleCallbacks(t *testing.T) {
	for _, action := range []string{"stop", "new request", "close"} {
		t.Run(action, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"data":{"access_token":"sent-token","id":1}}`)
			}))
			defer server.Close()
			a, q := testApp(t)
			a.selectRequest(extractionDraft(server.URL))
			a.send()
			update := nextAppUpdate(t, q)
			switch action {
			case "stop":
				a.stop()
			case "new request":
				a.newRequest()
			case "close":
				a.closing = true
			}
			update()
			if a.workspace.Settings.Environments[a.envName]["token"] != "" || a.canExtractResponse() {
				t.Fatal("cancelled or stale callback enabled variable extraction")
			}
		})
	}
}

func TestExtractionRulesSnapshotsAndValidation(t *testing.T) {
	a, _ := testApp(t)
	a.selectRequest(extractionDraft("https://example.test"))
	a.saveRequest()
	a.addHistory(a.workspace.Draft, "sent")
	a.copySavedRequest(a.workspace.Draft.ID)
	a.workspace.Draft.Extractors[0].Selector = "/changed"
	if a.workspace.Requests[0].Extractors[0].Selector != "/data/access_token" ||
		a.workspace.Requests[1].Extractors[0].Selector != "/data/access_token" ||
		a.workspace.History[0].Request.Extractors[0].Selector != "/data/access_token" {
		t.Fatal("draft, copy, favorite or history share mutable rules")
	}
	a.selectRequest(a.workspace.History[0].Request)
	snapshot := cloneWorkspace(a.workspace)
	a.workspace.Draft.Extractors[0].Variable = "changed"
	a.workspace.Requests[0].Extractors[0].Variable = "changed"
	a.workspace.History[0].Request.Extractors[0].Variable = "changed"
	store, err := core.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || !reflect.DeepEqual(loaded.Draft.Extractors, snapshot.Draft.Extractors) || loaded.Requests[0].Extractors[0].Variable != "token" ||
		loaded.History[0].Request.Extractors[0].Variable != "token" {
		t.Fatalf("restart or snapshots lost extraction config: %v", err)
	}
	a.workspace.Draft.Extractors[0].Variable = "bad name"
	before := appSnapshot(t, a.workspace.Requests)
	a.saveRequest()
	if a.notice == "" || appSnapshot(t, a.workspace.Requests) != before {
		t.Fatal("invalid extraction rules saved")
	}
}

func TestExtractionView(t *testing.T) {
	for _, theme := range []string{"浅色", "深色"} {
		t.Run(theme, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Trace", "trace-value")
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			a, q := testApp(t)
			a.workspace.Settings.Theme = theme
			a.selectRequest(appDraft(server.URL))
			tt := ui.NewTester(a.view, 940, 680)
			clickUI(t, tt, "提取")
			clickUI(t, tt, "添加提取规则")
			inputUI(t, tt, "目标变量 1", "trace")
			clickUI(t, tt, "提取来源 1")
			clickUI(t, tt, "提取响应头")
			inputUI(t, tt, "选择器 1", "X-Trace")
			for _, label := range []string{"目标变量 1", "选择器 1", "提取来源 1", "添加提取规则", "从当前响应提取"} {
				rect, ok := tt.Find(label)
				if !ok || rect.W < 30 || rect.X+rect.W > 940 || rect.Y+rect.H > 680 {
					t.Fatalf("extraction control outside minimum window %s: %+v", label, rect)
				}
			}
			clickUI(t, tt, "发送  ⌘ ↵")
			pumpApp(t, q, func() bool { return !a.busy })
			tt.Frame() // reflect the UI-thread completion before targeting enabled inputs
			if a.workspace.Settings.Environments[a.envName]["trace"] != "trace-value" || !a.canExtractResponse() {
				t.Fatalf("header extraction on empty 204 response failed: rules=%+v capture=%+v notice=%q", a.workspace.Draft.Extractors, a.capturedResponse, a.notice)
			}
			inputUI(t, tt, "目标变量 1", "trace_again")
			clickUI(t, tt, "从当前响应提取")
			if a.workspace.Settings.Environments[a.envName]["trace_again"] != "trace-value" {
				t.Fatalf("manual extraction after rule edit failed: rules=%+v notice=%q vars=%v can=%v", a.workspace.Draft.Extractors, a.notice, a.workspace.Settings.Environments[a.envName], a.canExtractResponse())
			}
			clickUI(t, tt, "启用提取规则 1")
			if !a.workspace.Draft.Extractors[0].Disabled || a.canExtractResponse() {
				t.Fatal("disabled rule remained executable")
			}
			clickUI(t, tt, "移除提取规则 1")
			if len(a.workspace.Draft.Extractors) != 0 {
				t.Fatal("rule removal failed")
			}
		})
	}
}
