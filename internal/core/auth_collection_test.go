package core

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCollectionAuthenticationValidation(t *testing.T) {
	for name, auth := range map[string]Authentication{
		"unknown mode":           {Mode: "oauth2"},
		"empty bearer":           {Mode: AuthBearer},
		"bearer control":         {Mode: AuthBearer, Token: "bad\nvalue"},
		"basic username colon":   {Mode: AuthBasic, Username: "bad:name"},
		"basic password control": {Mode: AuthBasic, Password: "bad\rvalue"},
		"empty API Key":          {Mode: AuthAPIKey},
		"bad API Key header":     {Mode: AuthAPIKey, Key: "bad key", Value: "value"},
		"reserved API Key":       {Mode: AuthAPIKey, Key: "content-length", Value: "1"},
		"API Key header control": {Mode: AuthAPIKey, Key: "X-Key", Value: "bad\nvalue"},
		"empty query key":        {Mode: AuthAPIKey, Location: APIKeyQuery},
		"unknown location":       {Mode: AuthAPIKey, Key: "X-Key", Location: "cookie"},
		"inactive size limit":    {Token: strings.Repeat("x", MaxInput+1)},
		"aggregate size limit":   {Username: strings.Repeat("x", MaxInput/2), Password: strings.Repeat("x", MaxInput/2+1)},
		"invalid token UTF-8":    {Token: "\xff"},
		"invalid username UTF-8": {Username: "\xff"},
		"invalid password UTF-8": {Password: "\xff"},
		"invalid key UTF-8":      {Key: "\xff"},
		"invalid value UTF-8":    {Value: "\xff"},
		"invalid mode UTF-8":     {Mode: "\xff"},
		"invalid location UTF-8": {Location: "\xff"},
	} {
		t.Run(name, func(t *testing.T) {
			r := NewRequest()
			r.Name, r.URL, r.Auth = name, "https://example.test", auth
			for _, mode := range []BodyMode{BodyRaw, BodyMultipart} {
				r.BodyMode = mode
				if err := checkImported(r); err == nil {
					t.Errorf("checkImported accepted invalid auth with body mode %q", mode)
				}
				if err := validateCollectionRequest(r); err == nil {
					t.Errorf("native validator accepted invalid auth with body mode %q", mode)
				}
			}
			for _, format := range []CollectionFormat{CollectionCurlGo, CollectionPostman} {
				result, err := ExportCollection(Workspace{Requests: []Request{r}}, CollectionExportOptions{Format: format})
				if err == nil || len(result.Data) != 0 {
					t.Errorf("%s export accepted invalid auth", format)
				}
			}
			if utf8.ValidString(string(auth.Mode) + auth.Token + auth.Username + auth.Password + auth.Key + auth.Value + string(auth.Location)) {
				doc := collectionNativeDocument(t)
				r.GroupID = "root"
				doc["requests"] = []Request{r}
				got, err := ImportCurlGoCollection(collectionNativeJSON(t, doc))
				if err == nil || len(got.Requests) != 0 {
					t.Error("native import accepted invalid auth")
				}
			}
		})
	}
}

func TestImportPostmanRejectsInvalidAuthentication(t *testing.T) {
	for name, auth := range map[string]postmanAuth{
		"empty bearer":         {Type: "bearer"},
		"bearer control":       {Type: "bearer", Fields: map[string]string{"token": "bad\nvalue"}},
		"basic username colon": {Type: "basic", Fields: map[string]string{"username": "bad:name"}},
		"basic control":        {Type: "basic", Fields: map[string]string{"password": "bad\nvalue"}},
		"empty API Key":        {Type: "apikey"},
		"reserved API Key":     {Type: "apikey", Fields: map[string]string{"key": "Transfer-Encoding", "value": "chunked"}},
		"unknown location":     {Type: "apikey", Fields: map[string]string{"key": "X-Key", "in": "cookie"}},
	} {
		t.Run(name, func(t *testing.T) {
			fields := []Pair{}
			for key, value := range auth.Fields {
				fields = append(fields, Pair{key, value})
			}
			got := importPostmanItems(t,
				postmanEntry("Good", map[string]any{"method": "GET", "url": "https://example.test"}),
				postmanEntry(name, map[string]any{"method": "GET", "url": "https://example.test", "auth": map[string]any{"type": auth.Type, auth.Type: fields}}),
			)
			if len(got.Requests) != 1 || got.Requests[0].Name != "Good" || !warned(got, name) {
				t.Fatalf("invalid auth should be skipped with a warning: %+v", got)
			}
		})
	}
}

func TestImportBrunoRejectsInvalidAuthentication(t *testing.T) {
	for name, helper := range map[string]string{
		"empty bearer":         "bearer\n}\n\nauth:bearer {\n",
		"bearer control":       "bearer\n}\n\nauth:bearer {\n  token: bad\x7fvalue\n",
		"basic username colon": "basic\n}\n\nauth:basic {\n  username: bad:name\n",
		"basic control":        "basic\n}\n\nauth:basic {\n  password: bad\x7fvalue\n",
		"empty API Key":        "apikey\n}\n\nauth:apikey {\n",
		"reserved API Key":     "apikey\n}\n\nauth:apikey {\n  key: Transfer-Encoding\n  value: chunked\n",
		"unknown location":     "apikey\n}\n\nauth:apikey {\n  key: X-Key\n  placement: cookie\n",
	} {
		t.Run(name, func(t *testing.T) {
			source := "get {\n  url: https://example.test\n  auth: " + helper + "}\n"
			if _, _, err := parseBrunoRequest(source, brunoContext{}); err == nil {
				t.Fatal("invalid Bruno auth should be rejected")
			}
		})
	}
}

func TestCollectionAuthenticationTemplateValidation(t *testing.T) {
	for name, auth := range map[string]Authentication{
		"manual inactive fields": {Token: "{{missing}}", Username: "bad:name", Password: "bad\nvalue", Key: "bad key", Location: "unused"},
		"bearer template":        {Mode: AuthBearer, Token: "{{token}}", Username: "bad:name", Password: "{{missing}}"},
		"basic templates":        {Mode: AuthBasic, Username: "{{user}}", Password: "{{pass}}", Token: "{{missing}}"},
		"header key template":    {Mode: AuthAPIKey, Key: "{{key_name}}", Value: "{{key}}", Token: "{{missing}}"},
		"query key template":     {Mode: AuthAPIKey, Key: "{{key_name}}", Value: "{{key}}", Location: APIKeyQuery, Password: "{{missing}}"},
	} {
		t.Run(name, func(t *testing.T) {
			r := NewRequest()
			r.Name, r.URL, r.Auth = name, "https://example.test", auth
			before, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if err := checkImported(r); err != nil {
				t.Fatalf("import validation must not expand templates: %v", err)
			}
			if err := validateCollectionRequest(r); err != nil {
				t.Fatalf("native validation must not expand templates or validate inactive shape: %v", err)
			}
			result, err := ExportCollection(Workspace{Requests: []Request{r}}, CollectionExportOptions{Format: CollectionCurlGo})
			if err != nil {
				t.Fatal(err)
			}
			got, err := ImportCurlGoCollection(result.Data)
			if err != nil || len(got.Requests) != 1 || got.Requests[0].Auth != auth {
				t.Fatalf("native auth config changed: %+v %v", got, err)
			}
			after, _ := json.Marshal(r)
			if string(before) != string(after) {
				t.Fatal("validation or export mutated auth")
			}
		})
	}
}

func TestCollectionExportAuthenticationRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name, keyName, header, value string
		auth                         Authentication
		fields                       map[string]string
	}{
		{name: "manual", keyName: "X-Key", auth: Authentication{}, fields: nil},
		{name: "bearer", keyName: "X-Key", auth: Authentication{Mode: AuthBearer, Token: "{{ token }}"}, fields: map[string]string{"token": "{{ token }}"}, header: "Authorization", value: "Bearer {{literal}}"},
		{name: "basic", keyName: "X-Key", auth: Authentication{Mode: AuthBasic, Username: "{{user}}", Password: "{{pass}}"}, fields: map[string]string{"username": "{{user}}", "password": "{{pass}}"}, header: "Authorization", value: "Basic " + base64.StdEncoding.EncodeToString([]byte("ada+user:s:cret&+="))},
		{name: "header API Key", keyName: "X-Key", auth: Authentication{Mode: AuthAPIKey, Key: "{{key_name}}", Value: "{{key}}", Location: APIKeyHeader}, fields: map[string]string{"key": "{{key_name}}", "value": "{{key}}", "in": "header"}, header: "X-Key", value: "a+b&c="},
		{name: "default header API Key", keyName: "X-Key", auth: Authentication{Mode: AuthAPIKey, Key: "{{key_name}}", Value: "{{key}}"}, fields: map[string]string{"key": "{{key_name}}", "value": "{{key}}", "in": "header"}, header: "X-Key", value: "a+b&c="},
		{name: "query API Key", keyName: "api_key", auth: Authentication{Mode: AuthAPIKey, Key: "{{key_name}}", Value: "{{key}}", Location: APIKeyQuery}, fields: map[string]string{"key": "{{key_name}}", "value": "{{key}}", "in": "query"}},
	} {
		for _, bodyMode := range []BodyMode{BodyRaw, BodyMultipart} {
			for _, format := range []CollectionFormat{CollectionCurlGo, CollectionPostman} {
				t.Run(tc.name+"/"+string(bodyMode)+"/"+string(format), func(t *testing.T) {
					r := NewRequest()
					r.Name, r.Method, r.URL = tc.name, "POST", "https://example.test/a%2fb?keep=a%20b&keep=a+b&api_key=old&api_key=older&flag#top"
					r.Auth = Authentication{
						Mode: tc.auth.Mode, Token: "{{inactive_token}}", Username: "{{inactive_user}}",
						Password: "{{inactive_pass}}", Key: "{{inactive_key}}", Value: "{{inactive_value}}", Location: "inactive",
					}
					switch tc.auth.Mode {
					case AuthBearer:
						r.Auth.Token = tc.auth.Token
					case AuthBasic:
						r.Auth.Username, r.Auth.Password = tc.auth.Username, tc.auth.Password
					case AuthAPIKey:
						r.Auth.Key, r.Auth.Value, r.Auth.Location = tc.auth.Key, tc.auth.Value, tc.auth.Location
					}
					r.Headers = []Pair{{"authorization", "{{manual}}"}, {"AUTHORIZATION", "second"}, {"x-key", "old"}, {"X-KEY", "older"}, {"Content-Type", "application/custom"}}
					r.BodyMode = bodyMode
					if bodyMode == BodyMultipart {
						r.FormParts = []FormPart{{ID: "part", Kind: FormText, Name: "note", Value: "{{note}}"}}
					} else {
						r.Body = "{{note}}"
					}
					settings := Settings{Environment: "test", Environments: map[string]map[string]string{"test": {
						"token": "{{literal}}", "user": "ada+user", "pass": "s:cret&+=", "key_name": tc.keyName,
						"key": "a+b&c=", "manual": "explicit", "note": "text", "unused_secret": "env-secret",
					}}}
					w := Workspace{Requests: []Request{r}, Settings: settings}
					before, _ := json.Marshal(w)
					result, err := ExportCollection(w, CollectionExportOptions{Format: format})
					if err != nil {
						t.Fatal(err)
					}
					if bytes.Contains(result.Data, []byte("env-secret")) || bytes.Contains(result.Data, []byte("ada+user")) || bytes.Contains(result.Data, []byte("{{literal}}")) {
						t.Fatal("default export resolved or included environment values")
					}
					var got CollectionImport
					if format == CollectionCurlGo {
						got, err = ImportCurlGoCollection(result.Data)
						if err != nil || len(got.Requests) != 1 || !reflect.DeepEqual(got.Requests[0], r) {
							t.Fatalf("native round trip must retain full auth and editor state: %+v %v", got, err)
						}
					} else {
						var document struct {
							Item []struct {
								Request struct {
									Auth   map[string]json.RawMessage `json:"auth"`
									Header []Pair                     `json:"header"`
									URL    string                     `json:"url"`
								} `json:"request"`
							} `json:"item"`
							Variable json.RawMessage `json:"variable"`
						}
						if err := json.Unmarshal(result.Data, &document); err != nil || len(document.Item) != 1 {
							t.Fatalf("invalid Postman export: %s %v", result.Data, err)
						}
						request := document.Item[0].Request
						if len(document.Variable) != 0 || request.URL != r.URL {
							t.Fatal("default Postman export changed URL or included environment")
						}
						wantHeaders := r.Headers
						if bodyMode == BodyMultipart {
							wantHeaders = r.Headers[:len(r.Headers)-1]
						}
						if !reflect.DeepEqual(request.Header, wantHeaders) {
							t.Fatalf("Postman export changed explicit headers: %v", request.Header)
						}
						if tc.auth.Mode == AuthManual {
							if len(request.Auth) != 0 {
								t.Fatalf("manual auth must not export inactive helpers: %v", request.Auth)
							}
						} else {
							var kind string
							if err := json.Unmarshal(request.Auth["type"], &kind); err != nil || kind != string(tc.auth.Mode) || len(request.Auth) != 2 {
								t.Fatalf("missing standard request.auth type: %v", request.Auth)
							}
							var attrs []struct{ Key, Value, Type string }
							if err := json.Unmarshal(request.Auth[kind], &attrs); err != nil {
								t.Fatalf("Postman auth helper must use a fields array: %v", err)
							}
							fields := map[string]string{}
							for _, attr := range attrs {
								if attr.Type != "string" {
									t.Errorf("auth attribute type = %q", attr.Type)
								}
								fields[attr.Key] = attr.Value
							}
							if len(attrs) != len(tc.fields) || !reflect.DeepEqual(fields, tc.fields) {
								t.Fatalf("active literal auth config changed: %v, want %v", fields, tc.fields)
							}
						}
						got, err = ImportPostman(result.Data)
						if err != nil || len(got.Requests) != 1 {
							t.Fatalf("Postman auth round trip failed: %+v %v", got, err)
						}
						wantAuth := tc.auth
						if wantAuth.Mode == AuthAPIKey && wantAuth.Location == "" {
							wantAuth.Location = APIKeyHeader
						}
						if got.Requests[0].Auth != wantAuth {
							t.Fatalf("Postman active auth config = %+v, want %+v", got.Requests[0].Auth, wantAuth)
						}
					}
					wire, err := Resolve(r, settings)
					if err != nil {
						t.Fatal(err)
					}
					back, err := Resolve(got.Requests[0], settings)
					if err != nil || back.URL != wire.URL || back.Method != wire.Method || back.Body != wire.Body || !reflect.DeepEqual(back.Headers, wire.Headers) {
						t.Fatalf("auth round trip changed Resolve wire: want %+v, got %+v, error %v", wire, back, err)
					}
					if tc.header != "" && !reflect.DeepEqual(HTTPHeaders(back.Headers).Values(tc.header), []string{tc.value}) {
						t.Fatalf("resolved auth header = %v", back.Headers)
					}
					if tc.auth.Mode == AuthAPIKey && tc.auth.Location == APIKeyQuery && back.URL != "https://example.test/a%2fb?keep=a%20b&keep=a+b&api_key=a%2Bb%26c%3D&flag#top" {
						t.Fatalf("query auth did not preserve unrelated raw encoding: %s", back.URL)
					}
					after, _ := json.Marshal(w)
					if !bytes.Equal(before, after) {
						t.Fatal("export or Resolve mutated the source workspace")
					}
				})
			}
		}
	}
}
