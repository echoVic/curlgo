package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCookieBackupStaysLoadable(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	large := NewWorkspace()
	large.Draft.Body = strings.Repeat("x", 15<<20+768<<10)
	if err := store.Save(large); err != nil {
		t.Fatal(err)
	}
	small := NewWorkspace()
	for i := 0; i < 100; i++ {
		small.Cookies = append(small.Cookies, SavedCookie{URL: "https://example.com/", Cookie: http.Cookie{Name: fmt.Sprintf("c%d", i), Value: strings.Repeat("v", 8<<10)}})
	}
	if err := store.Save(small); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Dir, "workspace.json")
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.Load()
	if recovered.Version != 1 || recovered.Draft.Body != large.Draft.Body {
		t.Fatalf("合并当前 Cookie 后备份超过读取上限，无法恢复：%v", err)
	}
}

func TestClearedCookiesStayClearedAfterBackupRecovery(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorkspace()
	w.Cookies = []SavedCookie{{URL: "https://example.com/", Cookie: http.Cookie{Name: "session", Value: "login"}}}
	if err := store.Save(w); err != nil {
		t.Fatal(err)
	}
	w.Cookies = nil
	if err := store.Save(w); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Dir, "workspace.json")
	backup, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	var saved Workspace
	if err := json.Unmarshal(backup, &saved); err != nil || len(saved.Cookies) != 0 {
		t.Fatal("清空后备份仍包含旧登录态")
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.Load()
	if err == nil || recovered.Version != 1 || len(recovered.Cookies) != 0 {
		t.Fatal("从备份恢复重新启用了已清空的 Cookie")
	}
}
