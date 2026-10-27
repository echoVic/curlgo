package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const MaxBody = 4 << 20
const MaxInput = 1 << 20

type Pair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type Request struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Protocol        string `json:"protocol"`
	Method          string `json:"method"`
	URL             string `json:"url"`
	Headers         []Pair `json:"headers,omitempty"`
	Body            string `json:"body,omitempty"`
	FollowRedirects bool   `json:"followRedirects"`
	TimeoutSeconds  int    `json:"timeoutSeconds"`
}
type Settings struct {
	GlobalHeaders []Pair                       `json:"globalHeaders,omitempty"`
	Environments  map[string]map[string]string `json:"environments"`
	Environment   string                       `json:"environment"`
	Theme         string                       `json:"theme"`
}
type History struct {
	Request Request   `json:"request"`
	At      time.Time `json:"at"`
	Status  string    `json:"status"`
}
type Workspace struct {
	Version  int       `json:"version"`
	Draft    Request   `json:"draft"`
	Requests []Request `json:"requests"`
	Settings Settings  `json:"settings"`
	History  []History `json:"history"`
}

func NewID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func NewRequest() Request {
	return Request{ID: NewID(), Name: "未命名请求", Protocol: "HTTP", Method: "GET", TimeoutSeconds: 30, FollowRedirects: true}
}
func NewWorkspace() Workspace {
	return Workspace{Version: 1, Draft: NewRequest(), Settings: Settings{Environment: "本地", Environments: map[string]map[string]string{"本地": {"base_url": "http://localhost:8080"}}, Theme: "浅色"}}
}

var variable = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_.-]*)\s*\}\}`)

func Expand(s string, vars map[string]string) (string, error) {
	var missing string
	out := variable.ReplaceAllStringFunc(s, func(v string) string {
		name := variable.FindStringSubmatch(v)[1]
		value, ok := vars[name]
		if !ok {
			missing = name
		}
		return value
	})
	if missing != "" {
		return "", fmt.Errorf("环境变量 %s 未定义", missing)
	}
	return out, nil
}

// Resolve does not mutate the saved template. Request headers override globals,
// case-insensitively; repeated request headers remain repeated on the wire.
func Resolve(r Request, s Settings) (Request, error) {
	if len(r.Body) > MaxInput || len(r.URL) > 65536 {
		return r, fmt.Errorf("请求超过大小限制")
	}
	vars := s.Environments[s.Environment]
	var err error
	if r.URL, err = Expand(strings.TrimSpace(r.URL), vars); err != nil {
		return r, err
	}
	u, err := url.Parse(r.URL)
	if err != nil || u.Hostname() == "" {
		return r, fmt.Errorf("请输入完整的请求地址")
	}
	if u.User != nil {
		return r, fmt.Errorf("请在 Authorization 请求头中填写认证信息")
	}
	if r.Protocol == "WebSocket" {
		if u.Scheme != "ws" && u.Scheme != "wss" {
			return r, fmt.Errorf("WebSocket 地址应以 ws:// 或 wss:// 开头")
		}
	} else if u.Scheme != "http" && u.Scheme != "https" {
		return r, fmt.Errorf("HTTP 地址应以 http:// 或 https:// 开头")
	}
	if r.Protocol != "HTTP" && r.Protocol != "SSE" && r.Protocol != "WebSocket" {
		return r, fmt.Errorf("不支持的协议")
	}
	if r.TimeoutSeconds < 1 || r.TimeoutSeconds > 3600 {
		return r, fmt.Errorf("超时应为 1–3600 秒")
	}
	if r.Body, err = Expand(r.Body, vars); err != nil {
		return r, err
	}
	if len(r.Body) > MaxInput || len(r.URL) > 65536 {
		return r, fmt.Errorf("变量替换后的请求超过大小限制")
	}
	override := map[string]bool{}
	for _, h := range r.Headers {
		override[strings.ToLower(strings.TrimSpace(h.Key))] = true
	}
	all := make([]Pair, 0, len(s.GlobalHeaders)+len(r.Headers))
	for _, h := range s.GlobalHeaders {
		if !override[strings.ToLower(strings.TrimSpace(h.Key))] {
			all = append(all, h)
		}
	}
	all = append(all, r.Headers...)
	for i := range all {
		all[i].Key = strings.TrimSpace(all[i].Key)
		if !validToken(all[i].Key) {
			return r, fmt.Errorf("请求头名称无效")
		}
		all[i].Value, err = Expand(all[i].Value, vars)
		if err != nil {
			return r, err
		}
		if !validHeaderValue(all[i].Value) {
			return r, fmt.Errorf("请求头值不能包含换行")
		}
		if strings.EqualFold(all[i].Key, "Content-Length") || strings.EqualFold(all[i].Key, "Transfer-Encoding") {
			return r, fmt.Errorf("Content-Length 和 Transfer-Encoding 由客户端自动计算，请移除它们")
		}
	}
	r.Headers = all
	if !validToken(r.Method) {
		return r, fmt.Errorf("请求方法无效")
	}
	return r, nil
}
func validToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r >= 127 || r <= 32 || strings.ContainsRune("()<>@,;:\\\"/[]?={}\t", r) {
			return false
		}
	}
	return true
}
func ParseHeaders(text string) ([]Pair, error) {
	var out []Pair
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.Trim(line, " \t")
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok || !validToken(strings.TrimSpace(k)) {
			return nil, fmt.Errorf("每行请求头应为 Name: value")
		}
		if !validHeaderValue(v) {
			return nil, fmt.Errorf("请求头值包含无效控制字符")
		}
		out = append(out, Pair{strings.TrimSpace(k), strings.Trim(v, " \t")})
	}
	return out, nil
}
func FormatHeaders(h []Pair) string {
	var b strings.Builder
	for _, p := range h {
		fmt.Fprintf(&b, "%s: %s\n", p.Key, p.Value)
	}
	return strings.TrimSuffix(b.String(), "\n")
}
func HTTPHeaders(p []Pair) http.Header {
	h := make(http.Header)
	for _, p := range p {
		h.Add(p.Key, p.Value)
	}
	return h
}

func validHeaderValue(s string) bool {
	for _, c := range []byte(s) {
		if c == 127 || (c < 32 && c != 9) {
			return false
		}
	}
	return true
}
