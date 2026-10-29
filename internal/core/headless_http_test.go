package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
)

type headlessHTTPResponseTransport struct{ response *http.Response }

func (t headlessHTTPResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return t.response, nil
}

func TestDoHTTPRawResponses(t *testing.T) {
	for _, status := range []int{200, 400, 404, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Add("X-Multi", "a")
				w.Header().Add("X-Multi", "b")
				w.WriteHeader(status)
				io.WriteString(w, `{"a":1}`)
			}))
			defer srv.Close()
			e := engineFixture(t)
			r := NewRequest()
			r.URL = srv.URL
			for _, limit := range []int{0, 3} {
				t.Run(fmt.Sprintf("limit=%d", limit), func(t *testing.T) {
					result, err := e.DoHTTP(context.Background(), r, HTTPOptions{MaxBodyBytes: limit})
					if err != nil {
						t.Fatal(err)
					}
					if result.Method != r.Method || result.URL != r.URL || result.Proto != "HTTP/1.1" ||
						result.StatusCode != status || result.Status != fmt.Sprintf("%d %s", status, http.StatusText(status)) {
						t.Fatalf("response metadata = %+v", result)
					}
					if got := result.Headers.Values("X-Multi"); !reflect.DeepEqual(got, []string{"a", "b"}) {
						t.Fatalf("X-Multi = %v, want [a b]", got)
					}
					want := `{"a":1}`
					if limit == 3 {
						want = `{"a`
					}
					if string(result.Body) != want || result.BodyTruncated != (limit == 3) {
						t.Fatalf("body = %q, truncated = %v", result.Body, result.BodyTruncated)
					}
					if result.Timing.Total <= 0 || result.Timing.TTFB <= 0 || result.Timing.TLS != 0 {
						t.Fatalf("timing = %+v", result.Timing)
					}
				})
			}
		})
	}
}

func TestDoHTTPCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := NewRequest()
	r.URL = "http://127.0.0.1:1"
	result, err := engineFixture(t).DoHTTP(ctx, r, HTTPOptions{})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Code != HTTPErrCanceled || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
	if result.Timing.Total <= 0 || result.Timing.TTFB != 0 {
		t.Fatalf("canceled timing = %+v", result.Timing)
	}
}

func TestDoHTTPReadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "10")
		w.Header().Set("X-Reply", "partial")
		io.WriteString(w, "abc")
	}))
	defer srv.Close()
	r := NewRequest()
	r.URL = srv.URL
	result, err := engineFixture(t).DoHTTP(context.Background(), r, HTTPOptions{})
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Code != HTTPErrBodyRead || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("read error = %v", err)
	}
	if string(result.Body) != "abc" || result.BodyTruncated || result.StatusCode != 200 ||
		result.Headers.Get("X-Reply") != "partial" || result.Method != r.Method || result.URL != r.URL {
		t.Fatalf("partial response = %+v", result)
	}
	if result.Timing.Total <= 0 || result.Timing.TTFB <= 0 {
		t.Fatalf("read error timing = %+v", result.Timing)
	}
}

func TestDoHTTPMultipartRedirectRemovedFile(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			for _, follow := range []bool{false, true} {
				t.Run(fmt.Sprintf("followRedirects=%v", follow), func(t *testing.T) {
					const payload = "original file payload"
					path := uploadFile(t, "upload.txt", []byte(payload))
					uploaded := make(chan struct{}, 1)
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						if req.URL.Path != "/initial" || req.Method != http.MethodPost {
							t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						if err := req.ParseMultipartForm(MaxInput); err != nil {
							t.Error(err)
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						defer req.MultipartForm.RemoveAll()
						file, header, err := req.FormFile("file")
						if err != nil {
							t.Error(err)
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						defer file.Close()
						data, err := io.ReadAll(file)
						if err != nil || string(data) != payload || header.Filename != "upload.txt" ||
							req.FormValue("note") != "original text" {
							t.Errorf("original upload: file = %q, filename = %q, note = %q, error = %v",
								data, header.Filename, req.FormValue("note"), err)
							w.WriteHeader(http.StatusBadRequest)
							return
						}
						if err := os.Remove(path); err != nil {
							t.Error(err)
							w.WriteHeader(http.StatusInternalServerError)
							return
						}
						uploaded <- struct{}{}
						w.Header().Set("Location", "/final")
						w.Header().Set("Content-Type", "text/plain")
						w.Header().Add("X-Multi", "a")
						w.Header().Add("X-Multi", "b")
						w.WriteHeader(status)
						io.WriteString(w, "redirect body")
					}))
					defer srv.Close()
					r := NewRequest()
					r.URL, r.Method = srv.URL+"/initial", http.MethodPost
					r.BodyMode, r.FollowRedirects = BodyMultipart, follow
					r.FormParts = []FormPart{
						{Kind: FormText, Name: "note", Value: "original text"},
						{Kind: FormFile, Name: "file", FilePath: path},
					}
					result, err := engineFixture(t).DoHTTP(context.Background(), r, HTTPOptions{})
					select {
					case <-uploaded:
					default:
						t.Fatal("original upload was not verified and removed before redirect")
					}
					if follow {
						var httpErr *HTTPError
						if !errors.As(err, &httpErr) || httpErr.Code != HTTPErrRedirect ||
							!errors.Is(err, os.ErrNotExist) {
							t.Fatalf("removed file replay error = %v, want REDIRECT_ERROR with missing-file cause", err)
						}
						return
					}
					if err != nil {
						t.Fatalf("disabled multipart redirects: error = %v", err)
					}
					if result.Method != http.MethodPost || result.URL != r.URL ||
						result.StatusCode != status || result.Status != fmt.Sprintf("%d %s", status, http.StatusText(status)) {
						t.Fatalf("initial redirect response metadata = %+v", result)
					}
					if result.Headers.Get("Location") != "/final" || result.Headers.Get("Content-Type") != "text/plain" ||
						!reflect.DeepEqual(result.Headers.Values("X-Multi"), []string{"a", "b"}) {
						t.Fatalf("initial redirect response headers = %v", result.Headers)
					}
					if string(result.Body) != "redirect body" || result.BodyTruncated {
						t.Fatalf("initial redirect response body = %q, truncated = %v", result.Body, result.BodyTruncated)
					}
				})
			}
		})
	}
}

func TestDoHTTPRedirectErrors(t *testing.T) {
	t.Run("malformedLocation", func(t *testing.T) {
		for _, status := range []int{301, 302, 303, 307, 308} {
			t.Run(fmt.Sprint(status), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Location", "http://%zz")
					w.WriteHeader(status)
					io.WriteString(w, "redirect body")
				}))
				defer srv.Close()
				for _, follow := range []bool{true, false} {
					t.Run(fmt.Sprintf("followRedirects=%v", follow), func(t *testing.T) {
						r := NewRequest()
						r.URL, r.FollowRedirects = srv.URL, follow
						result, err := engineFixture(t).DoHTTP(context.Background(), r, HTTPOptions{})
						if follow {
							var httpErr *HTTPError
							if !errors.As(err, &httpErr) || httpErr.Code != HTTPErrRedirect {
								t.Fatalf("malformed Location error = %v, want REDIRECT_ERROR", err)
							}
							if !errors.Is(err, url.EscapeError("%zz")) {
								t.Fatalf("malformed Location error lost URL escape cause: %v", err)
							}
						} else {
							if err != nil || string(result.Body) != "redirect body" {
								t.Fatalf("disabled redirects: body = %q, error = %v", result.Body, err)
							}
							if result.Method != http.MethodGet || result.URL != srv.URL {
								t.Fatalf("disabled redirects: method = %q, URL = %q", result.Method, result.URL)
							}
						}
						if result.StatusCode != status || result.Headers.Get("Location") != "http://%zz" ||
							result.Method != r.Method || result.URL != r.URL {
							t.Fatalf("redirect response = %+v", result)
						}
						if result.Timing.Total <= 0 || result.Timing.TTFB <= 0 {
							t.Fatalf("redirect timing = %+v", result.Timing)
						}
					})
				}
			})
		}
	})

	t.Run("malformedLocationWithoutRequest", func(t *testing.T) {
		original := &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": {"http://%zz"}},
			Body:       io.NopCloser(strings.NewReader("redirect body")),
		}
		transport := &httpRedirectTransport{Transport: &http.Transport{}}
		transport.RegisterProtocol("http", headlessHTTPResponseTransport{response: original})
		defer transport.CloseIdleConnections()
		req, err := http.NewRequest(http.MethodGet, "http://example.test/initial", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := (&http.Client{Transport: transport}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if transport.response != original {
			t.Fatal("original redirect response not retained")
		}
		result := responseHTTPResult(transport.response)
		body, err := io.ReadAll(transport.response.Body)
		if err != nil || string(body) != "redirect body" || result.StatusCode != http.StatusFound ||
			result.Headers.Get("Location") != "http://%zz" ||
			result.Method != http.MethodGet || result.URL != req.URL.String() {
			t.Fatalf("captured response = %+v, body = %q, error = %v", result, body, err)
		}
	})

	t.Run("validLocationConnectionFailure", func(t *testing.T) {
		target := httptest.NewServer(http.NotFoundHandler())
		target.Close()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", target.URL)
			w.WriteHeader(http.StatusFound)
		}))
		defer srv.Close()
		r := NewRequest()
		r.URL = srv.URL
		_, err := engineFixture(t).DoHTTP(context.Background(), r, HTTPOptions{})
		var httpErr *HTTPError
		var connectionErr *net.OpError
		if !errors.As(err, &httpErr) || httpErr.Code != HTTPErrConnect ||
			!errors.As(err, &connectionErr) || connectionErr.Op != "dial" {
			t.Fatalf("connection failure after valid redirect = %v, want CONNECT_ERROR with dial cause", err)
		}
	})

	t.Run("nonRedirectLocation", func(t *testing.T) {
		for _, status := range []int{200, 300, 304, 400} {
			t.Run(fmt.Sprint(status), func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Location", "http://%zz")
					w.WriteHeader(status)
				}))
				defer srv.Close()
				r := NewRequest()
				r.URL = srv.URL
				result, err := engineFixture(t).DoHTTP(context.Background(), r, HTTPOptions{})
				if err != nil || result.StatusCode != status || result.Headers.Get("Location") != "http://%zz" {
					t.Fatalf("non-redirect response = %+v, error = %v", result, err)
				}
			})
		}
	})
}
