package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestApplicationCookiePersistence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "logged-in", Path: "/"})
		}
		fmt.Fprint(w, r.Header.Get("Cookie"))
	}))
	defer srv.Close()
	a, q := testApp(t)
	a.saves = make(chan saveJob, 1)
	a.workspace.Draft = appDraft(srv.URL + "/login")
	a.send()
	pumpApp(t, q, func() bool { return !a.busy })
	job := <-a.saves
	if len(job.workspace.Cookies) != 1 {
		t.Fatal("Cookie 更新未排队持久化")
	}
	restarted := newApplication(job.workspace)
	defer restarted.engine.Close()
	updates := make(chan func(), 32)
	restarted.updates = func(fn func()) { updates <- fn }
	restarted.workspace.Draft = appDraft(srv.URL + "/echo")
	restarted.send()
	pumpApp(t, updates, func() bool { return !restarted.busy })
	if restarted.response != "session=logged-in" {
		t.Fatalf("重启后登录态未恢复：%q", restarted.response)
	}
	tt := ui.NewTester(a.view, 1200, 820)
	clickUI(t, tt, "Cookie")
	clickUI(t, tt, "清空所有 Cookie")
	if len((<-a.saves).workspace.Cookies) != 0 {
		t.Fatal("UI 清空没有删除保存的 Cookie")
	}
	a.closeWindow(&mygo.CloseEvent{})
	if len((<-a.saves).workspace.Cookies) != 0 {
		t.Fatal("关窗快照恢复了已清空的 Cookie")
	}
}
