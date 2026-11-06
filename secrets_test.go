package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/echoVic/curlgo/internal/core"
	"github.com/echoVic/curlgo/internal/vault"
	"github.com/egoist/mygo/ui"
)

type appSecretKeys struct {
	mu      sync.Mutex
	values  map[string][]byte
	entered chan struct{}
	release chan struct{}
}

func (s *appSecretKeys) Create(_ context.Context, id string, key []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = map[string][]byte{}
	}
	s.values[id] = append([]byte(nil), key...)
	return nil
}
func (s *appSecretKeys) Load(_ context.Context, id string) ([]byte, error) {
	if s.entered != nil {
		close(s.entered)
		<-s.release
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.values[id]...), nil
}
func (s *appSecretKeys) Remove(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, id)
	return nil
}

func appVault(t *testing.T, a *application) (string, *appSecretKeys) {
	t.Helper()
	if runtime.GOOS == "windows" {
		// The desktop vault requires POSIX private-directory permissions.
		t.Skip("desktop vault requires POSIX file modes")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "secret-vault.json")
	keys := &appSecretKeys{}
	v, err := vault.Open(path, keys)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Unlock(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.secrets = v
	t.Cleanup(a.lockSecrets)
	return path, keys
}

func TestPrivateExtractionWhileLockedLeavesOrdinaryVariablesUnchanged(t *testing.T) {
	a, _ := testApp(t)
	a.capturedResponse.body = []byte(`{"token":"private-value","id":"new-id"}`)
	rules := []core.ResponseExtractor{
		{Variable: "secret.token", Source: core.ExtractJSON, Selector: "/token"},
		{Variable: "id", Source: core.ExtractJSON, Selector: "/id"},
	}
	before := appSnapshot(t, a.workspace)
	a.applyExtractedVariables(rules, a.envName)
	if before != appSnapshot(t, a.workspace) || a.notice == "" || strings.Contains(a.notice, "private-value") {
		t.Fatal("locked private extraction leaked or partially changed environment")
	}
}

func TestSettingsRejectReservedSecretNamespace(t *testing.T) {
	a, _ := testApp(t)
	a.variables = `{"secret.token":"private-value"}`
	before := appSnapshot(t, a.workspace.Settings)
	a.applySettings()
	if before != appSnapshot(t, a.workspace.Settings) || a.notice == "" {
		t.Fatal("ordinary environment accepted managed secret name")
	}
}

func TestLockedSecretBlocksNetworkAndHistory(t *testing.T) {
	requests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests <- struct{}{} }))
	defer server.Close()
	a, _ := testApp(t)
	r := appDraft(server.URL)
	r.Auth = core.Authentication{Mode: core.AuthBearer, Token: "{{secret.token}}"}
	a.selectRequest(r)
	a.workspace.Settings.Environments[a.envName]["secret.token"] = "ordinary-fallback"
	a.send()
	if a.busy || len(a.workspace.History) != 0 || a.notice == "" {
		t.Fatal("locked request entered transport")
	}
	select {
	case <-requests:
		t.Fatal("locked request reached network")
	default:
	}
}

func TestPrivateVariablesSendAndPersistOnlyTemplates(t *testing.T) {
	seen := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("Authorization") + "|" + r.URL.Query().Get("token")
		fmt.Fprint(w, "authenticated")
	}))
	defer server.Close()
	a, q := testApp(t)
	path, keys := appVault(t, a)
	if err := a.secrets.Set(a.envName, "token", "private-credential"); err != nil {
		t.Fatal(err)
	}
	r := appDraft(server.URL + "?token={{secret.token}}")
	r.Auth = core.Authentication{Mode: core.AuthBearer, Token: "{{secret.token}}"}
	a.selectRequest(r)
	a.saveRequest()
	command, err := core.ExportCURL(a.workspace.Draft, a.workspace.Settings)
	if err != nil || strings.Contains(command, "private-credential") || !strings.Contains(command, "{{secret.token}}") {
		t.Fatalf("curl export exposed credential: %s %v", command, err)
	}
	a.send()
	pumpApp(t, q, func() bool { return !a.busy })
	if <-seen != "Bearer private-credential|private-credential" || a.notice != "" {
		t.Fatalf("private send failed: %s", a.notice)
	}
	if strings.Contains(appSnapshot(t, cloneWorkspace(a.workspace)), "private-credential") {
		t.Fatal("credential entered a workspace snapshot")
	}
	store, err := core.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := store.Save(cloneWorkspace(a.workspace)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"workspace.json", "workspace.json.bak"} {
		data, err := os.ReadFile(filepath.Join(store.Dir, name))
		if err != nil || strings.Contains(string(data), "private-credential") {
			t.Fatalf("credential persisted in %s: %v", name, err)
		}
	}
	a.lockSecrets()
	restarted, err := vault.Open(path, keys)
	if err != nil || restarted.Unlocked() {
		t.Fatalf("restarted vault did not start locked: %v", err)
	}
	if err := restarted.Unlock(context.Background()); err != nil || restarted.Values(a.envName)["token"] != "private-credential" {
		t.Fatalf("encrypted restart lost value: %v", err)
	}
	restarted.Lock()
	a.selectRequest(appDraft(server.URL))
	a.send()
	pumpApp(t, q, func() bool { return !a.busy })
	if <-seen != "|" || a.notice != "" {
		t.Fatal("ordinary request stopped working while locked")
	}
}

func TestPrivateMixedExtractionCommitsToSeparateStores(t *testing.T) {
	a, _ := testApp(t)
	path, _ := appVault(t, a)
	a.capturedResponse.body = []byte(`{"token":"private-response","id":"new-id"}`)
	rules := []core.ResponseExtractor{
		{Variable: "secret.token", Source: core.ExtractJSON, Selector: "/token"},
		{Variable: "id", Source: core.ExtractJSON, Selector: "/id"},
	}
	a.applyExtractedVariables(rules, a.envName)
	if a.secrets.Values(a.envName)["token"] != "private-response" ||
		a.workspace.Settings.Environments[a.envName]["id"] != "new-id" ||
		strings.Contains(appSnapshot(t, a.workspace), "private-response") || strings.Contains(a.variables+a.extractionNotice, "private-response") {
		t.Fatal("mixed extraction stored private values in ordinary workspace")
	}
	a.capturedResponse.body = []byte(`{"token":"new-private","id":"failed-id"}`)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	before := appSnapshot(t, a.workspace)
	a.applyExtractedVariables(rules, a.envName)
	if before != appSnapshot(t, a.workspace) || a.secrets.Values(a.envName)["token"] != "private-response" || a.notice == "" {
		t.Fatal("failed vault write partially applied extraction")
	}
}

func TestSecretViewMasksValuesAndClearsEditor(t *testing.T) {
	for _, theme := range []string{"浅色", "深色"} {
		t.Run(theme, func(t *testing.T) {
			a, q := testApp(t)
			appVault(t, a)
			a.workspace.Settings.Theme = theme
			a.lockSecrets()
			tt := ui.NewTester(a.view, 940, 680)
			clickUI(t, tt, "机密变量")
			clickUI(t, tt, "解锁")
			pumpApp(t, q, func() bool { return !a.secretBusy })
			tt.Frame()
			inputUI(t, tt, "机密变量名称", "token")
			inputUI(t, tt, "机密变量值", "private-input")
			for _, label := range []string{"机密变量名称", "机密变量值", "显示编辑值", "保存机密变量", "锁定机密变量"} {
				rect, ok := tt.Find(label)
				if !ok || rect.W < 30 || rect.X+rect.W > 940 || rect.Y+rect.H > 680 {
					t.Fatalf("secret control outside minimum window %s: %+v", label, rect)
				}
			}
			if strings.Contains(strings.Join(tt.Texts(), " "), "private-input") {
				t.Fatal("masked editor exposed private text")
			}
			clickUI(t, tt, "保存机密变量")
			if a.secretValue != "" || a.secretName != "" || a.secrets.Values(a.workspace.Settings.Environment)["token"] != "private-input" {
				t.Fatalf("secret save failed or kept edit buffer: %s", a.secretNotice)
			}
			clickUI(t, tt, "编辑机密 token")
			clickUI(t, tt, "显示编辑值")
			clickUI(t, tt, "返回请求")
			if a.secretValue != "" || a.showSecret {
				t.Fatal("leaving private page retained edit value")
			}
			clickUI(t, tt, "机密变量")
			clickUI(t, tt, "编辑机密 token")
			clickUI(t, tt, "锁定机密变量")
			if a.secrets.Unlocked() || a.secretValue != "" || tt.HasText("编辑机密 token") {
				t.Fatal("lock retained private inventory or edit buffer")
			}
			clickUI(t, tt, "解锁")
			pumpApp(t, q, func() bool { return !a.secretBusy })
			tt.Frame()
			clickUI(t, tt, "删除机密 token")
			if a.secrets.Values(a.workspace.Settings.Environment)["token"] != "" {
				t.Fatal("delete left private value")
			}
		})
	}
}

func TestCancelledUnlockCannotInstallKeyAfterLeavingPage(t *testing.T) {
	a, q := testApp(t)
	_, keys := appVault(t, a)
	a.lockSecrets()
	keys.entered, keys.release = make(chan struct{}), make(chan struct{})
	a.page = 6
	a.unlockSecrets()
	<-keys.entered
	a.leaveSecretPage()
	a.page = 0
	close(keys.release)
	nextAppUpdate(t, q)()
	if a.secrets.Unlocked() || a.secretBusy {
		t.Fatal("stale unlock installed a private key after cancellation")
	}
}

func TestClearedSecretCannotBeRestoredByUndo(t *testing.T) {
	a, _ := testApp(t)
	appVault(t, a)
	if err := a.secrets.Set(a.workspace.Settings.Environment, "token", "private-original"); err != nil {
		t.Fatal(err)
	}
	a.page = 6
	tt := ui.NewTester(a.view, 940, 680)
	clickUI(t, tt, "编辑机密 token")
	inputUI(t, tt, "机密变量值", "private-edit")
	clickUI(t, tt, "取消编辑")
	clickUI(t, tt, "机密变量值")
	tt.Key(ui.Cmd, ui.KeyZ)
	if a.secretValue != "" {
		t.Fatal("undo restored a cleared secret")
	}
}

func TestSecretEditorRejectsMultilinePasteWithoutChangingValue(t *testing.T) {
	a, _ := testApp(t)
	appVault(t, a)
	a.page, a.secretValue = 6, "private-original"
	tt := ui.NewTester(a.view, 940, 680)
	clickUI(t, tt, "机密变量值")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.SetClipboard("line1\nline2")
	tt.Command("paste")
	if a.secretValue != "private-original" || a.secretNotice == "" {
		t.Fatal("multiline paste silently changed the secret")
	}
	clickUI(t, tt, "机密变量值")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.SetClipboard("single-line")
	tt.Command("paste")
	if a.secretValue != "single-line" {
		t.Fatal("single-line paste was rejected")
	}
}
