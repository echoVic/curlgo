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

func assertionDraft(address string) core.Request {
	r := extractionDraft(address)
	r.Assertions = []core.ResponseAssertion{
		{ID: "status-rule", Name: "成功状态", Source: core.AssertStatus, Operator: core.AssertEquals, Expected: "200"},
		{ID: "json-rule", Source: core.AssertJSON, Operator: core.AssertEquals, Selector: "/ok", Expected: "true"},
	}
	return r
}

func TestResponseAssertionsGateEnvironmentUpdates(t *testing.T) {
	for _, tc := range []struct {
		name, body, code string
		status           int
		shortRead        bool
	}{
		{"pass", `{"ok":true,"data":{"access_token":"new","id":1}}`, "PASSED", 200, false},
		{"mismatch", `{"ok":false,"data":{"access_token":"new","id":1}}`, "MISMATCH", 200, false},
		{"invalid", `{"ok":`, "INVALID_JSON", 200, false},
		{"non2xx", `{"ok":true,"data":{"access_token":"new","id":1}}`, "MISMATCH", 401, false},
		{"truncated", strings.Repeat("x", core.MaxBody+1), "TRUNCATED_RESPONSE", 200, false},
		{"body read", `{"ok":true}`, "", 200, true},
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
			a.selectRequest(assertionDraft(server.URL))
			a.workspace.Settings.Environments[a.envName]["token"] = "old"
			before := appSnapshot(t, a.workspace.Settings)
			a.send()
			pumpApp(t, q, func() bool { return !a.busy })
			if tc.shortRead {
				if a.assertionReport != nil || a.canValidateResponse() {
					t.Fatal("failed transport was evaluated")
				}
				return
			}
			if a.assertionReport == nil || len(a.assertionReport.Results) != 2 || a.response == "" {
				t.Fatal("assertion report or HTTP response discarded")
			}
			passed := tc.name == "pass"
			if a.assertionReport.Passed != passed {
				t.Fatalf("wrong report: %+v", a.assertionReport)
			}
			if passed {
				if a.workspace.Settings.Environments[a.envName]["token"] != "new" {
					t.Fatal("passing assertions blocked extraction")
				}
			} else if appSnapshot(t, a.workspace.Settings) != before || a.notice == "" {
				t.Fatal("failed assertions changed environment or had no feedback")
			}
			found := false
			for _, result := range a.assertionReport.Results {
				found = found || result.Code == tc.code
			}
			if !found {
				t.Fatalf("expected %s: %+v", tc.code, a.assertionReport)
			}
		})
	}
}

func TestAssertionsUseSendTimeRulesAndManualEdits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"ok":true,"data":{"access_token":"new","id":1}}`)
	}))
	defer server.Close()
	a, q := testApp(t)
	a.selectRequest(assertionDraft(server.URL))
	a.send()
	update := nextAppUpdate(t, q)
	a.workspace.Draft.Assertions[0].Expected = "401"
	update()
	pumpApp(t, q, func() bool { return !a.busy })
	if a.assertionReport == nil || !a.assertionReport.Passed || a.workspace.Settings.Environments[a.envName]["token"] != "new" {
		t.Fatal("send-time assertion slice was not frozen")
	}
	a.workspace.Settings.Environments[a.envName]["token"] = "old"
	before := appSnapshot(t, a.workspace.Settings)
	a.validateCurrentResponse()
	if a.assertionReport.Passed || appSnapshot(t, a.workspace.Settings) != before {
		t.Fatal("manual validation ignored edited rules or wrote variables")
	}
	a.extractCurrentResponse()
	if appSnapshot(t, a.workspace.Settings) != before {
		t.Fatal("manual extraction bypassed edited assertions")
	}
	a.workspace.Draft.Assertions[0].Expected = "200"
	a.extractCurrentResponse()
	if a.workspace.Settings.Environments[a.envName]["token"] != "new" || !a.assertionReport.Passed {
		t.Fatal("manual extraction could not recover after fixing assertions")
	}
	a.newRequest()
	if a.assertionReport != nil || a.assertionNotice != "" || a.canValidateResponse() {
		t.Fatal("new request retained assertion state")
	}
}

func TestAssertionsNon2xxAndStaleResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"ok":true,"data":{"access_token":"new","id":1}}`)
	}))
	defer server.Close()
	for _, action := range []string{"complete", "stop", "new", "close"} {
		t.Run(action, func(t *testing.T) {
			a, q := testApp(t)
			r := assertionDraft(server.URL)
			r.Assertions[0].Expected = "401"
			a.selectRequest(r)
			before := appSnapshot(t, a.workspace.Settings)
			a.send()
			update := nextAppUpdate(t, q)
			switch action {
			case "stop":
				a.stop()
			case "new":
				a.newRequest()
			case "close":
				a.closing = true
			}
			update()
			if action == "complete" {
				pumpApp(t, q, func() bool { return !a.busy })
				if a.assertionReport == nil || !a.assertionReport.Passed || !a.canValidateResponse() {
					t.Fatal("expected 401 could not pass assertions")
				}
			} else if a.assertionReport != nil || a.canValidateResponse() {
				t.Fatal("stale response assertions executed")
			}
			if appSnapshot(t, a.workspace.Settings) != before || a.canExtractResponse() {
				t.Fatal("non2xx or stale response enabled extraction")
			}
		})
	}
}

func TestAssertionSnapshotsAndRestart(t *testing.T) {
	a, _ := testApp(t)
	a.selectRequest(assertionDraft("https://example.test"))
	a.saveRequest()
	a.addHistory(a.workspace.Draft, "sent")
	a.copySavedRequest(a.workspace.Draft.ID)
	a.workspace.Draft.Assertions[0].Expected = "401"
	if a.workspace.Requests[0].Assertions[0].Expected != "200" || a.workspace.Requests[1].Assertions[0].Expected != "200" ||
		a.workspace.History[0].Request.Assertions[0].Expected != "200" {
		t.Fatal("snapshots share assertion slices")
	}
	a.selectRequest(a.workspace.History[0].Request)
	snapshot := cloneWorkspace(a.workspace)
	a.workspace.Draft.Assertions[0].Expected = "500"
	a.workspace.Requests[0].Assertions[0].Expected = "500"
	a.workspace.History[0].Request.Assertions[0].Expected = "500"
	store, err := core.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(snapshot); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || !reflect.DeepEqual(loaded.Draft.Assertions, snapshot.Draft.Assertions) ||
		loaded.Requests[0].Assertions[0].Expected != "200" || loaded.History[0].Request.Assertions[0].Expected != "200" {
		t.Fatalf("restart lost assertions: %v", err)
	}
	a.workspace.Draft.Assertions[0].Expected = "bad"
	before := appSnapshot(t, a.workspace.Requests)
	a.saveRequest()
	if a.notice == "" || appSnapshot(t, a.workspace.Requests) != before {
		t.Fatal("invalid assertions saved")
	}
}

func TestAssertionViewAtMinimumWindow(t *testing.T) {
	for _, theme := range []string{"浅色", "深色"} {
		t.Run(theme, func(t *testing.T) {
			a, q := testApp(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"ok":true}`)
			}))
			defer server.Close()
			a.workspace.Settings.Theme = theme
			a.selectRequest(appDraft(server.URL))
			tt := ui.NewTester(a.view, 940, 680)
			clickUI(t, tt, "断言")
			clickUI(t, tt, "添加断言")
			inputUI(t, tt, "断言名称 1", "成功")
			inputUI(t, tt, "断言期望值 1", "200")
			for _, label := range []string{"断言名称 1", "断言来源 1", "断言操作 1", "断言期望值 1", "验证当前响应"} {
				rect, ok := tt.Find(label)
				if !ok || rect.W < 30 || rect.X+rect.W > 940 || rect.Y+rect.H > 680 {
					t.Fatalf("control outside minimum window %s: %+v", label, rect)
				}
			}
			clickUI(t, tt, "发送  ⌘ ↵")
			pumpApp(t, q, func() bool { return !a.busy })
			tt.Frame()
			if a.assertionReport == nil || !a.assertionReport.Passed {
				t.Fatal("UI rule was not evaluated")
			}
			clickUI(t, tt, "断言结果")
			if _, ok := tt.Find("断言通过"); !ok {
				t.Fatal("report not rendered")
			}
			clickUI(t, tt, "断言来源 1")
			clickUI(t, tt, "断言 JSON 字段")
			inputUI(t, tt, "断言选择器 1", "/ok")
			inputUI(t, tt, "断言期望值 1", "true")
			clickUI(t, tt, "验证当前响应")
			if a.assertionReport == nil || !a.assertionReport.Passed {
				t.Fatal("manual UI JSON rule failed")
			}
			clickUI(t, tt, "断言操作 1")
			clickUI(t, tt, "断言存在")
			clickUI(t, tt, "启用断言 1")
			if !a.workspace.Draft.Assertions[0].Disabled {
				t.Fatal("disable failed")
			}
			clickUI(t, tt, "移除断言 1")
			if len(a.workspace.Draft.Assertions) != 0 {
				t.Fatal("remove failed")
			}
		})
	}
}

func TestFailedAssertionReportFitsMinimumWindow(t *testing.T) {
	a, _ := testApp(t)
	a.workspace.Draft.Assertions = []core.ResponseAssertion{
		{ID: "status", Source: core.AssertStatus, Operator: core.AssertEquals, Expected: "201"},
	}
	a.requestTab, a.responseTab = 7, 2
	a.capturedResponse = responseCapture{
		requestID: a.workspace.Draft.ID, generation: a.generation, status: 200, complete: true, received: true,
	}
	a.evaluateResponseAssertions(a.workspace.Draft.Assertions)
	tt := ui.NewTester(a.view, 940, 680)
	tt.Frame()
	notice, ok := tt.Find("知道了")
	if !ok {
		t.Fatal("failure notice missing")
	}
	for _, label := range []string{"断言结果", "值与期望不符"} {
		rect, ok := tt.Find(label)
		if !ok || rect.Y+rect.H > notice.Y-10 {
			t.Fatalf("failed assertion report hidden by notice: %s %+v, notice %+v", label, rect, notice)
		}
	}
}
