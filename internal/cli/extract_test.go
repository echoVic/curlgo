package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/echoVic/curlgo/internal/core"
)

func TestCLIExtractionSuccessAndExplicitReuse(t *testing.T) {
	const payload = "{ \"data\": { \"token\": \"private_response_secret\", \"id\": 9007199254740993 } }\n"
	seen := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Session", "private_header_secret")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, payload)
			return
		}
		seen <- []string{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Session")}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	request := core.NewRequest()
	request.URL = server.URL + "/login"
	request.Extractors = []core.ResponseExtractor{
		{ID: "token-rule", Variable: "token", Source: core.ExtractJSON, Selector: "/data/token"},
		{ID: "id-rule", Variable: "id", Source: core.ExtractJSON, Selector: "/data/id"},
		{ID: "header-rule", Variable: "session", Source: core.ExtractHeader, Selector: "x-session"},
		{ID: "draft-rule", Disabled: true},
	}
	exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}))
	if exit != ExitOK || !value.OK || value.Error != nil || value.Result == nil ||
		value.Result.StatusCode != http.StatusCreated || value.Result.Body == nil ||
		value.Result.Body.Text == nil || *value.Result.Body.Text != payload {
		t.Fatalf("extraction lost the successful response: exit=%d, %s", exit, raw)
	}
	values, present := cliExtractionVariables(t, raw)
	want := map[string]string{
		"token": "private_response_secret", "id": "9007199254740993", "session": "private_header_secret",
	}
	if !present || !reflect.DeepEqual(values, want) {
		t.Fatalf("explicit extraction must return exact, unredacted values: got %v, want %v", values, want)
	}
	next := core.NewRequest()
	next.URL = server.URL + "/protected/{{id}}"
	next.Auth = core.Authentication{Mode: core.AuthBearer, Token: "{{token}}"}
	next.Headers = []core.Pair{{Key: "X-Session", Value: "{{session}}"}}
	settings := core.Settings{Environment: "explicit", Environments: map[string]map[string]string{"explicit": values}}
	exit, value, raw = invokeCLI(t, t.Context(), cliExtractionInput(t, next, settings))
	if exit != ExitOK || !value.OK || value.Result == nil || value.Result.StatusCode != http.StatusNoContent {
		t.Fatalf("explicitly reused extraction failed: exit=%d, %s", exit, raw)
	}
	if got := <-seen; !reflect.DeepEqual(got, []string{
		"/protected/9007199254740993", "Bearer private_response_secret", "private_header_secret",
	}) {
		t.Fatalf("reused variables changed on the wire: %v", got)
	}
	if _, present := cliExtractionVariables(t, raw); present {
		t.Fatal("a request without extraction rules returned extractedVariables")
	}
}

func TestCLIExtractionFailureKeepsResponseAtomic(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		rule       core.ResponseExtractor
	}{
		{"missing JSON field", `{"token":"private_response_secret"}`,
			core.ResponseExtractor{Variable: "private_rule_name", Source: core.ExtractJSON, Selector: "/private_missing_selector"}},
		{"non scalar JSON", `{"token":{"secret":"private_response_secret"}}`,
			core.ResponseExtractor{Variable: "token", Source: core.ExtractJSON, Selector: "/token"}},
		{"invalid JSON", `{"token":"private_response_secret"`,
			core.ResponseExtractor{Variable: "token", Source: core.ExtractJSON, Selector: "/token"}},
		{"ambiguous header", `{"token":"private_response_secret"}`,
			core.ResponseExtractor{Variable: "token", Source: core.ExtractHeader, Selector: "X-Repeated"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Good", "private_early_secret")
				w.Header().Add("X-Repeated", "first")
				w.Header().Add("X-Repeated", "second")
				http.SetCookie(w, &http.Cookie{Name: "session", Value: "captured", Path: "/"})
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			request := core.NewRequest()
			request.URL = server.URL
			request.Extractors = []core.ResponseExtractor{
				{Variable: "early", Source: core.ExtractHeader, Selector: "X-Good"},
				tc.rule,
			}
			jar := filepath.Join(t.TempDir(), "cookies.json")
			exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}), "--cookie-jar", jar)
			if exit != ExitExecution || value.OK || value.Error == nil ||
				value.Error.Code != "EXTRACTION_ERROR" || value.Error.Message != "Response variable extraction failed." {
				t.Fatalf("extraction failure must be sanitized and exit 2: exit=%d, %s", exit, raw)
			}
			if value.Result == nil || value.Result.StatusCode != http.StatusOK || value.Result.Body == nil ||
				value.Result.Body.Text == nil || *value.Result.Body.Text != tc.body ||
				value.Result.Headers.Get("X-Good") != "private_early_secret" {
				t.Fatalf("extraction failure lost the captured HTTP response: %s", raw)
			}
			if values, present := cliExtractionVariables(t, raw); present {
				t.Fatalf("extraction failure returned a partial map: %v", values)
			}
			cookies, err := core.LoadCookieFile(jar)
			if err != nil || len(cookies) != 1 || cookies[0].Cookie.Name != "session" || cookies[0].Cookie.Value != "captured" {
				t.Fatalf("extraction failure lost cookie capture: %+v, %v", cookies, err)
			}
		})
	}
}

func TestCLIExtractionSkipsNon2xxAndDisabledRules(t *testing.T) {
	active := []core.ResponseExtractor{
		{Variable: "early", Source: core.ExtractHeader, Selector: "X-Token"},
		{Variable: "token", Source: core.ExtractJSON, Selector: "/missing"},
	}
	for _, tc := range []struct {
		name   string
		status int
		rules  []core.ResponseExtractor
	}{
		{"not modified", http.StatusNotModified, active},
		{"client error", http.StatusBadRequest, active},
		{"server error", http.StatusInternalServerError, active},
		{"no rules", http.StatusOK, nil},
		{"disabled rules", http.StatusOK, []core.ResponseExtractor{{ID: "draft", Disabled: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Token", "private_response_secret")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, "not JSON")
			}))
			defer server.Close()
			request := core.NewRequest()
			request.URL, request.Extractors = server.URL, tc.rules
			exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}))
			if exit != ExitOK || !value.OK || value.Error != nil || value.Result == nil || value.Result.StatusCode != tc.status {
				t.Fatalf("existing status/no-rules policy changed: exit=%d, %s", exit, raw)
			}
			if values, present := cliExtractionVariables(t, raw); present {
				t.Fatalf("bypassed extraction returned variables: %v", values)
			}
		})
	}
}

func TestCLIExtractionHeaderOnlyNoContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Token", "private_response_secret")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	request := core.NewRequest()
	request.URL = server.URL
	request.Extractors = []core.ResponseExtractor{{Variable: "token", Source: core.ExtractHeader, Selector: "x-token"}}
	exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}))
	values, present := cliExtractionVariables(t, raw)
	if exit != ExitOK || !value.OK || value.Error != nil || value.Result == nil ||
		value.Result.StatusCode != http.StatusNoContent || value.Result.Body == nil || value.Result.Body.Bytes != 0 ||
		!present || !reflect.DeepEqual(values, map[string]string{"token": "private_response_secret"}) {
		t.Fatalf("header-only extraction must work with an empty 204 body: exit=%d, %s", exit, raw)
	}
}

func TestCLIExtractionRawBodyFileCapture(t *testing.T) {
	const payload = "{\n  \"token\": \"private_response_secret\", \"id\": 9007199254740993\n}\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()
	for _, tc := range []struct {
		name, selector string
		exit           ExitCode
	}{
		{"success", "/token", ExitOK},
		{"atomic failure", "/missing", ExitExecution},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := core.NewRequest()
			request.URL = server.URL
			request.Extractors = []core.ResponseExtractor{
				{Variable: "id", Source: core.ExtractJSON, Selector: "/id"},
				{Variable: "token", Source: core.ExtractJSON, Selector: tc.selector},
			}
			dir := t.TempDir()
			exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}),
				"--out-dir", dir, "--inline-body-bytes", "0")
			if exit != tc.exit || value.OK != (tc.exit == ExitOK) || value.Result == nil ||
				value.Result.StatusCode != http.StatusOK || value.Result.Body == nil {
				t.Fatalf("file capture/extraction failed: exit=%d, %s", exit, raw)
			}
			body := value.Result.Body
			if body.Mode != "file" || body.Text != nil || body.Bytes != len(payload) || body.Truncated {
				t.Fatalf("extraction changed raw body capture: %+v", body)
			}
			outputTestFile(t, body.Path, dir, []byte(payload))
			values, present := cliExtractionVariables(t, raw)
			if tc.exit == ExitOK {
				if value.Error != nil || !present || !reflect.DeepEqual(values, map[string]string{
					"id": "9007199254740993", "token": "private_response_secret",
				}) {
					t.Fatalf("extraction must use raw JSON despite file mode and Content-Type: %s", raw)
				}
			} else if value.Error == nil || value.Error.Code != "EXTRACTION_ERROR" ||
				value.Error.Message != "Response variable extraction failed." || present {
				t.Fatalf("file-mode extraction failure must remain atomic: %s", raw)
			}
		})
	}
}

func TestCLIExtractionTruncatedResponseKeepsCapture(t *testing.T) {
	payload := strings.Repeat("x", core.MaxBody+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Token", "private_response_secret")
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()
	request := core.NewRequest()
	request.URL = server.URL
	request.Extractors = []core.ResponseExtractor{{Variable: "token", Source: core.ExtractHeader, Selector: "X-Token"}}
	dir := t.TempDir()
	exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}),
		"--out-dir", dir, "--inline-body-bytes", "0")
	if exit != ExitExecution || value.OK || value.Error == nil || value.Error.Code != "EXTRACTION_ERROR" ||
		value.Error.Message != "Response variable extraction failed." || value.Result == nil || value.Result.Body == nil {
		t.Fatalf("truncation must prevent even header-only extraction: exit=%d, %s", exit, raw)
	}
	body := value.Result.Body
	if body.Mode != "file" || body.Bytes != core.MaxBody || !body.Truncated {
		t.Fatalf("truncated extraction lost bounded capture: %+v", body)
	}
	outputTestFile(t, body.Path, dir, []byte(payload[:core.MaxBody]))
	if values, present := cliExtractionVariables(t, raw); present {
		t.Fatalf("truncated response returned variables: %v", values)
	}
}

func TestCLIExtractionTransportFailureDoesNotExtract(t *testing.T) {
	const payload = `{"token":"private_response_secret"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()
	request := core.NewRequest()
	request.URL = server.URL
	request.Extractors = []core.ResponseExtractor{{Variable: "token", Source: core.ExtractJSON, Selector: "/token"}}
	exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}))
	if exit != ExitExecution || value.OK || value.Error == nil || value.Error.Code != "BODY_READ_ERROR" ||
		value.Result == nil || value.Result.Body == nil || value.Result.Body.Text == nil || *value.Result.Body.Text != payload {
		t.Fatalf("body-read error must win despite extractable partial JSON: exit=%d, %s", exit, raw)
	}
	if values, present := cliExtractionVariables(t, raw); present {
		t.Fatalf("failed transport returned extracted variables: %v", values)
	}
}

func TestCLIExtractionOutputErrorsTakePrecedence(t *testing.T) {
	const payload = `{"token":"private_response_secret"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()
	for _, cookieFailure := range []bool{false, true} {
		name := "body output failure"
		if cookieFailure {
			name = "cookie output failure"
		}
		t.Run(name, func(t *testing.T) {
			request := core.NewRequest()
			request.URL = server.URL
			request.Extractors = []core.ResponseExtractor{{Variable: "token", Source: core.ExtractJSON, Selector: "/missing"}}
			path := filepath.Join(t.TempDir(), "missing")
			args := []string{"--out-dir", path, "--inline-body-bytes", "0"}
			if cookieFailure {
				args = []string{"--cookie-jar", filepath.Join(path, "cookies.json")}
			}
			exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}), args...)
			if exit != ExitOutput || value.OK || value.Error == nil || value.Error.Code != "OUTPUT_ERROR" ||
				value.Error.Message != "Unable to write output (primary error: EXTRACTION_ERROR)." ||
				value.Result == nil || value.Result.StatusCode != http.StatusOK {
				t.Fatalf("output failure lost precedence or primary extraction error: exit=%d, %s", exit, raw)
			}
			if values, present := cliExtractionVariables(t, raw); present {
				t.Fatalf("failed extraction with output error returned variables: %v", values)
			}
			if cookieFailure {
				if value.Result.Body == nil || value.Result.Body.Text == nil || *value.Result.Body.Text != payload {
					t.Fatalf("cookie failure lost response body capture: %s", raw)
				}
			} else if value.Result.Body != nil {
				t.Fatalf("failed body output pretended to capture a body: %+v", value.Result.Body)
			}
		})
	}
}

func cliExtractionInput(t *testing.T, request core.Request, settings core.Settings) string {
	t.Helper()
	data, err := json.Marshal(struct {
		SchemaVersion int           `json:"schemaVersion"`
		Request       core.Request  `json:"request"`
		Settings      core.Settings `json:"settings"`
	}{SchemaVersion: 1, Request: request, Settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func cliExtractionVariables(t *testing.T, raw string) (map[string]string, bool) {
	t.Helper()
	var document struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		t.Fatal(err)
	}
	field, present := document.Result["extractedVariables"]
	var values map[string]string
	if present {
		if err := json.Unmarshal(field, &values); err != nil {
			t.Fatalf("invalid extractedVariables: %s, %v", field, err)
		}
	}
	return values, present
}
