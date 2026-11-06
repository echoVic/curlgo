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
	"strings"
	"sync/atomic"
	"testing"
)

func invokeCLI(t *testing.T, ctx context.Context, text string, extraArgs ...string) (ExitCode, envelope, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	args := append([]string{"--request", "-"}, extraArgs...)
	exit := Run(ctx, strings.NewReader(text), &stdout, &stderr, args)
	raw := stdout.String()
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value envelope
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("invalid CLI JSON: %v; stdout = %q", err, raw)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("stdout is not exactly one JSON document: %v; %q", err, raw)
	}
	if value.SchemaVersion != 1 || stderr.Len() != 0 {
		t.Fatalf("schema = %d, stderr = %q", value.SchemaVersion, stderr.String())
	}
	return exit, value, raw
}

func TestCLIIntegrationCookieJarAfterReadFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "authenticated", Path: "/", HttpOnly: true})
			w.Header().Set("Content-Length", "100")
			w.Header().Set("Content-Type", "text/plain")
			io.WriteString(w, "partial login")
			return
		}
		cookie, err := r.Cookie("session")
		if err != nil || cookie.Value != "authenticated" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		io.WriteString(w, "authenticated")
	}))
	defer server.Close()
	jar := filepath.Join(t.TempDir(), "cookies.json")
	exit, value, _ := invokeCLI(t, t.Context(), cliIntegrationInput(t, server.URL+"/login", nil), "--cookie-jar", jar)
	cliIntegrationFailure(t, exit, value, ExitExecution, "BODY_READ_ERROR")
	if value.Result == nil || value.Result.StatusCode != 200 || value.Result.Body == nil ||
		value.Result.Body.Text == nil || *value.Result.Body.Text != "partial login" || !measuredMilliseconds(value.Result.Timing.TTFBMs) {
		t.Fatalf("partial login response = %+v", value.Result)
	}
	info, err := os.Stat(jar)
	if err != nil || posixPermissions() && info.Mode().Perm() != 0600 {
		t.Fatalf("cookie jar mode = %v, %v", info, err)
	}
	auth := cliIntegrationInput(t, server.URL+"/auth", nil)
	exit, value, _ = invokeCLI(t, t.Context(), auth, "--cookie-jar", jar)
	if exit != ExitOK || !value.OK || value.Result == nil || value.Result.StatusCode != 200 ||
		value.Result.Body.Text == nil || *value.Result.Body.Text != "authenticated" {
		t.Fatalf("restored login = %d, %+v", exit, value)
	}
	exit, value, _ = invokeCLI(t, t.Context(), auth)
	if exit != ExitOK || !value.OK || value.Result == nil || value.Result.StatusCode != 401 {
		t.Fatalf("default invocation inherited cookies: %d, %+v", exit, value)
	}
}

func TestCLIIntegrationTLSConfiguration(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		io.WriteString(w, "trusted")
	}))
	defer server.Close()
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, []byte(ca), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, jsonCA string
		file         bool
		exit         ExitCode
		code         string
	}{
		{"untrusted", "", false, ExitExecution, "TLS_ERROR"},
		{"fileCA", "", true, ExitOK, ""},
		{"matchingJSONCA", " \n" + ca + "\t", true, ExitOK, ""},
		{"conflictingJSONCA", ca + ca, true, ExitInput, "INVALID_INPUT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := calls.Load()
			var args []string
			if tc.file {
				args = []string{"--ca-file", path}
			}
			exit, value, _ := invokeCLI(t, t.Context(),
				cliIntegrationInput(t, server.URL, map[string]any{"caCertPEM": tc.jsonCA}), args...)
			if tc.exit != ExitOK {
				cliIntegrationFailure(t, exit, value, tc.exit, tc.code)
				if value.Result != nil || calls.Load() != before {
					t.Fatal("failed TLS configuration yielded an application response")
				}
			} else if exit != ExitOK || !value.OK || value.Result == nil || value.Result.StatusCode != 200 ||
				value.Result.Body.Text == nil || *value.Result.Body.Text != "trusted" ||
				!measuredMilliseconds(value.Result.Timing.TLSMs) || calls.Load() != before+1 {
				t.Fatalf("trusted CLI result = %d, %+v", exit, value)
			}
		})
	}
}

func TestCLIIntegrationProxyConfiguration(t *testing.T) {
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !r.URL.IsAbs() || r.URL.Host != "api.test" {
			t.Errorf("proxy request = %s", r.URL)
		}
		io.WriteString(w, "proxied")
	}))
	defer proxy.Close()
	bare := strings.TrimPrefix(proxy.URL, "http://")
	input := cliIntegrationInput(t, "http://api.test", map[string]any{
		"proxy": map[string]any{"url": bare, "bypass": "unused.test"},
	})
	exit, value, _ := invokeCLI(t, t.Context(), input, "--proxy", " "+proxy.URL+"/ ")
	if exit != ExitOK || !value.OK || value.Result == nil || value.Result.StatusCode != 200 ||
		value.Result.Body.Text == nil || *value.Result.Body.Text != "proxied" || calls.Load() != 1 {
		t.Fatalf("normalized proxy result = %d, %+v", exit, value)
	}
	exit, value, _ = invokeCLI(t, t.Context(), input, "--proxy", "http://127.0.0.1:1")
	cliIntegrationFailure(t, exit, value, ExitInput, "INVALID_INPUT")
	if calls.Load() != 1 {
		t.Fatal("conflicting proxy settings reached transport")
	}
	t.Run("preservedBypass", func(t *testing.T) {
		// An address assigned to this machine exercises bypass without loopback's automatic bypass.
		addresses, err := net.InterfaceAddrs()
		if err != nil {
			t.Fatal(err)
		}
		var localIP net.IP
		for _, address := range addresses {
			if network, ok := address.(*net.IPNet); ok && network.IP.To4() != nil &&
				!network.IP.IsLoopback() && network.IP.IsGlobalUnicast() {
				localIP = network.IP
				break
			}
		}
		if localIP == nil {
			t.Skip("no non-loopback local IPv4 interface")
		}
		listener, err := net.Listen("tcp4", net.JoinHostPort(localIP.String(), "0"))
		if err != nil {
			t.Fatal(err)
		}
		target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			io.WriteString(w, "bypassed")
		}))
		target.Listener.Close()
		target.Listener = listener
		target.Start()
		defer target.Close()
		before := calls.Load()
		input := cliIntegrationInput(t, target.URL, map[string]any{
			"proxy": map[string]any{"url": bare, "bypass": localIP.String()},
		})
		exit, value, _ := invokeCLI(t, t.Context(), input, "--proxy", proxy.URL)
		if exit != ExitOK || !value.OK || value.Result == nil || value.Result.Body.Text == nil ||
			*value.Result.Body.Text != "bypassed" || calls.Load() != before {
			t.Fatalf("JSON bypass not preserved: %d, %+v", exit, value)
		}
	})
}

func TestCLIIntegrationInvalidFilesUnchanged(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		io.WriteString(w, "unexpected")
	}))
	defer server.Close()
	input := cliIntegrationInput(t, server.URL, nil)
	for _, tc := range []struct {
		name, flag string
		data       []byte
	}{
		{"oversizeCA", "--ca-file", bytes.Repeat([]byte("x"), (1<<20)+1)},
		{"corruptJar", "--cookie-jar", []byte(`{"version":1,"cookies": [private_cookie_secret`)},
		{"version2Jar", "--cookie-jar", []byte(`{"version":2,"cookies":[]}`)},
		{"oversizeJar", "--cookie-jar", bytes.Repeat([]byte("x"), (2<<20)+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "input")
			if err := os.WriteFile(path, tc.data, 0640); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			exit, value, raw := invokeCLI(t, t.Context(), input, tc.flag, path)
			cliIntegrationFailure(t, exit, value, ExitInput, "INVALID_INPUT")
			data, readErr := os.ReadFile(path)
			after, statErr := os.Stat(path)
			entries, dirErr := os.ReadDir(dir)
			if readErr != nil || statErr != nil || dirErr != nil || !bytes.Equal(data, tc.data) ||
				after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) ||
				len(entries) != 1 || calls.Load() != 0 || strings.Contains(raw, "private_cookie_secret") {
				t.Fatalf("rejected input changed or reached transport: read=%v, stat=%v, dir=%v", readErr, statErr, dirErr)
			}
		})
	}
}

func TestCLIIntegrationRequestFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "from file")
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(path, []byte(cliIntegrationInput(t, server.URL, nil)), 0600); err != nil {
		t.Fatal(err)
	}
	exit, value, _ := invokeCLI(t, t.Context(), "not read", "--request", path)
	if exit != ExitOK || !value.OK || value.Result == nil || value.Result.StatusCode != 200 ||
		value.Result.Body.Text == nil || *value.Result.Body.Text != "from file" {
		t.Fatalf("request file result = %d, %+v", exit, value)
	}
}

func TestCLIIntegrationOutputFailures(t *testing.T) {
	t.Run("canceledAndJarParentMissing", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			cancel()
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}))
		defer server.Close()
		defer close(release)
		defer cancel()
		input := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":%q,"method":"POST",
			"headers":[{"key":"Authorization","value":"private_header_secret"}],"body":"private_body_secret"}}`,
			server.URL+"/?token=private_url_secret")
		jar := filepath.Join(t.TempDir(), "missing", "jar.json")
		exit, value, raw := invokeCLI(t, ctx, input, "--cookie-jar", jar)
		cliIntegrationFailure(t, exit, value, ExitOutput, "OUTPUT_ERROR")
		if value.Result != nil || !strings.Contains(value.Error.Message, "CANCELED") {
			t.Fatalf("primary cancellation lost: %+v", value)
		}
		cliIntegrationNoSecrets(t, raw)
		if _, err := os.Stat(filepath.Dir(jar)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("missing jar parent was created: %v", err)
		}
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Response", "metadata")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte{0, 1, 0xff})
	}))
	defer server.Close()
	input := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":%q,"method":"POST",
		"headers":[{"key":"Authorization","value":"private_header_secret"}],"body":"private_body_secret"}}`, server.URL)
	t.Run("bodyDirectoryIsFile", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(path, []byte("unchanged"), 0600); err != nil {
			t.Fatal(err)
		}
		exit, value, raw := invokeCLI(t, t.Context(), input, "--out-dir", path)
		cliIntegrationFailure(t, exit, value, ExitOutput, "OUTPUT_ERROR")
		if value.Result == nil || value.Result.StatusCode != 202 || value.Result.Body != nil ||
			value.Result.Method != "POST" || value.Result.URL != server.URL ||
			value.Result.Proto != "HTTP/1.1" || value.Result.Status != "202 Accepted" ||
			value.Result.Headers.Get("X-Response") != "metadata" || !measuredMilliseconds(value.Result.Timing.TTFBMs) {
			t.Fatalf("output failure lost partial metadata: %+v", value.Result)
		}
		cliIntegrationNoSecrets(t, raw)
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "unchanged" {
			t.Fatalf("output directory file changed: %q, %v", data, err)
		}
	})
	t.Run("stdoutWriter", func(t *testing.T) {
		dir := t.TempDir()
		writer := &outputTestFailingWriter{err: errors.New("private_writer_secret")}
		var stderr bytes.Buffer
		exit := Run(t.Context(), strings.NewReader(input), writer, &stderr,
			[]string{"--request", "-", "--out-dir", dir})
		if exit != ExitOutput || writer.calls != 1 || writer.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("stdout failure = %d, writes=%d, stderr=%q", exit, writer.calls, stderr.String())
		}
		cliIntegrationNoSecrets(t, stderr.String())
		if strings.Contains(stderr.String(), "private_writer_secret") {
			t.Fatal("raw stdout error was echoed")
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("body was not captured in controlled directory: %v, %v", entries, err)
		}
	})
	t.Run("unwritableDirectory", func(t *testing.T) {
		if !posixPermissions() {
			t.Skip("chmod cannot make a directory unwritable on Windows")
		}
		if os.Geteuid() == 0 {
			t.Skip("root bypasses directory permissions")
		}
		dir := t.TempDir()
		if err := os.Chmod(dir, 0500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
		exit, value, _ := invokeCLI(t, t.Context(), input, "--out-dir", dir)
		cliIntegrationFailure(t, exit, value, ExitOutput, "OUTPUT_ERROR")
		entries, err := os.ReadDir(dir)
		info, statErr := os.Stat(dir)
		if err != nil || statErr != nil || len(entries) != 0 || info.Mode().Perm() != 0500 {
			t.Fatalf("failed output changed directory: %v, %v", err, statErr)
		}
	})
}

func TestCLIIntegrationBinaryCapture(t *testing.T) {
	payload := bytes.Repeat([]byte{0xff}, (4<<20)+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(payload)
	}))
	defer server.Close()
	input := cliIntegrationInput(t, server.URL, nil)
	dir := t.TempDir()
	old := filepath.Join(dir, "response-existing.body")
	if err := os.WriteFile(old, []byte("old body"), 0640); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for range 2 {
		exit, value, raw := invokeCLI(t, t.Context(), input, "--out-dir", dir)
		cliIntegrationCapturedBody(t, exit, value, raw, dir, payload[:4<<20])
		paths = append(paths, value.Result.Body.Path)
	}
	data, err := os.ReadFile(old)
	info, statErr := os.Stat(old)
	if err != nil || statErr != nil || string(data) != "old body" || posixPermissions() && info.Mode().Perm() != 0640 ||
		paths[0] == paths[1] || paths[0] == old || paths[1] == old {
		t.Fatalf("body paths collided or old body changed: %v, %v, %v", paths, err, statErr)
	}
	t.Run("defaultDirectoryRetained", func(t *testing.T) {
		parent := t.TempDir()
		setTempDir(t, parent)
		exit, value, raw := invokeCLI(t, t.Context(), input)
		if value.Result == nil || value.Result.Body == nil {
			t.Fatalf("missing captured result = %+v", value)
		}
		dir := filepath.Dir(value.Result.Body.Path)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() || posixPermissions() && info.Mode().Perm() != 0700 || filepath.Dir(dir) != parent {
			t.Fatalf("default body directory not private/retained: %v, %v", info, err)
		}
		cliIntegrationCapturedBody(t, exit, value, raw, dir, payload[:4<<20])
	})
}

func cliIntegrationCapturedBody(t *testing.T, exit ExitCode, value envelope, raw, dir string, want []byte) {
	t.Helper()
	if exit != ExitOK || !value.OK || value.Error != nil || value.Result == nil ||
		value.Result.StatusCode != 200 || value.Result.Body == nil {
		t.Fatalf("capture result = %d, %+v", exit, value)
	}
	body := value.Result.Body
	if body.Mode != "file" || body.Encoding != "binary" || body.Text != nil ||
		body.Bytes != 4<<20 || !body.Truncated || len(raw) > 16<<10 {
		t.Fatalf("capture metadata/stdout size = %+v, %d", body, len(raw))
	}
	outputTestFile(t, body.Path, dir, want)
}

func cliIntegrationInput(t *testing.T, address string, settings map[string]any) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "request": map[string]any{"url": address}, "settings": settings,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func cliIntegrationFailure(t *testing.T, exit ExitCode, value envelope, want ExitCode, code string) {
	t.Helper()
	if exit != want || value.OK || value.Error == nil || value.Error.Code != code || value.Error.Message == "" {
		t.Fatalf("failure = %d, %+v, want exit %d / %s", exit, value, want, code)
	}
}

func cliIntegrationNoSecrets(t *testing.T, raw string) {
	t.Helper()
	for _, secret := range []string{"private_url_secret", "private_header_secret", "private_body_secret"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("output contains request secret %q", secret)
		}
	}
}
