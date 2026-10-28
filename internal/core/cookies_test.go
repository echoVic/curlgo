package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestCookieRestart(t *testing.T) {
	e := engineFixture(t)
	u, _ := url.Parse("https://api.example.com/area/set")
	e.jar.SetCookies(u, []*http.Cookie{
		{Name: "session", Value: "login", HttpOnly: true},
		{Name: "secure", Value: "secret", Path: "/", Secure: true},
		{Name: "parent", Value: "shared", Path: "/", Domain: ".example.com"},
		{Name: "expired", Value: "gone", Path: "/", Expires: time.Unix(1, 0)},
		{Name: "public", Value: "bad", Path: "/", Domain: "com"},
	})
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorkspace()
	w.Cookies = e.CookieSnapshot()
	if err := store.Save(w); err != nil {
		t.Fatal(err)
	}
	w, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	restarted := engineFixture(t)
	restarted.RestoreCookies(w.Cookies)
	for _, address := range []string{
		"https://api.example.com/area/read", "http://api.example.com/area",
		"https://api.example.com/outside", "https://other.example.com/area",
		"wss://api.example.com/area", "ws://api.example.com/area",
	} {
		if !reflect.DeepEqual(cookieNames(e.Cookies(address)), cookieNames(restarted.Cookies(address))) {
			t.Fatalf("重启改变 Cookie 选择：%s", address)
		}
	}
	if len(w.Cookies) != 3 {
		t.Fatalf("快照保留了被拒绝或过期的 Cookie：%d", len(w.Cookies))
	}
}

func TestCookieExpirationIsAbsolute(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := engineFixture(t)
		u, _ := url.Parse("https://example.com/")
		e.jar.SetCookies(u, []*http.Cookie{{Name: "short", Value: "login", MaxAge: 10}})
		snapshot := e.CookieSnapshot()
		if len(snapshot) != 1 || snapshot[0].Cookie.MaxAge != 0 || snapshot[0].Cookie.Expires.IsZero() {
			t.Fatal("Max-Age 必须转换为绝对到期时间")
		}
		time.Sleep(8 * time.Second)
		restarted := engineFixture(t)
		restarted.RestoreCookies(snapshot)
		if len(restarted.Cookies(u.String())) != 1 {
			t.Fatal("尚未到期的 Cookie 丢失")
		}
		time.Sleep(3 * time.Second)
		restarted.RestoreCookies(snapshot)
		if len(restarted.Cookies(u.String())) != 0 || len(restarted.CookieSnapshot()) != 0 {
			t.Fatal("重启延长了 Cookie 的到期时间")
		}
	})
}

func TestCookieReplacementDeletionAndSnapshot(t *testing.T) {
	e := engineFixture(t)
	first, _ := url.Parse("https://api.example.com/a/set")
	second, _ := url.Parse("https://other.example.com/a/set")
	e.jar.SetCookies(first, []*http.Cookie{{Name: "token", Value: "first", Domain: "example.com", Path: "/a"}})
	snapshot := e.CookieSnapshot()
	e.jar.SetCookies(second, []*http.Cookie{{Name: "token", Value: "second", Domain: ".example.com", Path: "/a"}})
	if len(e.CookieSnapshot()) != 1 || snapshot[0].Cookie.Value != "first" {
		t.Fatal("替换重复 Cookie 或快照深拷贝失败")
	}
	e.jar.SetCookies(first, []*http.Cookie{{Name: "token", Domain: "example.com", Path: "/a", MaxAge: -1}})
	if len(e.CookieSnapshot()) != 0 {
		t.Fatal("服务器删除没有同步到快照")
	}
	e.jar.SetCookies(first, []*http.Cookie{{Name: "old", Value: "first"}})
	oldJar := e.jar
	e.ClearCookies()
	oldJar.SetCookies(first, []*http.Cookie{{Name: "old", Value: "late"}})
	if len(e.CookieSnapshot()) != 0 || len(e.Cookies(first.String())) != 0 {
		t.Fatal("清空后旧请求恢复了 Cookie")
	}
}

func TestCookieAutomaticCapture(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "login", Value: "value", HttpOnly: true})
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()
	e := engineFixture(t)
	changes := 0
	e.OnCookiesChanged(func() { changes++ })
	if _, err := collectRun(e, context.Background(), engineRequest(srv.URL)); err != nil {
		t.Fatal(err)
	}
	if changes != 1 || len(e.CookieSnapshot()) != 1 || !e.CookieSnapshot()[0].Cookie.HttpOnly {
		t.Fatal("HTTP 收到 Cookie 后没有保留元数据并通知保存")
	}
	e.ClearCookies()
	if changes != 2 {
		t.Fatal("清空没有通知保存")
	}
}

func TestCookieRestoreRejectsTamperedEntries(t *testing.T) {
	e := engineFixture(t)
	e.RestoreCookies([]SavedCookie{
		{URL: "https://example.com/", Cookie: http.Cookie{Name: "bad name", Value: "x"}},
		{URL: "https://example.com/", Cookie: http.Cookie{Name: "bad-value", Value: "a;b"}},
		{URL: "https://example.com/", Cookie: http.Cookie{Name: "other", Value: "x", Domain: "attacker.test"}},
		{URL: "file:///tmp/x", Cookie: http.Cookie{Name: "scheme", Value: "x"}},
		{URL: "https://example.com/", Cookie: http.Cookie{Name: "ok", Value: "kept"}},
	})
	got := e.CookieSnapshot()
	if len(got) != 1 || got[0].Cookie.Name != "ok" {
		t.Fatalf("恢复了被篡改或无效的 Cookie：%+v", got)
	}
}

func TestCookieAncientExpiresDeletes(t *testing.T) {
	e := engineFixture(t)
	u, _ := url.Parse("https://example.com/")
	e.jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "login"}})
	e.jar.SetCookies(u, []*http.Cookie{{Name: "session", Expires: time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC)}})
	if len(e.Cookies(u.String())) != 0 || len(e.CookieSnapshot()) != 0 {
		t.Fatal("早于 1601 年的 Expires 也表示删除，旧登录态不应保留")
	}
}

func TestCookieSnapshotBounded(t *testing.T) {
	// '<' is escaped to \u003c, so the limit must count encoded bytes.
	for _, char := range []string{"v", "<"} {
		t.Run(char, func(t *testing.T) {
			e := engineFixture(t)
			u, _ := url.Parse("https://example.com/")
			value := strings.Repeat(char, 8<<10)
			for i := 0; i < 300; i++ {
				e.jar.SetCookies(u, []*http.Cookie{{Name: fmt.Sprintf("c%d", i), Value: value}})
			}
			snapshot := e.CookieSnapshot()
			data, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if len(data) > MaxInput+64<<10 || len(snapshot) == 0 || snapshot[len(snapshot)-1].Cookie.Name != "c299" {
				t.Fatalf("Cookie 快照没有限制大小或丢失最新 Cookie：%d bytes, %d cookies", len(data), len(snapshot))
			}
			if len(e.Cookies(u.String())) != 300 {
				t.Fatal("持久化上限不应影响当前会话")
			}
		})
	}
}

func TestCookieSnapshotConcurrent(t *testing.T) {
	e := engineFixture(t)
	u, _ := url.Parse("https://example.com/")
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Go(func() {
			e.jar.SetCookies(u, []*http.Cookie{{Name: "a", Value: "b"}})
			data, err := json.Marshal(e.CookieSnapshot())
			if err != nil || len(data) == 0 {
				t.Error("并发快照失败")
			}
		})
	}
	wg.Wait()
}
