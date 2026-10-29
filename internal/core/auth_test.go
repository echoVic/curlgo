package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

func TestAuthenticationOnWire(t *testing.T) {
	for _, tc := range []struct {
		name, auth, header, value, query string
	}{
		{"bearer", `{"mode":"bearer","token":"{{token}}"}`, "Authorization", "Bearer {{literal}}", "keep=%2f&key=old&key=older&flag"},
		{"basic", `{"mode":"basic","username":"{{user}}","password":"{{pass}}"}`, "Authorization", "Basic " + base64.StdEncoding.EncodeToString([]byte("测试:p:a+ss")), "keep=%2f&key=old&key=older&flag"},
		{"api header", `{"mode":"apikey","key":"{{header}}","value":"{{key}}","location":"header"}`, "X-Api-Key", "a&=+ 中文", "keep=%2f&key=old&key=older&flag"},
		{"api query", `{"mode":"apikey","key":"key","value":"{{key}}","location":"query"}`, "", "", "keep=%2f&key=a%26%3D%2B+%E4%B8%AD%E6%96%87&flag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := make(chan *http.Request, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen <- r.Clone(r.Context())
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			r := NewRequest()
			r.URL = server.URL + "/?keep=%2f&key=old&key=older&flag#anchor"
			if err := json.Unmarshal([]byte(`{"auth":`+tc.auth+`}`), &r); err != nil {
				t.Fatal(err)
			}
			s := NewWorkspace().Settings
			s.Environments[s.Environment] = map[string]string{
				"token": "{{literal}}", "user": "测试", "pass": "p:a+ss", "header": "X-Api-Key", "key": "a&=+ 中文",
			}
			if tc.header != "" {
				r.Headers = []Pair{{strings.ToLower(tc.header), "{{overridden}}"}, {tc.header, "also overridden"}}
				s.GlobalHeaders = []Pair{{tc.header, "{{unused}}"}}
			}
			beforeR, beforeS := modelSnapshot(t, r), modelSnapshot(t, s)
			resolved, err := Resolve(r, s)
			if err != nil {
				t.Fatalf("auth helper must override inactive header templates: %v", err)
			}
			if !strings.HasSuffix(resolved.URL, "#anchor") {
				t.Fatal("fragment lost")
			}
			engine := NewEngine()
			defer engine.Close()
			if err := engine.Run(context.Background(), resolved, func(Event) {}); err != nil {
				t.Fatal(err)
			}
			got := <-seen
			if tc.header != "" && (got.Header.Get(tc.header) != tc.value || len(got.Header.Values(tc.header)) != 1) {
				t.Fatalf("wrong auth header: %v", got.Header)
			}
			if got.URL.RawQuery != tc.query {
				t.Fatalf("query = %q, want %q", got.URL.RawQuery, tc.query)
			}
			cmd, err := ExportCURL(r, s)
			if err != nil {
				t.Fatal(err)
			}
			imported, err := ImportCURL(cmd)
			if err != nil || imported.URL != resolved.URL || HTTPHeaders(imported.Headers).Get(tc.header) != HTTPHeaders(resolved.Headers).Get(tc.header) {
				t.Fatalf("curl differs from wire: %v", err)
			}
			if modelSnapshot(t, r) != beforeR || modelSnapshot(t, s) != beforeS {
				t.Fatal("sending/export changed saved templates")
			}
		})
	}
}

func TestAuthenticationValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		auth Authentication
		vars map[string]string
	}{
		{"unknown mode", Authentication{Mode: "oauth2", Token: "secret"}, nil},
		{"bearer newline", Authentication{Mode: AuthBearer, Token: "{{secret}}"}, map[string]string{"secret": "private\nsecret"}},
		{"empty bearer", Authentication{Mode: AuthBearer}, nil},
		{"basic colon", Authentication{Mode: AuthBasic, Username: "{{user}}"}, map[string]string{"user": "private:user"}},
		{"basic control", Authentication{Mode: AuthBasic, Password: "private\x00password"}, nil},
		{"missing variable", Authentication{Mode: AuthBasic, Password: "{{missing}}"}, nil},
		{"invalid header name", Authentication{Mode: AuthAPIKey, Key: "{{name}}"}, map[string]string{"name": "private name"}},
		{"api header newline", Authentication{Mode: AuthAPIKey, Key: "X-Key", Value: "{{secret}}"}, map[string]string{"secret": "private\rsecret"}},
		{"reserved content length", Authentication{Mode: AuthAPIKey, Key: "content-length", Value: "123"}, nil},
		{"reserved transfer encoding", Authentication{Mode: AuthAPIKey, Key: "{{name}}"}, map[string]string{"name": "Transfer-Encoding"}},
		{"empty query key", Authentication{Mode: AuthAPIKey, Location: APIKeyQuery}, nil},
		{"unknown location", Authentication{Mode: AuthAPIKey, Key: "key", Location: "body"}, nil},
		{"large template", Authentication{Mode: AuthBearer, Token: strings.Repeat("x", MaxInput+1)}, nil},
		{"large resolved token", Authentication{Mode: AuthBearer, Token: "{{secret}}"}, map[string]string{"secret": strings.Repeat("x", MaxInput+1)}},
		{"invalid utf8", Authentication{Mode: AuthBasic, Username: string([]byte{0xff})}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := modelRequest()
			r.Auth = tc.auth
			before := modelSnapshot(t, r)
			_, err := Resolve(r, Settings{Environment: "env", Environments: map[string]map[string]string{"env": tc.vars}})
			if err == nil {
				t.Fatal("invalid authentication accepted")
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret\n") {
				t.Fatal("credential leaked into error")
			}
			if modelSnapshot(t, r) != before {
				t.Fatal("validation changed saved credentials")
			}
		})
	}
}

func TestAuthenticationManualCompatibility(t *testing.T) {
	r := modelRequest()
	r.Auth = Authentication{Token: "{{inactive}}", Password: "\x00"}
	r.Headers = []Pair{{"authorization", "manual"}, {"Authorization", "second"}}
	resolved, err := Resolve(r, Settings{GlobalHeaders: []Pair{{"Authorization", "{{overridden}}"}}})
	if err != nil || FormatHeaders(resolved.Headers) != FormatHeaders(r.Headers) {
		t.Fatalf("manual mode changed existing headers: %v", err)
	}
}

func TestAuthenticationTemplateValidation(t *testing.T) {
	for _, a := range []Authentication{
		{Mode: AuthBearer, Token: "{{missing}}"},
		{Mode: AuthBasic, Username: "{{user}}", Password: "{{password}}"},
		{Mode: AuthAPIKey, Key: "X-{{header}}", Value: "{{value}}"},
		{Mode: AuthAPIKey, Location: APIKeyQuery, Key: "{{name}}", Value: "{{value}}"},
		{Token: "{{inactive}}", Password: "\x00"},
	} {
		if err := ValidateAuthentication(a); err != nil {
			t.Fatalf("saved auth template must validate without environment: %v", err)
		}
	}
	if err := ValidateAuthentication(Authentication{Mode: AuthBasic, Username: "bad:name"}); err == nil {
		t.Fatal("invalid template accepted")
	}
}

func TestAuthenticationRedirectIsolation(t *testing.T) {
	for _, auth := range []Authentication{
		{Mode: AuthBearer, Token: "private_token"},
		{Mode: AuthBasic, Username: "user", Password: "private_password"},
		{Mode: AuthAPIKey, Key: "X-API-Key", Value: "private_key"},
		{Mode: AuthAPIKey, Location: APIKeyQuery, Key: "access_key", Value: "private_query"},
	} {
		t.Run(string(auth.Mode)+string(auth.Location), func(t *testing.T) {
			seen := make(chan *http.Request, 2)
			var originURL string
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen <- r.Clone(r.Context())
				http.Redirect(w, r, originURL+"/final", http.StatusFound)
			}))
			defer destination.Close()
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/final":
					seen <- r.Clone(r.Context())
					w.WriteHeader(http.StatusNoContent)
				default:
					http.Redirect(w, r, destination.URL+"/?access_key=private_query&keep=%2f", http.StatusFound)
				}
			}))
			defer origin.Close()
			originURL = origin.URL
			r := NewRequest()
			r.Auth, r.URL = auth, origin.URL+"/?keep=%2f"
			resolved, err := Resolve(r, Settings{})
			if err != nil {
				t.Fatal(err)
			}
			e := NewEngine()
			defer e.Close()
			if err := e.Run(context.Background(), resolved, func(Event) {}); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				got := <-seen
				if got.Header.Get("Authorization") != "" || got.Header.Get("X-API-Key") != "" {
					t.Fatal("credential header sent across origin or restored after returning")
				}
				if auth.Location == APIKeyQuery &&
					(got.URL.Query().Get(auth.Key) != "" || strings.Contains(got.Referer(), "private_query")) {
					t.Fatal("query credential leaked through redirect URL or Referer")
				}
			}
		})
	}
}

func TestAuthenticationSameOriginRedirect(t *testing.T) {
	for _, auth := range []Authentication{
		{Mode: AuthBearer, Token: "test-token"},
		{Mode: AuthAPIKey, Key: "X-Key", Value: "test-key"},
		{Mode: AuthAPIKey, Location: APIKeyQuery, Key: "key", Value: "test-query"},
	} {
		t.Run(string(auth.Mode)+string(auth.Location), func(t *testing.T) {
			seen := make(chan *http.Request, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/" {
					http.Redirect(w, r, "/final?key=test-query&keep=%2f", http.StatusFound)
				} else {
					seen <- r.Clone(r.Context())
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			defer server.Close()
			r := NewRequest()
			r.URL, r.Auth = server.URL, auth
			resolved, err := Resolve(r, Settings{})
			if err != nil {
				t.Fatal(err)
			}
			e := NewEngine()
			defer e.Close()
			result, err := e.DoHTTP(context.Background(), resolved, HTTPOptions{})
			if err != nil || result.StatusCode != http.StatusNoContent {
				t.Fatalf("headless redirect failed: %v", err)
			}
			got := <-seen
			if header, ok := authenticationHeader(auth); ok && got.Header.Get(header.Key) != header.Value {
				t.Fatal("same origin redirect lost auth")
			}
			if auth.Location == APIKeyQuery && (got.URL.Query().Get(auth.Key) != auth.Value || got.Referer() != "") {
				t.Fatal("same origin query auth changed or leaked into Referer")
			}
		})
	}
}

func TestAuthenticationStreams(t *testing.T) {
	for _, protocol := range []string{"SSE", "WebSocket"} {
		t.Run(protocol, func(t *testing.T) {
			seen := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen <- r.Header.Get("Authorization")
				if protocol == "SSE" {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte("data: authenticated\n\n"))
				} else {
					conn, err := websocket.Accept(w, r, nil)
					if err == nil {
						defer conn.CloseNow()
						_ = conn.Write(r.Context(), websocket.MessageText, []byte("authenticated"))
					}
				}
			}))
			defer server.Close()
			r := NewRequest()
			r.Protocol, r.URL = protocol, server.URL
			if protocol == "WebSocket" {
				r.URL = strings.Replace(r.URL, "http://", "ws://", 1)
			}
			r.Auth = Authentication{Mode: AuthBearer, Token: "{{token}}"}
			resolved, err := Resolve(r, Settings{Environment: "env", Environments: map[string]map[string]string{"env": {"token": "stream-token"}}})
			if err != nil {
				t.Fatal(err)
			}
			e := NewEngine()
			defer e.Close()
			if protocol == "SSE" {
				var stream string
				err = e.Run(context.Background(), resolved, func(event Event) {
					if event.Kind == "sse" {
						stream += event.Text
					}
				})
				if err != nil || !strings.Contains(stream, "authenticated") {
					t.Fatalf("SSE auth failed: %v; %q", err, stream)
				}
			} else {
				socket, event, err := e.Connect(context.Background(), resolved)
				if err != nil || !strings.Contains(event.Status, "101") {
					t.Fatalf("WebSocket auth handshake failed: %v", err)
				}
				socket.Close()
			}
			if <-seen != "Bearer stream-token" {
				t.Fatal("stream auth missing")
			}
		})
	}
}

func TestAuthenticationManagedHeaders(t *testing.T) {
	for _, tc := range []struct {
		key  string
		mode BodyMode
	}{
		{"Host", BodyRaw},
		{"Content-Type", BodyMultipart},
	} {
		r := modelRequest()
		r.BodyMode = tc.mode
		r.Auth = Authentication{Mode: AuthAPIKey, Key: "{{key}}", Value: "private_key"}
		_, err := Resolve(r, Settings{Environment: "env", Environments: map[string]map[string]string{"env": {"key": tc.key}}})
		if err == nil {
			t.Errorf("accepted API Key header %s that the transport discards or overwrites", tc.key)
		}
		r.Auth.Key = tc.key
		if err := ValidateRequestAuthentication(r); err == nil {
			t.Errorf("saved template accepts managed auth header %s", tc.key)
		}
		if err := validateCollectionRequest(r); err == nil {
			t.Errorf("native collection accepts managed auth header %s", tc.key)
		}
	}
}

func TestAuthenticationNetworkErrorsHideQueryCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := server.URL
	server.Close()
	for _, protocol := range []string{"HTTP", "SSE", "WebSocket"} {
		r := NewRequest()
		r.Protocol, r.URL = protocol, address
		if protocol == "WebSocket" {
			r.URL = strings.Replace(r.URL, "http://", "ws://", 1)
		}
		r.Auth = Authentication{Mode: AuthAPIKey, Key: "api_key", Value: "private_key"}
		r.Auth.Location = APIKeyQuery
		resolved, err := Resolve(r, Settings{})
		if err != nil {
			t.Fatal(err)
		}
		e := NewEngine()
		if protocol == "WebSocket" {
			_, _, err = e.Connect(context.Background(), resolved)
		} else {
			err = e.Run(context.Background(), resolved, func(Event) {})
		}
		if err == nil || strings.Contains(err.Error(), "private_key") {
			t.Errorf("%s failure exposes query credentials: %v", protocol, err)
		}
		if protocol == "HTTP" {
			_, err = e.DoHTTP(context.Background(), resolved, HTTPOptions{})
			var typed *HTTPError
			if err == nil || strings.Contains(err.Error(), "private_key") || !errors.As(err, &typed) || typed.Code != HTTPErrConnect {
				t.Errorf("headless error leaked auth or lost classification: %v", err)
			}
		}
		e.Close()
	}
}

func TestAuthenticationQuerySkipsOverriddenTemplates(t *testing.T) {
	for _, query := range []string{
		"api_key={{missing}}&keep={{keep}}&flag",
		"{{key_name}}={{missing}}&api_key={{also_missing}}&keep={{keep}}&flag",
	} {
		r := modelRequest()
		r.URL = "https://example.test/?" + query + "#anchor"
		r.Auth = Authentication{Mode: AuthAPIKey, Key: "{{key_name}}", Value: "{{key}}", Location: APIKeyQuery}
		settings := Settings{Environment: "env", Environments: map[string]map[string]string{
			"env": {"key_name": "api_key", "key": "{{literal}}", "keep": "a +&"},
		}}
		before := modelSnapshot(t, r)
		got, err := Resolve(r, settings)
		if err != nil || got.URL != "https://example.test/?api_key=%7B%7Bliteral%7D%7D&keep=a+%2B%26&flag#anchor" {
			t.Fatalf("discarded query values must not expand: url=%q error=%v", got.URL, err)
		}
		if modelSnapshot(t, r) != before {
			t.Fatal("query credential changed template")
		}
	}
	r := modelRequest()
	r.URL = "{{full_url}}"
	r.Auth = Authentication{Mode: AuthAPIKey, Key: "api_key", Value: "private_key", Location: APIKeyQuery}
	got, err := Resolve(r, Settings{Environment: "env", Environments: map[string]map[string]string{
		"env": {"full_url": "https://example.test/?api_key=old&keep=%2f#anchor"},
	}})
	if err != nil || got.URL != "https://example.test/?api_key=private_key&keep=%2f#anchor" {
		t.Fatalf("auth broke full URL variables: %q %v", got.URL, err)
	}
}
