package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

// Keep real plugin preference reads/writes; replace only opening its GUI and
// main-thread notification delivery, which require a running native event loop.
type settingsUpdateBackend struct {
	nativeUpdateBackend
	checkCount int
	changed    func()
	offCount   int
}

func (b *settingsUpdateBackend) Enabled() bool    { return true }
func (b *settingsUpdateBackend) CheckForUpdates() { b.checkCount++ }
func (b *settingsUpdateBackend) OnChange(fn func()) func() {
	b.changed = fn
	return func() { b.offCount++; b.changed = nil }
}

func TestAppSettingsUpdateActionsPersistNativePreferences(t *testing.T) {
	dir := t.TempDir()
	mygo.App.SetPath(mygo.PathUserData, dir)
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, "") })
	mygo.Use(native.New(updater.Options{Interval: 4 * time.Hour, Language: "zh-Hans"}))
	a, queue := testApp(t)
	b := &settingsUpdateBackend{}
	a.attachUpdater(b)
	t.Cleanup(a.detachUpdater)
	a.page = 7
	tt := ui.NewTester(a.view, 940, 680)
	if !a.updatePrefs.automaticChecks || a.updatePrefs.automaticDownloads {
		t.Fatal("first launch must check automatically but require download opt-in")
	}
	clickUI(t, tt, "检查更新")
	if b.checkCount != 1 {
		t.Fatal("manual check did not reach updater backend")
	}
	// Click the labeled switch itself, not the text beside it.
	clickUI(t, tt, "允许后台自动检查")
	clickUI(t, tt, "允许自动下载并安装")
	if updater.AutomaticDownloads() {
		t.Fatal("automatic downloads enabled while background checks were off")
	}
	clickUI(t, tt, "允许后台自动检查")
	clickUI(t, tt, "允许自动下载并安装")
	data, err := os.ReadFile(filepath.Join(dir, "updater.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Checks    *bool `json:"automaticChecks"`
		Downloads bool  `json:"automaticDownloads"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Checks == nil || !*saved.Checks || !saved.Downloads {
		t.Fatalf("UI choices not persisted by native updater: %s", data)
	}
	// External preference changes (the native update window) refresh this page.
	updater.SetAutomaticDownloads(false)
	updater.SetAutomaticChecks(false)
	b.changed()
	nextAppUpdate(t, queue)()
	tt.Frame()
	if a.updatePrefs.automaticDownloads {
		t.Fatal("external update preference not refreshed")
	}
	// A notification copied before unsubscription must not touch a closed app.
	late := b.changed
	late()
	queued := nextAppUpdate(t, queue)
	a.closed = true
	a.detachUpdater()
	a.detachUpdater()
	updater.SetAutomaticChecks(true)
	queued()
	late()
	if b.offCount != 1 || a.updatePrefs.automaticChecks || len(queue) != 0 {
		t.Fatal("updater listener was not detached safely on close")
	}
	checks := b.checkCount
	a.checkForUpdates()
	if b.checkCount != checks {
		t.Fatal("closed app opened updater")
	}
}

func TestAppSettingsUsesPackagedVersion(t *testing.T) {
	old := mygo.App.Version()
	mygo.App.SetVersion("9.8.7")
	t.Cleanup(func() { mygo.App.SetVersion(old) })
	a, _ := testApp(t)
	a.page = 7
	tt := ui.NewTester(a.view, 940, 680)
	if !tt.HasText("9.8.7") {
		t.Fatal("settings ignored packaged application version")
	}
}
