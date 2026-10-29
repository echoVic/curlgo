package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/echoVic/curlgo/internal/core"
)

func TestDiagnosticInput(t *testing.T) {
	for _, tc := range []struct {
		name, data, code, message string
		asCurl                    bool
	}{
		{"malformed", `{"private_field":"private_token"`, "INVALID_INPUT", "Invalid request JSON envelope. Use one JSON document with supported fields.", false},
		{"unknown_envelope_field", `{"schemaVersion":1,"request":{},"private_field":"private_token"}`, "INVALID_INPUT", "Invalid request JSON envelope. Use one JSON document with supported fields.", false},
		{"unknown_settings_field", `{"schemaVersion":1,"request":{},"settings":{"private_field":"private_token"}}`, "INVALID_INPUT", "Invalid request JSON envelope. Use one JSON document with supported fields.", false},
		{"trailing_document", `{"schemaVersion":1,"request":{}} {"private_field":"private_token"}`, "INVALID_INPUT", "Invalid request JSON envelope. Use one JSON document with supported fields.", false},
		{"version_type", `{"schemaVersion":"private_token","request":{}}`, "INVALID_INPUT", "Invalid request JSON envelope. Use one JSON document with supported fields.", false},
		{"version_unsupported", `{"schemaVersion":987654321,"request":{}}`, "INVALID_INPUT", "Unsupported schema version. Use schemaVersion 1.", false},
		{"version_absent", `{"request":{}}`, "INVALID_INPUT", "Unsupported schema version. Use schemaVersion 1.", false},
		{"version_null", `{"schemaVersion":null,"request":{}}`, "INVALID_INPUT", "Unsupported schema version. Use schemaVersion 1.", false},
		{"version_before_missing_request", `{"schemaVersion":2}`, "INVALID_INPUT", "Unsupported schema version. Use schemaVersion 1.", false},
		{"request_absent", `{"schemaVersion":1}`, "INVALID_INPUT", "Missing request object. Provide a non-null request object.", false},
		{"request_null", `{"schemaVersion":1,"request": null }`, "INVALID_INPUT", "Missing request object. Provide a non-null request object.", false},
		{"request_array", `{"schemaVersion":1,"request":[]}`, "INVALID_INPUT", "Invalid request JSON fields. Check field types and supported fields.", false},
		{"request_unknown_field", `{"schemaVersion":1,"request":{"private_field":"private_token"}}`, "INVALID_INPUT", "Invalid request JSON fields. Check field types and supported fields.", false},
		{"request_type", `{"schemaVersion":1,"request":{"timeoutSeconds":"private_token"}}`, "INVALID_INPUT", "Invalid request JSON fields. Check field types and supported fields.", false},
		{"header_unknown_field", `{"schemaVersion":1,"request":{"headers":[{"private_field":"private_token"}]}}`, "INVALID_INPUT", "Invalid request JSON fields. Check field types and supported fields.", false},
		{"protocol", `{"schemaVersion":1,"request":{"protocol":"private_protocol","url":"https://private_host.invalid/private_path"}}`, "INVALID_INPUT", "Unsupported request protocol. Use HTTP.", false},
		{"unresolved", `{"schemaVersion":1,"request":{"url":"https://private_host.invalid/{{private_variable}}"}}`, "UNRESOLVED_VARIABLE", "Unresolved request variable.", false},
		{"invalid_request", `{"schemaVersion":1,"request":{"url":"https://private_user:private_password@private_host.invalid/private_path?token=private_token"}}`, "INVALID_REQUEST", "Invalid request.", false},
		{"unsupported_curl", `curl https://private_host.invalid/private_path --private_option private_token`, "INVALID_CURL", "Invalid or unsupported curl input. Use supported curl options without shell execution.", true},
		{"shell_curl", `curl https://private_host.invalid --data-raw $private_variable`, "INVALID_CURL", "Invalid or unsupported curl input. Use supported curl options without shell execution.", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, fail := decodeInput([]byte(tc.data), tc.asCurl)
			if fail == nil {
				t.Fatal("invalid input accepted")
			}
			if fail.Code != tc.code || fail.Exit != ExitInput || fail.Message != tc.message {
				t.Fatalf("failure=%+v, want %s/%d/%q", fail, tc.code, ExitInput, tc.message)
			}
			diagnosticTestSafe(t, fail.Message)
		})
	}
	for _, asCurl := range []bool{false, true} {
		_, fail := decodeInput(bytes.Repeat([]byte("x"), core.MaxInput+1), asCurl)
		if fail == nil || fail.Code != "INVALID_INPUT" || fail.Exit != ExitInput ||
			fail.Message != "Input exceeds 1 MiB. Reduce the input size." {
			t.Fatalf("oversized input failure=%+v", fail)
		}
	}
}

func TestDiagnosticUsage(t *testing.T) {
	for _, tc := range []struct {
		name, message string
		args          []string
	}{
		{"unknown_flag", "Invalid flag syntax. Use 'curlgo run --help' for usage.", []string{"--private_option=private_token"}},
		{"missing_value", "Invalid flag syntax. Use 'curlgo run --help' for usage.", []string{"--request"}},
		{"bad_integer", "Invalid flag syntax. Use 'curlgo run --help' for usage.", []string{"--inline-body-bytes=private_token"}},
		{"bad_help_boolean", "Invalid flag syntax. Use 'curlgo run --help' for usage.", []string{"--help=private_token"}},
		{"help_bad_flag", "Invalid flag syntax. Use 'curlgo run --help' for usage.", []string{"--help", "--private_option"}},
		{"extra_argument", "Unexpected positional arguments. Use only named run flags.", []string{"--request", "-", "private_path"}},
		{"help_extra_argument", "Unexpected positional arguments. Use only named run flags.", []string{"--help", "--", "private_path"}},
		{"missing_source", "Missing input source. Provide --request or --curl.", nil},
		{"disabled_help", "Missing input source. Provide --request or --curl.", []string{"--help=true", "-h=false"}},
		{"multiple_sources", "Conflicting input sources. Use only one of --request or --curl.", []string{"--request=-", "--curl=-"}},
		{"multiple_empty_sources", "Conflicting input sources. Use only one of --request or --curl.", []string{"--request=", "--curl="}},
		{"empty_request", "Empty input source. Provide a file or - for stdin.", []string{"--request="}},
		{"empty_curl", "Empty input source. Provide a file or - for stdin.", []string{"--curl="}},
		{"negative_inline", "Invalid inline body limit. Use a nonnegative value no larger than 4 MiB.", []string{"--request=-", "--inline-body-bytes=-1"}},
		{"large_inline", "Invalid inline body limit. Use a nonnegative value no larger than 4 MiB.", []string{"--request=-", "--inline-body-bytes=" + strconv.Itoa(core.MaxBody+1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &inputTestReader{Reader: strings.NewReader("private_token"), err: diagnosticOpaqueError{}}
			exit, value, raw := runTestInvoke(t, reader, tc.args...)
			diagnosticTestFailure(t, exit, value, raw, "INVALID_USAGE", tc.message)
			if reader.read != 0 {
				t.Fatal("usage error read input")
			}
		})
	}
}

func TestDiagnosticHelpBypassesExecutionConstraints(t *testing.T) {
	reader := &inputTestReader{Reader: strings.NewReader("private_token"), err: diagnosticOpaqueError{}}
	exit, value, raw := runTestInvoke(t, reader, "--help", "--request=", "--curl=private_path",
		"--inline-body-bytes=-1", "--ca-file=private_path", "--cookie-jar=private_path", "--proxy=private_token")
	if exit != ExitOK || !value.OK || value.Error != nil || value.Help == nil || reader.read != 0 {
		t.Fatalf("valid help failed: exit=%d, value=%+v, read=%d", exit, value, reader.read)
	}
	diagnosticTestSafe(t, raw)
}

func TestDiagnosticReadAndSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "private_path")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), core.MaxInput+1), 0600); err != nil {
		t.Fatal(err)
	}
	_, sizeErr := readLimited(strings.NewReader("xx"), 1)
	if sizeErr == nil {
		t.Fatal("oversized reader accepted")
	}
	for _, tc := range []struct {
		name, path, message string
		reader              func() io.Reader
	}{
		{"missing_file", filepath.Join(dir, "missing_private_path"), "Cannot read input source. Check that it is readable.", nil},
		{"directory", dir, "Cannot read input source. Check that it is readable.", nil},
		{"opaque_error", "-", "Cannot read input source. Check that it is readable.", func() io.Reader {
			return &inputTestReader{Reader: strings.NewReader(""), err: diagnosticOpaqueError{}}
		}},
		{"size_text_is_not_size_error", "-", "Cannot read input source. Check that it is readable.", func() io.Reader {
			return &inputTestReader{Reader: strings.NewReader(""), err: errors.New("input exceeds size limit")}
		}},
		{"wrapped_read_error", "-", "Cannot read input source. Check that it is readable.", func() io.Reader {
			return &inputTestReader{Reader: strings.NewReader(""), err: diagnosticOpaqueError{cause: &os.PathError{Op: "read", Path: "private_path", Err: os.ErrPermission}}}
		}},
		{"wrapped_size_error", "-", "Input exceeds 1 MiB. Reduce the input size.", func() io.Reader {
			return &inputTestReader{Reader: strings.NewReader(""), err: diagnosticOpaqueError{cause: sizeErr}}
		}},
		{"oversized_file", path, "Input exceeds 1 MiB. Reduce the input size.", nil},
		{"oversized_stdin", "-", "Input exceeds 1 MiB. Reduce the input size.", func() io.Reader {
			return strings.NewReader(strings.Repeat("x", core.MaxInput+1))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, flag := range []string{"--request", "--curl"} {
				var reader io.Reader
				if tc.reader != nil {
					reader = tc.reader()
				}
				exit, value, raw := runTestInvoke(t, reader, flag, tc.path)
				diagnosticTestFailure(t, exit, value, raw, "INVALID_INPUT", tc.message)
			}
		})
	}
}

func TestDiagnosticSizeErrorIdentity(t *testing.T) {
	_, first := readLimited(strings.NewReader("xx"), 1)
	_, second := readLimited(strings.NewReader("xx"), 1)
	if first == nil || second == nil || !errors.Is(diagnosticOpaqueError{cause: first}, second) {
		t.Fatal("size errors must share sentinel identity through wrapping")
	}
	if errors.Is(errors.New("input exceeds size limit"), second) {
		t.Fatal("unrelated reader error classified by text")
	}
}

func TestDiagnosticConfigurationAndCookies(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	tlsServer := httptest.NewTLSServer(nil)
	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: tlsServer.Certificate().Raw}))
	tlsServer.Close()
	dir := t.TempDir()
	fixtures := map[string]string{
		"ca": ca, "invalid_ca": "private_password",
		"large_ca":           strings.Repeat("x", core.MaxInput+1),
		"malformed_jar":      `{"version":1,"cookies":[private_token`,
		"unknown_jar_field":  `{"version":1,"cookies":[],"private_field":"private_token"}`,
		"version_jar":        `{"version":987654321,"cookies":[]}`,
		"absent_cookies_jar": `{"version":1}`,
		"null_cookies_jar":   `{"version":1,"cookies":null}`,
		"trailing_jar":       `{"version":1,"cookies":[]} {"private_field":"private_token"}`,
		"large_jar":          strings.Repeat("x", (2<<20)+1),
	}
	paths := make(map[string]string)
	for name, data := range fixtures {
		path := filepath.Join(dir, name+"_private_path")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		paths[name] = path
	}
	for _, tc := range []struct {
		name, message string
		settings      map[string]any
		args          []string
	}{
		{"ca_missing", "Cannot read CA file. Check that it is readable.", nil, []string{"--ca-file", filepath.Join(dir, "missing_private_path")}},
		{"ca_directory", "Cannot read CA file. Check that it is readable.", nil, []string{"--ca-file", dir}},
		{"ca_oversized", "CA file exceeds 1 MiB. Reduce the file size.", nil, []string{"--ca-file", paths["large_ca"]}},
		{"ca_conflict", "Conflicting CA sources. Use matching inline and file certificates.", map[string]any{"caCertPEM": ca + ca}, []string{"--ca-file", paths["ca"]}},
		{"ca_invalid_file", "Invalid CA certificates. Use valid certificate-only PEM.", nil, []string{"--ca-file", paths["invalid_ca"]}},
		{"ca_invalid_inline", "Invalid CA certificates. Use valid certificate-only PEM.", map[string]any{"caCertPEM": "private_password"}, nil},
		{"proxy_invalid_flag", "Invalid --proxy value. Use a supported proxy address.", nil, []string{"--proxy", "ftp://private_user:private_password@private_host.invalid"}},
		{"proxy_blank_flag", "Invalid --proxy value. Use a supported proxy address.", nil, []string{"--proxy", " "}},
		{"proxy_conflict", "Conflicting proxy sources. Use matching input and flag addresses.", map[string]any{"proxy": core.ProxySettings{URL: "http://private_user:private_password@private_host.invalid:7890"}}, []string{"--proxy", "http://private_host.invalid:7891"}},
		{"proxy_invalid_json", "Invalid input proxy address. Use a supported proxy address.", map[string]any{"proxy": core.ProxySettings{URL: "ftp://private_user:private_password@private_host.invalid"}}, nil},
		{"proxy_invalid_json_before_conflict", "Invalid input proxy address. Use a supported proxy address.", map[string]any{"proxy": core.ProxySettings{URL: "ftp://private_host.invalid"}}, []string{"--proxy", "http://private_host.invalid:7890"}},
		{"proxy_invalid_bypass", "Invalid proxy settings. Check the proxy address and bypass list.", map[string]any{"proxy": core.ProxySettings{URL: "http://private_host.invalid:7890", Bypass: "private_host.invalid\nprivate_token"}}, nil},
		{"proxy_bypass_with_matching_flag", "Invalid proxy settings. Check the proxy address and bypass list.", map[string]any{"proxy": core.ProxySettings{URL: "private_host.invalid:7890", Bypass: "private_host.invalid\rprivate_token"}}, []string{"--proxy", "http://private_host.invalid:7890/"}},
		{"cookie_directory", "Cannot read cookie jar. Check that it is readable.", nil, []string{"--cookie-jar", dir}},
		{"cookie_bad_path", "Cannot read cookie jar. Check that it is readable.", nil, []string{"--cookie-jar", paths["ca"] + "\x00private_path"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := inputTestJSON(t, map[string]any{
				"url":     server.URL + "/private_path?token=private_token",
				"headers": []core.Pair{{Key: "Authorization", Value: "Bearer private_password"}},
			}, tc.settings)
			for _, dryRun := range []bool{false, true} {
				args := append([]string{"--request", "-"}, tc.args...)
				if dryRun {
					args = append(args, "--dry-run")
				}
				exit, value, raw := runTestInvoke(t, bytes.NewReader(data), args...)
				diagnosticTestFailure(t, exit, value, raw, "INVALID_INPUT", tc.message)
			}
		})
	}
	for _, name := range []string{"malformed_jar", "unknown_jar_field", "version_jar", "absent_cookies_jar", "null_cookies_jar", "trailing_jar", "large_jar"} {
		t.Run(name, func(t *testing.T) {
			path := paths[name]
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, dryRun := range []bool{false, true} {
				args := []string{"--request", "-", "--cookie-jar", path}
				if dryRun {
					args = append(args, "--dry-run")
				}
				data := inputTestJSON(t, map[string]any{"url": server.URL}, nil)
				exit, value, raw := runTestInvoke(t, bytes.NewReader(data), args...)
				diagnosticTestFailure(t, exit, value, raw, "INVALID_INPUT", "Invalid or unsupported cookie jar. Use a version-1 cookie snapshot.")
			}
			got, readErr := os.ReadFile(path)
			after, statErr := os.Stat(path)
			if readErr != nil || statErr != nil || string(got) != fixtures[name] ||
				before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("rejected cookie jar was modified")
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("diagnostic failures sent %d HTTP requests", calls.Load())
	}
}

func TestDiagnosticRootHelp(t *testing.T) {
	for _, args := range [][]string{
		{"help", "private_path"}, {"help", "run", "private_token"},
		{"--help", "private_token"}, {"-h", "run"},
	} {
		reader := &inputTestReader{Reader: strings.NewReader("private_token"), err: diagnosticOpaqueError{}}
		var stdout, stderr bytes.Buffer
		exit, handled := Dispatch(context.Background(), reader, &stdout, &stderr, args)
		var value envelope
		if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if !handled || stderr.Len() != 0 || reader.read != 0 {
			t.Fatal("root help error performed input/output side effects")
		}
		diagnosticTestFailure(t, exit, value, stdout.String(), "INVALID_USAGE", "Invalid help command. Use 'curlgo help' or 'curlgo help run'.")
	}
}

func diagnosticTestFailure(t *testing.T, exit ExitCode, value envelope, raw, code, message string) {
	t.Helper()
	if exit != ExitInput || value.OK || value.Error == nil || value.Error.Code != code ||
		value.Error.Message != message || value.Result != nil || value.Help != nil || value.Validation != nil {
		t.Fatalf("exit=%d, envelope=%+v, want %s/%q", exit, value, code, message)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		t.Fatal(err)
	}
	outputTestKeys(t, outputTestObject(t, wire), "schemaVersion", "ok", "error")
	diagnosticTestSafe(t, raw)
}

func diagnosticTestSafe(t *testing.T, text string) {
	t.Helper()
	for _, secret := range []string{
		"private_", "987654321", "://", os.TempDir(), "/Users/", "Bearer ", "$private",
	} {
		if strings.Contains(text, secret) {
			t.Fatalf("diagnostic leaks input detail %q", secret)
		}
	}
}

type diagnosticOpaqueError struct{ cause error }

func (diagnosticOpaqueError) Error() string {
	panic("diagnostics must not format underlying errors")
}

func (e diagnosticOpaqueError) Unwrap() error { return e.cause }
