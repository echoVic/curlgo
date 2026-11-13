package core

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// proxyFixture is a minimal forward proxy: it relays plain HTTP requests and
// tunnels CONNECT, recording what it saw.
type proxyFixture struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
	auth     []string
}

func (p *proxyFixture) seen() ([]string, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.requests...), append([]string(nil), p.auth...)
}

// Resolve only fixture names locally; Windows does not resolve *.localhost.
// Keep the request host unchanged so proxy bypass and TLS hostname checks run.
func dialProxyFixture(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	switch host {
	case "proxy-test.localhost", "internal.localhost", "api.corp.localhost":
		address = net.JoinHostPort("127.0.0.1", port)
	}
	return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, address)
}

func proxyEngineFixture(t *testing.T) *Engine {
	t.Helper()
	e := engineFixture(t)
	e.transport.DialContext = dialProxyFixture
	return e
}

func newProxyFixture(t *testing.T, requireAuth string) *proxyFixture {
	t.Helper()
	p := &proxyFixture{}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialProxyFixture
	t.Cleanup(transport.CloseIdleConnections)
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.requests = append(p.requests, r.Method+" "+r.Host)
		p.auth = append(p.auth, r.Header.Get("Proxy-Authorization"))
		p.mu.Unlock()
		if requireAuth != "" && r.Header.Get("Proxy-Authorization") != requireAuth {
			w.Header().Set("Proxy-Authenticate", `Basic realm="proxy"`)
			http.Error(w, "proxy auth required", http.StatusProxyAuthRequired)
			return
		}
		if r.Method == http.MethodConnect || strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			target, err := dialProxyFixture(r.Context(), "tcp", r.Host)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				target.Close()
				return
			}
			if r.Method == http.MethodConnect {
				fmt.Fprint(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
			} else {
				// Forward the upgrade request as-is and splice the connections.
				r.Header.Del("Proxy-Authorization")
				r.Write(target)
			}
			go func() { io.Copy(target, buf); target.Close() }()
			io.Copy(conn, target)
			conn.Close()
			return
		}
		r.RequestURI = ""
		r.Header.Del("Proxy-Authorization")
		resp, err := transport.RoundTrip(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.Header().Set("X-Via-Proxy", "yes")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	t.Cleanup(p.Close)
	return p
}

func TestParseProxy(t *testing.T) {
	for _, tc := range []struct{ input, want, err string }{
		{input: "", want: ""},
		{input: "  ", want: ""},
		{input: "http://127.0.0.1:8888", want: "http://127.0.0.1:8888"},
		{input: "127.0.0.1:8888", want: "http://127.0.0.1:8888"},
		{input: "https://proxy.internal:443", want: "https://proxy.internal:443"},
		{input: "socks5://127.0.0.1:1080", want: "socks5://127.0.0.1:1080"},
		{input: "http://user:p%40ss@proxy.internal:8080", want: "http://user:p%40ss@proxy.internal:8080"},
		{input: "ftp://proxy:21", err: "代理"},
		{input: "http://", err: "代理"},
		{input: "http://proxy:8080/path", err: "代理"},
		{input: "http://proxy:8080?x=1", err: "代理"},
		{input: "http://proxy\n:8080", err: "代理"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseProxy(tc.input)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("应拒绝 %q：%v", tc.input, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if s := ""; got != nil {
				s = got.String()
				if s != tc.want {
					t.Fatalf("got %q want %q", s, tc.want)
				}
			} else if tc.want != "" {
				t.Fatalf("got nil want %q", tc.want)
			}
		})
	}
}

func TestEngineHTTPProxy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "target")
	}))
	defer target.Close()
	proxy := newProxyFixture(t, "")
	e := proxyEngineFixture(t)
	// Loopback IPs and "localhost" bypass the proxy; *.localhost still uses it.
	address := strings.Replace(target.URL, "127.0.0.1", "proxy-test.localhost", 1)
	if err := e.SetProxy(ProxySettings{URL: proxy.URL}); err != nil {
		t.Fatal(err)
	}
	events, err := collectRun(e, context.Background(), engineRequest(address))
	if err != nil {
		t.Fatal(err)
	}
	if events[0].Headers.Get("X-Via-Proxy") != "yes" || bodyEvent(t, events).Text != "target" {
		t.Fatalf("请求没有经过代理：%+v", events)
	}
	requests, _ := proxy.seen()
	if len(requests) != 1 || !strings.HasPrefix(requests[0], "POST proxy-test.localhost:") {
		t.Fatalf("代理收到的请求错误：%v", requests)
	}
	if err := e.SetProxy(ProxySettings{}); err != nil {
		t.Fatal(err)
	}
	if events, err := collectRun(e, context.Background(), engineRequest(address)); err != nil || events[0].Headers.Get("X-Via-Proxy") != "" {
		t.Fatalf("清除代理后应直连：%v", err)
	}
}

func TestEngineProxyBypass(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "direct") }))
	defer target.Close()
	proxy := newProxyFixture(t, "")
	e := proxyEngineFixture(t)
	if err := e.SetProxy(ProxySettings{URL: proxy.URL, Bypass: "internal.localhost, .corp.localhost"}); err != nil {
		t.Fatal(err)
	}
	// Loopback is always direct, like curl and browsers.
	if events, err := collectRun(e, context.Background(), engineRequest(target.URL)); err != nil || bodyEvent(t, events).Text != "direct" {
		t.Fatalf("本机地址应直连：%v", err)
	}
	for _, host := range []string{"internal.localhost", "api.corp.localhost"} {
		events, err := collectRun(e, context.Background(), engineRequest(strings.Replace(target.URL, "127.0.0.1", host, 1)))
		if err != nil || bodyEvent(t, events).Text != "direct" {
			t.Fatalf("%s 应绕过代理直连：%v", host, err)
		}
	}
	if requests, _ := proxy.seen(); len(requests) != 0 {
		t.Fatalf("绕过列表中的地址仍经过代理：%v", requests)
	}
}

func TestEngineProxyAuthentication(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	defer target.Close()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("ada:p@ss"))
	proxy := newProxyFixture(t, want)
	e := proxyEngineFixture(t)
	address := strings.Replace(target.URL, "127.0.0.1", "proxy-test.localhost", 1)
	if err := e.SetProxy(ProxySettings{URL: strings.Replace(proxy.URL, "http://", "http://ada:p%40ss@", 1)}); err != nil {
		t.Fatal(err)
	}
	events, err := collectRun(e, context.Background(), engineRequest(address))
	if err != nil || events[0].Status != "200 OK" {
		t.Fatalf("代理认证失败：%v %+v", err, events)
	}
	if _, auth := proxy.seen(); len(auth) != 1 || auth[0] != want {
		t.Fatalf("代理认证头错误：%v", auth)
	}
}

func TestEngineHTTPSThroughProxyKeepsTLSVerification(t *testing.T) {
	srv, ca := engineTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "tunneled")
	}), func(c *x509.Certificate) { c.DNSNames = append(c.DNSNames, "proxy-test.localhost") })
	proxy := newProxyFixture(t, "")
	e := proxyEngineFixture(t)
	address := strings.Replace(srv.URL, "127.0.0.1", "proxy-test.localhost", 1)
	if err := e.SetProxy(ProxySettings{URL: proxy.URL}); err != nil {
		t.Fatal(err)
	}
	if _, err := collectRun(e, context.Background(), engineRequest(address)); err == nil {
		t.Fatal("经代理隧道的 HTTPS 仍须校验证书")
	}
	if err := e.SetCACertificates(ca); err != nil {
		t.Fatal(err)
	}
	events, err := collectRun(e, context.Background(), engineRequest(address))
	if err != nil || bodyEvent(t, events).Text != "tunneled" {
		t.Fatalf("自定义 CA 与代理组合失败：%v", err)
	}
	if requests, _ := proxy.seen(); len(requests) == 0 || !strings.HasPrefix(requests[len(requests)-1], "CONNECT proxy-test.localhost:") {
		t.Fatalf("HTTPS 应通过 CONNECT 隧道：%v", requests)
	}
}

func TestEngineStreamingThroughProxy(t *testing.T) {
	t.Run("SSE", func(t *testing.T) {
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: proxied\n\n")
		}))
		defer target.Close()
		proxy := newProxyFixture(t, "")
		e := proxyEngineFixture(t)
		if err := e.SetProxy(ProxySettings{URL: proxy.URL}); err != nil {
			t.Fatal(err)
		}
		r := engineRequest(strings.Replace(target.URL, "127.0.0.1", "proxy-test.localhost", 1))
		r.Protocol = "SSE"
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		events, err := collectRun(e, ctx, r)
		if err != nil || len(events) != 2 || events[1].Text != "proxied" {
			t.Fatalf("SSE 没有经过代理：%v %+v", err, events)
		}
	})
	t.Run("WebSocket", func(t *testing.T) {
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer c.CloseNow()
			c.Write(r.Context(), websocket.MessageText, []byte("proxied"))
			c.Close(websocket.StatusNormalClosure, "")
		}))
		defer target.Close()
		proxy := newProxyFixture(t, "")
		e := proxyEngineFixture(t)
		if err := e.SetProxy(ProxySettings{URL: proxy.URL}); err != nil {
			t.Fatal(err)
		}
		r := engineRequest("ws" + strings.TrimPrefix(strings.Replace(target.URL, "127.0.0.1", "proxy-test.localhost", 1), "http"))
		r.Protocol = "WebSocket"
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s, _, err := e.Connect(ctx, r)
		if err != nil {
			t.Fatalf("WebSocket 没有经过代理：%v", err)
		}
		defer s.Close()
		var got []Event
		if err := s.Read(ctx, func(ev Event) { got = append(got, ev) }); err != nil || len(got) != 1 || got[0].Text != "proxied" {
			t.Fatalf("WebSocket 消息错误：%v %+v", err, got)
		}
		if requests, _ := proxy.seen(); len(requests) == 0 {
			t.Fatal("WebSocket 握手没有经过代理")
		}
	})
}

func TestEngineProxyUnreachableReportsProxy(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	l.Close()
	e := proxyEngineFixture(t)
	if err := e.SetProxy(ProxySettings{URL: "http://" + address}); err != nil {
		t.Fatal(err)
	}
	_, err = collectRun(e, context.Background(), engineRequest("http://proxy-test.localhost/"))
	if err == nil || !strings.Contains(err.Error(), "代理") {
		t.Fatalf("代理不可用时错误应说明是代理问题：%v", err)
	}
}

func TestEngineInvalidProxyKeepsPrevious(t *testing.T) {
	proxy := newProxyFixture(t, "")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	defer target.Close()
	e := proxyEngineFixture(t)
	if err := e.SetProxy(ProxySettings{URL: proxy.URL}); err != nil {
		t.Fatal(err)
	}
	if err := e.SetProxy(ProxySettings{URL: "ftp://bad"}); err == nil {
		t.Fatal("无效代理应被拒绝")
	}
	if _, err := collectRun(e, context.Background(), engineRequest(strings.Replace(target.URL, "127.0.0.1", "proxy-test.localhost", 1))); err != nil {
		t.Fatalf("无效输入改变了原代理：%v", err)
	}
}
