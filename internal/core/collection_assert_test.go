package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCollectionAssertionsRejectInvalidRules(t *testing.T) {
	good := ResponseAssertion{ID: "rule", Source: AssertStatus, Operator: AssertEquals, Expected: "200"}
	tooMany := make([]ResponseAssertion, MaxAssertions+1)
	for i := range tooMany {
		tooMany[i].Disabled = true
	}
	for _, tc := range []struct {
		name     string
		protocol string
		rules    []ResponseAssertion
	}{
		{"status contains", "HTTP", []ResponseAssertion{{Source: AssertStatus, Operator: AssertContains, Expected: "private_expected_secret"}}},
		{"unknown source", "HTTP", []ResponseAssertion{{Source: "script", Operator: AssertEquals, Expected: "private_expected_secret"}}},
		{"invalid status", "HTTP", []ResponseAssertion{{Source: AssertStatus, Operator: AssertEquals, Expected: "0200"}}},
		{"invalid pointer", "HTTP", []ResponseAssertion{{Source: AssertJSON, Operator: AssertExists, Selector: "private_selector_secret"}}},
		{"invalid JSON expected", "HTTP", []ResponseAssertion{{Source: AssertJSON, Operator: AssertEquals, Expected: "private_expected_secret"}}},
		{"invalid header", "HTTP", []ResponseAssertion{{Source: AssertHeader, Operator: AssertExists, Selector: "private header secret"}}},
		{"duplicate disabled ID", "HTTP", []ResponseAssertion{good, {ID: "rule", Disabled: true}}},
		{"too many disabled rules", "HTTP", tooMany},
		{"disabled oversized selector", "HTTP", []ResponseAssertion{{Disabled: true, Selector: strings.Repeat("x", 4097)}}},
		{"disabled oversized expected", "HTTP", []ResponseAssertion{{Disabled: true, Expected: strings.Repeat("x", 65537)}}},
		{"disabled invalid UTF-8", "HTTP", []ResponseAssertion{{Disabled: true, Expected: "\xff"}}},
		{"enabled SSE rule", "SSE", []ResponseAssertion{good}},
		{"enabled WebSocket rule", "WebSocket", []ResponseAssertion{good}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := NewRequest()
			request.Name, request.URL, request.Protocol, request.Assertions = tc.name, "https://example.test", tc.protocol, tc.rules
			for _, mode := range []BodyMode{BodyRaw, BodyMultipart} {
				request.BodyMode = mode
				err := checkImported(request)
				var configErr *AssertionConfigError
				if !errors.As(err, &configErr) {
					t.Errorf("generic validator accepted invalid assertions in %q mode: %v", mode, err)
				}
			}
			if request.Protocol == "WebSocket" {
				request.URL = "wss://example.test"
			}
			for _, mode := range []BodyMode{BodyRaw, BodyMultipart} {
				request.BodyMode = mode
				err := validateCollectionRequest(request)
				var configErr *AssertionConfigError
				if !errors.As(err, &configErr) {
					t.Errorf("native validator accepted invalid assertions in %q mode: %v", mode, err)
				}
			}
			request.BodyMode = BodyRaw
			for _, format := range []CollectionFormat{CollectionCurlGo, CollectionPostman} {
				if format == CollectionPostman && request.Protocol != "HTTP" {
					continue
				}
				result, err := ExportCollection(Workspace{Requests: []Request{request}}, CollectionExportOptions{Format: format})
				if err == nil || len(result.Data) != 0 {
					t.Errorf("%s export accepted invalid assertions", format)
				} else if strings.Contains(err.Error(), "private_") {
					t.Errorf("%s export leaked invalid rule values: %v", format, err)
				}
			}
			jsonSafe := true
			for _, rule := range tc.rules {
				jsonSafe = jsonSafe && utf8.ValidString(rule.ID+rule.Name+string(rule.Source)+string(rule.Operator)+rule.Selector+rule.Expected)
			}
			if jsonSafe {
				document := collectionNativeDocument(t)
				request.GroupID = "root"
				document["requests"] = []Request{request}
				got, err := ImportCurlGoCollection(collectionNativeJSON(t, document))
				if err == nil || len(got.Requests) != 0 {
					t.Errorf("native import accepted invalid assertions: count=%d, error=%v", len(got.Requests), err)
				} else if strings.Contains(err.Error(), "private_") {
					t.Errorf("native import leaked invalid rule values: %v", err)
				}
			}
		})
	}
}

func TestCollectionAssertionsUnknownRuleFieldRejected(t *testing.T) {
	document := collectionNativeDocument(t)
	request := document["requests"].([]Request)[0]
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	fields["assertions"] = []map[string]any{{
		"source": "status", "operator": "equals", "expected": "200", "script": "unsupported",
	}}
	document["requests"] = []map[string]any{fields}
	if got, err := ImportCurlGoCollection(collectionNativeJSON(t, document)); err == nil || len(got.Requests) != 0 {
		t.Fatalf("native collection accepted assertion scripts: %+v, %v", got, err)
	}
}

func TestCollectionAssertionsNativeRoundTripAndIsolation(t *testing.T) {
	for _, protocol := range []string{"HTTP", "SSE", "WebSocket"} {
		t.Run(protocol, func(t *testing.T) {
			request := NewRequest()
			request.Name, request.Protocol, request.URL = "Assertion template", protocol, "https://{{host}}/login"
			request.Assertions = []ResponseAssertion{
				{ID: "status", Name: "Status", Source: AssertStatus, Operator: AssertEquals, Expected: "201"},
				{ID: "header", Source: AssertHeader, Operator: AssertContains, Selector: "X-Token", Expected: "{{literal}}"},
				{ID: "json", Source: AssertJSON, Operator: AssertEquals, Selector: "/token", Expected: `"{{literal}}"`},
				{ID: "body", Source: AssertBody, Operator: AssertEquals, Expected: "literal"},
				{ID: "draft", Source: "unfinished", Operator: "draft", Selector: "unfinished", Expected: "editable", Disabled: true},
				{ID: "blank", Disabled: true},
			}
			if protocol != "HTTP" {
				for i := range request.Assertions {
					request.Assertions[i].Disabled = true
				}
				if protocol == "WebSocket" {
					request.URL = "wss://{{host}}/login"
				}
			}
			wantRules := slices.Clone(request.Assertions)
			workspace := Workspace{
				Requests: []Request{request},
				Settings: Settings{Environment: "private", Environments: map[string]map[string]string{
					"private": {"host": "private_runtime_secret", "literal": "private_runtime_secret"},
				}},
				Draft:   Request{Body: "private_runtime_secret"},
				History: []History{{Status: "private_runtime_secret"}},
			}
			before, _ := json.Marshal(workspace)
			result, err := ExportCollection(workspace, CollectionExportOptions{Format: CollectionCurlGo})
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(result.Data, []byte("private_runtime_secret")) || len(result.Warnings) != 0 {
				t.Fatalf("native export leaked runtime state or warned about preserved assertions: %v", result.Warnings)
			}
			got, err := ImportCurlGoCollection(result.Data)
			if err != nil || len(got.Requests) != 1 || !reflect.DeepEqual(got.Requests[0], request) {
				t.Fatalf("native assertions changed: %+v, %v", got, err)
			}
			after, _ := json.Marshal(workspace)
			if !bytes.Equal(before, after) {
				t.Fatal("export mutated assertion rules or expanded literal values")
			}
			workspace.Requests[0].Assertions[0].Expected = "599"
			if !reflect.DeepEqual(got.Requests[0].Assertions, wantRules) {
				t.Fatal("imported assertions alias the source workspace")
			}
			got.Requests[0].Assertions[4].Selector = "import_changed"
			if workspace.Requests[0].Assertions[4].Selector != wantRules[4].Selector {
				t.Fatal("source assertions alias the imported collection")
			}
			again, err := ImportCurlGoCollection(result.Data)
			if err != nil || len(again.Requests) != 1 || !reflect.DeepEqual(again.Requests[0].Assertions, wantRules) {
				t.Fatalf("exported assertion snapshot changed: %+v, %v", again, err)
			}
		})
	}
}

func TestCollectionAssertionsPostmanWarnsEveryRequestWithoutScripts(t *testing.T) {
	const affected = 201
	workspace := Workspace{Groups: []RequestGroup{{ID: "api", Name: "API"}}}
	for i := range affected + 1 {
		request := NewRequest()
		request.ID, request.Name, request.GroupID, request.URL = fmt.Sprintf("r%d", i), fmt.Sprintf("Request-%d", i), "api", "https://example.test"
		if i < affected {
			rule := ResponseAssertion{Source: AssertBody, Operator: AssertEquals, Expected: "private_expected_secret"}
			if i%2 == 1 {
				rule = ResponseAssertion{ID: "draft", Disabled: true}
			}
			request.Assertions = []ResponseAssertion{rule}
			request.TimeoutSeconds = 20
			request.QueryParams = []QueryParam{{ID: "draft-query", Disabled: true}}
		}
		if i == 0 {
			request.Extractors = []ResponseExtractor{{Variable: "token", Source: ExtractHeader, Selector: "X-Token"}}
		}
		workspace.Requests = append(workspace.Requests, request)
	}
	result, err := ExportCollection(workspace, CollectionExportOptions{Format: CollectionPostman})
	if err != nil || result.RequestCount != affected+1 {
		t.Fatalf("Postman export failed: count=%d, %v", result.RequestCount, err)
	}
	byRequest := map[string]int{}
	extractionWarnings := 0
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "\u63d0\u53d6") {
			extractionWarnings++
		}
		if !strings.Contains(warning, "\u65ad\u8a00") {
			continue
		}
		if !strings.Contains(warning, "Postman") || !strings.Contains(warning, "CurlGo") {
			t.Errorf("loss warning must name Postman and recommend CurlGo: %s", warning)
		}
		for _, request := range workspace.Requests {
			if strings.Contains(warning, "\u300c"+collectionRequestPath(workspace.Groups, request)+"\u300d") {
				byRequest[request.ID]++
			}
		}
	}
	if len(byRequest) != affected || extractionWarnings != 1 {
		t.Fatalf("loss warnings missing or capped: assertions=%d/%d, extraction=%d", len(byRequest), affected, extractionWarnings)
	}
	for _, request := range workspace.Requests {
		want := 0
		if len(request.Assertions) > 0 {
			want = 1
		}
		if byRequest[request.ID] != want {
			t.Errorf("assertion warning count for %s = %d, want %d", request.Name, byRequest[request.ID], want)
		}
	}
	if bytes.Contains(result.Data, []byte("private_expected_secret")) {
		t.Fatal("Postman export leaked discarded assertion expectations")
	}
	var document any
	if err := json.Unmarshal(result.Data, &document); err != nil {
		t.Fatal(err)
	}
	var check func(any)
	check = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			for key, child := range node {
				switch key {
				case "event", "script", "exec", "assertions", "response":
					t.Errorf("Postman generated unsupported assertion or runtime data: %s", key)
				}
				check(child)
			}
		case []any:
			for _, child := range node {
				check(child)
			}
		}
	}
	check(document)
	got, err := ImportPostman(result.Data)
	if err != nil || len(got.Requests) != affected+1 {
		t.Fatalf("Postman templates failed to roundtrip: count=%d, %v", len(got.Requests), err)
	}
	for _, request := range got.Requests {
		if len(request.Assertions) != 0 {
			t.Fatal("Postman import pretended to preserve assertions")
		}
	}
}
