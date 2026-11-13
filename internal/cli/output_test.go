package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/echoVic/curlgo/internal/core"
)

func TestFormatResultBodyModes(t *testing.T) {
	tests := []struct {
		name, contentType string
		body              []byte
		limit             int
		file, truncated   bool
	}{
		{"json_at_limit", "application/json; charset=utf-8", []byte("{ \"a\": 1 }\n"), 11, false, false},
		{"json_above_limit", "application/json", []byte(`{"a":1}`), 6, true, false},
		{"text_at_limit", "text/plain", []byte("hello"), 5, false, false},
		{"text_above_limit", "text/plain", []byte("hello"), 4, true, true},
		{"html", "text/html", []byte("<p>hello</p>"), 100, false, false},
		{"xml", "application/xml", []byte("<a/>"), 100, false, false},
		{"javascript", "application/javascript", []byte("alert(1)"), 100, false, false},
		{"form", "application/x-www-form-urlencoded", []byte("a=1&b=2"), 100, false, false},
		{"json_suffix", "application/problem+json", []byte(`{}`), 100, false, false},
		{"xml_suffix", "application/soap+xml", []byte("<a/>"), 100, false, false},
		{"uppercase_type", "Application/JSON; charset=UTF-8", []byte(`{}`), 100, false, false},
		{"binary_type", "application/octet-stream", []byte("hello"), 100, true, false},
		{"image_type", "image/png", []byte{0x89, 'P', 'N', 'G'}, 100, true, false},
		{"nul", "text/plain", []byte("a\x00b"), 100, true, false},
		{"control", "application/json", []byte("\x01"), 100, true, false},
		{"delete", "text/plain", []byte("a\x7fb"), 100, true, false},
		{"unicode_control", "text/plain", []byte("a\u0085b"), 100, true, false},
		{"invalid_utf8", "text/plain", []byte{0xff, 0xfe}, 100, true, false},
		{"allowed_whitespace", "text/plain", []byte("\t\r\n"), 100, false, false},
		{"missing_type_text", "", []byte("hello"), 100, false, false},
		{"missing_type_binary", "", []byte{0, 1, 2}, 100, true, false},
		{"missing_type_html", "", []byte("<!DOCTYPE html><html></html>"), 100, false, false},
		{"malformed_type", "not a media type", []byte("hello"), 100, true, false},
		{"empty", "text/plain", nil, 100, false, false},
		{"empty_zero_limit", "application/octet-stream", []byte{}, 0, false, true},
		{"empty_missing_type", "", nil, 0, false, false},
		{"nonempty_zero_limit", "text/plain", []byte("x"), 0, true, false},
		{"unicode_at_byte_limit", "text/plain", []byte("\u4e16\u754c"), 6, false, false},
		{"unicode_above_byte_limit", "text/plain", []byte("\u4e16\u754c"), 5, true, false},
		{"raw_json", "application/json", []byte("{\n  \"b\": 2, \"a\": 1\n}\n"), 100, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			input := core.HTTPResult{
				StatusCode: 200,
				Headers:    make(http.Header),
				Body:       tc.body, BodyTruncated: tc.truncated,
			}
			if tc.contentType != "" {
				input.Headers.Set("Content-Type", tc.contentType)
			}
			got, err := formatResult(input, tc.limit, dir)
			if err != nil || got == nil || got.Body == nil {
				t.Fatalf("formatResult = %+v, %v", got, err)
			}
			body := got.Body
			wantType := tc.contentType
			if wantType == "" {
				wantType = http.DetectContentType(tc.body)
			}
			if body.Bytes != len(tc.body) || body.Truncated != tc.truncated || body.ContentType != wantType {
				t.Fatalf("body metadata = %+v", body)
			}
			wire := outputTestObject(t, body)
			if tc.file {
				if body.Mode != "file" || body.Encoding != "binary" || body.Text != nil {
					t.Fatalf("file body = %+v", body)
				}
				if _, ok := wire["text"]; ok {
					t.Fatal("file body includes text")
				}
				if wire["path"] != body.Path {
					t.Fatal("file path missing from JSON")
				}
				outputTestFile(t, body.Path, dir, tc.body)
			} else {
				if body.Mode != "inline" || body.Encoding != "utf-8" || body.Text == nil ||
					*body.Text != string(tc.body) || body.Path != "" {
					t.Fatalf("inline body = %+v", body)
				}
				if wire["text"] != string(tc.body) {
					t.Fatalf("text changed or omitted in JSON: %+v", wire)
				}
				if _, ok := wire["path"]; ok {
					t.Fatal("inline body includes path")
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatalf("inline body created files: %v, %v", entries, err)
				}
			}
			outputTestKeys(t, wire, "mode", "encoding", "contentType", "bytes", "truncated", map[bool]string{true: "path", false: "text"}[tc.file])
		})
	}
}

func TestFormatResultMetadata(t *testing.T) {
	input := outputTestHTTPResult()
	got, err := formatResult(input, 100, t.TempDir())
	if err != nil || got == nil {
		t.Fatalf("formatResult = %+v, %v", got, err)
	}
	outputTestMetadata(t, got, input)
	input.Headers["X-Multi"][0] = "changed"
	input.Headers.Set("X-New", "changed")
	if !reflect.DeepEqual(got.Headers["X-Multi"], []string{"first", "second"}) ||
		got.Headers.Get("X-New") != "" {
		t.Fatalf("headers alias input: %v", got.Headers)
	}
	got.Headers["X-Multi"][1] = "output changed"
	if input.Headers["X-Multi"][1] != "second" {
		t.Fatal("output header values alias input")
	}
}

func TestFormatResultWithoutResponse(t *testing.T) {
	got, err := formatResult(core.HTTPResult{Body: []byte("ignored")}, 0, filepath.Join(t.TempDir(), "missing"))
	if got != nil || err != nil {
		t.Fatalf("no response = %+v, %v; want nil, nil", got, err)
	}
}

func TestFormatResultSaveFailurePreservesMetadata(t *testing.T) {
	input := outputTestHTTPResult()
	input.BodyTruncated = true
	got, err := formatResult(input, 0, filepath.Join(t.TempDir(), "missing"))
	if err == nil || got == nil || got.Body != nil {
		t.Fatalf("save failure = %+v, %v", got, err)
	}
	outputTestMetadata(t, got, input)
	wire := outputTestObject(t, got)
	if _, ok := wire["body"]; ok {
		t.Fatal("failed save left a body in JSON")
	}
	outputTestKeys(t, wire, "method", "url", "status", "proto", "statusCode", "headers", "timing")
}

func TestOutputEnvelopeJSON(t *testing.T) {
	result, err := formatResult(outputTestHTTPResult(), 100, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wire := outputTestObject(t, envelope{SchemaVersion: 1, OK: true, Result: result})
	outputTestKeys(t, wire, "schemaVersion", "ok", "result")
	if wire["schemaVersion"] != float64(1) || wire["ok"] != true {
		t.Fatalf("envelope = %+v", wire)
	}
	resultWire := wire["result"].(map[string]any)
	outputTestKeys(t, resultWire, "method", "url", "status", "proto", "statusCode", "headers", "body", "timing")
	timing := resultWire["timing"].(map[string]any)
	wantTiming := map[string]any{
		"dnsMs": 0.125, "connectMs": 1.5, "tlsMs": 2.75, "ttfbMs": 3.125, "totalMs": 4.5,
	}
	if !reflect.DeepEqual(timing, wantTiming) {
		t.Fatalf("timing JSON = %v, want %v", timing, wantTiming)
	}
	errorWire := outputTestObject(t, envelope{
		SchemaVersion: 1, OK: false, Error: &errorValue{Code: "TLS_ERROR", Message: "TLS request failed."},
	})
	outputTestKeys(t, errorWire, "schemaVersion", "ok", "error")
	if errorWire["ok"] != false || !reflect.DeepEqual(errorWire["error"], map[string]any{
		"code": "TLS_ERROR", "message": "TLS request failed.",
	}) {
		t.Fatalf("error envelope = %v", errorWire)
	}
}

func TestHTTPFailure(t *testing.T) {
	tests := []struct {
		code core.HTTPErrorCode
		exit ExitCode
	}{
		{core.HTTPErrInvalidRequest, ExitInput},
		{core.HTTPErrRequestBuild, ExitInput},
		{core.HTTPErrRedirect, ExitExecution},
		{core.HTTPErrTLS, ExitExecution},
		{core.HTTPErrConnect, ExitExecution},
		{core.HTTPErrTimeout, ExitExecution},
		{core.HTTPErrCanceled, ExitExecution},
		{core.HTTPErrBodyRead, ExitExecution},
	}
	for _, tc := range tests {
		t.Run(string(tc.code), func(t *testing.T) {
			var message string
			for _, secret := range []string{"private_url_token", "private_header_password"} {
				typed := &core.HTTPError{Code: tc.code, Op: secret, Err: errors.New(secret)}
				for _, err := range []error{typed, fmt.Errorf("%s: %w", secret, typed), errors.Join(errors.New(secret), typed)} {
					got := httpFailure(err)
					if got == nil || got.Code != string(tc.code) || got.Exit != tc.exit ||
						got.Message == "" || strings.Contains(got.Message, secret) {
						t.Fatalf("httpFailure = %+v", got)
					}
					if message != "" && got.Message != message {
						t.Fatal("safe message varies with raw error or wrapping")
					}
					message = got.Message
				}
			}
		})
	}
	if got := httpFailure(nil); got != nil {
		t.Fatalf("httpFailure(nil) = %+v", got)
	}
	want := httpFailure(&core.HTTPError{Code: core.HTTPErrConnect})
	var typedNil *core.HTTPError
	for _, err := range []error{
		errors.New("private_url_token"),
		&core.HTTPError{Code: "private_url_token", Op: "private_header_password"},
		typedNil,
	} {
		got := httpFailure(err)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("fallback = %+v, want %+v", got, want)
		}
	}
}

func TestOutputFailure(t *testing.T) {
	standalone := outputFailure(nil)
	if standalone == nil || standalone.Code != "OUTPUT_ERROR" || standalone.Exit != ExitOutput || standalone.Message == "" {
		t.Fatalf("outputFailure(nil) = %+v", standalone)
	}
	for _, code := range []string{"BODY_READ_ERROR", "TIMEOUT", "INVALID_REQUEST", ""} {
		t.Run(code, func(t *testing.T) {
			primary := &failure{Code: code, Message: "private_header_password", Exit: ExitExecution}
			before := *primary
			got := outputFailure(primary)
			if got == nil || got.Code != "OUTPUT_ERROR" || got.Exit != ExitOutput ||
				strings.Contains(got.Message, primary.Message) ||
				code != "" && !strings.Contains(got.Message, code) {
				t.Fatalf("outputFailure = %+v", got)
			}
			if *primary != before {
				t.Fatal("outputFailure modified the primary error")
			}
			primary.Message = "different_private_password"
			if next := outputFailure(primary); !reflect.DeepEqual(next, got) {
				t.Fatal("output failure message varies with raw primary message")
			}
			if code == "" && !reflect.DeepEqual(got, standalone) {
				t.Fatal("empty primary code changes standalone output failure")
			}
		})
	}
}

func TestSaveBodyExplicitDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0751); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(dir, "response-existing.body")
	if err := os.WriteFile(oldPath, []byte("old contents"), 0640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte{0, 0xff, '\n', 'x'}
	// The repository and TempDir can be on different Windows volumes.
	t.Chdir(filepath.Dir(dir))
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, dir)
	if err != nil {
		t.Fatal(err)
	}
	first, err := saveBody(body, relative)
	if err != nil {
		t.Fatal(err)
	}
	second, err := saveBody(body, dir)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first == oldPath || second == oldPath {
		t.Fatalf("body paths are not unique: %q, %q", first, second)
	}
	outputTestFile(t, first, dir, body)
	outputTestFile(t, second, dir, body)
	after, err := os.Stat(dir)
	if err != nil || after.Mode() != before.Mode() {
		t.Fatalf("explicit directory mode changed: %v, %v", after, err)
	}
	old, err := os.ReadFile(oldPath)
	info, statErr := os.Stat(oldPath)
	if err != nil || statErr != nil || string(old) != "old contents" || posixPermissions() && info.Mode().Perm() != 0640 {
		t.Fatalf("old file changed: %q, %v, %v", old, err, statErr)
	}
}

func TestSaveBodyRejectsInvalidDirectory(t *testing.T) {
	parent := t.TempDir()
	regular := filepath.Join(parent, "file")
	if err := os.WriteFile(regular, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(parent, "missing"), regular, filepath.Join(regular, "child")} {
		path, err := saveBody([]byte("body"), dir)
		if path != "" || err == nil {
			t.Fatalf("saveBody(%q) = %q, %v", dir, path, err)
		}
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 1 || entries[0].Name() != "file" {
		t.Fatalf("failed saves left artifacts: %v, %v", entries, err)
	}
}

func TestSaveBodyUnwritableExplicitDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	path, err := saveBody([]byte("body"), dir)
	if err == nil {
		_ = os.Remove(path)
		t.Skip("filesystem or current user bypasses directory write permissions")
	}
	if path != "" {
		t.Fatalf("failed save returned path %q", path)
	}
	info, statErr := os.Stat(dir)
	entries, readErr := os.ReadDir(dir)
	if statErr != nil || readErr != nil || info.Mode().Perm() != 0500 || len(entries) != 0 {
		t.Fatalf("failed save modified directory: %v, %v, %v, %v", info, entries, statErr, readErr)
	}
}

func TestSaveBodyDefaultDirectory(t *testing.T) {
	parent := t.TempDir()
	setTempDir(t, parent)
	body := []byte{0xff, 0, 1}
	first, err := saveBody(body, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := saveBody(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{first, second} {
		dir := filepath.Dir(path)
		if filepath.Dir(dir) != parent || !strings.HasPrefix(filepath.Base(dir), "curlgo-response-") {
			t.Fatalf("unexpected default directory: %q", dir)
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() || posixPermissions() && info.Mode().Perm() != 0700 {
			t.Fatalf("default directory not retained and private: %v, %v", info, err)
		}
	}
	if filepath.Dir(first) == filepath.Dir(second) {
		t.Fatal("default directories are not unique")
	}
	outputTestFile(t, first, filepath.Dir(first), body)
	outputTestFile(t, second, filepath.Dir(second), nil)
}

func TestSaveBodyDefaultDirectoryFailure(t *testing.T) {
	parent := t.TempDir()
	missing := filepath.Join(parent, "missing")
	setTempDir(t, missing)
	path, err := saveBody([]byte("body"), "")
	if err == nil || path != "" {
		t.Fatalf("invalid temp directory = %q, %v", path, err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed default save left artifacts: %v, %v", entries, err)
	}
}

func TestEmitOneDocument(t *testing.T) {
	for _, exit := range []ExitCode{ExitOK, ExitInput, ExitExecution, ExitOutput} {
		t.Run(fmt.Sprint(exit), func(t *testing.T) {
			value := envelope{SchemaVersion: 1, OK: exit == ExitOK}
			if exit != ExitOK {
				value.Error = &errorValue{Code: "BODY_READ_ERROR", Message: "Body read failed."}
			}
			var stdout, stderr bytes.Buffer
			if got := emit(&stdout, &stderr, value, exit); got != exit {
				t.Fatalf("exit = %d, want %d", got, exit)
			}
			expected, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stdout.Bytes(), append(expected, '\n')) || stderr.Len() != 0 {
				t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
			}
			decoder := json.NewDecoder(&stdout)
			var decoded envelope
			if err := decoder.Decode(&decoded); err != nil {
				t.Fatal(err)
			}
			if err := decoder.Decode(&decoded); err != io.EOF {
				t.Fatalf("extra JSON document: %v", err)
			}
		})
	}
}

func TestEmitWriterFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		partial int
		err     error
	}{
		{"failed_write", 0, errors.New("private_output_path")},
		{"partial_write_error", 12, errors.New("private_output_path")},
		{"short_write", 12, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := &outputTestFailingWriter{partial: tc.partial, err: tc.err}
			var stderr bytes.Buffer
			value := envelope{SchemaVersion: 1, OK: true}
			if got := emit(writer, &stderr, value, ExitExecution); got != ExitOutput {
				t.Fatalf("exit = %d", got)
			}
			expected, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if writer.calls != 1 || !bytes.Equal(writer.Bytes(), expected[:tc.partial]) {
				t.Fatalf("write failure emitted another document: calls=%d, bytes=%q", writer.calls, writer.Bytes())
			}
			if stderr.String() != "CurlGo: 无法写入结果 JSON\n" {
				t.Fatalf("unsafe diagnostic: %q", stderr.String())
			}
		})
	}
}

func TestEmitEncodeFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	value := envelope{SchemaVersion: 1, OK: true, Result: &resultValue{Timing: timingValue{TotalMs: math.NaN()}}}
	if got := emit(&stdout, &stderr, value, ExitOK); got != ExitOutput ||
		stdout.Len() != 0 || stderr.String() != "CurlGo: 无法写入结果 JSON\n" {
		t.Fatalf("encode failure: stdout=%q, stderr=%q", stdout.String(), stderr.String())
	}
}

func TestEmitIgnoresDiagnosticFailure(t *testing.T) {
	stdout := &outputTestFailingWriter{err: errors.New("private_stdout")}
	stderr := &outputTestFailingWriter{err: errors.New("private_stderr")}
	if got := emit(stdout, stderr, envelope{SchemaVersion: 1}, ExitOK); got != ExitOutput ||
		stdout.calls != 1 || stderr.calls != 1 {
		t.Fatalf("diagnostic failure: exit=%d, writes=%d/%d", got, stdout.calls, stderr.calls)
	}
}

type outputTestFailingWriter struct {
	bytes.Buffer
	partial, calls int
	err            error
}

func (w *outputTestFailingWriter) Write(p []byte) (int, error) {
	w.calls++
	_, _ = w.Buffer.Write(p[:w.partial])
	return w.partial, w.err
}

func outputTestHTTPResult() core.HTTPResult {
	return core.HTTPResult{
		Method: "PATCH", URL: "https://example.test/api?q=1", Status: "206 Partial Content",
		Proto: "HTTP/2.0", StatusCode: 206,
		Headers: http.Header{"Content-Type": {"application/json"}, "X-Multi": {"first", "second"}},
		Body:    []byte("{ \"b\": 2, \"a\": 1 }\n"),
		Timing: core.HTTPTiming{
			DNS: 125 * time.Microsecond, Connect: 1500 * time.Microsecond,
			TLS: 2750 * time.Microsecond, TTFB: 3125 * time.Microsecond, Total: 4500 * time.Microsecond,
		},
	}
}

func outputTestMetadata(t *testing.T, got *resultValue, input core.HTTPResult) {
	t.Helper()
	if got.Method != input.Method || got.URL != input.URL || got.Status != input.Status ||
		got.Proto != input.Proto || got.StatusCode != input.StatusCode || !reflect.DeepEqual(got.Headers, input.Headers) {
		t.Fatalf("metadata = %+v, want %+v", got, input)
	}
	wantTiming := timingValue{DNSMs: 0.125, ConnectMs: 1.5, TLSMs: 2.75, TTFBMs: 3.125, TotalMs: 4.5}
	if got.Timing != wantTiming {
		t.Fatalf("timing = %+v, want %+v", got.Timing, wantTiming)
	}
}

func outputTestFile(t *testing.T, path, dir string, body []byte) {
	t.Helper()
	absDir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) || filepath.Dir(path) != absDir ||
		!strings.HasPrefix(filepath.Base(path), "response-") || !strings.HasSuffix(path, ".body") {
		t.Fatalf("unexpected body path: %q", path)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || posixPermissions() && info.Mode().Perm() != 0600 {
		t.Fatalf("body file not retained and private: %v, %v", info, err)
	}
	saved, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(saved, body) {
		t.Fatalf("saved body = %v, %v; want %v", saved, err, body)
	}
}

func outputTestObject(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func outputTestKeys(t *testing.T, object map[string]any, keys ...string) {
	t.Helper()
	if len(object) != len(keys) {
		t.Fatalf("JSON keys = %v, want %v", object, keys)
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			t.Fatalf("missing JSON key %q in %v", key, object)
		}
	}
}
