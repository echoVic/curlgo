package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"image/png"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type mainCLIEnvelope struct {
	SchemaVersion int  `json:"schemaVersion"`
	OK            bool `json:"ok"`
	Result        *struct {
		Method     string          `json:"method"`
		URL        string          `json:"url"`
		Status     string          `json:"status"`
		Proto      string          `json:"proto"`
		StatusCode int             `json:"statusCode"`
		Headers    http.Header     `json:"headers"`
		Body       json.RawMessage `json:"body"`
		Timing     json.RawMessage `json:"timing"`
	} `json:"result"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Help       json.RawMessage `json:"help"`
	Validation json.RawMessage `json:"validation"`
}

func TestMainCLIAcceptance(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	dir := t.TempDir()
	binary := filepath.Join(dir, "curlgo")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	build.WaitDelay = time.Second
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build shared CLI/render binary: %v\n%s", err, output)
	}
	home := filepath.Join(dir, "home")
	config := filepath.Join(home, "config")
	if runtime.GOOS == "darwin" {
		config = filepath.Join(home, "Library", "Application Support")
	}
	workspace := filepath.Join(config, "QingzhouAPI")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"workspace.json":     `{"version":1,"draft":{"url":"https://gui.invalid"},"requests":[],"history":[],"settings":{},"cookies":[]}`,
		"workspace.json.bak": `{"version":1,"gui_backup":"untouched"}`,
	} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	tmp := filepath.Join(dir, "tmp")
	if err := os.Mkdir(tmp, 0700); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+config, "TMPDIR="+tmp)
	before := mainCLIConfigSnapshot(t, config)
	t.Cleanup(func() {
		if after := mainCLIConfigSnapshot(t, config); !reflect.DeepEqual(before, after) {
			t.Error("CLI/render process mutated the isolated GUI workspace/config")
		}
	})
	newCommand := func(child context.Context, args ...string) *exec.Cmd {
		command := exec.CommandContext(child, binary, args...)
		command.Env = env
		command.Dir = dir
		command.WaitDelay = time.Second
		return command
	}

	t.Run("machineReadableHelp", func(t *testing.T) {
		missing := filepath.Join(dir, "missing_private_marker")
		for _, args := range [][]string{
			{"help"}, {"help", "run"}, {"-h"}, {"--help"},
			{"run", "-h"}, {"run", "--help"},
			{"run", "--dry-run", "--request", missing, "--out-dir=", "--help"},
			{"run", "--request", "-", "--curl", missing, "--ca-file", missing,
				"--cookie-jar", missing, "--inline-body-bytes", "-1", "--help"},
		} {
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				child, stop := context.WithTimeout(ctx, 10*time.Second)
				defer stop()
				command := newCommand(child, args...)
				command.Stdin = strings.NewReader("private_marker")
				var stdout, stderr bytes.Buffer
				command.Stdout, command.Stderr = &stdout, &stderr
				if err := command.Run(); err != nil {
					t.Fatalf("help process: %v, stdout=%q, stderr=%q", err, stdout.String(), stderr.String())
				}
				value := mainCLIDecode(t, stdout.String())
				if !value.OK || value.Result != nil || value.Error != nil ||
					len(value.Help) == 0 || len(value.Validation) != 0 || stderr.Len() != 0 ||
					strings.Contains(stdout.String(), "private_marker") {
					t.Fatalf("help envelope=%+v, stderr=%q", value, stderr.String())
				}
			})
		}
	})

	t.Run("helpUsageErrors", func(t *testing.T) {
		for _, args := range [][]string{
			{"help", "private_marker"}, {"help", "run", "private_marker"},
			{"--help", "private_marker"}, {"-h", "run"},
			{"run", "--help", "--private_marker"},
			{"run", "--help", "--inline-body-bytes=private_marker"},
			{"run", "--help", "--dry-run=private_marker"},
		} {
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				child, stop := context.WithTimeout(ctx, 10*time.Second)
				defer stop()
				command := newCommand(child, args...)
				var stdout, stderr bytes.Buffer
				command.Stdout, command.Stderr = &stdout, &stderr
				err := command.Run()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 1 {
					t.Fatalf("usage exit=%v, stdout=%q, stderr=%q", err, stdout.String(), stderr.String())
				}
				value := mainCLIDecode(t, stdout.String())
				if value.OK || value.Result != nil || len(value.Help) != 0 || value.Error == nil ||
					value.Error.Code != "INVALID_USAGE" || value.Error.Message != "Invalid usage." ||
					stderr.Len() != 0 || strings.Contains(stdout.String(), "private_marker") {
					t.Fatalf("usage envelope=%+v, stderr=%q", value, stderr.String())
				}
			})
		}
	})

	t.Run("dryRunFilesWithoutSideEffects", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte{0, 255})
		}))
		defer server.Close()
		files := t.TempDir()
		out := filepath.Join(files, "out")
		if err := os.Mkdir(out, 0500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(out, 0700) })
		jar := filepath.Join(files, "new.cookies")
		for _, flag := range []string{"--request", "--curl"} {
			t.Run(flag, func(t *testing.T) {
				text := `{"schemaVersion":1,"request":{"url":"` + server.URL + `/private_marker","auth":{"mode":"bearer","token":"private_marker"}}}`
				if flag == "--curl" {
					text = "curl '" + server.URL + "/private_marker' -H 'Authorization: Bearer private_marker'"
				}
				input := filepath.Join(files, "private_marker.input")
				if err := os.WriteFile(input, []byte(text), 0640); err != nil {
					t.Fatal(err)
				}
				before := mainCLIConfigSnapshot(t, files)
				tmpBefore := mainCLIConfigSnapshot(t, tmp)
				child, stop := context.WithTimeout(ctx, 10*time.Second)
				defer stop()
				command := newCommand(child, "run", flag, input, "--dry-run", "--cookie-jar", jar,
					"--out-dir", out, "--inline-body-bytes=0")
				var stdout, stderr bytes.Buffer
				command.Stdout, command.Stderr = &stdout, &stderr
				if err := command.Run(); err != nil {
					t.Fatalf("dry-run process: %v, stdout=%q, stderr=%q", err, stdout.String(), stderr.String())
				}
				value := mainCLIDecode(t, stdout.String())
				var metadata map[string]any
				if err := json.Unmarshal(value.Validation, &metadata); err != nil {
					t.Fatalf("missing validation: %v, stdout=%q", err, stdout.String())
				}
				if !value.OK || value.Result != nil || value.Error != nil || len(value.Help) != 0 ||
					metadata["dryRun"] != true || metadata["method"] != "GET" ||
					metadata["headerCount"] != float64(1) || len(metadata) != 8 ||
					stderr.Len() != 0 || calls.Load() != 0 || strings.Contains(stdout.String(), "private_marker") ||
					strings.Contains(stdout.String(), server.URL) {
					t.Fatalf("dry-run envelope=%+v, stderr=%q, calls=%d", value, stderr.String(), calls.Load())
				}
				if !reflect.DeepEqual(before, mainCLIConfigSnapshot(t, files)) ||
					!reflect.DeepEqual(tmpBefore, mainCLIConfigSnapshot(t, tmp)) {
					t.Fatal("dry-run process changed files/directories")
				}
			})
		}
	})

	t.Run("requestAndCurlFiles404", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"missing":true}`)
		}))
		defer server.Close()
		for _, flag := range []string{"--request", "--curl"} {
			t.Run(flag, func(t *testing.T) {
				text := `{"schemaVersion":1,"request":{"url":"` + server.URL + `"}}`
				if flag == "--curl" {
					text = "curl '" + server.URL + "'"
				}
				input := filepath.Join(t.TempDir(), "input")
				if err := os.WriteFile(input, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
				child, stop := context.WithTimeout(ctx, 10*time.Second)
				defer stop()
				command := newCommand(child, "run", flag, input)
				var stdout, stderr bytes.Buffer
				command.Stdout, command.Stderr = &stdout, &stderr
				if err := command.Run(); err != nil {
					t.Fatalf("404 process must exit 0: %v, stderr=%q", err, stderr.String())
				}
				value := mainCLIDecode(t, stdout.String())
				if !value.OK || value.Error != nil || value.Result == nil ||
					value.Result.StatusCode != 404 || value.Result.Status != "404 Not Found" ||
					value.Result.Method != "GET" || value.Result.URL != server.URL || stderr.Len() != 0 {
					t.Fatalf("404 process response = %+v, stderr=%q", value, stderr.String())
				}
			})
		}
	})

	t.Run("actualSIGINT", func(t *testing.T) {
		ready := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			close(ready)
			<-r.Context().Done()
		}))
		defer server.Close()
		input := filepath.Join(t.TempDir(), "request.json")
		text := `{"schemaVersion":1,"request":{"url":"` + server.URL + `","timeoutSeconds":30}}`
		if err := os.WriteFile(input, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		child, stop := context.WithTimeout(ctx, 10*time.Second)
		defer stop()
		command := newCommand(child, "run", "--request", input)
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		select {
		case <-ready:
		case err := <-done:
			t.Fatalf("process exited before server readiness: %v", err)
		case <-child.Done():
			command.Process.Kill()
			<-done
			t.Fatalf("server never received request: %v", child.Err())
		}
		if err := command.Process.Signal(os.Interrupt); err != nil {
			command.Process.Kill()
			<-done
			t.Fatalf("send actual SIGINT: %v", err)
		}
		var err error
		select {
		case err = <-done:
		case <-child.Done():
			command.Process.Kill()
			<-done
			t.Fatalf("SIGINT process did not terminate: %v", child.Err())
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 2 {
			t.Fatalf("SIGINT exit = %v, stdout=%q, stderr=%q", err, stdout.String(), stderr.String())
		}
		value := mainCLIDecode(t, stdout.String())
		if value.OK || value.Result != nil || value.Error == nil || value.Error.Code != "CANCELED" ||
			value.Error.Message == "" || stderr.Len() != 0 {
			t.Fatalf("SIGINT response = %+v, stderr=%q", value, stderr.String())
		}
	})

	t.Run("lightAndDarkRender", func(t *testing.T) {
		var hashes [][32]byte
		for _, dark := range []bool{false, true} {
			path := filepath.Join(t.TempDir(), "preview.png")
			args := []string{"-render", path}
			if dark {
				args = append(args, "-dark")
			}
			child, stop := context.WithTimeout(ctx, 15*time.Second)
			command := newCommand(child, args...)
			output, err := command.CombinedOutput()
			stop()
			if err != nil {
				t.Fatalf("dark=%v render: %v\n%s", dark, err, output)
			}
			data, err := os.ReadFile(path)
			if err != nil || len(data) == 0 {
				t.Fatalf("dark=%v empty/missing PNG: %v", dark, err)
			}
			image, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("dark=%v invalid PNG: %v", dark, err)
			}
			bounds := image.Bounds()
			if bounds.Dx() != 1200 || bounds.Dy() != 820 {
				t.Fatalf("dark=%v PNG dimensions = %v", dark, bounds)
			}
			background := image.At(bounds.Min.X, bounds.Min.Y)
			nonempty := false
			for y := bounds.Min.Y; y < bounds.Max.Y && !nonempty; y += 10 {
				for x := bounds.Min.X; x < bounds.Max.X; x += 10 {
					r, g, b, a := image.At(x, y).RGBA()
					br, bg, bb, ba := background.RGBA()
					if r != br || g != bg || b != bb || a != ba {
						nonempty = true
						break
					}
				}
			}
			if !nonempty {
				t.Fatalf("dark=%v render is a blank surface", dark)
			}
			hashes = append(hashes, sha256.Sum256(data))
		}
		if hashes[0] == hashes[1] {
			t.Fatal("-dark did not change the rendered UI")
		}
	})
}

func mainCLIDecode(t *testing.T, raw string) mainCLIEnvelope {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value mainCLIEnvelope
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("process stdout is not valid JSON: %v, %q", err, raw)
	}
	if err := decoder.Decode(new(any)); err != io.EOF || value.SchemaVersion != 1 {
		t.Fatalf("process stdout must be one schema-1 JSON document: %v, %q", err, raw)
	}
	return value
}

func mainCLIConfigSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string)
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		value := info.Mode().String() + " " + info.ModTime().String()
		if !entry.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			hash := sha256.Sum256(data)
			value += " " + string(hash[:])
		}
		snapshot[relative] = value
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot isolated GUI configuration: %v", err)
	}
	return snapshot
}
