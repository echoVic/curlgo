package core

import (
	"reflect"
	"strings"
	"testing"
)

func TestCollectionExportHeaderPrecedence(t *testing.T) {
	tests := []struct {
		name             string
		globals, request []Pair
		omitEnvironment  bool
		reject           bool
	}{
		{"global template", []Pair{{"{{header_name}}", "private_global_value"}}, []Pair{{"X-Key", "new"}}, false, true},
		{"request template", []Pair{{"X-Key", "private_global_value"}}, []Pair{{"{{header_name}}", "new"}}, false, true},
		{"different templates", []Pair{{"{{global_name}}", "private_global_value"}}, []Pair{{"{{header_name}}", "new"}}, false, true},
		{"variable case differs", []Pair{{"{{name}}", "old"}}, []Pair{{"{{NAME}}", "new"}}, false, true},
		{"same template", []Pair{{"{{header_name}}", "old"}}, []Pair{{"{{header_name}}", "new"}}, false, false},
		{"only global template", []Pair{{"{{header_name}}", "old"}}, nil, false, false},
		{"only request template", nil, []Pair{{"{{header_name}}", "new"}}, false, false},
		{"static override", []Pair{{" x-key ", "old"}}, []Pair{{"X-Key", "new"}, {"X-Key", "second"}}, false, false},
		{"without environment", []Pair{{"{{header_name}}", "private_global_value"}}, []Pair{{"X-Key", "new"}}, true, false},
	}
	for _, format := range []CollectionFormat{CollectionCurlGo, CollectionPostman} {
		for _, tc := range tests {
			t.Run(string(format)+"/"+tc.name, func(t *testing.T) {
				r := NewRequest()
				r.ID, r.Name, r.URL = "request", "Header precedence", "https://example.test"
				r.Headers = tc.request
				w := Workspace{
					Version: 1, Requests: []Request{r},
					Settings: Settings{
						Environment: "local",
						Environments: map[string]map[string]string{
							"local": {"header_name": "X-Key", "global_name": "X-Key", "name": "X-Global", "NAME": "X-Request"},
						},
						GlobalHeaders: tc.globals,
					},
				}
				original, err := Resolve(r, w.Settings)
				if err != nil {
					t.Fatal(err)
				}
				result, err := ExportCollection(w, CollectionExportOptions{
					Format: format, IncludeEnvironment: !tc.omitEnvironment,
				})
				if err != nil {
					if !tc.reject {
						t.Fatal(err)
					}
					if !strings.Contains(err.Error(), "覆盖") ||
						strings.Contains(err.Error(), "header_name") ||
						strings.Contains(err.Error(), "private_global_value") {
						t.Fatalf("unsafe or unrelated export failure: %v", err)
					}
					return
				}
				var imported CollectionImport
				if format == CollectionCurlGo {
					imported, err = ImportCurlGoCollection(result.Data)
				} else {
					imported, err = ImportPostman(result.Data)
					imported.Environment = imported.Name
				}
				if err != nil || len(imported.Requests) != 1 {
					t.Fatalf("import: %v, requests=%d", err, len(imported.Requests))
				}
				if tc.omitEnvironment {
					if !reflect.DeepEqual(imported.Requests[0].Headers, r.Headers) {
						t.Fatal("request headers changed without including the environment")
					}
					return
				}
				resolved, err := Resolve(imported.Requests[0], Settings{
					Environment: imported.Environment, Environments: imported.Environments,
				})
				if err != nil {
					t.Fatal(err)
				}
				if tc.reject {
					t.Fatalf("ambiguous export accepted: original headers=%d, imported headers=%d",
						len(original.Headers), len(resolved.Headers))
				}
				if !reflect.DeepEqual(resolved.Headers, original.Headers) {
					t.Fatal("export changed effective request headers")
				}
			})
		}
	}
}
