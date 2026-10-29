package core

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestExtractScalarVariables(t *testing.T) {
	rules := []ResponseExtractor{
		{Variable: "token", Source: ExtractJSON, Selector: "/data/access_token"},
		{Variable: "id", Source: ExtractJSON, Selector: "/data/items/0/id"},
		{Variable: "escaped", Source: ExtractJSON, Selector: "/a~1b/~0/中文"},
		{Variable: "empty", Source: ExtractJSON, Selector: "/"},
		{Variable: "flag", Source: ExtractJSON, Selector: "/flag"},
		{Variable: "request_id", Source: ExtractHeader, Selector: "x-request-id"},
	}
	body := []byte(`{"data":{"access_token":"secret","items":[{"id":9007199254740993}]},"a/b":{"~":{"中文":"值"}},"":"","flag":false}`)
	headers := http.Header{"X-Request-ID": {"trace"}}
	got, err := ExtractVariables(rules, headers, body, false)
	want := map[string]string{"token": "secret", "id": "9007199254740993", "escaped": "值", "empty": "", "flag": "false", "request_id": "trace"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, %v; want %v", got, err, want)
	}
	for _, scalar := range []string{`"root"`, `1.2300e+20`, `true`} {
		got, err := ExtractVariables([]ResponseExtractor{{Variable: "root", Source: ExtractJSON}}, nil, []byte(scalar), false)
		if err != nil || got["root"] != strings.Trim(scalar, `"`) {
			t.Fatalf("root scalar: %v, %v", got, err)
		}
	}
}

func TestExtractFailureIsAtomicAndPrivate(t *testing.T) {
	for _, tc := range []struct {
		name, selector, body string
	}{
		{"missing", "/missing", `{"secret":"private"}`},
		{"null", "/secret", `{"secret":null}`},
		{"object", "/secret", `{"secret":{}}`},
		{"array", "/secret", `{"secret":[]}`},
		{"leading zero", "/secret/00", `{"secret":["private"]}`},
		{"negative", "/secret/-1", `{"secret":["private"]}`},
		{"append", "/secret/-", `{"secret":["private"]}`},
		{"overflow", "/secret/999999999999999999999999", `{"secret":["private"]}`},
		{"out of bounds", "/secret/2", `{"secret":["private"]}`},
		{"invalid json", "/secret", `{"secret":"private"`},
		{"trailing json", "/secret", `{"secret":"private"} {}`},
		{"invalid utf8", "/secret", "{\"secret\":\"private\xff\"}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rules := []ResponseExtractor{
				{Variable: "trace", Source: ExtractHeader, Selector: "X-Trace"},
				{Variable: "token", Source: ExtractJSON, Selector: tc.selector},
			}
			got, err := ExtractVariables(rules, http.Header{"X-Trace": {"private-header"}}, []byte(tc.body), false)
			var extractionErr *ExtractionError
			if got != nil || !errors.As(err, &extractionErr) || strings.Contains(err.Error(), "private") {
				t.Fatalf("partial results or unsafe error: %v, %v", got, err)
			}
		})
	}
	for name, h := range map[string]http.Header{
		"missing": nil, "empty": {"X-Trace": {}}, "repeated": {"X-Trace": {"one", "two"}},
		"casing ambiguity": {"X-Trace": {"one"}, "x-trace": {"two"}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ExtractVariables([]ResponseExtractor{{Variable: "trace", Source: ExtractHeader, Selector: "X-Trace"}}, h, nil, false)
			if got != nil || err == nil {
				t.Fatalf("ambiguous/missing header accepted: %v, %v", got, err)
			}
		})
	}
	rules := []ResponseExtractor{{Variable: "trace", Source: ExtractHeader, Selector: "X-Trace"}}
	got, err := ExtractVariables(rules, http.Header{"x-trace": {""}}, nil, false)
	if err != nil || got["trace"] != "" || len(got) != 1 {
		t.Fatalf("empty header on 204 failed: %v, %v", got, err)
	}
	got, err = ExtractVariables(rules, http.Header{"X-Trace": {"one"}}, nil, true)
	if got != nil || err == nil {
		t.Fatal("truncated response accepted")
	}
	got, err = ExtractVariables(nil, nil, []byte("invalid"), true)
	if got != nil || err != nil {
		t.Fatal("unconfigured extraction changed behavior")
	}
}

func TestValidateExtractors(t *testing.T) {
	valid := []ResponseExtractor{{ID: "rule-1", Variable: "access.token-1", Source: ExtractJSON, Selector: "/a~01"}}
	if err := ValidateExtractors(valid); err != nil {
		t.Fatal(err)
	}
	for name, rules := range map[string][]ResponseExtractor{
		"bad name":              {{Variable: "1token", Source: ExtractJSON}},
		"spaces":                {{Variable: "token value", Source: ExtractJSON}},
		"source":                {{Variable: "token", Source: "script"}},
		"pointer":               {{Variable: "token", Source: ExtractJSON, Selector: "$.token"}},
		"escape":                {{Variable: "token", Source: ExtractJSON, Selector: "/~2"}},
		"header":                {{Variable: "token", Source: ExtractHeader, Selector: "Bad\nHeader"}},
		"duplicate destination": {{Variable: "token", Source: ExtractJSON}, {Variable: "token", Source: ExtractJSON}},
		"duplicate ID":          {{ID: "same", Disabled: true}, {ID: "same", Disabled: true}},
		"bad ID":                {{ID: "bad\nid", Disabled: true}},
		"invalid UTF8":          {{Variable: "\xff", Disabled: true}},
		"large name":            {{Variable: strings.Repeat("x", 257), Disabled: true}},
		"large selector":        {{Selector: strings.Repeat("x", 4097), Disabled: true}},
		"too many":              make([]ResponseExtractor, MaxExtractors+1),
	} {
		t.Run(name, func(t *testing.T) {
			if ValidateExtractors(rules) == nil {
				t.Fatal("invalid rules accepted")
			}
		})
	}
	disabled := []ResponseExtractor{{ID: "editing", Variable: "incomplete name", Source: "editing", Selector: "editing", Disabled: true}}
	if err := ValidateExtractors(disabled); err != nil {
		t.Fatalf("disabled editor state rejected: %v", err)
	}
	if got, err := ExtractVariables(disabled, nil, nil, true); got != nil || err != nil {
		t.Fatalf("disabled rules executed: %v, %v", got, err)
	}
	r := NewRequest()
	r.URL, r.Extractors = "https://example.test", valid
	for _, protocol := range []string{"SSE", "WebSocket"} {
		r.Protocol = protocol
		if ValidateRequestExtractors(r) == nil {
			t.Fatal("active rules accepted on stream protocol")
		}
		r.Extractors = disabled
		if err := ValidateRequestExtractors(r); err != nil {
			t.Fatal(err)
		}
		r.Extractors = valid
	}
	r.Protocol = "HTTP"
	r.Extractors[0].Variable = "bad name"
	if _, err := Resolve(r, NewWorkspace().Settings); err == nil {
		t.Fatal("Resolve bypassed extraction validation")
	}
}

func TestExtractValueLimitAndInputIsolation(t *testing.T) {
	rules := []ResponseExtractor{{Variable: "token", Source: ExtractHeader, Selector: "X-Token"}}
	headers := http.Header{"X-Token": {strings.Repeat("x", MaxInput+1)}}
	if got, err := ExtractVariables(rules, headers, nil, false); got != nil || err == nil {
		t.Fatal("oversized variable accepted")
	}
	headers["X-Token"] = []string{"value"}
	beforeRules, beforeHeaders := append([]ResponseExtractor(nil), rules...), headers.Clone()
	_, err := ExtractVariables(rules, headers, nil, false)
	if err != nil || !reflect.DeepEqual(rules, beforeRules) || !reflect.DeepEqual(headers, beforeHeaders) {
		t.Fatal("extraction mutated its inputs")
	}
}
