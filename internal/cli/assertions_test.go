package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/echoVic/curlgo/internal/core"
)

func TestCLIAssertionsFailureRetainsResponseAndBlocksExtraction(t *testing.T) {
	const payload = `{"token":"private_actual_secret","value":"actual"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Token", "private_header_secret")
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "captured", Path: "/"})
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()
	request := core.NewRequest()
	request.URL = server.URL
	request.Assertions = []core.ResponseAssertion{
		{ID: "status", Name: "Status", Source: core.AssertStatus, Operator: core.AssertEquals, Expected: "201"},
		{ID: "draft", Disabled: true},
		{ID: "header", Source: core.AssertHeader, Operator: core.AssertEquals, Selector: "X-Token", Expected: "private_expected_secret"},
		{ID: "json", Source: core.AssertJSON, Operator: core.AssertExists, Selector: "/private_selector_secret"},
		{ID: "body", Source: core.AssertBody, Operator: core.AssertContains, Expected: "private_body_secret"},
		{ID: "present", Source: core.AssertJSON, Operator: core.AssertExists, Selector: "/value"},
	}
	request.Extractors = []core.ResponseExtractor{{Variable: "token", Source: core.ExtractHeader, Selector: "X-Token"}}
	jar := filepath.Join(t.TempDir(), "cookies.json")
	exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}), "--cookie-jar", jar)
	cliIntegrationFailure(t, exit, value, ExitAssertion, "ASSERTION_FAILED")
	if value.Error.Message != "Response assertions failed." {
		t.Fatalf("unsafe assertion error: %+v", value.Error)
	}
	if value.Result == nil || value.Result.StatusCode != http.StatusOK || value.Result.Body == nil ||
		value.Result.Body.Text == nil || *value.Result.Body.Text != payload ||
		value.Result.Headers.Get("X-Token") != "private_header_secret" {
		t.Fatalf("assertion failure lost captured response: %s", raw)
	}
	report, present := cliAssertionReport(t, raw)
	if !present || report.Passed || len(report.Results) != 5 {
		t.Fatalf("missing complete failed assertion report: %+v", report)
	}
	for i, code := range []string{"MISMATCH", "MISMATCH", "MISSING_VALUE", "MISMATCH", "PASSED"} {
		wantIndex := i
		if i > 0 {
			wantIndex++
		}
		got := report.Results[i]
		if got.Index != wantIndex || got.ID != request.Assertions[wantIndex].ID ||
			got.Name != request.Assertions[wantIndex].Name || got.Code != code || got.Passed != (code == "PASSED") {
			t.Errorf("result %d = %+v, want index %d / %s", i, got, wantIndex, code)
		}
	}
	feedback, err := json.Marshal(struct {
		Error  *errorValue
		Report core.AssertionReport
	}{value.Error, report})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{
		"private_actual_secret", "private_header_secret", "private_expected_secret", "private_selector_secret", "private_body_secret",
	} {
		if strings.Contains(string(feedback), secret) {
			t.Fatalf("assertion feedback leaked secret %q", secret)
		}
	}
	if values, present := cliExtractionVariables(t, raw); present {
		t.Fatalf("failed assertions returned extracted variables: %v", values)
	}
	cookies, err := core.LoadCookieFile(jar)
	if err != nil || len(cookies) != 1 || cookies[0].Cookie.Value != "captured" {
		t.Fatalf("assertion failure lost cookies: %+v, %v", cookies, err)
	}
}

func TestCLIAssertionsSuccessAllowsExtractionAndLiteralExpectations(t *testing.T) {
	const payload = `{"token":"private_response_secret","id":9007199254740993,"literal":"{{literal}}","active":true,"optional":null}`
	seen := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/protected" {
			seen <- r.Header.Get("Authorization")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Literal", "{{literal}}")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()
	request := core.NewRequest()
	request.URL = server.URL
	request.Assertions = []core.ResponseAssertion{
		{ID: "status", Source: core.AssertStatus, Operator: core.AssertEquals, Expected: "201"},
		{ID: "header", Source: core.AssertHeader, Operator: core.AssertEquals, Selector: "x-literal", Expected: "{{literal}}"},
		{ID: "number", Source: core.AssertJSON, Operator: core.AssertEquals, Selector: "/id", Expected: "9007199254740993.0"},
		{ID: "string", Source: core.AssertJSON, Operator: core.AssertEquals, Selector: "/literal", Expected: `"{{literal}}"`},
		{ID: "contains", Source: core.AssertJSON, Operator: core.AssertContains, Selector: "/literal", Expected: `"literal"`},
		{ID: "boolean", Source: core.AssertJSON, Operator: core.AssertEquals, Selector: "/active", Expected: "true"},
		{ID: "null", Source: core.AssertJSON, Operator: core.AssertEquals, Selector: "/optional", Expected: "null"},
		{ID: "body", Source: core.AssertBody, Operator: core.AssertEquals, Expected: payload},
	}
	request.Extractors = []core.ResponseExtractor{{Variable: "token", Source: core.ExtractJSON, Selector: "/token"}}
	settings := core.Settings{Environment: "explicit", Environments: map[string]map[string]string{
		"explicit": {"literal": "private_unexpanded_secret"},
	}}
	exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, settings))
	report, present := cliAssertionReport(t, raw)
	values, extracted := cliExtractionVariables(t, raw)
	if exit != ExitOK || !value.OK || value.Error != nil || !present || !report.Passed ||
		len(report.Results) != len(request.Assertions) || !extracted ||
		!reflect.DeepEqual(values, map[string]string{"token": "private_response_secret"}) {
		t.Fatalf("passing assertions must allow extraction: exit=%d, %s", exit, raw)
	}
	for i, result := range report.Results {
		if result.Index != i || result.ID != request.Assertions[i].ID || !result.Passed || result.Code != "PASSED" {
			t.Errorf("passed result %d = %+v", i, result)
		}
	}
	if value.Result == nil || value.Result.Body == nil || value.Result.Body.Text == nil || *value.Result.Body.Text != payload ||
		strings.Contains(raw, "private_unexpanded_secret") {
		t.Fatalf("literal assertions changed capture or expanded expectations: %s", raw)
	}
	next := core.NewRequest()
	next.URL = server.URL + "/protected"
	next.Auth = core.Authentication{Mode: core.AuthBearer, Token: "{{token}}"}
	settings.Environments["explicit"] = values
	exit, value, raw = invokeCLI(t, t.Context(), cliExtractionInput(t, next, settings))
	if exit != ExitOK || !value.OK || value.Result == nil || value.Result.StatusCode != http.StatusNoContent {
		t.Fatalf("explicit reuse after assertion pass failed: exit=%d, %s", exit, raw)
	}
	if got := <-seen; got != "Bearer private_response_secret" {
		t.Fatalf("extracted token reuse = %q", got)
	}
}

func TestCLIAssertionsNon2xxEvaluatedWithoutExtraction(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Token", "private_response_secret")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "not JSON")
			}))
			defer server.Close()
			for _, expected := range []int{status, http.StatusOK} {
				request := core.NewRequest()
				request.URL = server.URL
				request.Assertions = []core.ResponseAssertion{{Source: core.AssertStatus, Operator: core.AssertEquals, Expected: strconv.Itoa(expected)}}
				request.Extractors = []core.ResponseExtractor{
					{Variable: "early", Source: core.ExtractHeader, Selector: "X-Token"},
					{Variable: "token", Source: core.ExtractJSON, Selector: "/missing"},
				}
				exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}))
				report, present := cliAssertionReport(t, raw)
				if !present || len(report.Results) != 1 || report.Passed != (expected == status) ||
					value.Result == nil || value.Result.StatusCode != status {
					t.Fatalf("non-2xx response must evaluate assertions: exit=%d, %s", exit, raw)
				}
				if expected == status {
					if exit != ExitOK || !value.OK || value.Error != nil || report.Results[0].Code != "PASSED" {
						t.Fatalf("expected error status must pass assertions: exit=%d, %s", exit, raw)
					}
				} else {
					cliIntegrationFailure(t, exit, value, ExitAssertion, "ASSERTION_FAILED")
				}
				if values, present := cliExtractionVariables(t, raw); present {
					t.Fatalf("non-2xx assertions allowed extraction: %v", values)
				}
			}
		})
	}
}

func TestCLIAssertionsAbsentOrDisabledPreserveStatusPolicy(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "not JSON")
			}))
			defer server.Close()
			for _, rules := range [][]core.ResponseAssertion{nil, {{ID: "draft", Disabled: true}}} {
				request := core.NewRequest()
				request.URL, request.Assertions = server.URL, rules
				exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}))
				if exit != ExitOK || !value.OK || value.Error != nil || value.Result == nil || value.Result.StatusCode != status {
					t.Fatalf("no active assertion rules changed status policy: exit=%d, %s", exit, raw)
				}
				if report, present := cliAssertionReport(t, raw); present {
					t.Fatalf("no active rules must omit assertions: %+v", report)
				}
			}
		})
	}
}

func TestCLIAssertionsPassRetainsExtractionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"token":"private_response_secret"}`)
	}))
	defer server.Close()
	request := core.NewRequest()
	request.URL = server.URL
	request.Assertions = []core.ResponseAssertion{{Source: core.AssertStatus, Operator: core.AssertEquals, Expected: "200"}}
	request.Extractors = []core.ResponseExtractor{{Variable: "token", Source: core.ExtractJSON, Selector: "/missing"}}
	exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}))
	cliIntegrationFailure(t, exit, value, ExitExecution, "EXTRACTION_ERROR")
	report, present := cliAssertionReport(t, raw)
	if !present || !report.Passed || len(report.Results) != 1 || value.Result == nil || value.Result.Body == nil {
		t.Fatalf("extraction error lost passing assertion report or response: %s", raw)
	}
	if values, present := cliExtractionVariables(t, raw); present {
		t.Fatalf("extraction failure returned variables: %v", values)
	}
}

func TestCLIAssertionsEvaluateRawBodyFile(t *testing.T) {
	const payload = "{\n \"id\": 9007199254740993, \"token\": \"private_response_secret\"\n}\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()
	for _, expected := range []string{"9007199254740993", "9007199254740994"} {
		t.Run(expected, func(t *testing.T) {
			request := core.NewRequest()
			request.URL = server.URL
			request.Assertions = []core.ResponseAssertion{
				{Source: core.AssertJSON, Operator: core.AssertEquals, Selector: "/id", Expected: expected},
				{Source: core.AssertBody, Operator: core.AssertEquals, Expected: payload},
			}
			request.Extractors = []core.ResponseExtractor{{Variable: "token", Source: core.ExtractJSON, Selector: "/token"}}
			dir := t.TempDir()
			exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}),
				"--out-dir", dir, "--inline-body-bytes", "0")
			report, present := cliAssertionReport(t, raw)
			passed := expected == "9007199254740993"
			if !present || report.Passed != passed || len(report.Results) != 2 || !report.Results[1].Passed ||
				value.Result == nil || value.Result.Body == nil {
				t.Fatalf("assertions must evaluate complete raw body before formatting: exit=%d, %s", exit, raw)
			}
			body := value.Result.Body
			if body.Mode != "file" || body.Text != nil || body.Truncated || body.Bytes != len(payload) {
				t.Fatalf("assertions changed file capture: %+v", body)
			}
			outputTestFile(t, body.Path, dir, []byte(payload))
			values, extracted := cliExtractionVariables(t, raw)
			if passed {
				if exit != ExitOK || !value.OK || value.Error != nil || !extracted || values["token"] != "private_response_secret" {
					t.Fatalf("raw file assertion pass must allow extraction: exit=%d, %s", exit, raw)
				}
			} else {
				cliIntegrationFailure(t, exit, value, ExitAssertion, "ASSERTION_FAILED")
				if extracted {
					t.Fatalf("raw file assertion failure returned variables: %v", values)
				}
			}
		})
	}
}

func TestCLIAssertionsTruncatedResponseFailsEveryRule(t *testing.T) {
	payload := strings.Repeat("x", core.MaxBody+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Token", "private_response_secret")
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()
	request := core.NewRequest()
	request.URL = server.URL
	request.Assertions = []core.ResponseAssertion{
		{Source: core.AssertStatus, Operator: core.AssertEquals, Expected: "200"},
		{Source: core.AssertHeader, Operator: core.AssertExists, Selector: "X-Token"},
		{Source: core.AssertJSON, Operator: core.AssertExists},
		{Source: core.AssertBody, Operator: core.AssertContains, Expected: "x"},
	}
	request.Extractors = []core.ResponseExtractor{{Variable: "token", Source: core.ExtractHeader, Selector: "X-Token"}}
	dir := t.TempDir()
	exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}), "--out-dir", dir, "--inline-body-bytes", "0")
	cliIntegrationFailure(t, exit, value, ExitAssertion, "ASSERTION_FAILED")
	report, present := cliAssertionReport(t, raw)
	if !present || report.Passed || len(report.Results) != 4 || value.Result == nil || value.Result.Body == nil ||
		!value.Result.Body.Truncated || value.Result.Body.Bytes != core.MaxBody {
		t.Fatalf("truncation lost all-rule failure or bounded capture: %s", raw)
	}
	for _, result := range report.Results {
		if result.Passed || result.Code != "TRUNCATED_RESPONSE" {
			t.Errorf("truncated result = %+v", result)
		}
	}
	outputTestFile(t, value.Result.Body.Path, dir, []byte(payload[:core.MaxBody]))
	if values, present := cliExtractionVariables(t, raw); present {
		t.Fatalf("truncated assertions returned variables: %v", values)
	}
}

func TestCLIAssertionsTransportFailureIsNotEvaluated(t *testing.T) {
	const payload = `{"token":"private_response_secret"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()
	request := core.NewRequest()
	request.URL = server.URL
	request.Assertions = []core.ResponseAssertion{
		{Source: core.AssertStatus, Operator: core.AssertEquals, Expected: "200"},
		{Source: core.AssertJSON, Operator: core.AssertExists, Selector: "/token"},
	}
	request.Extractors = []core.ResponseExtractor{{Variable: "token", Source: core.ExtractJSON, Selector: "/token"}}
	exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}))
	cliIntegrationFailure(t, exit, value, ExitExecution, "BODY_READ_ERROR")
	if value.Result == nil || value.Result.Body == nil || value.Result.Body.Text == nil || *value.Result.Body.Text != payload {
		t.Fatalf("body-read error lost partial capture: %s", raw)
	}
	if report, present := cliAssertionReport(t, raw); present {
		t.Fatalf("partial response evaluated assertions: %+v", report)
	}
	if values, present := cliExtractionVariables(t, raw); present {
		t.Fatalf("partial response returned variables: %v", values)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	exit, value, raw = invokeCLI(t, ctx, cliExtractionInput(t, request, core.Settings{}))
	cliIntegrationFailure(t, exit, value, ExitExecution, "CANCELED")
	if value.Result != nil {
		t.Fatalf("pre-canceled request returned a response: %s", raw)
	}
	if report, present := cliAssertionReport(t, raw); present {
		t.Fatalf("canceled request evaluated assertions: %+v", report)
	}
	server.Close()
	exit, value, raw = invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}))
	cliIntegrationFailure(t, exit, value, ExitExecution, "CONNECT_ERROR")
	if value.Result != nil {
		t.Fatalf("connection failure returned a response: %s", raw)
	}
}

func TestCLIAssertionsOutputErrorsKeepPrimaryFailure(t *testing.T) {
	const payload = `{"token":"private_response_secret"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "captured", Path: "/"})
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()
	for _, cookieFailure := range []bool{false, true} {
		t.Run(strconv.FormatBool(cookieFailure), func(t *testing.T) {
			request := core.NewRequest()
			request.URL = server.URL
			request.Assertions = []core.ResponseAssertion{{Source: core.AssertStatus, Operator: core.AssertEquals, Expected: "201"}}
			request.Extractors = []core.ResponseExtractor{{Variable: "token", Source: core.ExtractJSON, Selector: "/token"}}
			missing := filepath.Join(t.TempDir(), "missing")
			args := []string{"--out-dir", missing, "--inline-body-bytes", "0"}
			if cookieFailure {
				args = []string{"--cookie-jar", filepath.Join(missing, "cookies.json")}
			} else {
				args = append(args, "--cookie-jar", filepath.Join(t.TempDir(), "cookies.json"))
			}
			exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}), args...)
			cliIntegrationFailure(t, exit, value, ExitOutput, "OUTPUT_ERROR")
			report, present := cliAssertionReport(t, raw)
			if value.Error.Message != "Unable to write output (primary error: ASSERTION_FAILED)." ||
				!present || report.Passed || len(report.Results) != 1 || value.Result == nil || value.Result.StatusCode != 200 {
				t.Fatalf("output error lost assertion primary error/report/response: %s", raw)
			}
			if values, present := cliExtractionVariables(t, raw); present {
				t.Fatalf("assertion and output failure returned variables: %v", values)
			}
			if cookieFailure {
				if value.Result.Body == nil || value.Result.Body.Text == nil || *value.Result.Body.Text != payload {
					t.Fatalf("cookie write failure lost body capture: %s", raw)
				}
			} else {
				if value.Result.Body != nil {
					t.Fatalf("failed body output pretended to retain a body: %+v", value.Result.Body)
				}
				cookies, err := core.LoadCookieFile(args[len(args)-1])
				if err != nil || len(cookies) != 1 || cookies[0].Cookie.Value != "captured" {
					t.Fatalf("body output failure skipped cookie save: %+v, %v", cookies, err)
				}
			}
		})
	}
}

func TestCLIAssertionsInvalidResponseReportsWithoutSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Add("X-Repeat", "private_actual_secret")
		w.Header().Add("X-Repeat", "second")
		_, _ = w.Write([]byte{0xff})
	}))
	defer server.Close()
	request := core.NewRequest()
	request.URL = server.URL
	request.Assertions = []core.ResponseAssertion{
		{Source: core.AssertStatus, Operator: core.AssertEquals, Expected: "200"},
		{Source: core.AssertHeader, Operator: core.AssertEquals, Selector: "x-repeat", Expected: "private_expected_secret"},
		{Source: core.AssertHeader, Operator: core.AssertExists, Selector: "X-Repeat"},
		{Source: core.AssertJSON, Operator: core.AssertExists, Selector: "/private_selector_secret"},
		{Source: core.AssertBody, Operator: core.AssertEquals, Expected: "private_body_secret"},
	}
	exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}), "--out-dir", t.TempDir())
	cliIntegrationFailure(t, exit, value, ExitAssertion, "ASSERTION_FAILED")
	report, present := cliAssertionReport(t, raw)
	if !present || report.Passed || len(report.Results) != len(request.Assertions) {
		t.Fatalf("invalid response lost assertion outcomes: %s", raw)
	}
	for i, code := range []string{"PASSED", "AMBIGUOUS_HEADER", "PASSED", "INVALID_JSON", "INVALID_BODY"} {
		if report.Results[i].Code != code || report.Results[i].Passed != (code == "PASSED") {
			t.Errorf("result %d = %+v, want %s", i, report.Results[i], code)
		}
	}
}

func TestCLIAssertionsDryRunValidatesWithoutResultsOrNetwork(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, "unexpected")
	}))
	defer server.Close()
	request := core.NewRequest()
	request.URL = server.URL
	request.Assertions = []core.ResponseAssertion{{Source: core.AssertStatus, Operator: core.AssertEquals, Expected: "599"}}
	jar := filepath.Join(t.TempDir(), "cookies.json")
	exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}), "--dry-run", "--cookie-jar", jar)
	if exit != ExitOK || !value.OK || value.Validation == nil || !value.Validation.DryRun || value.Result != nil ||
		value.Error != nil || strings.Contains(raw, `"assertions"`) || calls.Load() != 0 {
		t.Fatalf("dry-run must validate without evaluating assertions or sending: exit=%d, %s", exit, raw)
	}
	for _, dryRun := range []bool{false, true} {
		for _, rules := range [][]core.ResponseAssertion{
			{{Source: core.AssertStatus, Operator: core.AssertEquals, Expected: "0200"}},
			{{Source: core.AssertJSON, Operator: core.AssertEquals, Selector: "/private_selector_secret", Expected: "private_expected_secret"}},
			{{Source: core.AssertJSON, Operator: core.AssertExists, Selector: "private_selector_secret"}},
			{{ID: "same", Disabled: true}, {ID: "same", Disabled: true}},
			{{Disabled: true, Expected: strings.Repeat("x", 65537)}},
		} {
			request.Assertions = rules
			var args []string
			if dryRun {
				args = append(args, "--dry-run")
			}
			exit, value, raw := invokeCLI(t, t.Context(), cliExtractionInput(t, request, core.Settings{}), args...)
			cliIntegrationFailure(t, exit, value, ExitInput, "INVALID_REQUEST")
			if value.Result != nil || value.Validation != nil || strings.Contains(raw, "private_") || calls.Load() != 0 {
				t.Fatalf("invalid assertions leaked values or reached transport: exit=%d, %s", exit, raw)
			}
		}
	}
	invalidJSON := `{"schemaVersion":1,"request":{"url":` + strconv.Quote(server.URL) +
		`,"assertions":[{"source":"status","operator":"equals","expected":"200","script":"private_script_secret"}]}}`
	exit, value, raw = invokeCLI(t, t.Context(), invalidJSON, "--dry-run")
	cliIntegrationFailure(t, exit, value, ExitInput, "INVALID_INPUT")
	if value.Result != nil || value.Validation != nil || calls.Load() != 0 || strings.Contains(raw, "private_script_secret") {
		t.Fatalf("unsupported assertion fields reached transport or leaked: %s", raw)
	}
}

func TestCLIAssertionsHelpExitCode(t *testing.T) {
	exit, value, raw := runTestInvoke(t, nil, "--help")
	if exit != ExitOK || !value.OK || value.Help == nil || len(value.Help.ExitCodes) != 5 {
		t.Fatalf("help must describe all five exit codes: exit=%d, %s", exit, raw)
	}
	entry := value.Help.ExitCodes[4]
	if entry.Code != ExitAssertion || entry.Meaning == "" {
		t.Fatalf("assertion failure exit code is missing: %+v", entry)
	}
}

func cliAssertionReport(t *testing.T, raw string) (core.AssertionReport, bool) {
	t.Helper()
	var document struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		t.Fatal(err)
	}
	field, present := document.Result["assertions"]
	var report core.AssertionReport
	if present {
		decoder := json.NewDecoder(strings.NewReader(string(field)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&report); err != nil {
			t.Fatalf("invalid assertion report: %s, %v", field, err)
		}
		if reflect.DeepEqual(report, core.AssertionReport{}) {
			t.Fatal("assertions must not be null or an empty report")
		}
	}
	return report, present
}
