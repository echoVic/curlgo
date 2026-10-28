package core

import (
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/coder/websocket"
)

func engineFixture(t *testing.T) *Engine   { t.Helper(); e := NewEngine(); t.Cleanup(e.Close); return e }
func engineRequest(address string) Request { r := modelRequest(); r.URL = address; return r }
func collectRun(e *Engine, ctx context.Context, r Request) ([]Event, error) {
	var events []Event
	err := e.Run(ctx, r, func(v Event) { events = append(events, v) })
	return events, err
}
func bodyEvent(t *testing.T, events []Event) Event {
	t.Helper()
	for _, e := range events {
		if e.Kind == "body" {
			return e
		}
	}
	t.Fatal("未发出body事件")
	return Event{}
}

func engineTLSFixture(t *testing.T, handler http.Handler, changeLeaf func(*x509.Certificate)) (*httptest.Server, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "CurlGo test CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if changeLeaf != nil {
		changeLeaf(leaf)
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{leafDER, caDER}, PrivateKey: key}}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}))
}

func TestEngineCustomCA(t *testing.T) {
	srv, ca := engineTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "trusted")
	}), nil)
	e := engineFixture(t)
	r := engineRequest(srv.URL)
	if _, err := collectRun(e, context.Background(), r); err == nil {
		t.Fatal("未配置 CA 时不应信任自签证书")
	}
	if err := e.SetCACertificates(ca); err != nil {
		t.Fatal(err)
	}
	events, err := collectRun(e, context.Background(), r)
	if err != nil || bodyEvent(t, events).Text != "trusted" {
		t.Fatalf("配置自定义 CA 后 HTTPS 请求失败：%v", err)
	}
	if err := e.SetCACertificates(""); err != nil {
		t.Fatal(err)
	}
	if _, err := collectRun(e, context.Background(), r); err == nil {
		t.Fatal("清除 CA 后仍信任自签证书")
	}
}

func TestEngineInvalidCAKeepsTrust(t *testing.T) {
	srv, ca := engineTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "trusted")
	}), nil)
	for name, input := range map[string]string{
		"普通文本":         "not a certificate",
		"证书前有无效文本":     "invalid\n" + ca,
		"证书后有无效文本":     ca + "invalid",
		"损坏PEM后跟有效证书":  "-----BEGIN CERTIFICATE-----\ninvalid!\n-----END CERTIFICATE-----\n" + ca,
		"未闭合PEM后跟有效证书": "-----BEGIN CERTIFICATE-----\ninvalid!\n" + ca,
		"混入损坏证书":       ca + "-----BEGIN CERTIFICATE-----\nbm90LWEtdmFsaWQtY2VydA==\n-----END CERTIFICATE-----",
		"混入私钥":         ca + "-----BEGIN PRIVATE KEY-----\nYWJj\n-----END PRIVATE KEY-----",
		"PEM包含额外头部":    strings.Replace(ca, "-----BEGIN CERTIFICATE-----\n", "-----BEGIN CERTIFICATE-----\nComment: test\n\n", 1),
		"超过大小限制":       ca + strings.Repeat("\n", MaxInput),
	} {
		t.Run(name, func(t *testing.T) {
			e := engineFixture(t)
			if err := e.SetCACertificates(ca); err != nil {
				t.Fatal(err)
			}
			if err := e.SetCACertificates(input); err == nil {
				t.Fatal("无效 CA 配置未被拒绝")
			}
			events, err := collectRun(e, context.Background(), engineRequest(srv.URL))
			if err != nil || bodyEvent(t, events).Text != "trusted" {
				t.Fatalf("无效输入改变了原来的 CA 配置：%v", err)
			}
		})
	}
}

func TestEngineCustomCABundle(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "trusted") })
	first, ca1 := engineTLSFixture(t, handler, nil)
	second, ca2 := engineTLSFixture(t, handler, nil)
	e := engineFixture(t)
	if err := e.SetCACertificates(ca1 + "\n" + ca2); err != nil {
		t.Fatal(err)
	}
	for _, srv := range []*httptest.Server{first, second} {
		if _, err := collectRun(e, context.Background(), engineRequest(srv.URL)); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.SetCACertificates(ca2); err != nil {
		t.Fatal(err)
	}
	if _, err := collectRun(e, context.Background(), engineRequest(first.URL)); err == nil {
		t.Fatal("替换 CA 后仍信任旧 CA")
	}
	if _, err := collectRun(e, context.Background(), engineRequest(second.URL)); err != nil {
		t.Fatal(err)
	}
}

func TestEngineCustomCAStillValidatesTLS(t *testing.T) {
	for name, change := range map[string]func(*x509.Certificate){
		"主机名不匹配": func(cert *x509.Certificate) { cert.IPAddresses = nil },
		"证书过期":   func(cert *x509.Certificate) { cert.NotAfter = time.Now().Add(-time.Minute) },
	} {
		t.Run(name, func(t *testing.T) {
			srv, ca := engineTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				t.Error("证书校验失败后仍发出了请求")
			}), change)
			e := engineFixture(t)
			if err := e.SetCACertificates(ca); err != nil {
				t.Fatal(err)
			}
			events, err := collectRun(e, context.Background(), engineRequest(srv.URL))
			var certErr *tls.CertificateVerificationError
			if !errors.As(err, &certErr) || len(events) != 0 {
				t.Fatalf("自定义 CA 绕过了证书校验：%v", err)
			}
		})
	}
}

func TestEngineCustomCAStreaming(t *testing.T) {
	t.Run("SSE", func(t *testing.T) {
		srv, ca := engineTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: trusted\n\n")
		}), nil)
		e := engineFixture(t)
		r := engineRequest(srv.URL)
		r.Protocol = "SSE"
		if _, err := collectRun(e, context.Background(), r); err == nil {
			t.Fatal("SSE 默认信任了自签证书")
		}
		if err := e.SetCACertificates(ca); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		events, err := collectRun(e, ctx, r)
		if err != nil || len(events) != 2 || events[1].Kind != "sse" || events[1].Text != "trusted" {
			t.Fatalf("SSE 未使用自定义 CA：%v %+v", err, events)
		}
	})
	t.Run("WSS", func(t *testing.T) {
		srv, ca := engineTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			conn, err := websocket.Accept(w, req, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.CloseNow()
			if err := conn.Write(req.Context(), websocket.MessageText, []byte("trusted")); err != nil {
				t.Error(err)
			}
			conn.Close(websocket.StatusNormalClosure, "done")
		}), nil)
		e := engineFixture(t)
		r := engineRequest("wss" + strings.TrimPrefix(srv.URL, "https"))
		r.Protocol = "WebSocket"
		if socket, _, err := e.Connect(context.Background(), r); err == nil {
			socket.Close()
			t.Fatal("WSS 默认信任了自签证书")
		}
		if err := e.SetCACertificates(ca); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		socket, event, err := e.Connect(ctx, r)
		if err != nil || event.Status != "101 Switching Protocols" {
			t.Fatalf("WSS 未使用自定义 CA：%v %+v", err, event)
		}
		defer socket.Close()
		var messages []Event
		if err := socket.Read(ctx, func(ev Event) { messages = append(messages, ev) }); err != nil {
			t.Fatal(err)
		}
		if len(messages) != 1 || messages[0].Text != "trusted" {
			t.Fatalf("未收到 WSS 消息：%+v", messages)
		}
	})
}

func TestEngineCAUpdatesDuringRequests(t *testing.T) {
	srv, ca := engineTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "trusted")
	}), nil)
	e := engineFixture(t)
	if err := e.SetCACertificates(ca); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := e.SetCACertificates(ca); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := collectRun(e, context.Background(), engineRequest(srv.URL)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestEngineRunHTTP(t *testing.T) {
	t.Run("同主机HTTPS降级不转发认证", func(t *testing.T) {
		seen := make(chan http.Header, 1)
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen <- r.Header.Clone()
			fmt.Fprint(w, "ok")
		}))
		defer target.Close()
		source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://example.com/end", http.StatusFound)
		}))
		defer source.Close()
		e := engineFixture(t)
		// 两个逻辑地址保持同Host；只把传输层连接映射到本地测试服务器。
		e.transport = source.Client().Transport.(*http.Transport).Clone()
		e.transport.Proxy = nil
		e.transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			local := target.Listener.Addr().String()
			if strings.HasSuffix(address, ":443") {
				local = source.Listener.Addr().String()
			}
			return (&net.Dialer{}).DialContext(ctx, network, local)
		}
		r := engineRequest("https://example.com/start")
		r.Method, r.Body = "GET", ""
		r.Headers = []Pair{{"Authorization", "Bearer sample"}, {"Cookie", "session=sample"}, {"Proxy-Authorization", "Basic sample"}}
		_, err := collectRun(e, context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		for key, values := range <-seen {
			if key == "Authorization" || key == "Cookie" || key == "Proxy-Authorization" {
				t.Fatalf("HTTPS降级带出认证字段 %s，值数量%d", key, len(values))
			}
		}
	})
	t.Run("请求响应字段及JSON", func(t *testing.T) {
		type seen struct {
			method, body, host, contentType string
			headers                         []string
		}
		request := make(chan seen, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			request <- seen{r.Method, string(b), r.Host, r.Header.Get("Content-Type"), r.Header.Values("X-Dup")}
			w.Header().Set("X-Reply", "yes")
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"answer":42}`)
		}))
		defer srv.Close()
		r := engineRequest(srv.URL)
		r.Method = "PATCH"
		r.Body = "request-body"
		r.Headers = []Pair{{"Host", "virtual.example"}, {"X-Dup", "one"}, {"X-Dup", "two"}}
		events, err := collectRun(engineFixture(t), context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		got := <-request
		if !reflect.DeepEqual(got, seen{"PATCH", "request-body", "virtual.example", "application/octet-stream", []string{"one", "two"}}) {
			t.Fatalf("线上请求字段错误：%+v", got)
		}
		if len(events) != 2 || events[0].Kind != "headers" || events[0].Status != "201 Created" || events[0].Headers.Get("X-Reply") != "yes" {
			t.Fatalf("响应头事件错误：%+v", events)
		}
		body := bodyEvent(t, events)
		if body.Text != "{\n  \"answer\": 42\n}" || body.Bytes != len(`{"answer":42}`) || body.Truncated {
			t.Fatalf("JSON展示与原始字节计数错误：%+v", body)
		}
	})
	for _, follow := range []bool{false, true} {
		name := "不跟随"
		if follow {
			name = "跟随"
		}
		t.Run("重定向/"+name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/target" {
					fmt.Fprint(w, "target")
					return
				}
				http.Redirect(w, r, "/target", http.StatusFound)
			}))
			defer srv.Close()
			r := engineRequest(srv.URL)
			r.Method = "GET"
			r.Body = ""
			r.FollowRedirects = follow
			events, err := collectRun(engineFixture(t), context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if follow {
				if bodyEvent(t, events).Text != "target" || events[0].Status != "200 OK" {
					t.Fatal("未跟随重定向")
				}
			} else if events[0].Status != "302 Found" {
				t.Fatal("禁用后仍跟随重定向")
			}
		})
	}
	t.Run("跨主机重定向不转发认证", func(t *testing.T) {
		seen := make(chan http.Header, 1)
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen <- r.Header.Clone(); fmt.Fprint(w, "ok") }))
		defer target.Close()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
		defer srv.Close()
		r := engineRequest(srv.URL)
		r.Method = "GET"
		r.Headers = []Pair{{"Authorization", "Bearer sample"}, {"Cookie", "session=sample"}, {"Proxy-Authorization", "Basic sample"}}
		_, err := collectRun(engineFixture(t), context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		h := <-seen
		for _, key := range []string{"Authorization", "Cookie", "Proxy-Authorization"} {
			if h.Get(key) != "" {
				t.Fatalf("跨主机泄露%s", key)
			}
		}
	})
	for _, size := range []int{4 << 20, (4 << 20) + 1} {
		name := "恰好上限"
		if size > 4<<20 {
			name = "超过上限"
		}
		t.Run("响应截断/"+name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("x", size)) }))
			defer srv.Close()
			events, err := collectRun(engineFixture(t), context.Background(), engineRequest(srv.URL))
			if err != nil {
				t.Fatal(err)
			}
			b := bodyEvent(t, events)
			if len(b.Text) != 4<<20 || b.Bytes != 4<<20 || b.Truncated != (size > 4<<20) {
				t.Fatalf("长度或截断标记错误：长度=%d 字节=%d 标记=%v", len(b.Text), b.Bytes, b.Truncated)
			}
		})
	}
	t.Run("自动gzip解压", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Encoding", "gzip")
			g := gzip.NewWriter(w)
			fmt.Fprint(g, "解压后的正文")
			g.Close()
		}))
		defer srv.Close()
		events, err := collectRun(engineFixture(t), context.Background(), engineRequest(srv.URL))
		if err != nil {
			t.Fatal(err)
		}
		if bodyEvent(t, events).Text != "解压后的正文" {
			t.Fatal("gzip未自动解压")
		}
	})
	t.Run("非成功状态仍展示正文", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(422); fmt.Fprint(w, "invalid input") }))
		defer srv.Close()
		events, err := collectRun(engineFixture(t), context.Background(), engineRequest(srv.URL))
		if err != nil {
			t.Fatal(err)
		}
		if events[0].Status != "422 Unprocessable Entity" || bodyEvent(t, events).Text != "invalid input" {
			t.Fatal("丢失错误状态正文")
		}
	})
	t.Run("发送前取消", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		events, err := collectRun(engineFixture(t), ctx, engineRequest("http://127.0.0.1:1"))
		if !errors.Is(err, context.Canceled) || len(events) != 0 {
			t.Fatalf("取消应保持错误身份且无事件：%v %+v", err, events)
		}
	})
	t.Run("请求超时", func(t *testing.T) {
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}))
		defer srv.Close()
		defer close(release)
		r := engineRequest(srv.URL)
		r.TimeoutSeconds = 1
		_, err := collectRun(engineFixture(t), context.Background(), r)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("超时错误未传播：%v", err)
		}
	})
	t.Run("默认拒绝不可信TLS证书", func(t *testing.T) {
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "unexpected") }))
		srv.Config.ErrorLog = log.New(io.Discard, "", 0)
		srv.StartTLS()
		defer srv.Close()
		events, err := collectRun(engineFixture(t), context.Background(), engineRequest(srv.URL))
		if err == nil || len(events) != 0 {
			t.Fatal("绕过了证书验证")
		}
	})
	t.Run("共享Cookie自动回送", func(t *testing.T) {
		seen := make(chan string, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/set" {
				http.SetCookie(w, &http.Cookie{Name: "session", Value: "sample", Path: "/"})
			} else {
				seen <- r.Header.Get("Cookie")
			}
			fmt.Fprint(w, "ok")
		}))
		defer srv.Close()
		e := engineFixture(t)
		if _, err := collectRun(e, context.Background(), engineRequest(srv.URL+"/set")); err != nil {
			t.Fatal(err)
		}
		if _, err := collectRun(e, context.Background(), engineRequest(srv.URL+"/next")); err != nil {
			t.Fatal(err)
		}
		if <-seen != "session=sample" {
			t.Fatal("Cookie未跨请求共享")
		}
	})
}

func TestEngineRunSSE(t *testing.T) {
	t.Run("POST事件和Accept头", func(t *testing.T) {
		request := make(chan string, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			request <- r.Method + "|" + r.Header.Get("Accept") + "|" + string(b)
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			fmt.Fprint(w, "data: first\n\ndata: second\n\n")
		}))
		defer srv.Close()
		r := engineRequest(srv.URL)
		r.Protocol = "SSE"
		r.Body = "query"
		events, err := collectRun(engineFixture(t), context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		if <-request != "POST|text/event-stream|query" || len(events) != 3 || events[1].Text != "first" || events[2].Text != "second" {
			t.Fatalf("SSE请求/事件语义错误：%+v", events)
		}
	})
	for _, tc := range []struct {
		name, contentType string
		status            int
	}{{"非stream", "application/json", 200}, {"非2xx", "text/event-stream", 500}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, "data: ignored\n\n")
			}))
			defer srv.Close()
			r := engineRequest(srv.URL)
			r.Protocol = "SSE"
			events, err := collectRun(engineFixture(t), context.Background(), r)
			requireModelError(t, err, "SSE")
			if len(events) != 1 || events[0].Kind != "headers" {
				t.Fatalf("非法stream发出业务事件：%+v", events)
			}
		})
	}
	t.Run("主动取消不中途自动重连", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		requests := make(chan struct{}, 2)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests <- struct{}{}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: one\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}))
		defer srv.Close()
		r := engineRequest(srv.URL)
		r.Protocol = "SSE"
		err := engineFixture(t).Run(ctx, r, func(e Event) {
			if e.Kind == "sse" {
				cancel()
			}
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("取消错误未传播：%v", err)
		}
		if len(requests) != 1 {
			t.Fatal("取消后重连了SSE")
		}
	})
}

type failedSSEReader struct{ err error }

func (r failedSSEReader) Read([]byte) (int, error) { return 0, r.err }
func TestReadSSE(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        []Event
	}{
		{"混合换行与分块CRLF", "data: one\r\ndata: two\r\rdata: three\n\n", []Event{{Kind: "sse", Text: "one\ntwo", Name: "message"}, {Kind: "sse", Text: "three", Name: "message"}}},
		{"BOM注释名称和多行data", "\ufeff: comment\nevent: update\nid: 17\nretry: 250\ndata: a\ndata: b\n\n", []Event{{Kind: "sse", Text: "a\nb", Name: "update", ID: "17", Retry: "250"}}},
		{"id和retry持久名称重置", "id: keep\nretry: 12\nevent: first\ndata: a\n\ndata: b\n\n", []Event{{Kind: "sse", Text: "a", Name: "first", ID: "keep", Retry: "12"}, {Kind: "sse", Text: "b", Name: "message", ID: "keep", Retry: "12"}}},
		{"非法id和retry忽略", "id: keep\nretry: 12\nid: bad\x00id\nretry: -1\ndata: ok\n\n", []Event{{Kind: "sse", Text: "ok", Name: "message", ID: "keep", Retry: "12"}}},
		{"空id清除", "id: old\ndata: a\n\nid:\ndata: b\n\n", []Event{{Kind: "sse", Text: "a", Name: "message", ID: "old"}, {Kind: "sse", Text: "b", Name: "message"}}},
		{"空data仍派发", "data\n\n", []Event{{Kind: "sse", Name: "message"}}},
		{"只去除一个空格", "unknown: ignored\ndata:  value\n\n", []Event{{Kind: "sse", Text: " value", Name: "message"}}},
		{"没有data不派发", "id: 1\nevent: ignore\n\n:comment\n\n", nil},
		{"EOF丢弃无空行事件", "data: discard\n", nil},
		{"EOF丢弃未结束行", "data: discard", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []Event
			err := ReadSSE(iotest.OneByteReader(strings.NewReader(tc.input)), func(e Event) { got = append(got, e) })
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got=%+v want=%+v", got, tc.want)
			}
		})
	}
	t.Run("读取错误传播", func(t *testing.T) {
		sentinel := errors.New("reader failed")
		err := ReadSSE(failedSSEReader{sentinel}, func(Event) { t.Error("失败reader不可派发事件") })
		if !errors.Is(err, sentinel) {
			t.Fatalf("读错误丢失：%v", err)
		}
	})
	t.Run("事件大小限制", func(t *testing.T) {
		err := ReadSSE(strings.NewReader("data: "+strings.Repeat("x", 1<<20)+"\n\n"), func(Event) { t.Error("超大事件不应发出") })
		if err == nil {
			t.Fatal("超大事件没有被拒绝")
		}
	})
}

func socketFixture(t *testing.T, handler func(context.Context, *websocket.Conn)) (*Engine, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		handler(ctx, c)
	}))
	t.Cleanup(srv.Close)
	return engineFixture(t), "ws" + strings.TrimPrefix(srv.URL, "http")
}
func connectSocket(t *testing.T, e *Engine, address string) *Socket {
	t.Helper()
	r := engineRequest(address)
	r.Protocol = "WebSocket"
	s, _, err := e.Connect(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestEngineConnect(t *testing.T) {
	t.Run("握手头与Cookie及状态", func(t *testing.T) {
		seen := make(chan http.Header, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen <- r.Header.Clone()
			c, err := websocket.Accept(w, r, nil)
			if err == nil {
				defer c.CloseNow()
				c.Read(r.Context())
			}
		}))
		defer srv.Close()
		e := engineFixture(t)
		u, _ := url.Parse(srv.URL)
		e.jar.SetCookies(u, []*http.Cookie{{Name: "session", Value: "sample"}})
		r := engineRequest("ws" + strings.TrimPrefix(srv.URL, "http"))
		r.Protocol = "WebSocket"
		r.Headers = []Pair{{"X-Handshake", "yes"}}
		s, event, err := e.Connect(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		h := <-seen
		if h.Get("X-Handshake") != "yes" || h.Get("Cookie") != "session=sample" || event.Status != "101 Switching Protocols" || event.Kind != "headers" {
			t.Fatalf("握手字段丢失：%+v %+v", h, event)
		}
	})
	t.Run("握手拒绝仍返回响应信息", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Reason", "denied")
			http.Error(w, "denied", 403)
		}))
		defer srv.Close()
		r := engineRequest("ws" + strings.TrimPrefix(srv.URL, "http"))
		r.Protocol = "WebSocket"
		s, event, err := engineFixture(t).Connect(context.Background(), r)
		if err == nil || s != nil || event.Status != "403 Forbidden" || event.Headers.Get("X-Reason") != "denied" {
			t.Fatalf("握手错误信息丢失：%+v %v", event, err)
		}
	})
	t.Run("取消握手", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := engineRequest("ws://127.0.0.1:1")
		r.Protocol = "WebSocket"
		s, _, err := engineFixture(t).Connect(ctx, r)
		if s != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("取消握手未生效：%v", err)
		}
	})
}

func TestSocketRead(t *testing.T) {
	t.Run("文本二进制和正常关闭", func(t *testing.T) {
		e, address := socketFixture(t, func(ctx context.Context, c *websocket.Conn) {
			c.Write(ctx, websocket.MessageText, []byte("hello"))
			c.Write(ctx, websocket.MessageBinary, []byte{0, 1, 255})
			c.Close(websocket.StatusNormalClosure, "done")
		})
		s := connectSocket(t, e, address)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var events []Event
		err := s.Read(ctx, func(v Event) { events = append(events, v) })
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 2 || events[0].Kind != "ws" || events[0].Text != "hello" || events[0].Bytes != 5 || events[1].Bytes != 3 || !strings.Contains(events[1].Text, "AAH/") {
			t.Fatalf("帧语义丢失：%+v", events)
		}
	})
	t.Run("取消阻塞读取", func(t *testing.T) {
		e, address := socketFixture(t, func(ctx context.Context, c *websocket.Conn) { c.Read(ctx) })
		s := connectSocket(t, e, address)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := s.Read(ctx, func(Event) { t.Error("取消后仍派发") })
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("取消错误未传播：%v", err)
		}
	})
}

func TestSocketSend(t *testing.T) {
	t.Run("发送文本真实echo", func(t *testing.T) {
		e, address := socketFixture(t, func(ctx context.Context, c *websocket.Conn) {
			kind, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			c.Write(ctx, kind, b)
			c.Close(websocket.StatusNormalClosure, "done")
		})
		s := connectSocket(t, e, address)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.Send(ctx, "hello 世界"); err != nil {
			t.Fatal(err)
		}
		var got []Event
		if err := s.Read(ctx, func(e Event) { got = append(got, e) }); err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Text != "hello 世界" || got[0].Bytes != len("hello 世界") {
			t.Fatalf("echo内容改变：%+v", got)
		}
	})
	t.Run("拒绝超大消息", func(t *testing.T) {
		e, address := socketFixture(t, func(ctx context.Context, c *websocket.Conn) { c.Read(ctx) })
		s := connectSocket(t, e, address)
		err := s.Send(context.Background(), strings.Repeat("x", (1<<20)+1))
		requireModelError(t, err, "消息", "MiB")
	})
	t.Run("取消发送", func(t *testing.T) {
		e, address := socketFixture(t, func(ctx context.Context, c *websocket.Conn) { c.Read(ctx) })
		s := connectSocket(t, e, address)
		// 先持有真实写入器，使Send等待写锁，消除立即成功与取消同时就绪的竞争。
		writer, err := s.conn.Writer(context.Background(), websocket.MessageText)
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := s.Send(ctx, "data"); !errors.Is(err, context.Canceled) {
			t.Fatalf("取消发送未生效：%v", err)
		}
	})
}

func TestSocketClose(t *testing.T) {
	t.Run("关闭幂等并阻止发送", func(t *testing.T) {
		e, address := socketFixture(t, func(ctx context.Context, c *websocket.Conn) { c.Read(ctx) })
		s := connectSocket(t, e, address)
		s.Close()
		s.Close()
		if err := s.Send(context.Background(), "after-close"); err == nil {
			t.Fatal("关闭后仍可发送")
		}
	})
}

func cookieNames(cookies []*http.Cookie) map[string]string {
	m := map[string]string{}
	for _, c := range cookies {
		m[c.Name] = c.Value
	}
	return m
}
func TestEngineCookies(t *testing.T) {
	e := engineFixture(t)
	u, _ := url.Parse("https://api.example.com/area/set")
	e.jar.SetCookies(u, []*http.Cookie{{Name: "root", Value: "r", Path: "/"}, {Name: "area", Value: "a", Path: "/area"}, {Name: "secure", Value: "s", Path: "/", Secure: true}, {Name: "expired", Value: "gone", Path: "/", Expires: time.Unix(1, 0)}, {Name: "parent", Value: "p", Domain: "example.com", Path: "/"}, {Name: "public", Value: "bad", Domain: "com", Path: "/"}})
	for _, tc := range []struct {
		name, address string
		want          map[string]string
	}{
		{"HTTPS路径", "https://api.example.com/area/read", map[string]string{"root": "r", "area": "a", "secure": "s", "parent": "p"}},
		{"HTTP不发送Secure", "http://api.example.com/outside", map[string]string{"root": "r", "parent": "p"}},
		{"域名隔离及公共后缀", "https://other.example.com/area", map[string]string{"parent": "p"}},
		{"WSS映射", "wss://api.example.com/area", map[string]string{"root": "r", "area": "a", "secure": "s", "parent": "p"}},
		{"WS映射", "ws://api.example.com/outside", map[string]string{"root": "r", "parent": "p"}},
		{"非法地址", "http://%zz", map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := cookieNames(e.Cookies(tc.address))
			if !reflect.DeepEqual(got, tc.want) {
				b, _ := json.Marshal(got)
				t.Fatalf("cookie选择错误：%s want=%v", b, tc.want)
			}
		})
	}
}

func TestEngineClearCookies(t *testing.T) {
	t.Run("清空并隔离新旧jar", func(t *testing.T) {
		e := engineFixture(t)
		u, _ := url.Parse("https://example.com/")
		e.jar.SetCookies(u, []*http.Cookie{{Name: "old", Value: "value"}})
		if len(e.Cookies(u.String())) != 1 {
			t.Fatal("fixture未写入")
		}
		e.ClearCookies()
		if len(e.Cookies(u.String())) != 0 {
			t.Fatal("清空后仍有cookie")
		}
		e.jar.SetCookies(u, []*http.Cookie{{Name: "new", Value: "value"}})
		if !reflect.DeepEqual(cookieNames(e.Cookies(u.String())), map[string]string{"new": "value"}) {
			t.Fatal("清空后无法继续使用jar")
		}
	})
}
