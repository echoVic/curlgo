package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/echoVic/curlgo/internal/core"
)

func TestInputMissingSecretsAreSanitized(t *testing.T) {
	for _, field := range []string{"url", "header", "global_header", "body", "multipart", "bearer", "basic", "api_header", "api_query"} {
		t.Run(field, func(t *testing.T) {
			request := map[string]any{"url": "https://example.test"}
			settings := map[string]any{
				"environment": "active",
				"environments": map[string]map[string]string{
					"active": {"token": "ordinary", "secret.private_marker": "private_value"},
					"other":  {"secret.private_marker": "private_value"},
				},
			}
			const reference = "{{secret.private_marker}}"
			switch field {
			case "url":
				request["url"] = "https://example.test/" + reference
			case "header":
				request["headers"] = []core.Pair{{Key: "X-Token", Value: reference}}
			case "global_header":
				settings["globalHeaders"] = []core.Pair{{Key: "X-Token", Value: reference}}
			case "body":
				request["body"] = reference
			case "multipart":
				request["bodyMode"] = core.BodyMultipart
				request["formParts"] = []core.FormPart{{Kind: core.FormText, Name: "token", Value: reference}}
			case "bearer":
				request["auth"] = core.Authentication{Mode: core.AuthBearer, Token: reference}
			case "basic":
				request["auth"] = core.Authentication{Mode: core.AuthBasic, Username: "user", Password: reference}
			case "api_header":
				request["auth"] = core.Authentication{Mode: core.AuthAPIKey, Key: "X-Token", Value: reference}
			case "api_query":
				request["auth"] = core.Authentication{Mode: core.AuthAPIKey, Key: "token", Value: reference, Location: core.APIKeyQuery}
			}
			_, fail := decodeInput(inputTestJSON(t, request, settings), false)
			if fail == nil || fail.Code != "MISSING_SECRET" || fail.Exit != ExitInput {
				t.Fatalf("missing secret failure = %+v", fail)
			}
			diagnosticTestSafe(t, fail.Message)
		})
	}
	inputTestFailure(t, []byte(`curl 'https://example.test/{{secret.private_marker}}'`), true, "MISSING_SECRET")
}

func TestRunMissingSecretsNeverConnects(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	// Neither process variables nor the GUI vault is a CLI secret source.
	t.Setenv("token", "private_marker")
	t.Setenv("secret.token", "private_marker")
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	vault := filepath.Join(dir, "secret-vault.json")
	if err := os.WriteFile(vault, []byte("private_marker"), 0600); err != nil {
		t.Fatal(err)
	}
	before := dryRunTestSnapshot(t, dir)
	input := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":%q,"auth":{"mode":"bearer","token":"{{secret.token}}"}}}`, server.URL)
	for _, args := range [][]string{nil, {"--dry-run"}} {
		exit, value, raw := runTestInvoke(t, strings.NewReader(input), append([]string{"--request", "-"}, args...)...)
		dryRunTestFailure(t, exit, value, raw, ExitInput, "MISSING_SECRET")
	}
	after := dryRunTestSnapshot(t, dir)
	if !reflect.DeepEqual(before, after) || calls.Load() != 0 {
		t.Fatal("missing secret accessed the network or changed local files")
	}
}

func TestRunSecretsFileAuthenticationAndBodies(t *testing.T) {
	const secret = "private_marker&=+"
	data := []byte(`{"schemaVersion":1,"variables":{"token":"` + secret + `","header":"X-Private"}}`)
	path := secretsTestFile(t, data)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, header, want string
		auth               core.Authentication
	}{
		{"bearer", "Authorization", "Bearer " + secret, core.Authentication{Mode: core.AuthBearer, Token: "{{secret.token}}"}},
		{"basic", "Authorization", "Basic " + base64.StdEncoding.EncodeToString([]byte("user:"+secret)), core.Authentication{Mode: core.AuthBasic, Username: "{{user}}", Password: "{{secret.token}}"}},
		{"api_header", "X-Private", secret, core.Authentication{Mode: core.AuthAPIKey, Key: "{{secret.header}}", Value: "{{secret.token}}"}},
		{"api_query", "", secret, core.Authentication{Mode: core.AuthAPIKey, Key: "token", Value: "{{secret.token}}", Location: core.APIKeyQuery}},
	} {
		for _, multipart := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/multipart=%v", tc.name, multipart), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					auth := r.Header.Get(tc.header)
					if tc.header == "" {
						auth = r.URL.Query().Get("token")
						if len(r.URL.Query()["token"]) != 1 {
							t.Error("API query did not replace old values")
						}
					}
					if auth != tc.want || r.URL.Path != "/"+secret {
						t.Error("secret auth or URL was not injected")
					}
					var body string
					if multipart {
						if err := r.ParseMultipartForm(1 << 20); err != nil {
							t.Error(err)
						}
						if r.MultipartForm != nil {
							defer r.MultipartForm.RemoveAll()
							if values := r.MultipartForm.Value["token"]; len(values) == 1 {
								body = values[0]
							}
						}
					} else {
						data, _ := io.ReadAll(r.Body)
						body = string(data)
					}
					if body != secret {
						t.Error("secret body was not injected")
					}
					w.WriteHeader(http.StatusNoContent)
				}))
				defer server.Close()
				request := core.NewRequest()
				request.Method, request.URL, request.Body, request.Auth = "POST", "{{base_url}}/{{secret.token}}?token=old&token=older", "{{secret.token}}", tc.auth
				if multipart {
					request.BodyMode, request.Body = core.BodyMultipart, "{{unused_secret}}"
					request.FormParts = []core.FormPart{{Kind: core.FormText, Name: "token", Value: "{{secret.token}}"}}
				}
				settings := core.Settings{Environment: "active", Environments: map[string]map[string]string{
					"active": {"base_url": server.URL, "user": "user", "secret.token": "wrong"},
					"other":  {"base_url": "https://wrong.invalid", "user": "wrong"},
				}}
				exit, value, raw := runTestInvoke(t, strings.NewReader(cliExtractionInput(t, request, settings)),
					"--request", "-", "--secrets-file", path)
				if exit != ExitOK || !value.OK || value.Result == nil || value.Result.StatusCode != 204 {
					t.Fatalf("secrets-file request failed: %s", raw)
				}
				resolved, err := core.ResolveWithSecrets(request, settings, map[string]string{"token": secret, "header": "X-Private"})
				if err != nil || value.Result.URL != resolved.URL || strings.Contains(raw, base64.StdEncoding.EncodeToString([]byte("user:"+secret))) {
					t.Fatal("URL metadata changed or request credentials were echoed outside it")
				}
				secretsTestUnchanged(t, path, data, before)
			})
		}
	}
}

func TestRunSecretsFileCURL(t *testing.T) {
	path := secretsTestFile(t, []byte(`{"schemaVersion":1,"variables":{"token":"private_marker"}}`))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer private_marker" || string(data) != "private_marker" {
			t.Error("curl secret references were not injected")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	input := "curl '" + server.URL + "' -H 'Authorization: Bearer {{secret.token}}' --data-raw '{{secret.token}}'"
	exit, value, raw := runTestInvoke(t, strings.NewReader(input), "--curl", "-", "--secrets-file", path)
	if exit != ExitOK || !value.OK || value.Result == nil || value.Result.StatusCode != 204 || strings.Contains(raw, "private_marker") {
		t.Fatalf("curl file injection failed: %s", raw)
	}
}

func TestRunSecretsFileDryRunHasNoSideEffects(t *testing.T) {
	var connections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("dry run sent a request")
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	dir := t.TempDir()
	setTempDir(t, dir)
	data := []byte(`{"schemaVersion":1,"variables":{"token":"private_marker"}}`)
	path := secretsTestFile(t, data)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := dryRunTestSnapshot(t, dir)
	input := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":%q,"method":"POST","body":"{{secret.token}}",
		"auth":{"mode":"bearer","token":"{{secret.token}}"},
		"extractors":[{"variable":"secret.token","source":"header","selector":"X-Token"}]}}`, server.URL+"/{{secret.token}}")
	exit, value, raw := runTestInvoke(t, strings.NewReader(input), "--request", "-", "--secrets-file", path, "--dry-run",
		"--inline-body-bytes=0", "--out-dir", dir, "--cookie-jar", filepath.Join(dir, "new.cookies"))
	dryRunTestValidation(t, exit, raw)
	if value.Result != nil || connections.Load() != 0 || !reflect.DeepEqual(snapshot, dryRunTestSnapshot(t, dir)) {
		t.Fatal("secret dry run connected, extracted values, or wrote output")
	}
	secretsTestUnchanged(t, path, data, before)
}

func TestHelpSecretsFileContractAndNoReads(t *testing.T) {
	_, _, baseline := runTestInvoke(t, nil, "--help")
	for _, path := range []string{"", "-", filepath.Join(t.TempDir(), "missing_private_marker"), t.TempDir()} {
		reader := &inputTestReader{Reader: strings.NewReader("private_marker")}
		exit, value, raw := runTestInvoke(t, reader, "--secrets-file", path, "--request=-", "--help")
		if exit != ExitOK || value.Help == nil || reader.read != 0 || raw != baseline {
			t.Fatalf("help reads or echoes runtime secrets source: %s", raw)
		}
	}
	value := helpTestDecode(t, baseline)
	found := false
	for _, option := range value.Options {
		if option.Name == "secrets-file" {
			found = strings.Contains(option.Description, "1 MiB") && strings.Contains(option.Description, "schemaVersion") &&
				strings.Contains(option.Description, "Unix") && strings.Contains(option.Description, "Windows")
		}
	}
	if !found || !strings.Contains(baseline, "MISSING_SECRET") || !strings.Contains(baseline, "INVALID_SECRETS") {
		t.Fatal("help omits secret source constraints or error codes")
	}
}

func TestInputWithSecretsPreservesSettings(t *testing.T) {
	settings := core.Settings{Environment: "active", Environments: map[string]map[string]string{
		"active": {"public": "literal", "secret.token": "ordinary"},
	}}
	request := core.NewRequest()
	request.URL, request.Body = "https://example.test", "{{public}}:{{secret.token}}"
	got, fail := decodeInputWithSecrets([]byte(cliExtractionInput(t, request, settings)), false, map[string]string{"token": "private_marker"})
	if fail != nil || got.Request.Body != "literal:private_marker" || !reflect.DeepEqual(got.Settings, settings) {
		t.Fatalf("secret overlay mutated settings or changed decoding: fail=%+v", fail)
	}
	data, err := json.Marshal(got.Settings)
	if err != nil || bytes.Contains(data, []byte("private_marker")) {
		t.Fatal("injected secret was copied into settings")
	}
}

func TestRunSecretsFileRejectsInvalidSources(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	valid := `{"schemaVersion":1,"variables":{"token":"private_marker"}}`
	fixtures := map[string]string{
		"empty": "", "malformed": "{", "null": "null", "array": "[]",
		"missing_version": `{"variables":{}}`, "missing_variables": `{"schemaVersion":1}`,
		"null_version":        `{"schemaVersion":null,"variables":{}}`,
		"wrong_version":       `{"schemaVersion":2,"variables":{}}`,
		"string_version":      `{"schemaVersion":"1","variables":{}}`,
		"fractional_version":  `{"schemaVersion":1.0,"variables":{}}`,
		"null_variables":      `{"schemaVersion":1,"variables":null}`,
		"array_variables":     `{"schemaVersion":1,"variables":[]}`,
		"string_variables":    `{"schemaVersion":1,"variables":"private_marker"}`,
		"null_value":          `{"schemaVersion":1,"variables":{"token":null}}`,
		"numeric_value":       `{"schemaVersion":1,"variables":{"token":42}}`,
		"boolean_value":       `{"schemaVersion":1,"variables":{"token":true}}`,
		"object_value":        `{"schemaVersion":1,"variables":{"token":{}}}`,
		"array_value":         `{"schemaVersion":1,"variables":{"token":[]}}`,
		"prefixed_name":       `{"schemaVersion":1,"variables":{"secret.token":"private_marker"}}`,
		"blank_name":          `{"schemaVersion":1,"variables":{"":"private_marker"}}`,
		"invalid_name":        `{"schemaVersion":1,"variables":{"bad name":"private_marker"}}`,
		"unknown_field":       `{"schemaVersion":1,"variables":{},"private_marker":true}`,
		"incorrect_case":      `{"SchemaVersion":1,"variables":{}}`,
		"duplicate_version":   `{"schemaVersion":1,"schemaVersion":1,"variables":{}}`,
		"duplicate_variables": `{"schemaVersion":1,"variables":{},"variables":{}}`,
		"duplicate_name":      `{"schemaVersion":1,"variables":{"token":"one","token":"private_marker"}}`,
		"trailing_json":       valid + `{}`, "trailing_null": valid + `null`, "trailing_text": valid + `private_marker`,
		"invalid_utf8": "{\"schemaVersion\":1,\"variables\":{\"token\":\"\xff\"}}",
		"long_name":    `{"schemaVersion":1,"variables":{"` + strings.Repeat("n", 257) + `":"private_marker"}}`,
		"long_value":   `{"schemaVersion":1,"variables":{"token":"` + strings.Repeat("x", 65537) + `"}}`,
		"oversized":    valid + strings.Repeat(" ", core.MaxInput+1),
	}
	many := make(map[string]string)
	for i := range core.MaxSecrets + 1 {
		many[fmt.Sprintf("token%d", i)] = "private_marker"
	}
	data, err := json.Marshal(map[string]any{"schemaVersion": 1, "variables": many})
	if err != nil {
		t.Fatal(err)
	}
	fixtures["too_many"] = string(data)
	paths := map[string]string{
		"provided_empty": "", "stdin_dash": "-", "inline_json": valid,
		"missing": filepath.Join(t.TempDir(), "missing_private_marker"), "directory": t.TempDir(), "nonregular": os.DevNull,
	}
	for name, text := range fixtures {
		paths[name] = secretsTestFile(t, []byte(text))
	}
	if runtime.GOOS != "windows" {
		path := secretsTestFile(t, []byte(valid))
		if err := os.Chmod(path, 0644); err != nil {
			t.Fatal(err)
		}
		paths["public_permissions"] = path
	}
	for name, path := range paths {
		t.Run(name, func(t *testing.T) {
			input := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":%q}}`, server.URL)
			for _, dryRun := range []bool{false, true} {
				args := []string{"--request", "-", "--secrets-file", path}
				if dryRun {
					args = append(args, "--dry-run")
				}
				exit, value, raw := runTestInvoke(t, strings.NewReader(input), args...)
				dryRunTestFailure(t, exit, value, raw, ExitInput, "INVALID_SECRETS")
				diagnosticTestSafe(t, raw)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("invalid secrets input was sent to the network")
	}
}

func TestRunSecretsFileBoundsAndEmptyValues(t *testing.T) {
	for _, text := range []string{
		`{"schemaVersion":1,"variables":{}}`,
		`{"variables":{"token":""},"schemaVersion":1}`,
		`{"schemaVersion":1,"variables":{"token":"line 1\nline 2","nested.name-1":"\u4e2d\u6587"}}`,
	} {
		for _, atLimit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/atLimit=%v", text, atLimit), func(t *testing.T) {
				data := []byte(text)
				if atLimit {
					data = append(data, bytes.Repeat([]byte(" "), core.MaxInput-len(data))...)
				}
				path := secretsTestFile(t, data)
				exit, _, raw := runTestInvoke(t, strings.NewReader(`{"schemaVersion":1,"request":{"url":"https://example.test"}}`),
					"--request", "-", "--secrets-file", path, "--dry-run")
				dryRunTestValidation(t, exit, raw)
			})
		}
	}
}

func TestInputWithSecretsRejectsInvalidMap(t *testing.T) {
	input := []byte(`{"schemaVersion":1,"request":{"url":"https://example.test"}}`)
	_, fail := decodeInputWithSecrets(input, false, map[string]string{"secret.token": "private_marker"})
	if fail == nil || fail.Code != "INVALID_SECRETS" || fail.Exit != ExitInput {
		t.Fatalf("invalid injected map failure=%+v", fail)
	}
	diagnosticTestSafe(t, fail.Message)
}

func TestSecretsJSONRejectsLossyUnicodeEscapes(t *testing.T) {
	for _, value := range []string{`"\ud800"`, `"\udfff"`, `"\ud800x"`, `"\ud800\u0041"`, `"\ud800\ud800"`} {
		t.Run(value, func(t *testing.T) {
			_, fail := decodeSecrets([]byte(`{"schemaVersion":1,"variables":{"token":` + value + `}}`))
			if fail == nil || fail.Code != "INVALID_SECRETS" || fail.Exit != ExitInput {
				t.Fatal("malformed Unicode secret was silently replaced")
			}
			diagnosticTestSafe(t, fail.Message)
		})
	}
}

func TestSecretsJSONPreservesValidUnicodeEscapes(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{`"\ud83d\udd12"`, "\U0001f512"},
		{`"\ufffd"`, "\ufffd"},
		{`"\\ud800"`, `\ud800`},
		{`"\/path"`, "/path"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			values, fail := decodeSecrets([]byte(`{"schemaVersion":1,"variables":{"token":` + tc.value + `}}`))
			if fail != nil || values["token"] != tc.want {
				t.Fatal("valid Unicode or literal JSON escape was changed")
			}
		})
	}
}

func TestRunSecretExtractionReturnsValuesWithoutUpdatingFile(t *testing.T) {
	data := []byte(`{"schemaVersion":1,"variables":{"token":"private_input_marker"}}`)
	path := secretsTestFile(t, data)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private_input_marker" {
			t.Error("private input was not used")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Session", "private_header_marker")
		_, _ = io.WriteString(w, `{"token":"private_response_marker"}`)
	}))
	defer server.Close()
	request := core.NewRequest()
	request.URL = server.URL
	request.Auth = core.Authentication{Mode: core.AuthBearer, Token: "{{secret.token}}"}
	request.Extractors = []core.ResponseExtractor{
		{Variable: "secret.token", Source: core.ExtractJSON, Selector: "/token"},
		{Variable: "session", Source: core.ExtractHeader, Selector: "X-Session"},
	}
	exit, value, raw := runTestInvoke(t, strings.NewReader(cliExtractionInput(t, request, core.Settings{})),
		"--request", "-", "--secrets-file", path)
	values, present := cliExtractionVariables(t, raw)
	want := map[string]string{"secret.token": "private_response_marker", "session": "private_header_marker"}
	if exit != ExitOK || !value.OK || !present || !reflect.DeepEqual(values, want) ||
		value.Result.Body.Text == nil || *value.Result.Body.Text != `{"token":"private_response_marker"}` {
		t.Fatal("explicit extraction or raw response values were redacted")
	}
	if strings.Contains(raw, "private_input_marker") {
		t.Fatal("private input was echoed outside the raw response")
	}
	secretsTestUnchanged(t, path, data, before)
}

func TestRunSecretsFilePreservesRedirectPolicyAndURLMetadata(t *testing.T) {
	path := secretsTestFile(t, []byte(`{"schemaVersion":1,"variables":{"token":"private_marker"}}`))
	for _, follow := range []bool{false, true} {
		t.Run(fmt.Sprintf("follow=%v", follow), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "Bearer private_marker" {
					t.Error("redirect request lost same-origin auth")
				}
				if r.URL.Path == "/start" {
					w.Header().Set("Location", "/done")
					w.WriteHeader(http.StatusFound)
				} else {
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			defer server.Close()
			input := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":%q,"followRedirects":%v,"auth":{"mode":"bearer","token":"{{secret.token}}"}}}`, server.URL+"/start", follow)
			exit, value, raw := runTestInvoke(t, strings.NewReader(input), "--request", "-", "--secrets-file", path)
			wantStatus, wantCalls := http.StatusFound, int32(1)
			wantURL := server.URL + "/start"
			if follow {
				wantStatus, wantCalls = http.StatusNoContent, 2
				wantURL = server.URL + "/done"
			}
			if exit != ExitOK || !value.OK || value.Result == nil || value.Result.StatusCode != wantStatus ||
				calls.Load() != wantCalls || value.Result.URL != wantURL || strings.Contains(raw, "private_marker") {
				t.Fatalf("private request changed redirect policy or URL metadata: %s", raw)
			}
		})
	}
}

func secretsTestFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "private_marker.secrets.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func secretsTestUnchanged(t *testing.T, path string, data []byte, before os.FileInfo) {
	t.Helper()
	after, statErr := os.Stat(path)
	actual, readErr := os.ReadFile(path)
	if statErr != nil || readErr != nil || !bytes.Equal(actual, data) ||
		after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("private secrets input was modified")
	}
}
