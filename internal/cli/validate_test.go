package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDryRunJSONAndCurlWithoutConnections(t *testing.T) {
	for _, tls := range []bool{false, true} {
		t.Run(fmt.Sprintf("tls=%v", tls), func(t *testing.T) {
			var connections, requests atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.WriteHeader(http.StatusNoContent)
			}))
			server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					connections.Add(1)
				}
			}
			if tls {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			for _, flag := range []string{"--request", "--curl"} {
				input := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":%q}}`, server.URL+"/private_marker")
				if flag == "--curl" {
					input = "curl '" + server.URL + "/private_marker'"
				}
				exit, _, raw := runTestInvoke(t, strings.NewReader(input), flag, "-", "--dry-run")
				got := dryRunTestValidation(t, exit, raw)
				want := map[string]any{
					"dryRun": true, "method": "GET", "protocol": "HTTP", "bodyMode": "",
					"headerCount": float64(0), "formPartCount": float64(0),
					"timeoutSeconds": float64(30), "followRedirects": flag == "--request",
				}
				if !reflect.DeepEqual(got, want) || connections.Load() != 0 || requests.Load() != 0 ||
					strings.Contains(raw, server.URL) {
					t.Fatalf("validation=%v, connections=%d, requests=%d", got, connections.Load(), requests.Load())
				}
			}
		})
	}
}

func TestDryRunProxyDoesNotConnect(t *testing.T) {
	var connections atomic.Int32
	proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	proxy.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	proxy.Start()
	defer proxy.Close()
	// A non-loopback target avoids the engine's unconditional loopback bypass.
	for _, scheme := range []string{"http", "https"} {
		input := cliIntegrationInput(t, scheme+"://private_marker.invalid", map[string]any{
			"proxy": map[string]any{"url": proxy.URL, "bypass": "unused.invalid"},
		})
		exit, _, raw := invokeCLI(t, t.Context(), input, "--dry-run", "--proxy", proxy.URL+"/")
		dryRunTestValidation(t, exit, raw)
		if connections.Load() != 0 {
			t.Fatal("dry run connected to proxy")
		}
	}
}

func TestDryRunCurlLocation(t *testing.T) {
	exit, _, raw := runTestInvoke(t, strings.NewReader("curl -L 'https://private_marker.invalid'"), "--curl", "-", "--dry-run")
	got := dryRunTestValidation(t, exit, raw)
	if got["followRedirects"] != true {
		t.Fatalf("curl location metadata=%v", got)
	}
}

func TestDryRunResolvedMetadataAndAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name, auth string
		count      float64
	}{
		{"manual", `{}`, 4},
		{"bearer", `{"mode":"bearer","token":"{{secret}}"}`, 4},
		{"basic", `{"mode":"basic","username":"private_marker","password":"{{secret}}"}`, 4},
		{"apiHeader", `{"mode":"apikey","key":"X-Key","value":"{{secret}}"}`, 5},
		{"apiQuery", `{"mode":"apikey","key":"token","value":"{{secret}}","location":"query"}`, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := fmt.Sprintf(`{"schemaVersion":1,"request":{
				"url":"{{base}}/private_marker?token={{shadowed}}","method":"PATCH",
				"auth":%s,"headers":[{"key":"x-shared","value":"{{secret}}"},{"key":"X-Repeated","value":"one"},{"key":"X-Repeated","value":"two"}],
				"body":"{{secret}}","timeoutSeconds":7,"followRedirects":false},
				"settings":{"globalHeaders":[{"key":"X-Shared","value":"{{missing}}"},{"key":"Authorization","value":"{{secret}}"}],
				"environment":"env","environments":{"env":{"base":"https://private_marker.invalid","secret":"private_marker","shadowed":"private_marker"}}}}`, tc.auth)
			exit, _, raw := invokeCLI(t, t.Context(), input, "--dry-run")
			got := dryRunTestValidation(t, exit, raw)
			if got["headerCount"] != tc.count || got["method"] != "PATCH" ||
				got["timeoutSeconds"] != float64(7) || got["followRedirects"] != false {
				t.Fatalf("resolved metadata=%v", got)
			}
		})
	}
}

func TestDryRunFilesAndDirectoriesUnchanged(t *testing.T) {
	dir := t.TempDir()
	tmp := t.TempDir()
	setTempDir(t, tmp)
	out := filepath.Join(dir, "output")
	if err := os.Mkdir(out, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(out, 0700) })
	server := httptest.NewTLSServer(nil)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	server.Close()
	input := filepath.Join(dir, "private_marker.json")
	jar := filepath.Join(dir, "private_marker.cookies")
	caFile := filepath.Join(dir, "private_marker.pem")
	for path, data := range map[string][]byte{
		input:  []byte(`{"schemaVersion":1,"request":{"url":"https://private_marker.invalid","body":"private_marker"}}`),
		jar:    []byte(`{"version":1,"cookies":[{"url":"https://private_marker.invalid","cookie":{"Name":"session","Value":"private_marker","Path":"/"}}]}`),
		caFile: ca,
	} {
		if err := os.WriteFile(path, data, 0640); err != nil {
			t.Fatal(err)
		}
	}
	before := dryRunTestSnapshot(t, dir)
	for _, args := range [][]string{
		{"--cookie-jar", jar, "--out-dir", out},
		{"--cookie-jar", filepath.Join(dir, "new.cookies")},
		nil,
	} {
		reader := &inputTestReader{Reader: strings.NewReader("must not read")}
		flags := append([]string{"--request", input, "--dry-run", "--ca-file", caFile, "--inline-body-bytes=0"}, args...)
		exit, _, raw := runTestInvoke(t, reader, flags...)
		dryRunTestValidation(t, exit, raw)
		if reader.read != 0 || !reflect.DeepEqual(before, dryRunTestSnapshot(t, dir)) {
			t.Fatal("dry run read stdin or changed inputs/output paths")
		}
		entries, err := os.ReadDir(tmp)
		if err != nil || len(entries) != 0 {
			t.Fatalf("dry run created default body directory: %v, %v", entries, err)
		}
	}
}

func TestDryRunLocalFailures(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "private_marker.file")
	jar := filepath.Join(dir, "private_marker.cookies")
	if err := os.WriteFile(file, []byte("private_marker"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jar, []byte(`{"version":1,"cookies":null}`), 0640); err != nil {
		t.Fatal(err)
	}
	valid := `{"schemaVersion":1,"request":{"url":"https://private_marker.invalid"}}`
	for _, tc := range []struct {
		name, input, code string
		args              []string
		exit              ExitCode
	}{
		{"unknownField", `{"schemaVersion":1,"request":{"url":"https://private_marker.invalid","unknown":true}}`, "INVALID_INPUT", nil, ExitInput},
		{"trailingJSON", valid + `{}`, "INVALID_INPUT", nil, ExitInput},
		{"unresolved", `{"schemaVersion":1,"request":{"url":"https://private_marker.invalid/{{missing}}"}}`, "UNRESOLVED_VARIABLE", nil, ExitInput},
		{"invalidAuth", `{"schemaVersion":1,"request":{"url":"https://private_marker.invalid","auth":{"mode":"bearer","token":"private_marker\n"}}}`, "INVALID_REQUEST", nil, ExitInput},
		{"invalidSettingsCA", `{"schemaVersion":1,"request":{"url":"https://private_marker.invalid"},"settings":{"caCertPEM":"private_marker"}}`, "INVALID_INPUT", nil, ExitInput},
		{"invalidSettingsProxy", `{"schemaVersion":1,"request":{"url":"https://private_marker.invalid"},"settings":{"proxy":{"url":"ftp://private_marker"}}}`, "INVALID_INPUT", nil, ExitInput},
		{"invalidCA", valid, "INVALID_INPUT", []string{"--ca-file", file}, ExitInput},
		{"missingCA", valid, "INVALID_INPUT", []string{"--ca-file", filepath.Join(dir, "missing.pem")}, ExitInput},
		{"invalidProxy", valid, "INVALID_INPUT", []string{"--proxy", "ftp://private_marker"}, ExitInput},
		{"invalidJar", valid, "INVALID_INPUT", []string{"--cookie-jar", jar}, ExitInput},
		{"jarDirectory", valid, "INVALID_INPUT", []string{"--cookie-jar", dir}, ExitInput},
		{"missingOutDir", valid, "OUTPUT_ERROR", []string{"--out-dir", filepath.Join(dir, "missing")}, ExitOutput},
		{"outDirIsFile", valid, "OUTPUT_ERROR", []string{"--out-dir", file}, ExitOutput},
		{"emptyOutDir", valid, "OUTPUT_ERROR", []string{"--out-dir="}, ExitOutput},
		{"missingJarParent", valid, "OUTPUT_ERROR", []string{"--cookie-jar", filepath.Join(dir, "missing", "jar")}, ExitOutput},
		{"jarParentIsFile", valid, "OUTPUT_ERROR", []string{"--cookie-jar", filepath.Join(file, "jar")}, ExitOutput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := dryRunTestSnapshot(t, dir)
			args := append([]string{"--dry-run"}, tc.args...)
			exit, value, raw := invokeCLI(t, t.Context(), tc.input, args...)
			dryRunTestFailure(t, exit, value, raw, tc.exit, tc.code)
			if !reflect.DeepEqual(before, dryRunTestSnapshot(t, dir)) {
				t.Fatal("failed preflight changed local files")
			}
		})
	}
}

// An explicitly empty --out-dir names no directory. A real run fails exactly
// as the dry run does and before the request is sent; omitting the flag still
// selects the temporary-directory fallback.
func TestRunEmptyOutDirFailsBeforeSending(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte{0, 1, 0xff})
	}))
	defer server.Close()
	tmp := t.TempDir()
	setTempDir(t, tmp)
	input := cliIntegrationInput(t, server.URL, nil)
	exit, value, dryRaw := invokeCLI(t, t.Context(), input, "--dry-run", "--out-dir=")
	dryRunTestFailure(t, exit, value, dryRaw, ExitOutput, "OUTPUT_ERROR")
	for _, args := range [][]string{{"--out-dir="}, {"--out-dir", ""}} {
		exit, value, raw := invokeCLI(t, t.Context(), input, args...)
		dryRunTestFailure(t, exit, value, raw, ExitOutput, "OUTPUT_ERROR")
		if raw != dryRaw || value.Error.Message != "Unable to write output." || hits.Load() != 0 {
			t.Fatalf("args=%q: output=%s, dry run=%s, requests=%d", args, raw, dryRaw, hits.Load())
		}
	}
	if entries, err := os.ReadDir(tmp); err != nil || len(entries) != 0 {
		t.Fatalf("empty --out-dir used the temporary-directory fallback: %v, %v", entries, err)
	}
	exit, value, raw := invokeCLI(t, t.Context(), input)
	if exit != ExitOK || !value.OK || value.Result == nil || value.Result.Body == nil || value.Result.Body.Path == "" ||
		filepath.Dir(filepath.Dir(value.Result.Body.Path)) != tmp || hits.Load() != 1 {
		t.Fatalf("omitted --out-dir = %d, %s, requests=%d", exit, raw, hits.Load())
	}
}

func TestDryRunMultipartPreflight(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "private_marker.bin")
	if err := os.WriteFile(path, []byte("private_marker"), 0640); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path string
		valid      bool
	}{
		{"regular", path, true},
		{"missing", filepath.Join(dir, "missing"), false},
		{"directory", dir, false},
		{"nonregular", os.DevNull, false},
		{"invalidFilename", filepath.Join(dir, "private_marker\n.bin"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "nonregular" && !posixPermissions() {
				t.Skip("os.DevNull is NUL on Windows, not a non-regular file fixture")
			}
			input := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":"https://private_marker.invalid",
				"method":"POST","bodyMode":"multipart","headers":[{"key":"Content-Type","value":"{{missing}}"}],
				"formParts":[{"kind":"text","name":"title","value":"{{secret}}"},{"kind":"file","name":"file","filePath":%q}]},
				"settings":{"environment":"env","environments":{"env":{"secret":"private_marker"}}}}`, tc.path)
			before := dryRunTestSnapshot(t, dir)
			exit, value, raw := invokeCLI(t, t.Context(), input, "--dry-run")
			if tc.valid {
				got := dryRunTestValidation(t, exit, raw)
				if got["bodyMode"] != "multipart" || got["formPartCount"] != float64(2) || got["headerCount"] != float64(0) {
					t.Fatalf("multipart metadata=%v", got)
				}
			} else {
				dryRunTestFailure(t, exit, value, raw, ExitInput, "REQUEST_BUILD_ERROR")
			}
			if !reflect.DeepEqual(before, dryRunTestSnapshot(t, dir)) {
				t.Fatal("multipart preflight modified file")
			}
		})
	}
	if posixPermissions() && os.Geteuid() != 0 {
		if err := os.Chmod(path, 0000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0640) })
		input := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":"https://private_marker.invalid","bodyMode":"multipart","formParts":[{"kind":"file","name":"file","filePath":%q}]}}`, path)
		exit, value, raw := invokeCLI(t, t.Context(), input, "--dry-run")
		dryRunTestFailure(t, exit, value, raw, ExitInput, "REQUEST_BUILD_ERROR")
	}
}

func TestDryRunCanceledAndDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	deadline, stop := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer stop()
	missing := filepath.Join(t.TempDir(), "missing")
	input := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":"https://private_marker.invalid","bodyMode":"multipart","formParts":[{"kind":"file","name":"file","filePath":%q}]}}`, missing)
	for _, tc := range []struct {
		name, code string
		ctx        context.Context
	}{
		{"canceled", "CANCELED", ctx},
		{"deadline", "TIMEOUT", deadline},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exit, value, raw := invokeCLI(t, tc.ctx, input, "--dry-run")
			dryRunTestFailure(t, exit, value, raw, ExitExecution, tc.code)
		})
	}
}

func TestDryRunContextExpiresDuringPreflight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private_marker.bin")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	input := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":"https://private_marker.invalid","bodyMode":"multipart","formParts":[{"kind":"file","name":"file","filePath":%q}]}}`, path)
	for _, tc := range []struct {
		err  error
		code string
	}{
		{context.Canceled, "CANCELED"},
		{context.DeadlineExceeded, "TIMEOUT"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			// Model cancellation during local preparation without a timing race.
			ctx := &dryRunTestExpiringContext{Context: t.Context(), err: tc.err}
			exit, value, raw := invokeCLI(t, ctx, input, "--dry-run")
			dryRunTestFailure(t, exit, value, raw, ExitExecution, tc.code)
		})
	}
}

type dryRunTestExpiringContext struct {
	context.Context
	err     error
	checked bool
}

func (c *dryRunTestExpiringContext) Err() error {
	if !c.checked {
		c.checked = true
		return nil
	}
	return c.err
}

func TestDryRunWriterFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		partial int
		err     error
	}{
		{"error", 0, errors.New("private_marker")},
		{"shortWrite", 12, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := &outputTestFailingWriter{partial: tc.partial, err: tc.err}
			var stderr bytes.Buffer
			input := `{"schemaVersion":1,"request":{"url":"https://private_marker.invalid"}}`
			exit := Run(t.Context(), strings.NewReader(input), writer, &stderr, []string{"--request", "-", "--dry-run"})
			if exit != ExitOutput || writer.calls != 1 || strings.Contains(stderr.String(), "private_marker") {
				t.Fatalf("exit=%d, writes=%d, stderr=%q", exit, writer.calls, stderr.String())
			}
		})
	}
}

func dryRunTestValidation(t *testing.T, exit ExitCode, raw string) map[string]any {
	t.Helper()
	var wire map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	if err := decoder.Decode(&wire); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("not one JSON document: %v", err)
	}
	outputTestKeys(t, wire, "schemaVersion", "ok", "validation")
	got, ok := wire["validation"].(map[string]any)
	if exit != ExitOK || wire["ok"] != true || wire["schemaVersion"] != float64(1) || !ok ||
		strings.Contains(raw, "private_marker") {
		t.Fatalf("exit=%d, validation output=%s", exit, raw)
	}
	outputTestKeys(t, got, "dryRun", "method", "protocol", "bodyMode", "headerCount", "formPartCount", "timeoutSeconds", "followRedirects")
	return got
}

func dryRunTestFailure(t *testing.T, exit ExitCode, value envelope, raw string, want ExitCode, code string) {
	t.Helper()
	cliIntegrationFailure(t, exit, value, want, code)
	var wire map[string]any
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		t.Fatal(err)
	}
	outputTestKeys(t, wire, "schemaVersion", "ok", "error")
	if strings.Contains(raw, "private_marker") {
		t.Fatalf("failure leaked private input: %s", raw)
	}
}

func dryRunTestSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		value := info.Mode().String() + " " + info.ModTime().String()
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			value += " " + string(data)
		}
		snapshot[path] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
