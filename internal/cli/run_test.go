package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/echoVic/curlgo/internal/core"
)

func TestRunJSONAndCurl(t *testing.T) {
	for _, status := range []int{200, 404, 500} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"status":"ok"}`)
			}))
			defer server.Close()
			for _, asCurl := range []bool{false, true} {
				text := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":%q}}`, server.URL)
				args := []string{"--request", "-"}
				if asCurl {
					text, args = "curl '"+server.URL+"'", []string{"--curl", "-"}
				}
				exit, value, _ := runTestInvoke(t, strings.NewReader(text), args...)
				if exit != ExitOK || !value.OK || value.Error != nil || value.Result == nil {
					t.Fatalf("curl=%v: exit=%d, envelope=%+v", asCurl, exit, value)
				}
				result := value.Result
				if result.StatusCode != status || result.Status != fmt.Sprintf("%d %s", status, http.StatusText(status)) ||
					result.Method != "GET" || result.URL != server.URL || result.Body == nil ||
					result.Body.Mode != "inline" || result.Body.Text == nil || *result.Body.Text != `{"status":"ok"}` {
					t.Fatalf("curl=%v: result=%+v", asCurl, result)
				}
			}
		})
	}
}

func TestRunRejectsUsage(t *testing.T) {
	for i, args := range [][]string{
		{},
		{"--request"},
		{"--request", ""},
		{"--request", "-", "--curl", "-"},
		{"--request", "", "--curl", "-"},
		{"--request", "-", "--curl", ""},
		{"--private_marker"},
		{"--request", "-", "--inline-body-bytes", "-1"},
		{"--request", "-", "--inline-body-bytes", strconv.Itoa(core.MaxBody + 1)},
		{"--request", "-", "--inline-body-bytes", "private_marker"},
		{"--request", "-", "private_marker"},
		{"run", "--request", "-"},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			reader := &inputTestReader{Reader: strings.NewReader("private_marker")}
			exit, value, raw := runTestInvoke(t, reader, args...)
			if exit != ExitInput || value.OK || value.Result != nil || value.Error == nil ||
				value.Error.Code != "INVALID_USAGE" || value.Error.Message == "" ||
				strings.Contains(raw, "private_marker") || reader.read != 0 {
				t.Fatalf("exit=%d, envelope=%+v, read=%d", exit, value, reader.read)
			}
		})
	}
}

func TestRunFileInputs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()
	for _, flag := range []string{"--request", "--curl"} {
		t.Run(flag, func(t *testing.T) {
			text := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":%q}}`, server.URL)
			if flag == "--curl" {
				text = "curl '" + server.URL + "'"
			}
			path := filepath.Join(t.TempDir(), "input")
			if err := os.WriteFile(path, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			reader := &inputTestReader{Reader: strings.NewReader("unused")}
			exit, value, _ := runTestInvoke(t, reader, flag, path)
			if exit != ExitOK || !value.OK || value.Result == nil || value.Result.StatusCode != 200 || reader.read != 0 {
				t.Fatalf("exit=%d, envelope=%+v, stdin read=%d", exit, value, reader.read)
			}
		})
	}
}

func TestRunInputFailures(t *testing.T) {
	dir := t.TempDir()
	oversized := filepath.Join(dir, "private_marker")
	if err := os.WriteFile(oversized, bytes.Repeat([]byte("x"), core.MaxInput+1), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, flag, path, code string
		stdin                  io.Reader
	}{
		{"missing_file", "--request", filepath.Join(dir, "missing_private_marker"), "INVALID_INPUT", nil},
		{"directory", "--curl", dir, "INVALID_INPUT", nil},
		{"oversized_file", "--request", oversized, "INVALID_INPUT", nil},
		{"oversized_stdin", "--curl", "-", "INVALID_INPUT", strings.NewReader(strings.Repeat("x", core.MaxInput+2))},
		{"reader_error", "--request", "-", "INVALID_INPUT", &inputTestReader{Reader: strings.NewReader("{}"), err: errors.New("private_marker")}},
		{"decode_json", "--request", "-", "INVALID_INPUT", strings.NewReader(`{"private_marker":true}`)},
		{"decode_curl", "--curl", "-", "INVALID_CURL", strings.NewReader("private_marker")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exit, value, raw := runTestInvoke(t, tc.stdin, tc.flag, tc.path)
			if exit != ExitInput || value.OK || value.Result != nil || value.Error == nil ||
				value.Error.Code != tc.code || value.Error.Message == "" || strings.Contains(raw, "private_marker") {
				t.Fatalf("exit=%d, envelope=%+v", exit, value)
			}
		})
	}
}

func TestReadSourceBoundsAndLiteralDash(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("-", []byte("literal file"), 0600); err != nil {
		t.Fatal(err)
	}
	if data, err := readSource("-", nil); err != nil || string(data) != "literal file" {
		t.Fatalf("literal dash file: data=%q, err=%v", data, err)
	}
	for _, size := range []int{core.MaxInput, core.MaxInput + 2} {
		reader := &inputTestReader{Reader: strings.NewReader(strings.Repeat("x", size))}
		data, err := readSource("-", reader)
		wantRead := min(size, core.MaxInput+1)
		if (err != nil) != (size > core.MaxInput) || reader.read != wantRead ||
			err == nil && len(data) != size {
			t.Fatalf("size=%d: bytes=%d, read=%d, err=%v", size, len(data), reader.read, err)
		}
	}
}

func TestConfigureEngine(t *testing.T) {
	t.Chdir(t.TempDir())
	server := httptest.NewTLSServer(nil)
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	server.Close()
	for name, content := range map[string]string{
		"ca.pem": ca, "-": ca, "empty.pem": "", "invalid.pem": "private_marker",
		"oversized.pem": strings.Repeat("x", core.MaxInput+1),
	} {
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	proxy := core.ProxySettings{URL: "proxy.test:7890", Bypass: "api.test"}
	for _, tc := range []struct {
		name     string
		settings core.Settings
		options  runOptions
		wantFail bool
	}{
		{"empty", core.Settings{}, runOptions{}, false},
		{"empty_ca_file", core.Settings{}, runOptions{caFile: "empty.pem"}, false},
		{"equal_ca", core.Settings{CACertPEM: " \n" + ca + "\t"}, runOptions{caFile: "ca.pem"}, false},
		{"conflicting_ca", core.Settings{CACertPEM: ca + ca}, runOptions{caFile: "ca.pem"}, true},
		{"literal_ca_dash", core.Settings{CACertPEM: ca}, runOptions{caFile: "-"}, false},
		{"invalid_json_ca", core.Settings{CACertPEM: "private_marker"}, runOptions{}, true},
		{"invalid_ca_file", core.Settings{}, runOptions{caFile: "invalid.pem"}, true},
		{"oversized_ca_file", core.Settings{}, runOptions{caFile: "oversized.pem"}, true},
		{"missing_ca_file", core.Settings{}, runOptions{caFile: "missing_private_marker"}, true},
		{"normalized_proxy", core.Settings{Proxy: proxy}, runOptions{proxy: " http://proxy.test:7890/ "}, false},
		{"conflicting_proxy", core.Settings{Proxy: proxy}, runOptions{proxy: "http://proxy.test:7891"}, true},
		{"invalid_json_proxy", core.Settings{Proxy: core.ProxySettings{URL: "ftp://private_marker"}}, runOptions{}, true},
		{"invalid_flag_proxy", core.Settings{}, runOptions{proxy: "ftp://private_marker"}, true},
		{"blank_flag_proxy", core.Settings{}, runOptions{proxy: " "}, true},
		{"preserve_json_bypass", core.Settings{Proxy: core.ProxySettings{URL: proxy.URL, Bypass: "api.test\nprivate_marker"}}, runOptions{proxy: "http://proxy.test:7890/"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := core.NewEngine()
			defer engine.Close()
			fail := configureEngine(engine, tc.settings, tc.options)
			if (fail != nil) != tc.wantFail {
				t.Fatalf("failure=%+v, want failure=%v", fail, tc.wantFail)
			}
			if fail != nil && (fail.Code != "INVALID_INPUT" || fail.Exit != ExitInput ||
				fail.Message == "" || strings.Contains(fail.Message, "private_marker")) {
				t.Fatalf("unsafe or unmapped configuration failure: %+v", fail)
			}
		})
	}
}

func runTestInvoke(t *testing.T, stdin io.Reader, args ...string) (ExitCode, envelope, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), stdin, &stdout, &stderr, args)
	raw := stdout.String()
	decoder := json.NewDecoder(&stdout)
	var value envelope
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("invalid result JSON: %v; stdout=%q", err, raw)
	}
	if err := decoder.Decode(&envelope{}); err != io.EOF {
		t.Fatalf("expected one JSON document, got %v", err)
	}
	if value.SchemaVersion != 1 || stderr.Len() != 0 {
		t.Fatalf("schema=%d, stderr=%q", value.SchemaVersion, stderr.String())
	}
	return exit, value, raw
}
