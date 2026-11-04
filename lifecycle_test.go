package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/egoist/mygo"
)

func startLifecycleSaver(t *testing.T) (*application, chan func()) {
	t.Helper()
	a, q := testApp(t)
	var err error
	a.store, err = core.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.startSaving()
	t.Cleanup(func() { close(a.saves) })
	return a, q
}

func TestApplicationBeforeQuit(t *testing.T) {
	t.Run("final flush lets the original quit or relaunch close proceed", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			a, _ := startLifecycleSaver(t)
			a.closeNative = func() { t.Error("quit hook must not start a separate native close") }
			a.workspace.Draft.Name = "最终退出快照"
			a.persist()
			quit := &mygo.QuitEvent{}
			a.beforeQuit(quit)
			closeEvent := &mygo.CloseEvent{}
			a.closeWindow(closeEvent)
			if quit.DefaultPrevented() || closeEvent.DefaultPrevented() {
				t.Fatal("successful quit/relaunch was canceled by the asynchronous final save")
			}
			saved, err := a.store.Load()
			if err != nil || saved.Draft.Name != "最终退出快照" {
				t.Fatalf("quit returned before its final snapshot reached disk: %v, %q", err, saved.Draft.Name)
			}
		})
	})
	t.Run("quit takes over a queued asynchronous window close", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			a, q := startLifecycleSaver(t)
			closeCalls := 0
			a.closeNative = func() { closeCalls++ }
			a.workspace.Draft.Name = "旧关窗快照"
			a.closeWindow(&mygo.CloseEvent{})
			// No UI callback or wait: Quit must accept a final job even while
			// the previous close is queued or its save is already in progress.
			a.workspace.Draft.Name = "接管关窗后的退出快照"
			quit := &mygo.QuitEvent{}
			a.beforeQuit(quit)
			if quit.DefaultPrevented() || !a.saveReady {
				t.Fatal("pending asynchronous final save prevented quit takeover")
			}
			saved, err := a.store.Load()
			if err != nil || saved.Draft.Name != "接管关窗后的退出快照" {
				t.Fatalf("old final job replaced the quit snapshot: %v, %q", err, saved.Draft.Name)
			}
			synctest.Wait()
			for len(q) > 0 {
				(<-q)()
			}
			if closeCalls != 0 || !a.saveReady {
				t.Fatal("old final callback started a separate close after quit takeover")
			}
		})
	})
	t.Run("quit needs no main-thread save completion callback", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			a, q := startLifecycleSaver(t)
			a.closeNative = func() { t.Error("quit flush started an asynchronous close") }
			// Even a full completion queue cannot hold up the final disk result.
			// Calling Window.Update is nonblocking in production; the synchronous
			// job must not depend on calling it at all.
			for len(q) < cap(q) {
				q <- func() {}
			}
			quit := &mygo.QuitEvent{}
			a.beforeQuit(quit)
			if quit.DefaultPrevented() || !a.saveReady {
				t.Fatal("quit waited for main-thread completion delivery")
			}
		})
	})
	for _, state := range []string{"read-only", "closed", "already saved"} {
		t.Run(state+" quit needs no saver", func(t *testing.T) {
			a, _ := testApp(t)
			a.readOnly = state == "read-only"
			a.closed = state == "closed"
			a.saveReady = state == "already saved"
			event := &mygo.QuitEvent{}
			a.beforeQuit(event)
			if event.DefaultPrevented() {
				t.Fatal("safe quit was canceled")
			}
		})
	}
	t.Run("failure retains the window and supports quit retry", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			a, _ := startLifecycleSaver(t)
			a.closeNative = func() { t.Error("failed quit closed the window") }
			path := filepath.Join(a.store.Dir, "workspace.json")
			if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
				t.Fatal(err)
			}
			quit := &mygo.QuitEvent{}
			a.beforeQuit(quit)
			if !quit.DefaultPrevented() || a.closing || a.closed || a.saveReady || !strings.Contains(a.notice, "保存失败") {
				t.Fatal("failed quit did not preserve an editable window and show the save error")
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			a.workspace.Draft.Name = "退出重试的新内容"
			a.persist()
			retry := &mygo.QuitEvent{}
			a.beforeQuit(retry)
			if retry.DefaultPrevented() || !a.saveReady {
				t.Fatal("quit could not retry after a failed final save")
			}
			saved, err := a.store.Load()
			if err != nil || saved.Draft.Name != "退出重试的新内容" {
				t.Fatalf("retry lost the latest workspace: %v, %q", err, saved.Draft.Name)
			}
		})
	})
	for _, oldSaveFails := range []bool{false, true} {
		name := "pending window close success"
		if oldSaveFails {
			name = "pending window close failure"
		}
		t.Run(name+" cannot override a newer failed quit", func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a, q := startLifecycleSaver(t)
				closeCalls := 0
				a.closeNative = func() { closeCalls++ }
				path := filepath.Join(a.store.Dir, "workspace.json")
				if oldSaveFails {
					if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				a.closeWindow(&mygo.CloseEvent{})
				synctest.Wait()
				oldCompletion := <-q      // saved, but its main-thread callback is still pending
				a.workspace.Version = 999 // the newer final flush must fail validation
				quit := &mygo.QuitEvent{}
				a.beforeQuit(quit)
				if !quit.DefaultPrevented() {
					t.Fatal("quit with a failed final flush was allowed")
				}
				notice := a.notice
				oldCompletion()
				if closeCalls != 0 || a.saveReady || a.closing || a.closed || a.notice != notice {
					t.Fatal("old asynchronous final completion overrode the failed quit")
				}
				a.workspace.Version = 1
				if oldSaveFails {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
				retry := &mygo.QuitEvent{}
				a.beforeQuit(retry)
				if retry.DefaultPrevented() || !a.saveReady {
					t.Fatal("retry was blocked by the old asynchronous final job")
				}
			})
		})
	}
}
