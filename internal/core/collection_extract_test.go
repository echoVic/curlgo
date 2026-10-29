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

func TestCollectionExtractionRejectsInvalidRules(t *testing.T) {
	good := ResponseExtractor{ID: "rule", Variable: "token", Source: ExtractJSON, Selector: "/token"}
	tooMany := make([]ResponseExtractor, MaxExtractors+1)
	for i := range tooMany {
		tooMany[i].Disabled = true
	}
	for _, tc := range []struct {
		name     string
		protocol string
		rules    []ResponseExtractor
	}{
		{"unknown source", "HTTP", []ResponseExtractor{{Variable: "token", Source: "script", Selector: "/token"}}},
		{"invalid variable", "HTTP", []ResponseExtractor{{Variable: "bad name", Source: ExtractJSON, Selector: "/token"}}},
		{"invalid pointer", "HTTP", []ResponseExtractor{{Variable: "token", Source: ExtractJSON, Selector: "token"}}},
		{"invalid header", "HTTP", []ResponseExtractor{{Variable: "token", Source: ExtractHeader, Selector: "bad header"}}},
		{"duplicate variable", "HTTP", []ResponseExtractor{good, {Variable: "token", Source: ExtractJSON, Selector: "/id"}}},
		{"duplicate disabled ID", "HTTP", []ResponseExtractor{good, {ID: "rule", Disabled: true}}},
		{"too many disabled rules", "HTTP", tooMany},
		{"disabled oversized selector", "HTTP", []ResponseExtractor{{Disabled: true, Selector: strings.Repeat("x", 4097)}}},
		{"disabled invalid UTF-8", "HTTP", []ResponseExtractor{{Disabled: true, Selector: "\xff"}}},
		{"enabled SSE rule", "SSE", []ResponseExtractor{good}},
		{"enabled WebSocket rule", "WebSocket", []ResponseExtractor{good}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := NewRequest()
			request.Name, request.URL, request.Protocol, request.Extractors = tc.name, "https://example.test", tc.protocol, tc.rules
			for _, mode := range []BodyMode{BodyRaw, BodyMultipart} {
				request.BodyMode = mode
				err := checkImported(request)
				var extractionErr *ExtractionError
				if !errors.As(err, &extractionErr) {
					t.Errorf("checkImported must reject extraction rules with body mode %q: %v", mode, err)
				}
			}
			if request.Protocol == "WebSocket" {
				request.URL = "wss://example.test"
			}
			for _, mode := range []BodyMode{BodyRaw, BodyMultipart} {
				request.BodyMode = mode
				err := validateCollectionRequest(request)
				var extractionErr *ExtractionError
				if !errors.As(err, &extractionErr) {
					t.Errorf("native validator must reject extraction rules with body mode %q: %v", mode, err)
				}
			}
			request.BodyMode = BodyRaw
			for _, format := range []CollectionFormat{CollectionCurlGo, CollectionPostman} {
				if format == CollectionPostman && request.Protocol != "HTTP" {
					continue
				}
				result, err := ExportCollection(Workspace{Requests: []Request{request}}, CollectionExportOptions{Format: format})
				if err == nil || len(result.Data) != 0 {
					t.Errorf("%s export accepted invalid extraction rules", format)
				}
			}
			jsonSafe := true
			for _, rule := range tc.rules {
				jsonSafe = jsonSafe && utf8.ValidString(rule.ID+rule.Variable+string(rule.Source)+rule.Selector)
			}
			if jsonSafe {
				document := collectionNativeDocument(t)
				request.GroupID = "root"
				document["requests"] = []Request{request}
				got, err := ImportCurlGoCollection(collectionNativeJSON(t, document))
				if err == nil || len(got.Requests) != 0 {
					t.Errorf("native import accepted invalid extraction rules: count=%d, error=%v", len(got.Requests), err)
				}
			}
		})
	}
}

func TestCollectionExtractionUnknownJSONFieldRejected(t *testing.T) {
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
	fields["extractors"] = []map[string]any{{
		"variable": "token", "source": "json", "selector": "/token", "script": "not supported",
	}}
	document["requests"] = []map[string]any{fields}
	if got, err := ImportCurlGoCollection(collectionNativeJSON(t, document)); err == nil || len(got.Requests) != 0 {
		t.Fatalf("native collection accepted an unsupported extraction script: %+v, %v", got, err)
	}
}

func TestCollectionExtractionNativeRoundTripAndIsolation(t *testing.T) {
	for _, protocol := range []string{"HTTP", "SSE", "WebSocket"} {
		t.Run(protocol, func(t *testing.T) {
			request := NewRequest()
			request.Name, request.Protocol, request.URL = "Extraction template", protocol, "https://{{host}}/login"
			request.Extractors = []ResponseExtractor{
				{ID: "json", Variable: "token", Source: ExtractJSON, Selector: "/data/token"},
				{ID: "header", Variable: "session", Source: ExtractHeader, Selector: "X-Session"},
				{ID: "disabled", Variable: "token", Source: "draft-source", Selector: "unfinished", Disabled: true},
				{ID: "blank", Disabled: true},
			}
			if protocol != "HTTP" {
				for i := range request.Extractors {
					request.Extractors[i].Disabled = true
				}
				if protocol == "WebSocket" {
					request.URL = "wss://{{host}}/login"
				}
			}
			wantRules := slices.Clone(request.Extractors)
			workspace := Workspace{
				Requests: []Request{request},
				Settings: Settings{Environment: "private", Environments: map[string]map[string]string{
					"private": {"host": "private_runtime_secret", "token": "private_runtime_secret"},
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
				t.Fatalf("native export leaked runtime state or warned about preserved rules: %v", result.Warnings)
			}
			got, err := ImportCurlGoCollection(result.Data)
			if err != nil || len(got.Requests) != 1 || !reflect.DeepEqual(got.Requests[0], request) {
				t.Fatalf("native extraction metadata changed: %+v, %v", got, err)
			}
			after, _ := json.Marshal(workspace)
			if !bytes.Equal(before, after) {
				t.Fatal("native validation or export mutated the workspace")
			}
			snapshot := bytes.Clone(result.Data)
			workspace.Requests[0].Extractors[0].Variable = "source_changed"
			if !reflect.DeepEqual(got.Requests[0].Extractors, wantRules) {
				t.Fatal("imported extraction rules alias the source workspace")
			}
			got.Requests[0].Extractors[2].Selector = "import_changed"
			if workspace.Requests[0].Extractors[2].Selector != wantRules[2].Selector {
				t.Fatal("source extraction rules alias the imported collection")
			}
			again, err := ImportCurlGoCollection(result.Data)
			if err != nil || len(again.Requests) != 1 ||
				!reflect.DeepEqual(again.Requests[0].Extractors, wantRules) || !bytes.Equal(result.Data, snapshot) {
				t.Fatalf("exported extraction snapshot changed after mutations: %+v, %v", again, err)
			}
		})
	}
}

func TestCollectionExtractionPostmanWarningsNoScripts(t *testing.T) {
	workspace := Workspace{Groups: []RequestGroup{{ID: "api", Name: "API"}}}
	const affected = 201
	for i := range affected + 1 {
		request := NewRequest()
		request.ID, request.Name, request.GroupID, request.URL = fmt.Sprintf("r%d", i), fmt.Sprintf("Request-%d", i), "api", "https://example.test"
		if i < affected {
			rule := ResponseExtractor{Variable: "token", Source: ExtractHeader, Selector: "X-Token"}
			if i%2 == 1 {
				rule = ResponseExtractor{ID: "draft", Source: "draft-source", Selector: "unfinished", Disabled: true}
			}
			request.Extractors = []ResponseExtractor{rule}
		}
		workspace.Requests = append(workspace.Requests, request)
	}
	result, err := ExportCollection(workspace, CollectionExportOptions{Format: CollectionPostman})
	if err != nil || result.RequestCount != affected+1 {
		t.Fatalf("Postman export failed: count=%d, %v", result.RequestCount, err)
	}
	if len(result.Warnings) != affected {
		t.Fatalf("Postman must warn for every request with active or disabled rules: got %d, want %d", len(result.Warnings), affected)
	}
	for _, request := range workspace.Requests[:affected] {
		path := "\u300c" + collectionRequestPath(workspace.Groups, request) + "\u300d"
		found := false
		for _, warning := range result.Warnings {
			if strings.Contains(warning, path) && strings.Contains(warning, "Postman") && strings.Contains(warning, "CurlGo") {
				found = true
			}
		}
		if !found {
			t.Errorf("missing extraction-loss warning recommending CurlGo for %s", path)
		}
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
				case "event", "script", "exec", "extractors", "response":
					t.Errorf("Postman export generated unsupported extraction or response data: %s", key)
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
		t.Fatalf("Postman request templates failed to roundtrip: count=%d, %v", len(got.Requests), err)
	}
	for _, request := range got.Requests {
		if len(request.Extractors) != 0 {
			t.Fatal("Postman import pretended to preserve extraction rules")
		}
	}
}
