package core

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHeadlessIntegrationTLSAndDNS(t *testing.T) {
	t.Run("TLS", func(t *testing.T) {
		server, ca := engineTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			io.WriteString(w, "trusted")
		}), nil)
		e := engineFixture(t)
		r := engineRequest(server.URL)
		result, err := e.DoHTTP(t.Context(), r, HTTPOptions{})
		headlessIntegrationError(t, err, HTTPErrTLS)
		if result.StatusCode != 0 || result.Timing.TLS <= 0 || result.Timing.TTFB != 0 {
			t.Fatalf("untrusted response/timing = %+v", result)
		}
		if err := e.SetCACertificates(ca); err != nil {
			t.Fatal(err)
		}
		result, err = e.DoHTTP(t.Context(), r, HTTPOptions{})
		if err != nil || result.StatusCode != 200 || string(result.Body) != "trusted" ||
			result.Timing.TLS <= 0 || result.Timing.TTFB <= 0 {
			t.Fatalf("trusted response = %+v, error = %v", result, err)
		}
	})
	t.Run("DNS", func(t *testing.T) {
		e := engineFixture(t)
		dnsErr := &net.DNSError{Err: "fixture lookup failed", Name: "api.test", IsNotFound: true}
		var observed atomic.Bool
		e.transport.DialContext = func(ctx context.Context, _, address string) (net.Conn, error) {
			if address != "api.test:80" {
				t.Errorf("unexpected dial %q", address)
			}
			trace := httptrace.ContextClientTrace(ctx)
			if trace == nil || trace.DNSStart == nil || trace.DNSDone == nil {
				t.Error("DNS trace callbacks missing")
				return nil, dnsErr
			}
			trace.DNSStart(httptrace.DNSStartInfo{Host: "api.test"})
			trace.DNSDone(httptrace.DNSDoneInfo{Err: dnsErr})
			observed.Store(true)
			return nil, dnsErr
		}
		result, err := e.DoHTTP(t.Context(), engineRequest("http://api.test"), HTTPOptions{})
		headlessIntegrationError(t, err, HTTPErrConnect)
		if !errors.Is(err, dnsErr) || !observed.Load() || result.StatusCode != 0 ||
			result.Timing.DNS <= 0 || result.Timing.TTFB != 0 || result.Timing.TLS != 0 {
			t.Fatalf("DNS result = %+v, error = %v, observed = %v", result, err, observed.Load())
		}
	})
}

func TestHeadlessIntegrationRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			if r.Method != http.MethodPost {
				t.Errorf("initial method = %s", r.Method)
			}
			w.Header().Set("Location", "/final")
			w.WriteHeader(http.StatusFound)
			io.WriteString(w, "original redirect")
		case "/final":
			body, err := io.ReadAll(r.Body)
			if err != nil || r.Method != http.MethodGet || len(body) != 0 {
				t.Errorf("final method/body = %s %q, %v", r.Method, body, err)
			}
			io.WriteString(w, "final")
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		}
	}))
	defer server.Close()
	for _, follow := range []bool{true, false} {
		r := engineRequest(server.URL + "/start")
		r.Method, r.Body, r.FollowRedirects = http.MethodPost, "posted", follow
		result, err := engineFixture(t).DoHTTP(t.Context(), r, HTTPOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if follow {
			if result.Method != http.MethodGet || result.URL != server.URL+"/final" ||
				result.StatusCode != 200 || string(result.Body) != "final" {
				t.Fatalf("followed result = %+v", result)
			}
		} else if result.Method != http.MethodPost || result.URL != r.URL ||
			result.StatusCode != 302 || result.Headers.Get("Location") != "/final" ||
			string(result.Body) != "original redirect" {
			t.Fatalf("unfollowed result = %+v", result)
		}
	}
	result, err := engineFixture(t).DoHTTP(t.Context(), engineRequest(server.URL+"/loop"), HTTPOptions{})
	headlessIntegrationError(t, err, HTTPErrRedirect)
	if result.StatusCode != 302 || result.URL != server.URL+"/loop" || result.Timing.TTFB <= 0 {
		t.Fatalf("loop partial result = %+v", result)
	}
}

func TestHeadlessIntegrationRedirectCredentials(t *testing.T) {
	for _, crossHost := range []bool{false, true} {
		name := "port"
		if crossHost {
			name = "host"
		}
		t.Run(name, func(t *testing.T) {
			var finalCalls atomic.Int32
			checkInitial := func(r *http.Request) {
				for _, key := range []string{"Authorization", "Cookie", "Proxy-Authorization"} {
					if r.Header.Get(key) == "" {
						t.Errorf("initial request missing %s", key)
					}
				}
			}
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if crossHost && r.URL.Path == "/initial" {
					checkInitial(r)
					_, port, err := net.SplitHostPort(r.Host)
					if err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					http.Redirect(w, r, "http://"+net.JoinHostPort("other.test", port)+"/final", http.StatusFound)
					return
				}
				finalCalls.Add(1)
				for _, key := range []string{"Authorization", "Cookie", "Proxy-Authorization"} {
					if r.Header.Get(key) != "" {
						t.Errorf("redirect forwarded %s", key)
					}
				}
				if r.Header.Get("X-Keep") != "kept" {
					t.Error("ordinary header was stripped")
				}
				io.WriteString(w, "safe")
			}))
			defer target.Close()
			finalURL := target.URL + "/final"
			if crossHost {
				finalURL = strings.Replace(finalURL, "127.0.0.1", "other.test", 1)
			}
			initial := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				checkInitial(r)
				http.Redirect(w, r, finalURL, http.StatusFound)
			}))
			defer initial.Close()
			e := engineFixture(t)
			e.transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(address)
				if err != nil {
					return nil, err
				}
				if host == "other.test" {
					address = net.JoinHostPort("127.0.0.1", port)
				} else if host != "127.0.0.1" {
					return nil, errors.New("nonlocal dial forbidden")
				}
				return (&net.Dialer{}).DialContext(ctx, network, address)
			}
			r := engineRequest(initial.URL)
			if crossHost {
				r.URL = target.URL + "/initial"
			}
			r.Headers = []Pair{
				{Key: "Authorization", Value: "Bearer private"},
				{Key: "Cookie", Value: "session=private"},
				{Key: "Proxy-Authorization", Value: "Basic private"},
				{Key: "X-Keep", Value: "kept"},
			}
			result, err := e.DoHTTP(t.Context(), r, HTTPOptions{})
			if err != nil || result.URL != finalURL || result.StatusCode != 200 ||
				string(result.Body) != "safe" || finalCalls.Load() != 1 {
				t.Fatalf("redirect result = %+v, error = %v, calls = %d", result, err, finalCalls.Load())
			}
		})
	}
}

func TestHeadlessIntegrationMultipart(t *testing.T) {
	for _, changed := range []bool{false, true} {
		name := "replay307"
		if changed {
			name = "changedSize"
		}
		t.Run(name, func(t *testing.T) {
			payload := []byte("original upload")
			path := uploadFile(t, "payload.txt", payload)
			initialBody := make(chan []byte, 1)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				body, err := io.ReadAll(r.Body)
				if err != nil || r.Method != http.MethodPost {
					t.Errorf("upload method/body error = %s, %v", r.Method, err)
				}
				r.Body = io.NopCloser(bytes.NewReader(body))
				if err := r.ParseMultipartForm(MaxInput); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				defer r.MultipartForm.RemoveAll()
				file, header, err := r.FormFile("file")
				if err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				data, readErr := io.ReadAll(file)
				file.Close()
				if readErr != nil || !bytes.Equal(data, payload) || header.Filename != "payload.txt" ||
					r.FormValue("note") != "text part" {
					t.Errorf("multipart payload/metadata = %q, %v", data, readErr)
				}
				if r.URL.Path == "/start" {
					initialBody <- body
					if changed {
						if err := os.WriteFile(path, []byte("changed upload with different size"), 0600); err != nil {
							t.Error(err)
						}
					}
					w.Header().Set("Location", "/final")
					w.WriteHeader(http.StatusTemporaryRedirect)
				} else {
					if !bytes.Equal(body, <-initialBody) {
						t.Error("redirect changed multipart bytes or boundary")
					}
					io.WriteString(w, "uploaded")
				}
			}))
			defer server.Close()
			r := engineRequest(server.URL + "/start")
			r.Method, r.BodyMode = http.MethodPost, BodyMultipart
			r.FormParts = []FormPart{
				{Kind: FormText, Name: "note", Value: "text part"},
				{Kind: FormFile, Name: "file", FilePath: path},
			}
			result, err := engineFixture(t).DoHTTP(t.Context(), r, HTTPOptions{})
			if changed {
				headlessIntegrationError(t, err, HTTPErrRedirect)
				if calls.Load() != 1 {
					t.Fatal("size-changed upload reached redirect target")
				}
			} else if err != nil || result.StatusCode != 200 || result.URL != server.URL+"/final" ||
				string(result.Body) != "uploaded" || calls.Load() != 2 {
				t.Fatalf("multipart result = %+v, error = %v, calls = %d", result, err, calls.Load())
			}
			r.FormParts[1].FilePath = path + ".missing"
			_, err = engineFixture(t).DoHTTP(t.Context(), r, HTTPOptions{})
			headlessIntegrationError(t, err, HTTPErrRequestBuild)
			if !errors.Is(err, os.ErrNotExist) || calls.Load() != map[bool]int32{true: 1, false: 2}[changed] {
				t.Fatalf("missing upload did not fail before transport: %v", err)
			}
		})
	}
}

func TestHeadlessIntegrationProxyRouting(t *testing.T) {
	var proxyCalls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		if !r.URL.IsAbs() || r.URL.Host != "api.test" {
			t.Errorf("proxy request URL = %s", r.URL)
		}
		io.WriteString(w, "proxy")
	}))
	defer proxy.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "direct")
	}))
	defer target.Close()
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		t.Setenv(key, proxy.URL)
	}
	t.Setenv("NO_PROXY", "*")
	t.Setenv("no_proxy", "*")
	for _, tc := range []struct {
		name, address, bypass, want string
		explicit                    bool
	}{
		{"explicit", "http://api.test", "", "proxy", true},
		{"loopback", target.URL, "", "direct", true},
		{"bypass", "http://api.test", "api.test", "direct", true},
		{"environmentIgnored", "http://api.test", "", "direct", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.explicit {
				t.Setenv("NO_PROXY", "")
				t.Setenv("no_proxy", "")
			}
			before := proxyCalls.Load()
			e := engineFixture(t)
			if tc.explicit {
				if err := e.SetProxy(ProxySettings{URL: proxy.URL, Bypass: tc.bypass}); err != nil {
					t.Fatal(err)
				}
			}
			e.transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				if address == "api.test:80" {
					address = target.Listener.Addr().String()
				}
				host, _, err := net.SplitHostPort(address)
				if err != nil || host != "127.0.0.1" {
					return nil, errors.New("nonlocal dial forbidden")
				}
				return (&net.Dialer{}).DialContext(ctx, network, address)
			}
			result, err := e.DoHTTP(t.Context(), engineRequest(tc.address), HTTPOptions{})
			if err != nil || result.StatusCode != 200 || string(result.Body) != tc.want {
				t.Fatalf("routing result = %+v, error = %v", result, err)
			}
			wantCalls := before
			if tc.want == "proxy" {
				wantCalls++
			}
			if proxyCalls.Load() != wantCalls {
				t.Fatalf("proxy calls = %d, want %d", proxyCalls.Load(), wantCalls)
			}
		})
	}
	secure, ca := engineTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "secure direct")
	}), func(cert *x509.Certificate) {
		cert.DNSNames = append(cert.DNSNames, "api.test")
	})
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	e := engineFixture(t)
	if err := e.SetCACertificates(ca); err != nil {
		t.Fatal(err)
	}
	secureAddress := secure.Listener.Addr().String()
	_, port, err := net.SplitHostPort(secureAddress)
	if err != nil {
		t.Fatal(err)
	}
	address := net.JoinHostPort("api.test", port)
	e.transport.DialContext = func(ctx context.Context, network, dialAddress string) (net.Conn, error) {
		switch dialAddress {
		case address:
			dialAddress = secureAddress
		case proxy.Listener.Addr().String():
		default:
			return nil, errors.New("nonlocal dial forbidden")
		}
		return (&net.Dialer{}).DialContext(ctx, network, dialAddress)
	}
	before := proxyCalls.Load()
	result, err := e.DoHTTP(t.Context(), engineRequest("https://"+address), HTTPOptions{})
	if err != nil || string(result.Body) != "secure direct" || proxyCalls.Load() != before {
		t.Fatalf("HTTPS environment proxy was not ignored: %+v, %v", result, err)
	}
}

func TestHeadlessIntegrationResponseFailuresAndGzip(t *testing.T) {
	const applicationBody = "{ \"b\": 2,\n  \"a\": 1 }\n"
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/short":
			w.Header().Set("Content-Length", "100")
			w.Header().Set("X-Partial", "retained")
			io.WriteString(w, "partial")
		case "/headers":
			w.Header().Set("X-Large", strings.Repeat("x", (256<<10)+1))
			io.WriteString(w, "unread")
		case "/timeout":
			io.Copy(io.Discard, r.Body)
			select {
			case <-r.Context().Done():
			case <-release:
			}
		case "/gzip":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Encoding", "gzip")
			z := gzip.NewWriter(w)
			io.WriteString(z, applicationBody)
			z.Close()
		}
	}))
	defer server.Close()
	defer close(release)
	t.Run("shortBody", func(t *testing.T) {
		result, err := engineFixture(t).DoHTTP(t.Context(), engineRequest(server.URL+"/short"), HTTPOptions{})
		headlessIntegrationError(t, err, HTTPErrBodyRead)
		if !errors.Is(err, io.ErrUnexpectedEOF) || result.StatusCode != 200 || string(result.Body) != "partial" ||
			result.Headers.Get("X-Partial") != "retained" || result.BodyTruncated || result.Timing.TTFB <= 0 {
			t.Fatalf("short body result = %+v, error = %v", result, err)
		}
	})
	t.Run("headerLimit", func(t *testing.T) {
		result, err := engineFixture(t).DoHTTP(t.Context(), engineRequest(server.URL+"/headers"), HTTPOptions{})
		headlessIntegrationError(t, err, HTTPErrConnect)
		if result.StatusCode != 0 || len(result.Body) != 0 {
			t.Fatalf("oversized headers yielded a response = %+v", result)
		}
	})
	t.Run("contextTimeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		result, err := engineFixture(t).DoHTTP(ctx, engineRequest(server.URL+"/timeout"), HTTPOptions{})
		headlessIntegrationError(t, err, HTTPErrTimeout)
		if !errors.Is(err, context.DeadlineExceeded) || result.StatusCode != 0 || result.Timing.TTFB != 0 {
			t.Fatalf("timeout result = %+v, error = %v", result, err)
		}
	})
	t.Run("gzipApplicationBytes", func(t *testing.T) {
		result, err := engineFixture(t).DoHTTP(t.Context(), engineRequest(server.URL+"/gzip"), HTTPOptions{})
		if err != nil || string(result.Body) != applicationBody || result.BodyTruncated ||
			result.Headers.Get("Content-Encoding") != "" || result.Headers.Get("Content-Type") != "application/json" {
			t.Fatalf("gzip result = %+v, error = %v", result, err)
		}
	})
}

func headlessIntegrationError(t *testing.T, err error, code HTTPErrorCode) {
	t.Helper()
	var typed *HTTPError
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}
