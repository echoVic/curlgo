package cli

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCLIAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name, auth, header, want string
	}{
		{"bearer", `{"mode":"bearer","token":"{{secret}}"}`, "Authorization", "Bearer private_auth_secret"},
		{"basic", `{"mode":"basic","username":"user","password":"{{secret}}"}`, "Authorization", "Basic " + base64.StdEncoding.EncodeToString([]byte("user:private_auth_secret"))},
		{"api header", `{"mode":"apikey","key":"X-Key","value":"{{secret}}"}`, "X-Key", "private_auth_secret"},
		{"api query", `{"mode":"apikey","key":"key","value":"a&=+ 中文","location":"query"}`, "", "a&=+ 中文"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.header != "" {
					seen <- r.Header.Get(tc.header)
				} else {
					if len(r.URL.Query()["key"]) != 1 {
						t.Error("API key duplicates retained")
					}
					seen <- r.URL.Query().Get("key")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			input := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":%q,"auth":%s},
				"settings":{"environment":"env","environments":{"env":{"secret":"private_auth_secret"}}}}`,
				server.URL+"?key=old&key=older", tc.auth)
			exit, value, raw := invokeCLI(t, context.Background(), input)
			if exit != ExitOK || !value.OK || value.Result == nil || value.Result.StatusCode != 204 {
				t.Fatalf("CLI auth rejected: %s", raw)
			}
			if got := <-seen; got != tc.want {
				t.Fatalf("wire auth = %q want %q", got, tc.want)
			}
			if strings.Contains(raw, "private_auth_secret") || strings.Contains(raw, "dXNlcjpw") {
				t.Fatal("request auth header appeared in CLI output")
			}
		})
	}
}

func TestCLIAuthenticationErrorsAreSanitized(t *testing.T) {
	for _, tc := range []struct{ auth, code string }{
		{`{"mode":"bearer","token":"private_auth_secret\ninjection"}`, "INVALID_REQUEST"},
		{`{"mode":"basic","username":"private_auth_secret:invalid"}`, "INVALID_REQUEST"},
		{`{"mode":"basic","password":"{{missing}}"}`, "UNRESOLVED_VARIABLE"},
		{`{"mode":"apikey","key":"Content-Length","value":"private_auth_secret"}`, "INVALID_REQUEST"},
		{`{"mode":"apikey","key":"Host","value":"private_auth_secret"}`, "INVALID_REQUEST"},
		{`{"mode":"oauth2","token":"private_auth_secret"}`, "INVALID_REQUEST"},
		{`{"mode":"bearer","token":"private_auth_secret","unknown":true}`, "INVALID_INPUT"},
	} {
		input := `{"schemaVersion":1,"request":{"url":"https://example.test","auth":` + tc.auth + `}}`
		exit, value, raw := invokeCLI(t, context.Background(), input)
		if exit != ExitInput || value.OK || value.Error == nil || value.Error.Code != tc.code {
			t.Fatalf("wrong failure: %s", raw)
		}
		if strings.Contains(raw, "private_auth_secret") {
			t.Fatal("invalid credential leaked in CLI error")
		}
	}
}

func TestCLIAuthenticationOverridesMissingQueryVariable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "new" || len(r.URL.Query()["key"]) != 1 {
			t.Error("CLI query replacement failed")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	text := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":%q,"auth":{"mode":"apikey","location":"query","key":"key","value":"new"}}}`,
		server.URL+"?key={{missing}}")
	exit, value, raw := invokeCLI(t, context.Background(), text)
	if exit != ExitOK || !value.OK || value.Result.StatusCode != 204 {
		t.Fatalf("CLI expanded shadowed query variable: %s", raw)
	}
}
