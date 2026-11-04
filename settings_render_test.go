package main

import (
	"bytes"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Catch missing page selection in the shipped render command and unintended
// plugin initialization (which would create updater state in the temp home).
func TestAppSettingsRenderCommand(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "curlgo")
	home := filepath.Join(dir, "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, dark := range []bool{false, true} {
		path := filepath.Join(dir, "settings.png")
		args := []string{"--render", path, "--render-page", "7"}
		if dark {
			args = append(args, "--dark")
		}
		cmd := exec.CommandContext(t.Context(), binary, args...)
		cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+home, "MYGO_FRAME_STATS=all")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("render settings: %v\n%s", err, out)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := png.Decode(bytes.NewReader(data))
		if err != nil || got.Bounds().Dx() != 1200 || got.Bounds().Dy() != 820 {
			t.Fatalf("invalid settings render: %v", err)
		}
		a, _ := testApp(t)
		a.page = 7
		if dark {
			a.workspace.Settings.Theme = "深色"
		}
		want := ui.NewTester(a.view, 1200, 820).Image()
		// The whole content pane must match page 7, not the request page.
		for y := 80; y < 760; y += 3 {
			for x := 255; x < 1170; x += 3 {
				if got.At(x, y) != want.At(x, y) {
					t.Fatalf("settings command rendered another page at %d,%d", x, y)
				}
			}
		}
	}
	for _, path := range []string{filepath.Join(home, "CurlGo", "updater.json"), filepath.Join(home, "Library", "Application Support", "CurlGo", "updater.json")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("render initialized updater: %s", path)
		}
	}
}
