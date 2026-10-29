package core

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestAssertionsEvaluateEveryRuleWithoutLeakingValues(t *testing.T) {
	rules := []ResponseAssertion{
		{ID: "status", Name: "成功", Source: AssertStatus, Operator: AssertEquals, Expected: "200"},
		{Disabled: true},
		{Source: AssertHeader, Operator: AssertContains, Selector: "x-token", Expected: "private"},
		{Source: AssertJSON, Operator: AssertEquals, Selector: "/flag", Expected: "true"},
		{Source: AssertBody, Operator: AssertContains, Expected: "missing-private"},
	}
	report, err := EvaluateAssertions(rules, 201, http.Header{"X-Token": {"private-header"}}, []byte(`{"flag":true,"secret":"private-body"}`), false)
	if err != nil || report.Passed || len(report.Results) != 4 {
		t.Fatalf("report = %+v, %v", report, err)
	}
	for i, want := range []struct {
		index int
		code  string
	}{{0, "MISMATCH"}, {2, "PASSED"}, {3, "PASSED"}, {4, "MISMATCH"}} {
		got := report.Results[i]
		if got.Index != want.index || got.Code != want.code || got.Passed != (want.code == "PASSED") {
			t.Fatalf("result %d: %+v", i, got)
		}
	}
	data, _ := json.Marshal(report)
	if strings.Contains(string(data), "private") || report.Results[0].ID != "status" || report.Results[0].Name != "成功" {
		t.Fatalf("unsafe or missing metadata: %s", data)
	}
	for _, rules := range [][]ResponseAssertion{nil, {{Disabled: true}}} {
		report, err := EvaluateAssertions(rules, 500, nil, nil, true)
		if err != nil || !report.Passed || len(report.Results) != 0 {
			t.Fatalf("unconfigured contract changed: %+v, %v", report, err)
		}
	}
}

func TestAssertionResponseSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, body, expected, selector, code string
		source                               AssertionSource
		op                                   AssertionOperator
		headers                              http.Header
		truncated                            bool
	}{
		{name: "exact large number", body: `9007199254740993`, expected: `9007199254740993`, code: "PASSED"},
		{name: "distinct large number", body: `9007199254740993`, expected: `9007199254740992`, code: "MISMATCH"},
		{name: "semantic number", body: `1.2300e2`, expected: `123`, code: "PASSED"},
		{name: "zero signs", body: `-0e99999`, expected: `0`, code: "PASSED"},
		{name: "huge exponent", body: `10e999999999999999999999999`, expected: `1e1000000000000000000000000`, code: "PASSED"},
		{name: "negative exponent", body: `12e-999999999999999999999999`, expected: `1.2e-999999999999999999999998`, code: "PASSED"},
		{name: "number versus string", body: `200`, expected: `"200"`, code: "TYPE_MISMATCH"},
		{name: "null equals", body: `null`, expected: `null`, code: "PASSED"},
		{name: "null exists", body: `{"x":null}`, selector: "/x", op: AssertExists, code: "PASSED"},
		{name: "object exists", body: `{}`, op: AssertExists, code: "PASSED"},
		{name: "object equality", body: `{}`, expected: `null`, code: "NON_SCALAR"},
		{name: "missing versus null", body: `{}`, expected: `null`, selector: "/x", code: "MISSING_VALUE"},
		{name: "escaped pointer", body: `{"a/b":{"~":["值"]}}`, expected: `"值"`, selector: "/a~1b/~0/0", code: "PASSED"},
		{name: "array leading zero", body: `["x"]`, expected: `"x"`, selector: "/00", code: "MISSING_VALUE"},
		{name: "json contains", body: `"中文内容"`, expected: `"内容"`, op: AssertContains, code: "PASSED"},
		{name: "contains nonstring", body: `200`, expected: `"2"`, op: AssertContains, code: "TYPE_MISMATCH"},
		{name: "invalid json exists", body: `{`, op: AssertExists, code: "INVALID_JSON"},
		{name: "trailing json", body: `{} {}`, op: AssertExists, code: "INVALID_JSON"},
		{name: "invalid utf8 json", body: "\"\xff\"", expected: `""`, code: "INVALID_JSON"},
		{name: "header empty exists", source: AssertHeader, selector: "x-empty", op: AssertExists, headers: http.Header{"X-Empty": {""}}, code: "PASSED"},
		{name: "header repeated exists", source: AssertHeader, selector: "X", op: AssertExists, headers: http.Header{"X": {"a", "b"}}, code: "PASSED"},
		{name: "header repeated equals", source: AssertHeader, selector: "X", expected: "a", headers: http.Header{"X": {"a"}, "x": {"b"}}, code: "AMBIGUOUS_HEADER"},
		{name: "header missing", source: AssertHeader, selector: "X", op: AssertExists, code: "MISSING_VALUE"},
		{name: "body equality", source: AssertBody, body: "中文", expected: "中文", code: "PASSED"},
		{name: "body empty", source: AssertBody, code: "PASSED"},
		{name: "body invalid utf8", source: AssertBody, body: "\xff", code: "INVALID_BODY"},
		{name: "truncated status", source: AssertStatus, expected: "200", truncated: true, code: "TRUNCATED_RESPONSE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.source == "" {
				tc.source = AssertJSON
			}
			if tc.op == "" {
				tc.op = AssertEquals
			}
			rules := []ResponseAssertion{{Source: tc.source, Operator: tc.op, Selector: tc.selector, Expected: tc.expected}}
			report, err := EvaluateAssertions(rules, 200, tc.headers, []byte(tc.body), tc.truncated)
			if err != nil || len(report.Results) != 1 || report.Results[0].Code != tc.code || report.Passed != (tc.code == "PASSED") {
				t.Fatalf("report = %+v, %v; want %s", report, err, tc.code)
			}
		})
	}
}

func TestAssertionConfigurationAndRequestValidation(t *testing.T) {
	for name, rules := range map[string][]ResponseAssertion{
		"status range":      {{Source: AssertStatus, Operator: AssertEquals, Expected: "600"}},
		"status leading 0":  {{Source: AssertStatus, Operator: AssertEquals, Expected: "0200"}},
		"status operator":   {{Source: AssertStatus, Operator: AssertContains, Expected: "200"}},
		"status selector":   {{Source: AssertStatus, Operator: AssertEquals, Selector: "/x", Expected: "200"}},
		"unknown source":    {{Source: "script", Operator: AssertEquals}},
		"unknown operator":  {{Source: AssertBody, Operator: "regex"}},
		"body exists":       {{Source: AssertBody, Operator: AssertExists}},
		"header token":      {{Source: AssertHeader, Operator: AssertExists, Selector: "X\nY"}},
		"json pointer":      {{Source: AssertJSON, Operator: AssertExists, Selector: "/~2"}},
		"json literal":      {{Source: AssertJSON, Operator: AssertEquals, Expected: "unquoted"}},
		"json object":       {{Source: AssertJSON, Operator: AssertEquals, Expected: "{}"}},
		"json contains":     {{Source: AssertJSON, Operator: AssertContains, Expected: "200"}},
		"duplicate id":      {{ID: "same", Disabled: true}, {ID: "same", Disabled: true}},
		"invalid id":        {{ID: "bad id", Disabled: true}},
		"invalid encoding":  {{Expected: "\xff", Disabled: true}},
		"control name":      {{Name: "x\n", Disabled: true}},
		"large name":        {{Name: strings.Repeat("x", 257), Disabled: true}},
		"large selector":    {{Selector: strings.Repeat("x", 4097), Disabled: true}},
		"large expected":    {{Expected: strings.Repeat("x", 65537), Disabled: true}},
		"too many":          make([]ResponseAssertion, MaxAssertions+1),
		"aggregate too big": repeatedAssertions(20, strings.Repeat("x", 65536)),
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateAssertions(rules)
			var configError *AssertionConfigError
			if !errors.As(err, &configError) {
				t.Fatalf("invalid config accepted: %v", err)
			}
		})
	}
	valid := []ResponseAssertion{{ID: "one", Source: AssertJSON, Operator: AssertExists, Expected: "ignored editing value"}}
	if err := ValidateAssertions(valid); err != nil {
		t.Fatal(err)
	}
	r := NewRequest()
	r.URL, r.Assertions = "https://example.test", valid
	if _, err := Resolve(r, NewWorkspace().Settings); err != nil {
		t.Fatal(err)
	}
	r.Protocol = "SSE"
	if _, err := Resolve(r, NewWorkspace().Settings); err == nil {
		t.Fatal("enabled assertions accepted for SSE")
	}
	r.Assertions = []ResponseAssertion{{Source: "editing", Disabled: true}}
	if _, err := Resolve(r, NewWorkspace().Settings); err != nil {
		t.Fatal(err)
	}
}

func repeatedAssertions(count int, expected string) []ResponseAssertion {
	rules := make([]ResponseAssertion, count)
	for i := range rules {
		rules[i] = ResponseAssertion{Expected: expected, Disabled: true}
	}
	return rules
}
