package core

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Event struct {
	Kind      string
	Text      string
	Status    string
	Headers   http.Header
	Elapsed   time.Duration
	Bytes     int
	Truncated bool
	ID        string
	Name      string
	Retry     string
}
type Engine struct {
	mu            sync.RWMutex
	jar           *persistentJar
	roots         *x509.CertPool
	transport     *http.Transport
	cookieChanged func()
}

func NewEngine() *Engine {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.DialContext = (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	t.ResponseHeaderTimeout = 30 * time.Second
	e := &Engine{transport: t}
	e.jar = newCookieJar(e.cookiesChanged)
	return e
}

// ParseCACertificates adds PEM certificates to the system roots. Blank input
// returns nil to use the system's default trust; private keys are not accepted.
func ParseCACertificates(text string) (*x509.CertPool, error) {
	if len(text) > MaxInput {
		return nil, fmt.Errorf("CA 证书超过 1 MiB")
	}
	data := bytes.TrimSpace([]byte(text))
	if len(data) == 0 {
		return nil, nil
	}
	var certs []*x509.Certificate
	for len(data) > 0 {
		if !bytes.HasPrefix(data, []byte("-----BEGIN CERTIFICATE-----")) {
			return nil, fmt.Errorf("CA 证书只能包含 PEM 证书，不能包含私钥或其他文本")
		}
		block, rest := pem.Decode(data)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, fmt.Errorf("CA 证书应为有效的 PEM 证书")
		}
		// pem.Decode can skip a malformed block and return a later one.
		if bytes.Count(data[:len(data)-len(rest)], []byte("-----BEGIN ")) != 1 {
			return nil, fmt.Errorf("CA 证书包含损坏的 PEM 证书")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("CA 证书内容无效：%w", err)
		}
		certs = append(certs, cert)
		data = bytes.TrimSpace(rest)
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("无法加载系统 CA 证书：%w", err)
	}
	for _, cert := range certs {
		roots.AddCert(cert)
	}
	return roots, nil
}
func (e *Engine) SetCACertificates(text string) error {
	roots, err := ParseCACertificates(text)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.roots = roots
	e.mu.Unlock()
	return nil
}
func (e *Engine) ClearCookies() {
	j := newCookieJar(e.cookiesChanged)
	e.mu.Lock()
	e.jar = j
	e.mu.Unlock()
	e.cookiesChanged()
}
func (e *Engine) Cookies(raw string) []*http.Cookie {
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	if u.Scheme == "ws" {
		u.Scheme = "http"
	}
	if u.Scheme == "wss" {
		u.Scheme = "https"
	}
	e.mu.RLock()
	j := e.jar
	e.mu.RUnlock()
	return j.Cookies(u)
}
func (e *Engine) Close() { e.transport.CloseIdleConnections() }
func (e *Engine) client(r Request) *http.Client {
	e.mu.RLock()
	j, roots := e.jar, e.roots
	e.mu.RUnlock()
	t := e.transport.Clone()
	if roots != nil {
		if t.TLSClientConfig == nil {
			t.TLSClientConfig = &tls.Config{}
		}
		t.TLSClientConfig.RootCAs = roots
	}
	t.ResponseHeaderTimeout = time.Duration(r.TimeoutSeconds) * time.Second
	c := &http.Client{Transport: t, Jar: j}
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if !r.FollowRedirects {
			return http.ErrUseLastResponse
		}
		if len(via) >= 10 {
			return fmt.Errorf("重定向超过 10 次")
		}
		if len(via) > 0 && (req.URL.Host != via[0].URL.Host || req.URL.Scheme != via[0].URL.Scheme) {
			req.Header.Del("Authorization")
			req.Header.Del("Cookie")
			req.Header.Del("Proxy-Authorization")
		}
		return nil
	}
	return c
}
func (e *Engine) Run(ctx context.Context, r Request, emit func(Event)) error {
	if r.Protocol == "WebSocket" {
		return fmt.Errorf("请通过连接操作打开 WebSocket")
	}
	if r.Protocol == "HTTP" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(r.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, r.URL, strings.NewReader(r.Body))
	if err != nil {
		return err
	}
	req.Header = HTTPHeaders(r.Headers)
	if r.BodyMode == BodyMultipart {
		boundary := multipart.NewWriter(io.Discard).Boundary()
		body, size, err := openMultipart(r.FormParts, boundary)
		if err != nil {
			return err
		}
		req.Body, req.ContentLength = body, size
		defer body.Close()
		req.GetBody = func() (io.ReadCloser, error) {
			body, length, err := openMultipart(r.FormParts, boundary)
			if err != nil {
				return nil, err
			}
			if length != size {
				body.Close()
				return nil, fmt.Errorf("上传文件在重定向前发生变化，请重新发送")
			}
			return body, nil
		}
		req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	} else if r.BodyMode != BodyRaw {
		return fmt.Errorf("不支持的正文类型")
	}
	if host := req.Header.Get("Host"); host != "" {
		req.Host = host
		req.Header.Del("Host")
	}
	if r.Protocol == "SSE" && req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "text/event-stream")
	}
	if r.Body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	c := e.client(r)
	defer c.CloseIdleConnections()
	start := time.Now()
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	emit(Event{Kind: "headers", Status: resp.Status, Headers: resp.Header.Clone(), Elapsed: time.Since(start)})
	if r.Protocol == "SSE" {
		mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if resp.StatusCode < 200 || resp.StatusCode >= 300 || mt != "text/event-stream" {
			return fmt.Errorf("SSE 需要 2xx 和 text/event-stream，实际为 %s / %s", resp.Status, mt)
		}
		return ReadSSE(resp.Body, emit)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return err
	}
	truncated := len(data) > MaxBody
	if truncated {
		data = data[:MaxBody]
	}
	body := string(data)
	if !truncated && json.Valid(data) {
		var formatted bytes.Buffer
		if json.Indent(&formatted, data, "", "  ") == nil {
			body = formatted.String()
		}
	}
	emit(Event{Kind: "body", Text: body, Bytes: len(data), Elapsed: time.Since(start), Truncated: truncated})
	return nil
}

// ReadSSE follows the event-stream line grammar including CR, LF and CRLF.
// Only a blank line dispatches an event; an incomplete event at EOF is discarded.
func ReadSSE(r io.Reader, emit func(Event)) error {
	sc := bufio.NewScanner(r)
	sc.Split(splitLines)
	sc.Buffer(make([]byte, 4096), MaxInput)
	var data []string
	size := 0
	id := ""
	name := ""
	retry := ""
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, "\ufeff")
			first = false
		}
		if line == "" {
			if len(data) > 0 {
				if name == "" {
					name = "message"
				}
				emit(Event{Kind: "sse", Text: strings.Join(data, "\n"), ID: id, Name: name, Retry: retry})
			}
			data = nil
			size = 0
			name = ""
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "data":
			size += len(value) + 1
			if size > MaxInput {
				return fmt.Errorf("单个 SSE 事件超过 1 MiB")
			}
			data = append(data, value)
		case "event":
			name = value
		case "id":
			if !strings.ContainsRune(value, 0) {
				id = value
			}
		case "retry":
			valid := len(value) > 0
			for _, c := range value {
				if c < '0' || c > '9' {
					valid = false
				}
			}
			if valid {
				retry = value
			}
		}
	}
	return sc.Err()
}
func splitLines(b []byte, atEOF bool) (int, []byte, error) {
	for i, c := range b {
		if c == '\n' {
			return i + 1, b[:i], nil
		}
		if c == '\r' {
			if i+1 == len(b) && !atEOF {
				return 0, nil, nil
			}
			n := 1
			if i+1 < len(b) && b[i+1] == '\n' {
				n = 2
			}
			return i + n, b[:i], nil
		}
	}
	if atEOF && len(b) > 0 {
		return len(b), b, nil
	}
	return 0, nil, nil
}

type Socket struct{ conn *websocket.Conn }

func (e *Engine) Connect(ctx context.Context, r Request) (*Socket, Event, error) {
	c := e.client(r)
	c.Timeout = time.Duration(r.TimeoutSeconds) * time.Second
	start := time.Now()
	conn, resp, err := websocket.Dial(ctx, r.URL, &websocket.DialOptions{HTTPClient: c, HTTPHeader: HTTPHeaders(r.Headers)})
	ev := Event{Kind: "headers", Elapsed: time.Since(start)}
	if resp != nil {
		ev.Status = resp.Status
		ev.Headers = resp.Header.Clone()
	}
	if err != nil {
		c.CloseIdleConnections()
		return nil, ev, err
	}
	conn.SetReadLimit(MaxInput)
	return &Socket{conn}, ev, nil
}
func (s *Socket) Read(ctx context.Context, emit func(Event)) error {
	for {
		kind, data, err := s.conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure || websocket.CloseStatus(err) == websocket.StatusGoingAway {
				return nil
			}
			return err
		}
		text := string(data)
		if kind == websocket.MessageBinary {
			text = "二进制（base64）: " + base64.StdEncoding.EncodeToString(data)
		}
		emit(Event{Kind: "ws", Text: text, Bytes: len(data)})
	}
}
func (s *Socket) Send(ctx context.Context, text string) error {
	if len(text) > MaxInput {
		return fmt.Errorf("消息超过 1 MiB")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.conn.Write(ctx, websocket.MessageText, []byte(text))
}
func (s *Socket) Close() { _ = s.conn.CloseNow() }
