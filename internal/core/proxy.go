package core

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/http/httpproxy"
)

// ProxySettings routes HTTP, SSE and WebSocket traffic. An empty URL connects
// directly. Bypass uses NO_PROXY syntax; loopback addresses never use the proxy.
type ProxySettings struct {
	URL    string `json:"url,omitempty"`
	Bypass string `json:"bypass,omitempty"`
}

// ParseProxy validates a proxy address. A bare host:port means http://.
func ParseProxy(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if strings.ContainsAny(raw, "\r\n\t ") {
		return nil, fmt.Errorf("代理地址不能包含空白字符")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("代理地址无效")
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("代理只支持 http、https 和 socks5")
	}
	if u.Hostname() == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("代理地址应为 协议://主机:端口")
	}
	u.Path = ""
	return u, nil
}

func proxyFunc(s ProxySettings) (func(*http.Request) (*url.URL, error), error) {
	u, err := ParseProxy(s.URL)
	if err != nil || u == nil {
		return nil, err
	}
	if strings.ContainsAny(s.Bypass, "\r\n") {
		return nil, fmt.Errorf("代理绕过列表请用逗号分隔")
	}
	cfg := httpproxy.Config{HTTPProxy: u.String(), HTTPSProxy: u.String(), NoProxy: s.Bypass}
	match := cfg.ProxyFunc()
	return func(r *http.Request) (*url.URL, error) { return match(r.URL) }, nil
}

// ValidateProxy reports whether SetProxy would accept s.
func ValidateProxy(s ProxySettings) error {
	_, err := proxyFunc(s)
	return err
}

// SetProxy replaces the proxy for new requests. Invalid input keeps the
// previous setting.
func (e *Engine) SetProxy(s ProxySettings) error {
	fn, err := proxyFunc(s)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.proxy = fn
	e.mu.Unlock()
	return nil
}

// proxyError names the proxy when it, not the target, failed to answer.
func proxyError(err error) error {
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "proxyconnect" {
		return fmt.Errorf("无法连接代理服务器，请检查代理设置：%w", err)
	}
	return err
}
