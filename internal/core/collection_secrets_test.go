package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExternalCollectionImportsFilterReservedVariables(t *testing.T) {
	postman, err := ImportPostman(postmanCollection(t, map[string]any{
		"variable": []map[string]string{
			{"key": "token", "value": "ordinary"},
			{"key": "secret.token", "value": "private-marker"},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for name, text := range map[string]string{
		"bruno.json":             `{"name":"Bruno"}`,
		"collection.bru":         "vars:pre-request {\n token: ordinary\n secret.collection: private-marker\n}\n",
		"environments/local.bru": "vars {\n secret.token: private-marker\n}\n",
	} {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	bruno, err := ImportBrunoDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, imported := range []CollectionImport{postman, bruno} {
		if len(imported.Environments) == 0 {
			t.Fatal("ordinary imported variables were discarded")
		}
		for _, variables := range imported.Environments {
			if !reflect.DeepEqual(variables, map[string]string{"token": "ordinary"}) {
				t.Fatal("import retained a reserved variable or changed ordinary values")
			}
		}
		warnings := strings.Join(imported.Warnings, "\n")
		if !strings.Contains(warnings, "secret.*") || strings.Contains(warnings, "private-marker") {
			t.Fatal("missing or unsafe reserved-variable filtering warning")
		}
	}
}

func TestCollectionImportRejectsReservedSecretEnvironmentValues(t *testing.T) {
	for _, env := range []string{"Chosen", "Other"} {
		for _, name := range []string{SecretPrefix, SecretPrefix + "token", SecretPrefix + "nested.name"} {
			t.Run(env+"/"+name, func(t *testing.T) {
				doc := collectionNativeDocument(t)
				doc["environment"] = "Chosen"
				environments := map[string]map[string]string{
					"Chosen": {"token": "ordinary"}, "Other": {},
				}
				environments[env][name] = "private_marker"
				doc["environments"] = environments
				got, err := ImportCurlGoCollection(collectionNativeJSON(t, doc))
				if err == nil || len(got.Requests) != 0 || len(got.Environments) != 0 {
					t.Fatal("reserved secret values were imported or partially exposed")
				}
				if strings.Contains(err.Error(), "private_marker") || strings.Contains(err.Error(), "nested.name") {
					t.Fatal("reserved namespace error echoes variable values or names")
				}
			})
		}
	}
}

func TestCollectionExportFiltersReservedSecretValuesWithoutMutation(t *testing.T) {
	for _, format := range []CollectionFormat{CollectionCurlGo, CollectionPostman} {
		for _, include := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/include=%v", format, include), func(t *testing.T) {
				request := NewRequest()
				request.URL, request.Body = "https://example.test/{{secret.token}}", "{{secret.token}}"
				request.Headers = []Pair{{Key: "X-Token", Value: "{{secret.token}}"}}
				request.Auth = Authentication{Mode: AuthBearer, Token: "{{secret.token}}"}
				w := Workspace{
					Requests: []Request{request},
					Settings: Settings{Environment: "Chosen", Environments: map[string]map[string]string{
						"Chosen": {"token": "ordinary", "secret": "not-reserved", "Secret.token": "case-sensitive", SecretPrefix + "token": "private_marker"},
						"Other":  {SecretPrefix + "token": "private_other_marker", SecretPrefix: "private_empty_marker"},
					}},
				}
				before, err := json.Marshal(w)
				if err != nil {
					t.Fatal(err)
				}
				result, err := ExportCollection(w, CollectionExportOptions{Format: format, IncludeEnvironment: include})
				if err != nil {
					t.Fatal(err)
				}
				for _, value := range []string{"private_marker", "private_other_marker", "private_empty_marker"} {
					if bytes.Contains(result.Data, []byte(value)) || strings.Contains(strings.Join(result.Warnings, "\n"), value) {
						t.Fatal("export payload or warnings contain reserved secret values")
					}
				}
				warnings := strings.Join(result.Warnings, "\n")
				if include && !strings.Contains(warnings, "secret.*") {
					t.Fatalf("missing reserved-namespace loss warning: %v", result.Warnings)
				}
				var public map[string]string
				if format == CollectionCurlGo {
					got, err := ImportCurlGoCollection(result.Data)
					if err != nil || len(got.Requests) != 1 || !reflect.DeepEqual(got.Requests[0], request) {
						t.Fatalf("native export lost private templates: %v", err)
					}
					public = got.Environments["Chosen"]
					if include && (got.Environment != "Chosen" || got.Environments["Other"] == nil || len(got.Environments["Other"]) != 0) {
						t.Fatal("filtered environments were removed or changed")
					}
				} else {
					var document struct {
						Variable []exportPostmanVariable `json:"variable"`
						Item     []exportPostmanItem     `json:"item"`
					}
					if err := json.Unmarshal(result.Data, &document); err != nil || len(document.Item) != 1 || document.Item[0].Request == nil {
						t.Fatal("invalid Postman snapshot")
					}
					r := document.Item[0].Request
					if r.URL != request.URL || r.Body.Raw != request.Body || !reflect.DeepEqual(r.Header, request.Headers) ||
						r.Auth == nil || len(r.Auth.Bearer) != 1 || r.Auth.Bearer[0].Value != request.Auth.Token {
						t.Fatal("Postman export expanded or lost private references")
					}
					if include {
						public = make(map[string]string)
						for _, v := range document.Variable {
							public[v.Key] = v.Value
						}
					}
				}
				if include {
					want := map[string]string{"token": "ordinary", "secret": "not-reserved", "Secret.token": "case-sensitive"}
					if !reflect.DeepEqual(public, want) {
						t.Fatalf("ordinary variables changed: %v", public)
					}
				} else if len(public) != 0 {
					t.Fatal("default export included ordinary environment values")
				}
				after, err := json.Marshal(w)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("export mutated the original workspace")
				}
			})
		}
	}
}

func TestCollectionSecretReferencesRemainTemplates(t *testing.T) {
	for _, auth := range []Authentication{
		{Mode: AuthBearer, Token: "{{secret.token}}"},
		{Mode: AuthBasic, Username: "{{user}}", Password: "{{secret.password}}"},
		{Mode: AuthAPIKey, Key: "X-Key", Value: "{{secret.token}}"},
		{Mode: AuthAPIKey, Key: "{{secret.key}}", Value: "{{secret.token}}", Location: APIKeyQuery},
	} {
		for _, format := range []CollectionFormat{CollectionCurlGo, CollectionPostman} {
			t.Run(string(auth.Mode)+"/"+string(auth.Location)+"/"+string(format), func(t *testing.T) {
				r := NewRequest()
				r.URL, r.Method, r.BodyMode = "{{secret.base_url}}/path?q={{secret.query}}", "POST", BodyMultipart
				r.FormParts = []FormPart{{Kind: FormText, Name: "token", Value: "{{secret.token}}"}}
				r.Headers = []Pair{{Key: "{{secret.header}}", Value: "{{secret.token}}"}}
				r.Auth = auth
				w := Workspace{Requests: []Request{r}, Settings: Settings{
					Environment: "Chosen", Environments: map[string]map[string]string{"Chosen": {"user": "ordinary"}},
				}}
				result, err := ExportCollection(w, CollectionExportOptions{Format: format, IncludeEnvironment: true})
				if err != nil {
					t.Fatal(err)
				}
				var got CollectionImport
				if format == CollectionCurlGo {
					got, err = ImportCurlGoCollection(result.Data)
				} else {
					got, err = ImportPostman(result.Data)
				}
				if err != nil || len(got.Requests) != 1 {
					t.Fatalf("template-only collection import failed: %v", err)
				}
				back := got.Requests[0]
				if back.URL != r.URL || !reflect.DeepEqual(back.Headers, r.Headers) || len(back.FormParts) != 1 || back.FormParts[0].Value != r.FormParts[0].Value ||
					back.Auth.Mode != auth.Mode || back.Auth.Token != auth.Token || back.Auth.Password != auth.Password ||
					back.Auth.Key != auth.Key || back.Auth.Value != auth.Value {
					t.Fatal("private references were expanded or omitted during collection round trip")
				}
			})
		}
	}
}
