package core

import (
	"encoding/base64"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestSecretNamespaceDoesNotFallBackToOrdinaryEnvironment(t *testing.T) {
	r := NewRequest()
	r.URL = "https://example.test"
	r.Auth = Authentication{Mode: AuthBearer, Token: "{{secret.token}}"}
	s := Settings{Environment: "local", Environments: map[string]map[string]string{"local": {"secret.token": "plain-secret"}}}
	if _, err := Resolve(r, s); err == nil {
		t.Fatal("ordinary environment supplied a reserved secret")
	}
}

func TestResolveWithSecretsKeepsTemplatesAndEnvironments(t *testing.T) {
	r := NewRequest()
	r.URL = "{{base}}/api?token={{secret.token}}"
	r.Body = "{{secret.payload}}"
	r.Auth = Authentication{Mode: AuthBearer, Token: "{{secret.token}}"}
	s := Settings{Environment: "local", Environments: map[string]map[string]string{"local": {"base": "https://example.test", "secret.token": "ignored"}}}
	secrets := map[string]string{"token": "private & token", "payload": "{{literal}}\nvalue"}
	got, err := ResolveWithSecrets(r, s, secrets)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(got.URL)
	if u.Query().Get("token") != secrets["token"] || got.Body != secrets["payload"] || HTTPHeaders(got.Headers).Get("Authorization") != "Bearer "+secrets["token"] {
		t.Fatalf("private resolution incorrect: %q", got.URL)
	}
	if r.Body != "{{secret.payload}}" || s.Environments["local"]["secret.token"] != "ignored" || len(secrets) != 2 {
		t.Fatal("resolution mutated saved inputs")
	}
	_, err = ResolveWithSecrets(r, s, map[string]string{"token": "x"})
	var missing *MissingSecretError
	if !errors.As(err, &missing) || strings.Contains(err.Error(), "payload") {
		t.Fatalf("missing private value must have a sanitized typed error: %v", err)
	}
}

func TestSecretResolutionIsNotRecursiveAndValidatesHeaders(t *testing.T) {
	r := NewRequest()
	r.URL = "https://example.test"
	r.Body = "{{payload}}"
	s := Settings{Environment: "local", Environments: map[string]map[string]string{"local": {"payload": "{{secret.token}}"}}}
	got, err := ResolveWithSecrets(r, s, map[string]string{"token": "private"})
	if err != nil || got.Body != "{{secret.token}}" {
		t.Fatalf("ordinary replacement was expanded again: %q %v", got.Body, err)
	}
	r.Headers = []Pair{{"X-Token", "{{secret.token}}"}}
	if _, err := ResolveWithSecrets(r, s, map[string]string{"token": "bad\nheader"}); err == nil {
		t.Fatal("private value bypassed HTTP header validation")
	}
}

func TestPrivateExportDoesNotActivateEncodedLiteralReferences(t *testing.T) {
	r := NewRequest()
	r.URL = "https://example.test/%7B%7Bsecret.token%7D%7D?literal={{literal}}&actual={{secret.token}}"
	r.Auth = Authentication{Mode: AuthBearer, Token: "{{secret.token}}"}
	s := Settings{Environment: "local", Environments: map[string]map[string]string{"local": {"literal": "{{secret.token}}"}}}
	command, err := ExportCURL(r, s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ImportCURL(command)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"token": "private"}
	got, err = ResolveWithSecrets(got, Settings{}, values)
	want, wantErr := ResolveWithSecrets(r, s, values)
	if err != nil || wantErr != nil || got.URL != want.URL {
		t.Fatalf("encoded literal became an active private reference: %q vs %q %v", got.URL, want.URL, err)
	}
}

func TestValidateSecretValues(t *testing.T) {
	for _, values := range []map[string]string{
		{"secret.token": "x"}, {"": "x"}, {"bad name": "x"}, {"token": string([]byte{255})},
		{"token": strings.Repeat("x", 65537)},
	} {
		if ValidateSecretValues(values) == nil {
			t.Fatalf("accepted invalid private mapping")
		}
	}
	if err := ValidateSecretValues(map[string]string{"token": "", "data.value": "line\nvalue"}); err != nil {
		t.Fatal(err)
	}
}

func TestExportCURLPreservesPrivateReferencesWhileLocked(t *testing.T) {
	for _, auth := range []Authentication{
		{Mode: AuthBearer, Token: "{{secret.token}}"},
		{Mode: AuthBasic, Username: "{{user}}", Password: "{{secret.password}}"},
		{Mode: AuthAPIKey, Key: "X-Key", Value: "{{secret.token}}"},
		{Mode: AuthAPIKey, Location: APIKeyQuery, Key: "api_key", Value: "{{secret.token}}"},
	} {
		t.Run(string(auth.Mode)+string(auth.Location)+auth.Key, func(t *testing.T) {
			r := NewRequest()
			r.URL = "https://{{secret.host}}/api?token={{secret.token}}"
			placeholders := []string{"{{secret.host}}", "{{secret.token}}", "{{secret.payload}}"}
			if auth.Mode == AuthAPIKey && auth.Location == APIKeyQuery {
				r.URL = "https://example.test/api?token={{secret.token}}"
				placeholders = []string{"{{secret.token}}", "{{secret.payload}}"}
			}
			r.Body = "{{secret.payload}}"
			r.Headers = []Pair{{"Content-Type", "application/octet-stream"}}
			r.Auth = auth
			s := Settings{Environment: "local", Environments: map[string]map[string]string{"local": {"user": "public-user"}}}
			command, err := ExportCURL(r, s)
			if err != nil {
				t.Fatal(err)
			}
			for _, placeholder := range placeholders {
				if !strings.Contains(command, placeholder) {
					t.Fatalf("lost placeholder %s in %s", placeholder, command)
				}
			}
			if auth.Mode == AuthBasic && (!strings.Contains(command, "--user 'public-user:{{secret.password}}'") ||
				strings.Contains(command, base64.StdEncoding.EncodeToString([]byte("public-user:{{secret.password}}")))) {
				t.Fatalf("Basic private template was encoded: %s", command)
			}
			imported, err := ImportCURL(command)
			if err != nil {
				t.Fatal(err)
			}
			values := map[string]string{"host": "example.test", "token": "private token", "payload": "private body", "password": "private password", "key": "Key"}
			want, err := ResolveWithSecrets(r, s, values)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := ResolveWithSecrets(imported, Settings{}, values)
			if err != nil {
				t.Fatal(err)
			}
			if actual.URL != want.URL || actual.Body != want.Body || !reflect.DeepEqual(HTTPHeaders(actual.Headers), HTTPHeaders(want.Headers)) {
				t.Fatalf("private curl roundtrip changed request:\n%+v\n%+v", actual, want)
			}
		})
	}
}

func TestPrivateExportRejectsUnrepresentableAuthTemplates(t *testing.T) {
	r := NewRequest()
	r.URL = "https://example.test?api_key=old"
	r.Auth = Authentication{Mode: AuthAPIKey, Location: APIKeyQuery, Key: "{{secret.key}}", Value: "{{secret.token}}"}
	if command, err := ExportCURL(r, Settings{}); err == nil || command != "" {
		t.Fatal("export silently lost dynamic API Key shadowing")
	}
	r.URL = "https://example.test"
	r.Auth = Authentication{Mode: AuthBasic, Username: "{{user}}", Password: "{{secret.password}}"}
	s := Settings{Environment: "local", Environments: map[string]map[string]string{"local": {"user": "{{literal}}"}}}
	if command, err := ExportCURL(r, s); err == nil || command != "" {
		t.Fatal("export reactivated a public literal in private Basic auth")
	}
}

func TestPrivateExportRejectsAmbiguousURLAndHeaderTemplates(t *testing.T) {
	for _, name := range []string{"full_url", "query_key", "request_header", "global_header", "auth_header", "auth_global_header"} {
		t.Run(name, func(t *testing.T) {
			r := NewRequest()
			r.URL = "https://example.test"
			s := Settings{}
			values := map[string]string{"url": "https://example.test/?keep=1&api_key=old", "token": "T", "header": "X-Key", "query_key": "api_key"}
			switch name {
			case "full_url":
				r.URL = "{{secret.url}}"
				r.Auth = Authentication{Mode: AuthAPIKey, Location: APIKeyQuery, Key: "api_key", Value: "{{secret.token}}"}
			case "query_key":
				r.URL = "https://example.test/?keep=1&{{secret.query_key}}=old"
				r.Auth = Authentication{Mode: AuthAPIKey, Location: APIKeyQuery, Key: "api_key", Value: "{{secret.token}}"}
			case "request_header":
				r.Headers = []Pair{{"{{secret.header}}", "new"}}
				s.GlobalHeaders = []Pair{{"X-Key", "old"}}
			case "global_header":
				r.Headers = []Pair{{"X-Key", "new"}}
				s.GlobalHeaders = []Pair{{"{{secret.header}}", "old"}}
			case "auth_header":
				r.Headers = []Pair{{"{{secret.header}}", "old"}}
				r.Auth = Authentication{Mode: AuthAPIKey, Key: "X-Key", Value: "new"}
			case "auth_global_header":
				s.GlobalHeaders = []Pair{{"{{secret.header}}", "old"}}
				r.Auth = Authentication{Mode: AuthAPIKey, Key: "X-Key", Value: "new"}
			}
			wire, err := ResolveWithSecrets(r, s, values)
			if err != nil {
				t.Fatal(err)
			}
			if name == "full_url" || name == "query_key" {
				u, _ := url.Parse(wire.URL)
				if u.Query().Get("api_key") != "T" || u.Query().Get("keep") != "1" {
					t.Fatal("direct sending lost query authentication")
				}
			} else if got := HTTPHeaders(wire.Headers).Values("X-Key"); !reflect.DeepEqual(got, []string{"new"}) {
				t.Fatal("direct sending lost header override")
			}
			if command, err := ExportCURL(r, s); err == nil || command != "" {
				t.Fatal("copy curl silently changed unknown query or header ownership")
			}
		})
	}
}

func TestExportPrivateURLShapesAndMultipart(t *testing.T) {
	for _, address := range []string{"{{secret.url}}/api", "{{secret.scheme}}://example.test", "https://example.test?q={{secret.token}}"} {
		r := NewRequest()
		r.URL = address
		r.BodyMode = BodyMultipart
		r.FormParts = []FormPart{{Kind: FormText, Name: "token", Value: "{{secret.token}}"}}
		command, err := ExportCURL(r, Settings{})
		if err != nil || !strings.Contains(command, address) || !strings.Contains(command, "token={{secret.token}}") {
			t.Fatalf("private URL or multipart lost: %s %v", command, err)
		}
	}
	r := NewRequest()
	r.URL = "https://example.test/{{public}}"
	if _, err := ExportCURL(r, Settings{}); err == nil {
		t.Fatal("unresolved ordinary variable ignored")
	}
}
