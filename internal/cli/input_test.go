package cli

import (
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/echoVic/curlgo/internal/core"
)

func TestInputDefaults(t *testing.T) {
	got, fail := decodeInput([]byte(`{"schemaVersion":1,"request":{"url":"https://example.test"}}`), false)
	if fail != nil {
		t.Fatalf("unexpected failure code: %s", fail.Code)
	}
	if got.Request.URL != "https://example.test" || got.Request.Method != "GET" ||
		got.Request.Protocol != "HTTP" || got.Request.TimeoutSeconds != 30 ||
		!got.Request.FollowRedirects || !reflect.DeepEqual(got.Settings, core.Settings{}) {
		t.Fatalf("defaults changed: %+v", got)
	}
}

func TestInputExplicitValues(t *testing.T) {
	data := []byte(`{"schemaVersion":1,"request":{"url":"https://example.test","method":"PATCH","protocol":"HTTP","timeoutSeconds":3600,"followRedirects":false}}`)
	got, fail := decodeInput(data, false)
	if fail != nil {
		t.Fatalf("unexpected failure code: %s", fail.Code)
	}
	if got.Request.Method != "PATCH" || got.Request.TimeoutSeconds != 3600 || got.Request.FollowRedirects {
		t.Fatalf("explicit values replaced by defaults: %+v", got.Request)
	}
}

func TestInputResolve(t *testing.T) {
	request := map[string]any{
		"url":  " {{base}}/api/{{id}}?q={{query}} ",
		"body": `{"id":"{{id}}"}`,
		"headers": []core.Pair{
			{Key: " x-shared ", Value: "{{id}}"},
			{Key: "X-Shared", Value: "second"},
		},
		"queryParams": []core.QueryParam{{Key: "q", Value: "{{query}}", Disabled: false}},
	}
	settings := map[string]any{
		"environment": "active",
		"environments": map[string]map[string]string{
			"active":   {"base": "https://example.test", "id": "42", "query": "a&b"},
			"inactive": {"id": "wrong"},
		},
		"globalHeaders": []core.Pair{
			{Key: "X-Shared", Value: "{{overridden_missing}}"},
			{Key: "X-Global", Value: "{{id}}"},
		},
	}
	got, fail := decodeInput(inputTestJSON(t, request, settings), false)
	if fail != nil {
		t.Fatalf("unexpected failure code: %s", fail.Code)
	}
	if got.Request.URL != "https://example.test/api/42?q=a%26b" || got.Request.Body != `{"id":"42"}` {
		t.Fatalf("variables not resolved: %+v", got.Request)
	}
	wantHeaders := []core.Pair{
		{Key: "X-Global", Value: "42"},
		{Key: "x-shared", Value: "42"},
		{Key: "X-Shared", Value: "second"},
	}
	if !reflect.DeepEqual(got.Request.Headers, wantHeaders) {
		t.Fatalf("header override changed: %+v", got.Request.Headers)
	}
	if len(got.Request.QueryParams) != 1 || got.Request.QueryParams[0].Value != "{{query}}" {
		t.Fatal("query metadata changed")
	}
	wantSettings := core.Settings{
		Environment:   "active",
		Environments:  settings["environments"].(map[string]map[string]string),
		GlobalHeaders: settings["globalHeaders"].([]core.Pair),
	}
	if !reflect.DeepEqual(got.Settings, wantSettings) {
		t.Fatal("settings changed during resolution")
	}
}

func TestInputMultipart(t *testing.T) {
	// The CLI accepts only absolute local file paths; a t.TempDir()-rooted
	// path is absolute on every OS, unlike a "/..." literal on Windows.
	filePath := filepath.Join(t.TempDir(), "input-test-file")
	request := map[string]any{
		"url":      "https://example.test",
		"bodyMode": "multipart",
		"body":     "{{ignored_raw_body}}",
		"formParts": []core.FormPart{
			{Kind: core.FormText, Name: "field", Value: "{{text}}"},
			{Kind: core.FormFile, Name: "upload", FilePath: filePath},
		},
		"headers": []core.Pair{{Key: "Content-Type", Value: "incorrect"}},
	}
	settings := map[string]any{
		"environment":  "active",
		"environments": map[string]map[string]string{"active": {"text": "resolved"}},
	}
	got, fail := decodeInput(inputTestJSON(t, request, settings), false)
	if fail != nil {
		t.Fatalf("unexpected failure code: %s", fail.Code)
	}
	if got.Request.BodyMode != core.BodyMultipart || got.Request.Body != "" || len(got.Request.Headers) != 0 {
		t.Fatalf("multipart not normalized: %+v", got.Request)
	}
	if len(got.Request.FormParts) != 2 || got.Request.FormParts[0].Value != "resolved" ||
		got.Request.FormParts[1].FilePath != filePath {
		t.Fatalf("multipart fields changed: %+v", got.Request.FormParts)
	}
}

func TestInputInvalid(t *testing.T) {
	tests := []struct {
		name, data, code string
	}{
		{"empty", "", "INVALID_INPUT"},
		{"whitespace", " \n\t ", "INVALID_INPUT"},
		{"invalid_json", "{", "INVALID_INPUT"},
		{"empty_object", `{}`, "INVALID_INPUT"},
		{"null_document", `null`, "INVALID_INPUT"},
		{"array_document", `[]`, "INVALID_INPUT"},
		{"missing_version", `{"request":{"url":"https://example.test"}}`, "INVALID_INPUT"},
		{"null_version", `{"schemaVersion":null,"request":{"url":"https://example.test"}}`, "INVALID_INPUT"},
		{"version_two", `{"schemaVersion":2,"request":{"url":"https://example.test"}}`, "INVALID_INPUT"},
		{"decimal_version", `{"schemaVersion":1.0,"request":{"url":"https://example.test"}}`, "INVALID_INPUT"},
		{"string_version", `{"schemaVersion":"1","request":{"url":"https://example.test"}}`, "INVALID_INPUT"},
		{"missing_request", `{"schemaVersion":1}`, "INVALID_INPUT"},
		{"null_request", `{"schemaVersion":1,"request":null}`, "INVALID_INPUT"},
		{"array_request", `{"schemaVersion":1,"request":[]}`, "INVALID_INPUT"},
		{"unknown_top", `{"schemaVersion":1,"request":{"url":"https://example.test"},"private_marker":true}`, "INVALID_INPUT"},
		{"unknown_request", `{"schemaVersion":1,"request":{"url":"https://example.test","private_marker":true}}`, "INVALID_INPUT"},
		{"unknown_settings", `{"schemaVersion":1,"request":{"url":"https://example.test"},"settings":{"private_marker":true}}`, "INVALID_INPUT"},
		{"unknown_header", `{"schemaVersion":1,"request":{"url":"https://example.test","headers":[{"key":"X-Test","value":"x","private_marker":true}]}}`, "INVALID_INPUT"},
		{"unknown_global_header", `{"schemaVersion":1,"request":{"url":"https://example.test"},"settings":{"globalHeaders":[{"key":"X-Test","value":"x","private_marker":true}]}}`, "INVALID_INPUT"},
		{"unknown_form_part", `{"schemaVersion":1,"request":{"url":"https://example.test","bodyMode":"multipart","formParts":[{"kind":"text","name":"x","private_marker":true}]}}`, "INVALID_INPUT"},
		{"unknown_query_param", `{"schemaVersion":1,"request":{"url":"https://example.test","queryParams":[{"key":"x","value":"y","private_marker":true}]}}`, "INVALID_INPUT"},
		{"unknown_proxy", `{"schemaVersion":1,"request":{"url":"https://example.test"},"settings":{"proxy":{"private_marker":true}}}`, "INVALID_INPUT"},
		{"trailing_document", `{"schemaVersion":1,"request":{"url":"https://example.test"}} {}`, "INVALID_INPUT"},
		{"trailing_null", `{"schemaVersion":1,"request":{"url":"https://example.test"}} null`, "INVALID_INPUT"},
		{"trailing_garbage", `{"schemaVersion":1,"request":{"url":"https://example.test"}} private_marker`, "INVALID_INPUT"},
		{"sse", `{"schemaVersion":1,"request":{"url":"https://example.test","protocol":"SSE"}}`, "INVALID_INPUT"},
		{"websocket", `{"schemaVersion":1,"request":{"url":"wss://example.test","protocol":"WebSocket"}}`, "INVALID_INPUT"},
		{"unknown_protocol", `{"schemaVersion":1,"request":{"url":"https://example.test","protocol":"private_marker"}}`, "INVALID_INPUT"},
		{"empty_protocol", `{"schemaVersion":1,"request":{"url":"https://example.test","protocol":""}}`, "INVALID_INPUT"},
		{"timeout_zero", `{"schemaVersion":1,"request":{"url":"https://example.test","timeoutSeconds":0}}`, "INVALID_REQUEST"},
		{"timeout_large", `{"schemaVersion":1,"request":{"url":"https://example.test","timeoutSeconds":3601}}`, "INVALID_REQUEST"},
		{"empty_method", `{"schemaVersion":1,"request":{"url":"https://example.test","method":""}}`, "INVALID_REQUEST"},
		{"bad_header_name", `{"schemaVersion":1,"request":{"url":"https://example.test","headers":[{"key":"private marker","value":"x"}]}}`, "INVALID_REQUEST"},
		{"bad_header_value", `{"schemaVersion":1,"request":{"url":"https://example.test","headers":[{"key":"X-Test","value":"private_marker\r\nsecret"}]}}`, "INVALID_REQUEST"},
		{"reserved_header", `{"schemaVersion":1,"request":{"url":"https://example.test","headers":[{"key":"Content-Length","value":"0"}]}}`, "INVALID_REQUEST"},
		{"bad_url", `{"schemaVersion":1,"request":{"url":"/private_marker/token"}}`, "INVALID_REQUEST"},
		{"empty_request", `{"schemaVersion":1,"request":{}}`, "INVALID_REQUEST"},
		{"bad_body_mode", `{"schemaVersion":1,"request":{"url":"https://example.test","bodyMode":"private_marker"}}`, "INVALID_REQUEST"},
		{"bad_form_part", `{"schemaVersion":1,"request":{"url":"https://example.test","bodyMode":"multipart","formParts":[{"kind":"file","name":"private_marker","filePath":"relative"}]}}`, "INVALID_REQUEST"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inputTestFailure(t, []byte(tc.data), false, tc.code)
		})
	}
}

func TestInputUnresolvedVariables(t *testing.T) {
	messages := make(map[string]string)
	for _, field := range []string{"url", "header", "global_header", "body", "form_field"} {
		t.Run(field, func(t *testing.T) {
			request := map[string]any{"url": "https://example.test"}
			settings := map[string]any{
				"environment":  "active",
				"environments": map[string]map[string]string{"active": {}, "inactive": {"private_marker": "unused"}},
			}
			switch field {
			case "url":
				request["url"] = "https://example.test/{{private_marker}}"
			case "header":
				request["headers"] = []core.Pair{{Key: "Authorization", Value: "{{private_marker}}"}}
			case "global_header":
				settings["globalHeaders"] = []core.Pair{{Key: "Authorization", Value: "{{private_marker}}"}}
			case "body":
				request["body"] = "{{private_marker}}"
			case "form_field":
				request["bodyMode"] = "multipart"
				request["formParts"] = []core.FormPart{{Kind: core.FormText, Name: "field", Value: "{{private_marker}}"}}
			}
			message := inputTestFailure(t, inputTestJSON(t, request, settings), false, "UNRESOLVED_VARIABLE")
			if previous, ok := messages["unresolved"]; ok && message != previous {
				t.Fatal("unresolved-variable message varies by field")
			}
			messages["unresolved"] = message
		})
	}
	inputTestFailure(t, []byte(`curl 'https://example.test/{{private_marker}}'`), true, "UNRESOLVED_VARIABLE")
}

func TestInputCURLSafe(t *testing.T) {
	for _, tc := range []struct {
		name, command, body string
		multipart           bool
		redirects           bool
	}{
		{"literal_shell_chars", `curl https://example.test --data-raw '$literal;|&'`, "$literal;|&", false, false},
		{"literal_at_text", `curl https://example.test --data-raw '@text'`, "@text", false, false},
		{"form_string", `curl https://example.test --form-string 'field=$literal;|&'`, "$literal;|&", true, false},
		{"form_string_at_text", `curl https://example.test --form-string 'field=@text'`, "@text", true, false},
		{"location", `curl -L https://example.test --data-raw text`, "text", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, fail := decodeInput([]byte(tc.command), true)
			if fail != nil {
				t.Fatalf("unexpected failure code: %s", fail.Code)
			}
			if got.Request.Method != "POST" || got.Request.Protocol != "HTTP" ||
				got.Request.URL != "https://example.test" || got.Request.TimeoutSeconds != 30 ||
				got.Request.FollowRedirects != tc.redirects {
				t.Fatalf("curl request changed: %+v", got.Request)
			}
			if tc.multipart {
				if got.Request.BodyMode != core.BodyMultipart || len(got.Request.FormParts) != 1 ||
					got.Request.FormParts[0].Kind != core.FormText || got.Request.FormParts[0].Value != tc.body {
					t.Fatalf("literal form changed: %+v", got.Request)
				}
			} else if got.Request.Body != tc.body {
				t.Fatalf("literal body changed: %q", got.Request.Body)
			}
		})
	}
}

func TestInputCURLReject(t *testing.T) {
	commands := []string{
		"",
		"not-curl private_marker",
		`curl https://example.test ; private_marker`,
		`curl https://example.test | private_marker`,
		`curl https://example.test & private_marker`,
		`curl https://example.test --data-raw $TOKEN`,
		`curl https://example.test --data-raw $(id)`,
		"curl https://example.test --data-raw `id`",
		`curl https://example.test --data @/private_marker/token`,
		`curl https://example.test -F upload=@/private_marker/token`,
		`curl https://example.test --insecure`,
		`curl https://example.test --private_marker`,
		`curl https://example.test --data-raw "unterminated`,
	}
	var message string
	for _, command := range commands {
		got := inputTestFailure(t, []byte(command), true, "INVALID_CURL")
		if message != "" && got != message {
			t.Fatal("curl message varies by parser error")
		}
		message = got
	}
	inputTestFailure(t, []byte(`curl https://example.test -X ''`), true, "INVALID_REQUEST")
	inputTestFailure(t, []byte(`curl /private_marker/token`), true, "INVALID_REQUEST")
}

func TestInputSizeLimit(t *testing.T) {
	const document = `{"schemaVersion":1,"request":{"url":"https://example.test"}}`
	atLimit := []byte(document + strings.Repeat(" ", core.MaxInput-len(document)))
	if _, fail := decodeInput(atLimit, false); fail != nil {
		t.Fatalf("limit-sized input rejected: %s", fail.Code)
	}
	for _, asCurl := range []bool{false, true} {
		inputTestFailure(t, append(atLimit, ' '), asCurl, "INVALID_INPUT")
	}
}

func TestInputReadLimited(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		limit      int
		wantError  bool
		wantRead   int
	}{
		{"empty", "", 4, false, 0},
		{"below_limit", "abc", 4, false, 3},
		{"at_limit", "abcd", 4, false, 4},
		{"over_limit", "abcdefghi", 4, true, 5},
		{"zero_empty", "", 0, false, 0},
		{"zero_nonempty", "abc", 0, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &inputTestReader{Reader: strings.NewReader(tc.text)}
			got, err := readLimited(reader, tc.limit)
			if (err != nil) != tc.wantError {
				t.Fatalf("error presence = %v, want %v", err != nil, tc.wantError)
			}
			if reader.read != tc.wantRead {
				t.Fatalf("read %d bytes, want %d", reader.read, tc.wantRead)
			}
			if !tc.wantError && string(got) != tc.text {
				t.Fatalf("read content changed: %q", got)
			}
		})
	}
	sentinel := errors.New("reader failure")
	reader := &inputTestReader{Reader: strings.NewReader("abc"), err: sentinel}
	if _, err := readLimited(reader, 4); !errors.Is(err, sentinel) {
		t.Fatal("reader error not preserved")
	}
}

func TestInputDecodeOne(t *testing.T) {
	type document struct {
		Value int `json:"value"`
	}
	for _, text := range []string{`{"value":1}`, " \n{\"value\":1}\t "} {
		var got document
		if err := decodeOne([]byte(text), &got); err != nil || got.Value != 1 {
			t.Fatal("single valid document rejected")
		}
	}
	for _, text := range []string{"", "{", `{"value":1,"private_marker":true}`, `{"value":1} {}`, `{"value":1} null`, `{"value":1} private_marker`} {
		var got document
		if err := decodeOne([]byte(text), &got); err == nil {
			t.Fatal("invalid or multiple documents accepted")
		}
	}
}

func TestInputFailureContract(t *testing.T) {
	codes := []ExitCode{ExitOK, ExitInput, ExitExecution, ExitOutput}
	for i, code := range codes {
		if int(code) != i {
			t.Fatalf("exit code %d = %d", i, code)
		}
	}
	got := inputFailure("INVALID_INPUT", "Invalid input.")
	if got.Code != "INVALID_INPUT" || got.Message != "Invalid input." || got.Exit != ExitInput {
		t.Fatal("input failure contract changed")
	}
}

func inputTestJSON(t *testing.T, request, settings map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{"schemaVersion": 1, "request": request, "settings": settings})
	if err != nil {
		t.Fatal("could not encode test fixture")
	}
	return data
}

func inputTestFailure(t *testing.T, data []byte, asCurl bool, code string) string {
	t.Helper()
	_, fail := decodeInput(data, asCurl)
	if fail == nil {
		t.Fatalf("expected failure code %s", code)
	}
	if fail.Code != code || fail.Exit != ExitInput {
		t.Fatalf("failure code/exit = %s/%d, want %s/%d", fail.Code, fail.Exit, code, ExitInput)
	}
	if fail.Message == "" || strings.Contains(fail.Message, "private_marker") ||
		strings.Contains(fail.Message, "TOKEN") || strings.Contains(fail.Message, "example.test") {
		t.Fatal("failure message empty or leaks input details")
	}
	return fail.Message
}

type inputTestReader struct {
	io.Reader
	read int
	err  error
}

func (r *inputTestReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += n
	if r.err != nil {
		return n, r.err
	}
	return n, err
}
