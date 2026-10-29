# CurlGo API-Test CLI 与 Agent Skill Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付同一个 Go 二进制中的 `curlgo run` 单请求 HTTP 执行器，以及作为项目能力分发的 `api-test` skill。

**Architecture:** 保持 `Engine.Run` 和 GUI 执行链路，新建 `Engine.DoHTTP`，复用请求模型、curl 解析、CA、代理与 Cookie Jar。CLI 通过显式输入和状态配置调用核心，输出版本化 JSON；skill 只规范调用和结果判定。

**Tech Stack:** Go 1.27.3、标准库 `net/http` / `httptrace` / `encoding/json` / `flag` / `testing`，现有 MyGo 0.7.1 与 `golang.org/x/net`；不新增依赖。

**目录修订：** 用户明确要求技能作为项目能力管理，源码已迁移至 [skills/api-test/SKILL.md](../../../skills/api-test/SKILL.md)，参考文档随包放入 `references/`。下文已完成步骤中的 `.trae/skills/api-test` 路径保留为首次实现记录；后续维护使用新目录。

---

## 执行依据与边界

- 已批准设计：[API-Test v1 spec](../specs/2026-10-29-api-test-cli-design.md)，提交 `aacb440`。
- 本文件是实施计划；其中代码是实施时的具体起点，复选框记录实施进度。
- 用户已于 2026-10-29 授权开始实施。当前使用 `feat/api-test-cli` 分支，**不使用 git worktree**。
- 计划以 `aacb440` 为源码基线；编写期间工作区另有 query/UI 改动。实施前先读最新 diff，保留已有改动，尤其核对 `model.go` 的 `Resolve`，不要用基线内容整文件覆盖。
- 不改 `Engine.Run`、GUI workspace 格式及收藏回放；不加入断言 DSL、集合 runner、MCP、headless SSE/WebSocket。
- 每任务先运行新增测试观察失败，再实现并验证，通过后只提交列出的文件。未跟踪的 `.trae-html-share-packages/`、`design-mockup.html`、`test-report.html` 不属于本工作。
- 以下测试为同包测试，可使用现有核心测试夹具 `engineTLSFixture(t, handler, nil)` 和 `newProxyFixture(t, auth)`。

实施开始时运行：

```sh
git status --short
git switch -c feat/api-test-cli
```

预期：成功创建普通分支，已有无关文件保持原状。

## 文件职责与依赖

| 文件 | 职责 | 任务 |
|---|---|---|
| `internal/core/headless_error.go` | 强类型错误和 HTTP 数据类型 | 1 |
| `internal/core/model.go` | 未定义变量的强类型错误，保留现有文案 | 1 |
| `internal/core/headless_trace.go` | 同步的分阶段 timing 采集与冻结快照 | 2 |
| `internal/core/headless_http.go` | `DoHTTP`：构建、传输、原始正文捕获 | 3 |
| `internal/core/cookie_file.go` | 独立、版本化 Cookie 文件读写 | 4 |
| `internal/cli/errors.go` | CLI 退出码与固定输入错误 | 5 |
| `internal/cli/input.go` | 有界输入、严格 JSON、curl 与 Resolve | 5 |
| `internal/cli/output.go` | envelope、退出码映射、正文文件和输出失败 | 6 |
| `internal/cli/run.go` | 参数、显式状态、一次执行和一次输出 | 7 |
| `main.go` | GUI 初始化前分发 `run` | 7 |
| 对应 `_test.go` 与 `main_cli_test.go` | 单元、网络、进程及 GUI 回归验收 | 1–8 |
| `.trae/skills/api-test/SKILL.md` | Agent 操作规则和安全对照表 | 9 |
| `README.md` | CLI 使用与现有能力说明 | 9 |

任务顺序：1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9。代码示例使用固定、安全的错误 message；原始错误仅在核心通过 `Unwrap` 保留，不直接写到 CLI。

## Task 1: 建立强类型错误和结果类型

**Files:**

- Create: `internal/core/headless_error.go`
- Modify: `internal/core/model.go`，`Expand` 的 missing 分支
- Create: `internal/core/headless_error_test.go`

- [x] **Step 1: 新建以下失败测试。**

```go
package core

import (
	"context"
	"errors"
	"testing"
)

func TestHeadlessErrorTypes(t *testing.T) {
	var err error = &HTTPError{Code: HTTPErrTimeout, Op: "do", Err: context.DeadlineExceeded}
	var got *HTTPError
	if !errors.As(err, &got) || got.Code != HTTPErrTimeout ||
		!errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("typed error lost: %v", err)
	}
	_, err = Expand("{{token}}", nil)
	var missing *UnresolvedVariableError
	if !errors.As(err, &missing) || missing.Name != "token" ||
		err.Error() != "环境变量 token 未定义" {
		t.Fatalf("variable error lost: %v", err)
	}
}
```

- [x] **Step 2: 运行 `go test ./internal/core -run '^TestHeadlessErrorTypes$' -count=1`。** 预期：编译失败，缺少 `HTTPError` 等类型。
- [x] **Step 3: 创建 `headless_error.go`，内容如下。**

```go
package core

import (
	"fmt"
	"net/http"
	"time"
)

type HTTPOptions struct{ MaxBodyBytes int }

type HTTPTiming struct {
	DNS, Connect, TLS, TTFB, Total time.Duration
}

type HTTPResult struct {
	Method, URL, Proto, Status string
	StatusCode                int
	Headers                   http.Header
	Body                      []byte
	BodyTruncated             bool
	Timing                    HTTPTiming
}

type HTTPErrorCode string

const (
	HTTPErrInvalidRequest HTTPErrorCode = "INVALID_REQUEST"
	HTTPErrRequestBuild   HTTPErrorCode = "REQUEST_BUILD_ERROR"
	HTTPErrRedirect       HTTPErrorCode = "REDIRECT_ERROR"
	HTTPErrTLS            HTTPErrorCode = "TLS_ERROR"
	HTTPErrConnect        HTTPErrorCode = "CONNECT_ERROR"
	HTTPErrTimeout        HTTPErrorCode = "TIMEOUT"
	HTTPErrCanceled       HTTPErrorCode = "CANCELED"
	HTTPErrBodyRead       HTTPErrorCode = "BODY_READ_ERROR"
)

type HTTPError struct {
	Code HTTPErrorCode
	Op   string
	Err  error
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s: %s: %v", e.Code, e.Op, e.Err)
}
func (e *HTTPError) Unwrap() error { return e.Err }

type UnresolvedVariableError struct{ Name string }

func (e *UnresolvedVariableError) Error() string {
	return fmt.Sprintf("环境变量 %s 未定义", e.Name)
}
```

- [x] **Step 4: 将 `Expand` 的 missing 分支替换为以下代码。**

```go
if missing != "" {
	return "", &UnresolvedVariableError{Name: missing}
}
```

- [x] **Step 5: 执行 `gofmt -w internal/core/headless_error.go internal/core/headless_error_test.go internal/core/model.go` 和 `go test ./internal/core -run 'TestHeadlessErrorTypes|TestExpand|TestResolve' -count=1`。** 预期：PASS，既有错误文案不变。
- [x] **Step 6: 提交。**

```sh
git add internal/core/headless_error.go internal/core/headless_error_test.go internal/core/model.go
git commit -m "feat(core): define typed headless HTTP results and errors"
```

## Task 2: 实现可并发采集的分阶段 timing

**Files:**

- Create: `internal/core/headless_trace.go`
- Create: `internal/core/headless_trace_test.go`

- [x] **Step 1: 添加以下确定性测试。** 阶段测试用逻辑时钟；并发测试只查竞态和非负数，不查网络墙钟上限。

```go
package core

import (
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"testing"
	"time"
)

func TestHeadlessTracePhases(t *testing.T) {
	start := time.Unix(0, 0)
	now := start
	state, trace := newHTTPTrace(start, func() time.Time { return now })
	now = start.Add(time.Millisecond)
	trace.DNSStart(httptrace.DNSStartInfo{Host: "example.test"})
	now = start.Add(3 * time.Millisecond)
	trace.DNSDone(httptrace.DNSDoneInfo{})
	trace.ConnectStart("tcp", "a:80")
	now = start.Add(4 * time.Millisecond)
	trace.ConnectStart("tcp", "b:80")
	now = start.Add(6 * time.Millisecond)
	trace.ConnectDone("tcp", "b:80", nil)
	now = start.Add(8 * time.Millisecond)
	trace.ConnectDone("tcp", "a:80", nil)
	trace.TLSHandshakeStart()
	now = start.Add(11 * time.Millisecond)
	trace.TLSHandshakeDone(tls.ConnectionState{}, nil)
	trace.GotFirstResponseByte()
	now = start.Add(15 * time.Millisecond)
	trace.GotFirstResponseByte() // 最后一个重定向响应的首字节
	now = start.Add(19 * time.Millisecond)
	got := state.finish()
	want := HTTPTiming{
		DNS: 2 * time.Millisecond, Connect: 7 * time.Millisecond,
		TLS: 3 * time.Millisecond, TTFB: 15 * time.Millisecond,
		Total: 19 * time.Millisecond,
	}
	if got != want {
		t.Fatalf("timing: got %+v want %+v", got, want)
	}
	now = start.Add(time.Second)
	trace.GotFirstResponseByte()
	if state.finish() != want {
		t.Fatal("late callback changed frozen timing")
	}
}

func TestHeadlessTraceConcurrent(t *testing.T) {
	state, trace := newHTTPTrace(time.Now(), time.Now)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			trace.ConnectStart("tcp", "a:80")
			trace.ConnectDone("tcp", "a:80", nil)
			trace.GotFirstResponseByte()
			state.finish()
		})
	}
	wg.Wait()
	got := state.finish()
	if got.Total < 0 || got.Connect < 0 || got.TTFB < 0 {
		t.Fatal(got)
	}
}

func TestHeadlessTraceNoFirstByte(t *testing.T) {
	start := time.Unix(0, 0)
	state, _ := newHTTPTrace(start, func() time.Time { return start.Add(time.Second) })
	got := state.finish()
	if got.TTFB != 0 || got.DNS != 0 || got.TLS != 0 || got.Total != time.Second {
		t.Fatal(got)
	}
}
```

- [x] **Step 2: 运行 `go test ./internal/core -run '^TestHeadlessTrace' -count=1`。** 预期：缺少 `newHTTPTrace`，编译失败。
- [x] **Step 3: 创建 `headless_trace.go`。** DNS/TLS 的 Go trace 回调不提供 attempt ID，使用开始队列；connect 按 network/address 区分并行拨号。HTTP 重定向串行，完成阶段累加；`finish` 冻结结果，忽略迟到回调。

```go
package core

import (
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"time"
)

type httpTraceState struct {
	mu      sync.Mutex
	start   time.Time
	now     func() time.Time
	timing  HTTPTiming
	pending map[string][]time.Time
	frozen  bool
}

func newHTTPTrace(start time.Time, now func() time.Time) (*httpTraceState, *httptrace.ClientTrace) {
	s := &httpTraceState{start: start, now: now, pending: make(map[string][]time.Time)}
	trace := &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) { s.begin("dns") },
		DNSDone:  func(httptrace.DNSDoneInfo) { s.end("dns", &s.timing.DNS) },
		ConnectStart: func(network, addr string) {
			s.begin("connect:" + network + "\x00" + addr)
		},
		ConnectDone: func(network, addr string, _ error) {
			s.end("connect:"+network+"\x00"+addr, &s.timing.Connect)
		},
		TLSHandshakeStart: func() { s.begin("tls") },
		TLSHandshakeDone:  func(tls.ConnectionState, error) { s.end("tls", &s.timing.TLS) },
		GotFirstResponseByte: func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if !s.frozen {
				s.timing.TTFB = s.now().Sub(s.start)
			}
		},
	}
	return s, trace
}

func (s *httpTraceState) begin(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.frozen {
		s.pending[key] = append(s.pending[key], s.now())
	}
}

func (s *httpTraceState) end(key string, total *time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	queue := s.pending[key]
	if s.frozen || len(queue) == 0 {
		return
	}
	*total += s.now().Sub(queue[0])
	if len(queue) == 1 {
		delete(s.pending, key)
	} else {
		s.pending[key] = queue[1:]
	}
}

func (s *httpTraceState) finish() HTTPTiming {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.frozen {
		s.timing.Total = s.now().Sub(s.start)
		s.frozen = true
	}
	return s.timing
}
```

- [x] **Step 4: 运行 `gofmt -w internal/core/headless_trace.go internal/core/headless_trace_test.go` 和 `go test -race ./internal/core -run '^TestHeadlessTrace' -count=1`。** 预期：PASS，没有 data race。
- [x] **Step 5: 提交。**

```sh
git add internal/core/headless_trace.go internal/core/headless_trace_test.go
git commit -m "feat(core): collect synchronized HTTP phase timings"
```

## Task 3: 新增 DoHTTP 旁路执行器

**Files:**

- Create: `internal/core/headless_http.go`
- Create: `internal/core/headless_http_test.go`

- [x] **Step 1: 添加以下网络测试。**

```go
package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestDoHTTPRawResponses(t *testing.T) {
	for _, status := range []int{200, 400, 404, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Add("X-Multi", "a")
				w.Header().Add("X-Multi", "b")
				w.WriteHeader(status)
				fmt.Fprint(w, "{\"a\":1}")
			}))
			defer srv.Close()
			e := NewEngine()
			defer e.Close()
			r := NewRequest()
			r.URL = srv.URL
			got, err := e.DoHTTP(context.Background(), r, HTTPOptions{})
			if err != nil || got.StatusCode != status || string(got.Body) != "{\"a\":1}" ||
				!reflect.DeepEqual(got.Headers.Values("X-Multi"), []string{"a", "b"}) {
				t.Fatalf("response: %+v %v", got, err)
			}
			if got.Timing.Total <= 0 || got.Timing.TTFB <= 0 || got.Timing.TLS != 0 {
				t.Fatal(got.Timing)
			}
			got, err = e.DoHTTP(context.Background(), r, HTTPOptions{MaxBodyBytes: 3})
			if err != nil || !got.BodyTruncated || string(got.Body) != "{\"a" {
				t.Fatalf("truncation: %+v %v", got, err)
			}
		})
	}
}

func TestDoHTTPCanceled(t *testing.T) {
	e := NewEngine()
	defer e.Close()
	r := NewRequest()
	r.URL = "http://127.0.0.1:1/"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := e.DoHTTP(ctx, r, HTTPOptions{})
	var typed *HTTPError
	if !errors.As(err, &typed) || typed.Code != HTTPErrCanceled {
		t.Fatalf("canceled: %v", err)
	}
}
```

- [x] **Step 2: 运行 `go test ./internal/core -run '^TestDoHTTP' -count=1`。** 预期：缺少 `DoHTTP`，编译失败。
- [x] **Step 3: 创建 `headless_http.go`，内容如下。** `client()` 仍提供 CA、Cookie、代理和重定向安全策略；只在旁路 clone 的 Transport 上设 256 KiB 响应头上限。

实施审查补充：`http.Client` 在 `CheckRedirect` 前可能因无效 `Location` 失败；新增回归测试并通过传输层捕获解析错误，归类为 `REDIRECT_ERROR`，不匹配错误文案。关闭重定向时仍正常返回 3xx；有效重定向后的连接失败仍归类为 `CONNECT_ERROR`。307/308 还会在回调前调用 `GetBody`，因此只在启用跟随时安装 multipart 重放函数，避免关闭跟随时重开文件或丢失 3xx。

```go
package core

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"time"
)

func classifyHTTPError(ctx context.Context, op string, fallback HTTPErrorCode, err error) error {
	code := fallback
	var typed *HTTPError
	var verify *tls.CertificateVerificationError
	var record tls.RecordHeaderError
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var timeout net.Error
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		code = HTTPErrTimeout
	case errors.Is(ctx.Err(), context.Canceled), errors.Is(err, context.Canceled):
		code = HTTPErrCanceled
	case errors.As(err, &timeout) && timeout.Timeout():
		code = HTTPErrTimeout
	case errors.As(err, &typed):
		code = typed.Code
	case errors.As(err, &verify), errors.As(err, &record), errors.As(err, &unknown),
		errors.As(err, &hostname), errors.As(err, &invalid):
		code = HTTPErrTLS
	}
	return &HTTPError{Code: code, Op: op, Err: err}
}

func responseHTTPResult(resp *http.Response) HTTPResult {
	return HTTPResult{
		Method: resp.Request.Method, URL: resp.Request.URL.String(),
		Proto: resp.Proto, StatusCode: resp.StatusCode,
		Status: resp.Status, Headers: resp.Header.Clone(),
	}
}

func (e *Engine) DoHTTP(parent context.Context, r Request, options HTTPOptions) (result HTTPResult, err error) {
	start := time.Now()
	state, trace := newHTTPTrace(start, time.Now)
	defer func() { result.Timing = state.finish() }()
	u, parseErr := url.Parse(r.URL)
	if r.Protocol != "HTTP" || parseErr != nil || u.Hostname() == "" ||
		(u.Scheme != "http" && u.Scheme != "https") || r.TimeoutSeconds < 1 ||
		r.TimeoutSeconds > 3600 {
		return result, &HTTPError{Code: HTTPErrInvalidRequest, Op: "validate", Err: fmt.Errorf("HTTP 请求无效")}
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(r.TimeoutSeconds)*time.Second)
	defer cancel()
	ctx = httptrace.WithClientTrace(ctx, trace)
	if err := ctx.Err(); err != nil {
		return result, classifyHTTPError(ctx, "build", HTTPErrRequestBuild, err)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, r.URL, strings.NewReader(r.Body))
	if err != nil {
		return result, classifyHTTPError(ctx, "build", HTTPErrRequestBuild, err)
	}
	req.Header = HTTPHeaders(r.Headers)
	switch r.BodyMode {
	case BodyRaw:
		if r.Body != "" && req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/octet-stream")
		}
	case BodyMultipart:
		boundary := multipart.NewWriter(io.Discard).Boundary()
		body, size, err := openMultipart(r.FormParts, boundary)
		if err != nil {
			return result, classifyHTTPError(ctx, "multipart", HTTPErrRequestBuild, err)
		}
		defer body.Close()
		req.Body, req.ContentLength = body, size
		req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
		req.GetBody = func() (io.ReadCloser, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			next, length, err := openMultipart(r.FormParts, boundary)
			if err != nil {
				return nil, &HTTPError{Code: HTTPErrRedirect, Op: "replay", Err: err}
			}
			if length != size {
				next.Close()
				return nil, &HTTPError{Code: HTTPErrRedirect, Op: "replay", Err: fmt.Errorf("上传文件在重定向前发生变化")}
			}
			return next, nil
		}
	default:
		return result, &HTTPError{Code: HTTPErrInvalidRequest, Op: "body", Err: fmt.Errorf("正文类型无效")}
	}
	if host := req.Header.Get("Host"); host != "" {
		req.Host = host
		req.Header.Del("Host")
	}
	client := e.client(r)
	defer client.CloseIdleConnections()
	if transport, ok := client.Transport.(*http.Transport); ok {
		transport.MaxResponseHeaderBytes = 256 << 10
	}
	check := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		err := check(next, via)
		if err == nil || errors.Is(err, http.ErrUseLastResponse) {
			return err
		}
		return &HTTPError{Code: HTTPErrRedirect, Op: "redirect", Err: err}
	}
	resp, err := client.Do(req)
	if resp != nil {
		result = responseHTTPResult(resp)
	}
	if err != nil {
		state.finish()
		return result, classifyHTTPError(ctx, "do", HTTPErrConnect, proxyError(err))
	}
	defer resp.Body.Close()
	limit := options.MaxBodyBytes
	if limit <= 0 || limit > MaxBody {
		limit = MaxBody
	}
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	result.BodyTruncated = len(data) > limit
	if result.BodyTruncated {
		data = data[:limit]
	}
	result.Body = data
	state.finish()
	if readErr != nil && !result.BodyTruncated {
		return result, classifyHTTPError(ctx, "read", HTTPErrBodyRead, readErr)
	}
	return result, nil
}
```

- [x] **Step 4: 运行 `gofmt -w internal/core/headless_http.go internal/core/headless_http_test.go` 和 `go test ./internal/core -count=1`。** 预期：PASS，包括既有 `Engine.Run` / SSE / WebSocket 测试。
- [x] **Step 5: 提交。**

```sh
git add internal/core/headless_http.go internal/core/headless_http_test.go
git commit -m "feat(core): add raw headless HTTP execution"
```

## Task 4: 增加独立 Cookie Jar 文件

**Files:**

- Create: `internal/core/cookie_file.go`
- Create: `internal/core/cookie_file_test.go`
- Reuse without modification: `internal/core/store.go:atomicWrite`、`cookies.go:SavedCookie`

- [x] **Step 1: 添加以下失败测试。**

```go
package core

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestCookieFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.json")
	got, err := LoadCookieFile(path)
	if err != nil || len(got) != 0 {
		t.Fatalf("new jar: %v %v", got, err)
	}
	saved := []SavedCookie{{
		URL: "https://example.com/",
		Cookie: http.Cookie{Name: "session", Value: "abc", Path: "/", HttpOnly: true},
	}}
	if err := SaveCookieFile(path, saved); err != nil {
		t.Fatal(err)
	}
	got, err = LoadCookieFile(path)
	info, statErr := os.Stat(path)
	if err != nil || statErr != nil || info.Mode().Perm() != 0600 ||
		len(got) != 1 || !got[0].Cookie.HttpOnly || got[0].Cookie.Value != "abc" {
		t.Fatalf("persisted: %v %v %v", got, err, statErr)
	}
}

func TestCookieFileRejectsCorruptOriginal(t *testing.T) {
	for _, data := range [][]byte{
		[]byte("{"), []byte(`{"version":2,"cookies":[]}`),
		[]byte(`{"version":1,"cookies":[]} {}`),
		bytes.Repeat([]byte(" "), (2<<20)+1),
	} {
		path := filepath.Join(t.TempDir(), "cookies.json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadCookieFile(path); err == nil {
			t.Fatal("corrupt load accepted")
		}
		if err := SaveCookieFile(path, nil); err == nil {
			t.Fatal("corrupt original overwritten")
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(after, data) {
			t.Fatal("original changed")
		}
	}
}
```

- [x] **Step 2: 运行 `go test ./internal/core -run '^TestCookieFile' -count=1`。** 预期：缺少文件 API，编译失败。
- [x] **Step 3: 创建 `cookie_file.go`。** 读取上限包括 envelope；写前再检查原件，避免损坏文件被覆盖。不创建父目录、不引入跨进程锁。

```go
package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const maxCookieFile = 2 << 20

type cookieFile struct {
	Version int           `json:"version"`
	Cookies []SavedCookie `json:"cookies"`
}

func LoadCookieFile(path string) ([]SavedCookie, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCookieFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCookieFile {
		return nil, fmt.Errorf("Cookie 文件超过 2 MiB")
	}
	var envelope cookieFile
	// 同时拒绝未知字段与追加文档。
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("Cookie 文件应只含一个 JSON 文档")
	}
	if envelope.Version != 1 {
		return nil, fmt.Errorf("不支持的 Cookie 文件版本")
	}
	return envelope.Cookies, nil
}

func SaveCookieFile(path string, cookies []SavedCookie) error {
	if _, err := LoadCookieFile(path); err != nil {
		return err
	}
	if cookies == nil {
		cookies = []SavedCookie{}
	}
	data, err := json.Marshal(cookieFile{Version: 1, Cookies: cookies})
	if err != nil {
		return err
	}
	if len(data) > maxCookieFile {
		return fmt.Errorf("Cookie 文件超过 2 MiB")
	}
	return atomicWrite(path, data)
}
```

- [x] **Step 4: 运行 `gofmt -w internal/core/cookie_file.go internal/core/cookie_file_test.go` 和 `go test ./internal/core -run 'TestCookieFile|TestCookie|TestStore' -count=1`。** 预期：PASS，文件权限 0600，损坏原件保持不变。
- [x] **Step 5: 提交。**

```sh
git add internal/core/cookie_file.go internal/core/cookie_file_test.go
git commit -m "feat(core): persist explicit versioned cookie jars"
```

## Task 5: 实现严格、有界的请求输入

**Files:**

- Create: `internal/cli/errors.go`
- Create: `internal/cli/input.go`
- Create: `internal/cli/input_test.go`

- [x] **Step 1: 添加以下失败测试。**

```go
package cli

import (
	"strings"
	"testing"

	"github.com/echoVic/curlgo/internal/core"
)

func TestInputDefaultsAndExplicitValues(t *testing.T) {
	input, fail := decodeInput([]byte(`{"schemaVersion":1,"request":{"url":"http://localhost/"}}`), false)
	if fail != nil || input.Request.Method != "GET" || input.Request.Protocol != "HTTP" ||
		input.Request.TimeoutSeconds != 30 || !input.Request.FollowRedirects {
		t.Fatalf("defaults: %+v %+v", input, fail)
	}
	input, fail = decodeInput([]byte(`{"schemaVersion":1,"request":{"url":"http://localhost/","followRedirects":false}}`), false)
	if fail != nil || input.Request.FollowRedirects {
		t.Fatal("explicit false lost")
	}
	input, fail = decodeInput([]byte(`{"schemaVersion":1,"request":{"url":"{{base}}/","headers":[{"key":"X-Test","value":"local"}]},"settings":{"environment":"dev","environments":{"dev":{"base":"http://localhost"}},"globalHeaders":[{"key":"X-Test","value":"global"}]}}`), false)
	if fail != nil || input.Request.URL != "http://localhost/" ||
		len(input.Request.Headers) != 1 || input.Request.Headers[0].Value != "local" {
		t.Fatalf("resolve: %+v %+v", input, fail)
	}
}

func TestInputRejectsInvalidDocuments(t *testing.T) {
	for _, text := range []string{
		`{`, `{"request":{"url":"http://localhost/"}}`,
		`{"schemaVersion":2,"request":{"url":"http://localhost/"}}`,
		`{"schemaVersion":1.0,"request":{"url":"http://localhost/"}}`,
		`{"schemaVersion":1,"request":null}`,
		`{"schemaVersion":1,"request":{"url":"http://localhost/","unknown":true}}`,
		`{"schemaVersion":1,"request":{"url":"http://localhost/"},"unknown":true}`,
		`{"schemaVersion":1,"request":{"url":"http://localhost/"}} {}`,
		`{"schemaVersion":1,"request":{"url":"http://localhost/","protocol":"SSE"}}`,
		`{"schemaVersion":1,"request":{"url":"http://localhost/","protocol":"WebSocket"}}`,
		`{"schemaVersion":1,"request":{"url":"http://localhost/","timeoutSeconds":0}}`,
		`{"schemaVersion":1,"request":{"url":"http://localhost/","method":""}}`,
		`{"schemaVersion":1,"request":{"url":"http://localhost/","headers":[{"key":"Bad Header","value":"x"}]}}`,
	} {
		if _, fail := decodeInput([]byte(text), false); fail == nil {
			t.Fatalf("accepted: %s", text)
		}
	}
	_, fail := decodeInput([]byte(`{"schemaVersion":1,"request":{"url":"{{missing}}/"}}`), false)
	if fail == nil || fail.Code != "UNRESOLVED_VARIABLE" {
		t.Fatal(fail)
	}
	_, err := readLimited(strings.NewReader(strings.Repeat("x", core.MaxInput+1)), core.MaxInput)
	if err == nil {
		t.Fatal("oversized input accepted")
	}
}

func TestInputSafeCurl(t *testing.T) {
	input, fail := decodeInput([]byte(`curl 'http://localhost/' --data-raw '$literal;|&'`), true)
	if fail != nil || input.Request.Body != "$literal;|&" {
		t.Fatalf("literal: %+v %+v", input, fail)
	}
	for _, text := range []string{
		"curl http://localhost/; touch /tmp/curlgo-injection",
		"curl http://localhost/ | cat",
		"curl http://localhost/ &",
		"curl http://localhost/ --data $TOKEN",
		"curl http://localhost/ --data $(id)",
		"curl http://localhost/ --data `id`",
		"curl http://localhost/ --data @/etc/passwd",
		"curl http://localhost/ -F upload=@/etc/passwd",
		"curl http://localhost/ --insecure",
	} {
		_, fail := decodeInput([]byte(text), true)
		if fail == nil || fail.Code != "INVALID_CURL" {
			t.Fatalf("unsafe curl accepted: %s", text)
		}
	}
}
```

- [x] **Step 2: 运行 `go test ./internal/cli -run '^TestInput' -count=1`。** 预期：缺少 `decodeInput`，编译失败。
- [x] **Step 3: 创建 `errors.go`，定义 CLI 共享类型。**

```go
package cli

type ExitCode int

const (
	ExitOK ExitCode = iota
	ExitInput
	ExitExecution
	ExitOutput
)

type failure struct {
	Code, Message string
	Exit          ExitCode
}

func inputFailure(code, message string) *failure {
	return &failure{Code: code, Message: message, Exit: ExitInput}
}
```

- [x] **Step 4: 创建 `input.go`。** 两次严格解码用于保留 Request 的省略默认值；显式 false、0、空 method 不会被默认值重新覆盖。

```go
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/echoVic/curlgo/internal/core"
)

type inputData struct {
	Request  core.Request
	Settings core.Settings
}

type requestEnvelope struct {
	SchemaVersion int             `json:"schemaVersion"`
	Request       json.RawMessage `json:"request"`
	Settings      core.Settings   `json:"settings"`
}

func readLimited(reader io.Reader, limit int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("输入超过大小限制")
	}
	return data, nil
}

func decodeOne(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("只接受一个 JSON 文档")
	}
	return nil
}

func decodeInput(data []byte, asCurl bool) (inputData, *failure) {
	var input inputData
	if len(data) > core.MaxInput {
		return input, inputFailure("INVALID_INPUT", "请求输入超过 1 MiB")
	}
	if asCurl {
		r, err := core.ImportCURL(string(data))
		if err != nil {
			return input, inputFailure("INVALID_CURL", "curl 输入不受支持或无效")
		}
		input.Request = r
	} else {
		var envelope requestEnvelope
		if err := decodeOne(data, &envelope); err != nil || envelope.SchemaVersion != 1 ||
			len(envelope.Request) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Request), []byte("null")) {
			return input, inputFailure("INVALID_INPUT", "请求 JSON 或 schemaVersion 无效")
		}
		r := core.Request{
			Method: "GET", Protocol: "HTTP",
			TimeoutSeconds: 30, FollowRedirects: true,
		}
		if err := decodeOne(envelope.Request, &r); err != nil {
			return input, inputFailure("INVALID_INPUT", "请求 JSON 字段无效")
		}
		input.Request, input.Settings = r, envelope.Settings
	}
	if input.Request.Protocol != "HTTP" {
		return input, inputFailure("INVALID_INPUT", "run 只支持 HTTP 和 HTTPS")
	}
	resolved, err := core.Resolve(input.Request, input.Settings)
	if err != nil {
		var missing *core.UnresolvedVariableError
		if errors.As(err, &missing) {
			return input, inputFailure("UNRESOLVED_VARIABLE", "请求引用了未定义的环境变量")
		}
		return input, inputFailure("INVALID_REQUEST", "请求不符合 URL、Header、正文或超时约束")
	}
	input.Request = resolved
	return input, nil
}
```

- [x] **Step 5: 执行 `gofmt -w internal/cli/errors.go internal/cli/input.go internal/cli/input_test.go` 和 `go test ./internal/cli -run '^TestInput' -count=1`。** 预期：PASS。
- [x] **Step 6: 提交。**

```sh
git add internal/cli/errors.go internal/cli/input.go internal/cli/input_test.go
git commit -m "feat(cli): decode strict JSON and safe curl requests"
```

## Task 6: 定义结果 envelope、正文交付和退出码

**Files:**

- Create: `internal/cli/output.go`
- Create: `internal/cli/output_test.go`

- [x] **Step 1: 添加以下失败测试。**

```go
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/echoVic/curlgo/internal/core"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken writer") }

func TestOutputBodyModes(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, text, mode string
		limit                        int
	}{
		{"text", "application/json", `{"a":1}`, "inline", 256 << 10},
		{"threshold", "text/plain", "abcd", "file", 3},
		{"binary", "application/octet-stream", "\x00\xff", "file", 256 << 10},
		{"zero", "text/plain", "x", "file", 0},
		{"empty", "application/octet-stream", "", "inline", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			r := core.HTTPResult{
				Method: "GET", URL: "http://example.test/", StatusCode: 200,
				Headers: http.Header{"Content-Type": []string{tc.contentType}},
				Body: []byte(tc.text), BodyTruncated: true,
				Timing: core.HTTPTiming{Total: 3420 * time.Microsecond},
			}
			got, err := formatResult(r, tc.limit, dir)
			if err != nil || got.Body.Mode != tc.mode || got.Body.Bytes != len(tc.text) ||
				!got.Body.Truncated || got.Timing.TotalMs != 3.42 {
				t.Fatalf("body: %+v %v", got, err)
			}
			if tc.mode == "file" {
				data, err := os.ReadFile(got.Body.Path)
				info, statErr := os.Stat(got.Body.Path)
				if err != nil || statErr != nil || string(data) != tc.text ||
					info.Mode().Perm() != 0600 || !filepath.IsAbs(got.Body.Path) {
					t.Fatalf("file: %v %v", err, statErr)
				}
			} else if got.Body.Text == nil || *got.Body.Text != tc.text {
				t.Fatal("inline text missing")
			}
		})
	}
}

func TestOutputSingleEnvelopeAndWriterFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	value := envelope{SchemaVersion: 1, OK: false, Error: &errorValue{Code: "TIMEOUT", Message: "请求超时"}}
	if emit(&stdout, &stderr, value, ExitExecution) != ExitExecution {
		t.Fatal("exit lost")
	}
	if !json.Valid(stdout.Bytes()) || bytes.Count(stdout.Bytes(), []byte("\n")) != 1 {
		t.Fatal("not one JSON document")
	}
	if emit(failingWriter{}, &stderr, value, ExitExecution) != ExitOutput ||
		!strings.Contains(stderr.String(), "无法写入结果 JSON") {
		t.Fatal("stdout failure not reported")
	}
}

func TestOutputHTTPErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		code core.HTTPErrorCode
		exit ExitCode
	}{
		{core.HTTPErrInvalidRequest, ExitInput}, {core.HTTPErrRequestBuild, ExitInput},
		{core.HTTPErrRedirect, ExitExecution}, {core.HTTPErrTLS, ExitExecution},
		{core.HTTPErrConnect, ExitExecution}, {core.HTTPErrTimeout, ExitExecution},
		{core.HTTPErrCanceled, ExitExecution}, {core.HTTPErrBodyRead, ExitExecution},
	} {
		fail := httpFailure(&core.HTTPError{Code: tc.code, Err: errors.New("secret URL token")})
		if fail.Exit != tc.exit || fail.Code != string(tc.code) || strings.Contains(fail.Message, "secret") {
			t.Fatal(fail)
		}
	}
}
```

- [x] **Step 2: 运行 `go test ./internal/cli -run '^TestOutput' -count=1`。** 预期：缺少输出类型及函数，编译失败。
- [x] **Step 3: 创建 `output.go`。**

```go
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/echoVic/curlgo/internal/core"
)

func httpFailure(err error) *failure {
	if err == nil {
		return nil
	}
	var typed *core.HTTPError
	if !errors.As(err, &typed) {
		return &failure{Code: "CONNECT_ERROR", Message: "HTTP 执行失败", Exit: ExitExecution}
	}
	code, exit, message := string(typed.Code), ExitExecution, "HTTP 执行失败"
	switch typed.Code {
	case core.HTTPErrInvalidRequest:
		exit, message = ExitInput, "请求无效"
	case core.HTTPErrRequestBuild:
		exit, message = ExitInput, "无法构建请求或读取显式上传文件"
	case core.HTTPErrRedirect:
		message = "重定向失败"
	case core.HTTPErrTLS:
		message = "TLS 连接或证书校验失败"
	case core.HTTPErrConnect:
		message = "无法连接目标或代理"
	case core.HTTPErrTimeout:
		message = "请求超时"
	case core.HTTPErrCanceled:
		message = "请求已取消"
	case core.HTTPErrBodyRead:
		message = "响应正文读取失败"
	}
	return &failure{Code: code, Message: message, Exit: exit}
}

func outputFailure(primary *failure) *failure {
	message := "无法保存响应正文或 Cookie"
	if primary != nil {
		message += "；原执行错误：" + primary.Code
	}
	return &failure{Code: "OUTPUT_ERROR", Message: message, Exit: ExitOutput}
}

type envelope struct {
	SchemaVersion int          `json:"schemaVersion"`
	OK            bool         `json:"ok"`
	Result        *resultValue `json:"result,omitempty"`
	Error         *errorValue  `json:"error,omitempty"`
}

type errorValue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type resultValue struct {
	Method     string      `json:"method"`
	URL        string      `json:"url"`
	StatusCode int         `json:"statusCode"`
	Status     string      `json:"status"`
	Proto      string      `json:"proto"`
	Headers    http.Header `json:"headers"`
	Body       *bodyValue  `json:"body,omitempty"`
	Timing     timingValue `json:"timing"`
}

type bodyValue struct {
	Mode        string  `json:"mode"`
	Encoding    string  `json:"encoding"`
	ContentType string  `json:"contentType"`
	Bytes       int     `json:"bytes"`
	Text        *string `json:"text,omitempty"`
	Path        string  `json:"path,omitempty"`
	Truncated   bool    `json:"truncated"`
}

type timingValue struct {
	DNSMs     float64 `json:"dnsMs"`
	ConnectMs float64 `json:"connectMs"`
	TLSMs     float64 `json:"tlsMs"`
	TTFBMs    float64 `json:"ttfbMs"`
	TotalMs   float64 `json:"totalMs"`
}

func milliseconds(value time.Duration) float64 {
	return float64(value) / float64(time.Millisecond)
}

func isTextBody(data []byte, contentType string) bool {
	if !utf8.Valid(data) {
		return false
	}
	for _, b := range data {
		if b == 127 || b < 32 && b != '\t' && b != '\r' && b != '\n' {
			return false
		}
	}
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	return strings.HasPrefix(media, "text/") || media == "application/json" ||
		media == "application/xml" || media == "application/javascript" ||
		media == "application/x-www-form-urlencoded" ||
		strings.HasSuffix(media, "+json") || strings.HasSuffix(media, "+xml")
}

func saveBody(data []byte, dir string) (path string, err error) {
	temporary := dir == ""
	if temporary {
		dir, err = os.MkdirTemp("", "curlgo-response-*")
		if err != nil {
			return "", err
		}
	}
	defer func() {
		if err != nil && temporary {
			os.RemoveAll(dir)
		}
	}()
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("输出路径不是目录")
	}
	file, err := os.CreateTemp(dir, "response-*.body")
	if err != nil {
		return "", err
	}
	path = file.Name()
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func formatResult(r core.HTTPResult, limit int, dir string) (*resultValue, error) {
	if r.StatusCode == 0 {
		return nil, nil
	}
	result := &resultValue{
		Method: r.Method, URL: r.URL, StatusCode: r.StatusCode,
		Status: r.Status, Proto: r.Proto, Headers: r.Headers.Clone(),
		Timing: timingValue{
			DNSMs: milliseconds(r.Timing.DNS), ConnectMs: milliseconds(r.Timing.Connect),
			TLSMs: milliseconds(r.Timing.TLS), TTFBMs: milliseconds(r.Timing.TTFB),
			TotalMs: milliseconds(r.Timing.Total),
		},
	}
	body := &bodyValue{
		ContentType: r.Headers.Get("Content-Type"),
		Bytes: len(r.Body), Truncated: r.BodyTruncated,
	}
	if len(r.Body) == 0 || len(r.Body) <= limit && isTextBody(r.Body, body.ContentType) {
		text := string(r.Body)
		body.Mode, body.Encoding, body.Text = "inline", "utf-8", &text
	} else {
		path, err := saveBody(r.Body, dir)
		if err != nil {
			return result, err
		}
		body.Mode, body.Encoding, body.Path = "file", "binary", path
	}
	result.Body = body
	return result, nil
}

func emit(stdout, stderr io.Writer, value envelope, exit ExitCode) ExitCode {
	if err := json.NewEncoder(stdout).Encode(value); err != nil {
		fmt.Fprintln(stderr, "CurlGo: 无法写入结果 JSON")
		return ExitOutput
	}
	return exit
}
```

- [x] **Step 4: 执行 `gofmt -w internal/cli/output.go internal/cli/output_test.go` 和 `go test ./internal/cli -count=1`。** 预期：Task 5/6 全部 PASS。核实正文 UTF-8 字节数、二进制原样保存、empty text 字段存在、timing 小数毫秒保留。
- [x] **Step 5: 提交。**

```sh
git add internal/cli/output.go internal/cli/output_test.go
git commit -m "feat(cli): emit versioned results and bounded body files"
```

## Task 7: 串联 CLI 参数、显式状态与主入口

**Files:**

- Create: `internal/cli/run.go`
- Create: `internal/cli/run_test.go`
- Modify: `main.go` imports 与 `main` 开头

- [x] **Step 1: 添加以下失败测试。**

```go
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunJSONAndCurl(t *testing.T) {
	for _, status := range []int{200, 404, 500} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			fmt.Fprint(w, `{"status":"ok"}`)
		}))
		for _, asCurl := range []bool{false, true} {
			text := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":%q}}`, srv.URL)
			args := []string{"--request", "-"}
			if asCurl {
				text, args = "curl '"+srv.URL+"'", []string{"--curl", "-"}
			}
			var stdout, stderr bytes.Buffer
			exit := Run(context.Background(), strings.NewReader(text), &stdout, &stderr, args)
			var result envelope
			err := json.Unmarshal(stdout.Bytes(), &result)
			if exit != ExitOK || err != nil || result.SchemaVersion != 1 ||
				!result.OK || result.Result.StatusCode != status || stderr.Len() != 0 {
				t.Fatalf("run: %d %s %s", exit, &stdout, &stderr)
			}
		}
		srv.Close()
	}
}

func TestRunRejectsUsage(t *testing.T) {
	for _, args := range [][]string{
		{}, {"--request", "-", "--curl", "-"}, {"--unknown"},
		{"--request", "-", "--inline-body-bytes", "-1"},
		{"--request", "-", "--inline-body-bytes", "4194305"},
		{"--request", "-", "extra"},
	} {
		var stdout, stderr bytes.Buffer
		if Run(context.Background(), strings.NewReader("{}"), &stdout, &stderr, args) != ExitInput {
			t.Fatal("invalid usage accepted")
		}
		var result envelope
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil ||
			result.SchemaVersion != 1 || result.Error.Code != "INVALID_USAGE" {
			t.Fatal(&stdout)
		}
	}
}
```

- [x] **Step 2: 运行 `go test ./internal/cli -run '^TestRun' -count=1`。** 预期：缺少 `Run`，编译失败。
- [x] **Step 3: 创建 `run.go`。** `Run` 参数不含 `run` 本身；FlagSet 不向 stdout/stderr 打印可能包含输入的解析错误。

```go
package cli

import (
	"context"
	"flag"
	"io"
	"os"
	"strings"

	"github.com/echoVic/curlgo/internal/core"
)

type runOptions struct {
	request, curl, caFile, cookieJar, proxy, outDir string
	inlineBytes                                   int
}

func readSource(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" && stdin != nil {
		return readLimited(stdin, core.MaxInput)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readLimited(file, core.MaxInput)
}

func configureEngine(e *core.Engine, settings core.Settings, options runOptions) *failure {
	ca := settings.CACertPEM
	if options.caFile != "" {
		data, err := readSource(options.caFile, nil)
		if err != nil {
			return inputFailure("INVALID_INPUT", "无法读取 CA 文件或文件超过 1 MiB")
		}
		if ca != "" && strings.TrimSpace(ca) != strings.TrimSpace(string(data)) {
			return inputFailure("INVALID_INPUT", "JSON CA 与 --ca-file 不一致")
		}
		ca = string(data)
	}
	if err := e.SetCACertificates(ca); err != nil {
		return inputFailure("INVALID_INPUT", "CA 证书无效")
	}
	proxy := settings.Proxy
	if options.proxy != "" {
		commandURL, err := core.ParseProxy(options.proxy)
		if err != nil || commandURL == nil {
			return inputFailure("INVALID_INPUT", "代理地址无效")
		}
		if proxy.URL != "" {
			inputURL, err := core.ParseProxy(proxy.URL)
			if err != nil || inputURL == nil || commandURL.String() != inputURL.String() {
				return inputFailure("INVALID_INPUT", "JSON 代理与 --proxy 不一致")
			}
		}
		proxy.URL = commandURL.String()
	}
	if err := e.SetProxy(proxy); err != nil {
		return inputFailure("INVALID_INPUT", "代理或绕过列表无效")
	}
	return nil
}

func Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) ExitCode {
	options := runOptions{}
	flags := flag.NewFlagSet("curlgo run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.request, "request", "", "请求 JSON 路径或 -")
	flags.StringVar(&options.curl, "curl", "", "curl 文本路径或 -")
	flags.StringVar(&options.caFile, "ca-file", "", "CA PEM 文件")
	flags.StringVar(&options.cookieJar, "cookie-jar", "", "Cookie Jar 文件")
	flags.StringVar(&options.proxy, "proxy", "", "显式代理")
	flags.StringVar(&options.outDir, "out-dir", "", "已存在的正文目录")
	flags.IntVar(&options.inlineBytes, "inline-body-bytes", 256<<10, "正文内联阈值")
	respondFailure := func(fail *failure) ExitCode {
		return emit(stdout, stderr, envelope{
			SchemaVersion: 1,
			Error: &errorValue{Code: fail.Code, Message: fail.Message},
		}, fail.Exit)
	}
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 ||
		(options.request == "") == (options.curl == "") ||
		options.inlineBytes < 0 || options.inlineBytes > core.MaxBody {
		return respondFailure(inputFailure("INVALID_USAGE", "需选择 --request 或 --curl，阈值须为 0–4194304"))
	}
	path := options.request
	if path == "" {
		path = options.curl
	}
	data, err := readSource(path, stdin)
	if err != nil {
		return respondFailure(inputFailure("INVALID_INPUT", "无法读取请求输入或输入超过 1 MiB"))
	}
	input, fail := decodeInput(data, options.curl != "")
	if fail != nil {
		return respondFailure(fail)
	}
	engine := core.NewEngine()
	defer engine.Close()
	if fail := configureEngine(engine, input.Settings, options); fail != nil {
		return respondFailure(fail)
	}
	if options.cookieJar != "" {
		cookies, err := core.LoadCookieFile(options.cookieJar)
		if err != nil {
			return respondFailure(inputFailure("INVALID_INPUT", "Cookie 文件不可读、损坏或版本不受支持"))
		}
		engine.RestoreCookies(cookies)
	}
	result, executionErr := engine.DoHTTP(ctx, input.Request, core.HTTPOptions{})
	fail = httpFailure(executionErr)
	wireResult, bodyErr := formatResult(result, options.inlineBytes, options.outDir)
	var cookieErr error
	if options.cookieJar != "" {
		cookieErr = core.SaveCookieFile(options.cookieJar, engine.CookieSnapshot())
	}
	if bodyErr != nil || cookieErr != nil {
		fail = outputFailure(fail)
	}
	value := envelope{SchemaVersion: 1, OK: fail == nil, Result: wireResult}
	exit := ExitOK
	if fail != nil {
		value.Error = &errorValue{Code: fail.Code, Message: fail.Message}
		exit = fail.Exit
	}
	return emit(stdout, stderr, value, exit)
}
```

- [x] **Step 4: 修改 `main.go`。** 添加下面三个 import，并将分发放在 `preview := flag.String(...)` 前；现有 GUI 与 `-render` 代码保持原有顺序。

```go
import (
	"context"
	"os/signal"

	"github.com/echoVic/curlgo/internal/cli"
)
```

在 `func main()` 开头插入：

```go
if len(os.Args) > 1 && os.Args[1] == "run" {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	exit := cli.Run(ctx, os.Stdin, os.Stdout, os.Stderr, os.Args[2:])
	stop()
	os.Exit(int(exit))
}
```

`os.Exit` 位于 CLI 资源已释放之后；SIGINT 走 context 取消，输出 `CANCELED` / 2。已有 `os` import 直接复用。

- [x] **Step 5: 运行 `gofmt -w internal/cli/run.go internal/cli/run_test.go main.go` 和 `go test ./... -count=1`。** 预期：PASS；Task 8 再覆盖真实进程分发和显式状态。
- [x] **Step 6: 提交。**

```sh
git add internal/cli/run.go internal/cli/run_test.go main.go
git commit -m "feat(cli): add run command with explicit request state"
```

## Task 8: 补全网络、状态、安全与真实进程验收

**Files:**

- Create: `internal/core/headless_integration_test.go`
- Create: `internal/cli/integration_test.go`
- Create: `main_cli_test.go`
- 必要的修复限定在本计划已列出的新执行链路，不通过改写现有 GUI 测试消除失败。

- [x] **Step 1: 添加以下核心验收测试。** DNS 失败用 DialContext 注入，不依赖外网；TLS 使用现有私有 CA 夹具。

```go
package core

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requireHTTPCode(t *testing.T, err error, code HTTPErrorCode) {
	t.Helper()
	var typed *HTTPError
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("got %v, want %s", err, code)
	}
}

func TestDoHTTPTLSAndDNSFailure(t *testing.T) {
	srv, ca := engineTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "trusted")
	}), nil)
	e := NewEngine()
	defer e.Close()
	r := NewRequest()
	r.URL = srv.URL
	result, err := e.DoHTTP(context.Background(), r, HTTPOptions{})
	requireHTTPCode(t, err, HTTPErrTLS)
	if result.Timing.TLS <= 0 || result.Timing.TTFB != 0 {
		t.Fatal(result.Timing)
	}
	if err := e.SetCACertificates(ca); err != nil {
		t.Fatal(err)
	}
	result, err = e.DoHTTP(context.Background(), r, HTTPOptions{})
	if err != nil || string(result.Body) != "trusted" || result.Timing.TLS <= 0 {
		t.Fatalf("CA: %+v %v", result, err)
	}
	e.transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		trace := httptrace.ContextClientTrace(ctx)
		if trace == nil {
			t.Error("trace not attached to dial context")
			return nil, errors.New("trace missing")
		}
		trace.DNSStart(httptrace.DNSStartInfo{Host: "unreachable.test"})
		err := &net.DNSError{Err: "synthetic DNS failure", Name: "unreachable.test"}
		trace.DNSDone(httptrace.DNSDoneInfo{Err: err})
		return nil, err
	}
	r.URL = "http://unreachable.test/"
	result, err = e.DoHTTP(context.Background(), r, HTTPOptions{})
	requireHTTPCode(t, err, HTTPErrConnect)
	if result.Timing.DNS < 0 || result.Timing.TTFB != 0 {
		t.Fatal(result.Timing)
	}
}

func TestDoHTTPRedirectPolicyAndLimit(t *testing.T) {
	seen := make(chan http.Header, 2)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		fmt.Fprint(w, "final")
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/final", http.StatusFound)
	}))
	defer origin.Close()
	e := NewEngine()
	defer e.Close()
	r := NewRequest()
	r.URL, r.Method, r.Body = origin.URL, "POST", "data"
	r.Headers = []Pair{{"Authorization", "secret-auth"}, {"Cookie", "secret-cookie"}, {"Proxy-Authorization", "secret-proxy"}}
	result, err := e.DoHTTP(context.Background(), r, HTTPOptions{})
	if err != nil || result.Method != "GET" || result.URL != target.URL+"/final" {
		t.Fatalf("final request: %+v %v", result, err)
	}
	headers := <-seen
	for _, name := range []string{"Authorization", "Cookie", "Proxy-Authorization"} {
		if headers.Get(name) != "" {
			t.Fatal("cross-host header leaked", name)
		}
	}
	r.FollowRedirects = false
	result, err = e.DoHTTP(context.Background(), r, HTTPOptions{})
	if err != nil || result.StatusCode != 302 || result.URL != origin.URL || result.Method != "POST" {
		t.Fatalf("no follow: %+v %v", result, err)
	}
	loop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer loop.Close()
	r.URL, r.FollowRedirects = loop.URL, true
	_, err = e.DoHTTP(context.Background(), r, HTTPOptions{})
	requireHTTPCode(t, err, HTTPErrRedirect)
}

func TestDoHTTPMultipartReplay(t *testing.T) {
	for _, mutate := range []bool{false, true} {
		t.Run(fmt.Sprint(mutate), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "upload.txt")
			if err := os.WriteFile(path, []byte("payload"), 0600); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/start" {
					io.Copy(io.Discard, r.Body)
					if mutate {
						if err := os.WriteFile(path, []byte("changed-size"), 0600); err != nil {
							t.Error(err)
						}
					}
					http.Redirect(w, r, "/final", http.StatusTemporaryRedirect)
					return
				}
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				defer r.MultipartForm.RemoveAll()
				file, _, err := r.FormFile("upload")
				if err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				defer file.Close()
				data, err := io.ReadAll(file)
				if err != nil || string(data) != "payload" || r.FormValue("name") != "alice" {
					t.Errorf("multipart: %s %v", data, err)
				}
				fmt.Fprint(w, "uploaded")
			}))
			defer srv.Close()
			e := NewEngine()
			defer e.Close()
			r := NewRequest()
			r.URL, r.Method, r.BodyMode = srv.URL+"/start", "POST", BodyMultipart
			r.FormParts = []FormPart{
				{Kind: FormText, Name: "name", Value: "alice"},
				{Kind: FormFile, Name: "upload", FilePath: path},
			}
			result, err := e.DoHTTP(context.Background(), r, HTTPOptions{})
			if mutate {
				requireHTTPCode(t, err, HTTPErrRedirect)
			} else if err != nil || string(result.Body) != "uploaded" {
				t.Fatalf("upload: %+v %v", result, err)
			}
			r.FormParts[1].FilePath = filepath.Join(t.TempDir(), "missing")
			_, err = e.DoHTTP(context.Background(), r, HTTPOptions{})
			requireHTTPCode(t, err, HTTPErrRequestBuild)
		})
	}
}

func TestDoHTTPProxyAndExplicitDirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "direct")
	}))
	defer target.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "api.test" {
			t.Errorf("proxy request host: %s", r.URL.Host)
		}
		fmt.Fprint(w, "proxied")
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	e := NewEngine()
	defer e.Close()
	r := NewRequest()
	r.URL = "http://api.test/"
	if err := e.SetProxy(ProxySettings{URL: proxy.URL}); err != nil {
		t.Fatal(err)
	}
	result, err := e.DoHTTP(context.Background(), r, HTTPOptions{})
	if err != nil || string(result.Body) != "proxied" {
		t.Fatalf("proxy: %+v %v", result, err)
	}
	r.URL = target.URL
	result, err = e.DoHTTP(context.Background(), r, HTTPOptions{})
	if err != nil || string(result.Body) != "direct" {
		t.Fatal("loopback did not bypass proxy", err)
	}
	e.transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, target.Listener.Addr().String())
	}
	r.URL = "http://api.test/"
	if err := e.SetProxy(ProxySettings{URL: proxy.URL, Bypass: "api.test"}); err != nil {
		t.Fatal(err)
	}
	result, err = e.DoHTTP(context.Background(), r, HTTPOptions{})
	if err != nil || string(result.Body) != "direct" {
		t.Fatal("explicit bypass failed", err)
	}
	if err := e.SetProxy(ProxySettings{}); err != nil {
		t.Fatal(err)
	}
	result, err = e.DoHTTP(context.Background(), r, HTTPOptions{})
	if err != nil || string(result.Body) != "direct" {
		t.Fatal("environment proxy affected default direct request", err)
	}
}

func TestDoHTTPBodyFailureTimeoutAndHeaderLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/short":
			w.Header().Set("Content-Length", "100")
			fmt.Fprint(w, "abc")
		case "/headers":
			w.Header().Set("X-Huge", strings.Repeat("x", (256<<10)+1))
			fmt.Fprint(w, "ok")
		case "/slow":
			<-r.Context().Done()
		case "/gzip":
			w.Header().Set("Content-Encoding", "gzip")
			writer := gzip.NewWriter(w)
			writer.Write([]byte("decompressed"))
			writer.Close()
		}
	}))
	defer srv.Close()
	e := NewEngine()
	defer e.Close()
	r := NewRequest()
	r.URL = srv.URL + "/short"
	result, err := e.DoHTTP(context.Background(), r, HTTPOptions{})
	requireHTTPCode(t, err, HTTPErrBodyRead)
	if result.StatusCode != 200 || string(result.Body) != "abc" || result.Timing.TTFB <= 0 {
		t.Fatal(result)
	}
	r.URL = srv.URL + "/headers"
	_, err = e.DoHTTP(context.Background(), r, HTTPOptions{})
	requireHTTPCode(t, err, HTTPErrConnect)
	r.URL = srv.URL + "/slow"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	result, err = e.DoHTTP(ctx, r, HTTPOptions{})
	requireHTTPCode(t, err, HTTPErrTimeout)
	if result.Timing.TTFB != 0 {
		t.Fatal("invented first-byte timing")
	}
	r.URL = srv.URL + "/gzip"
	result, err = e.DoHTTP(context.Background(), r, HTTPOptions{})
	if err != nil || string(result.Body) != "decompressed" || len(result.Body) != 12 {
		t.Fatalf("application bytes: %+v %v", result, err)
	}
}
```

- [x] **Step 2: 运行 `go test ./internal/core -run 'TestDoHTTP|TestHeadlessIntegration' -count=1`。** 预期：PASS；若失败，按断言定位已有新链路的具体原因再修复。不要把 TLS/DNS 测试改成公网请求。
- [x] **Step 3: 添加以下 CLI 集成测试。**

```go
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/echoVic/curlgo/internal/core"
)

func invokeCLI(t *testing.T, ctx context.Context, text string, extra ...string) (ExitCode, envelope, string) {
	t.Helper()
	args := append([]string{"--request", "-"}, extra...)
	var stdout, stderr bytes.Buffer
	exit := Run(ctx, strings.NewReader(text), &stdout, &stderr, args)
	var value envelope
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil || value.SchemaVersion != 1 {
		t.Fatalf("invalid envelope: %s %v", &stdout, err)
	}
	return exit, value, stdout.String()
}

func TestCLICookiePersistenceAfterBodyFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		cookie, err := r.Cookie("session")
		if err == nil && cookie.Value == "saved" {
			fmt.Fprint(w, "authenticated")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "saved", Path: "/"})
		w.Header().Set("Content-Length", "100")
		fmt.Fprint(w, "partial")
	}))
	defer srv.Close()
	jar := filepath.Join(t.TempDir(), "cookies.json")
	text := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":%q}}`, srv.URL)
	exit, value, _ := invokeCLI(t, context.Background(), text, "--cookie-jar", jar)
	if exit != ExitExecution || value.Error.Code != "BODY_READ_ERROR" || value.Result.StatusCode != 200 {
		t.Fatal("body failure not represented", exit, value.Error)
	}
	exit, value, _ = invokeCLI(t, context.Background(), text, "--cookie-jar", jar)
	if exit != ExitOK || !value.OK || *value.Result.Body.Text != "authenticated" {
		t.Fatal("cookie not saved after failure", exit)
	}
	exit, _, _ = invokeCLI(t, context.Background(), text)
	if exit != ExitExecution {
		t.Fatal("default process unexpectedly inherited cookies")
	}
	info, err := os.Stat(jar)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("jar permission", err)
	}
}

func TestCLICAProxyConflictsAndInputFiles(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "trusted")
	}))
	defer srv.Close()
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(ca, data, 0600); err != nil {
		t.Fatal(err)
	}
	text := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":%q}}`, srv.URL)
	exit, value, _ := invokeCLI(t, context.Background(), text)
	if exit != ExitExecution || value.Error.Code != "TLS_ERROR" {
		t.Fatal("untrusted TLS accepted")
	}
	exit, value, _ = invokeCLI(t, context.Background(), text, "--ca-file", ca)
	if exit != ExitOK || !value.OK {
		t.Fatal("CA file failed")
	}
	conflict := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":%q},"settings":{"caCertPEM":"different"}}`, srv.URL)
	exit, value, _ = invokeCLI(t, context.Background(), conflict, "--ca-file", ca)
	if exit != ExitInput || value.Error.Code != "INVALID_INPUT" {
		t.Fatal("CA conflict silently overwritten")
	}
	engine := core.NewEngine()
	defer engine.Close()
	if fail := configureEngine(engine, core.Settings{
		Proxy: core.ProxySettings{URL: "127.0.0.1:7890", Bypass: "api.test"},
	}, runOptions{proxy: "http://127.0.0.1:7890/"}); fail != nil {
		t.Fatal("normalized equal proxy rejected", fail)
	}
	if fail := configureEngine(engine, core.Settings{
		Proxy: core.ProxySettings{URL: "http://127.0.0.1:7890"},
	}, runOptions{proxy: "http://127.0.0.1:7891"}); fail == nil {
		t.Fatal("proxy conflict silently overwritten")
	}
	if err := os.WriteFile(ca, bytes.Repeat([]byte("x"), core.MaxInput+1), 0600); err != nil {
		t.Fatal(err)
	}
	exit, _, _ = invokeCLI(t, context.Background(), text, "--ca-file", ca)
	if exit != ExitInput {
		t.Fatal("oversized CA accepted")
	}
	requestFile := filepath.Join(dir, "request.json")
	if err := os.WriteFile(requestFile, []byte(`{"schemaVersion":1,"request":{"url":"http://localhost/"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if data, err := readSource(requestFile, nil); err != nil || !json.Valid(data) {
		t.Fatal("request file read failed", err)
	}
}

func TestCLIOutputFailurePreservesExecutionAndSecrets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	jar := filepath.Join(t.TempDir(), "missing-parent", "cookies.json")
	text := `{"schemaVersion":1,"request":{"url":"http://localhost/?token=url-secret","headers":[{"key":"Authorization","value":"header-secret"},{"key":"Cookie","value":"cookie-secret"}],"body":"body-secret"}}`
	exit, value, raw := invokeCLI(t, ctx, text, "--cookie-jar", jar)
	if exit != ExitOutput || value.Error.Code != "OUTPUT_ERROR" ||
		!strings.Contains(value.Error.Message, "CANCELED") {
		t.Fatal("main execution error lost", value.Error)
	}
	for _, secret := range []string{"url-secret", "header-secret", "cookie-secret", "body-secret"} {
		if strings.Contains(raw, secret) {
			t.Fatal("request secret echoed in error", secret)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte{0, 255})
	}))
	defer srv.Close()
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	text = fmt.Sprintf(`{"schemaVersion":1,"request":{"url":%q,"headers":[{"key":"Authorization","value":"header-secret"},{"key":"Cookie","value":"cookie-secret"}],"body":"body-secret"}}`, srv.URL)
	exit, value, raw = invokeCLI(t, context.Background(), text, "--out-dir", blocker)
	if exit != ExitOutput || value.Error.Code != "OUTPUT_ERROR" || value.Result.StatusCode != 200 {
		t.Fatal("body output failure not represented")
	}
	for _, secret := range []string{"header-secret", "cookie-secret", "body-secret"} {
		if strings.Contains(raw, secret) {
			t.Fatal("request secret echoed on response", secret)
		}
	}
	if Run(context.Background(), strings.NewReader(text), failingWriter{}, &bytes.Buffer{},
		[]string{"--request", "-", "--out-dir", t.TempDir()}) != ExitOutput {
		t.Fatal("stdout error exit lost")
	}
}

func TestCLITruncationAndTemporaryBodyFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(bytes.Repeat([]byte{42}, core.MaxBody+1))
	}))
	defer srv.Close()
	text := fmt.Sprintf(`{"schemaVersion":1,"request":{"url":%q}}`, srv.URL)
	dir := t.TempDir()
	exit, value, raw := invokeCLI(t, context.Background(), text, "--out-dir", dir)
	body := value.Result.Body
	if exit != ExitOK || !value.OK || body.Mode != "file" ||
		!body.Truncated || body.Bytes != core.MaxBody || len(raw) > 300<<10 {
		t.Fatal("bounded truncation contract lost")
	}
	first := body.Path
	exit, value, _ = invokeCLI(t, context.Background(), text, "--out-dir", dir)
	if exit != ExitOK || value.Result.Body.Path == first {
		t.Fatal("body file overwritten")
	}
	info, err := os.Stat(first)
	if err != nil || info.Size() != core.MaxBody {
		t.Fatal("old file changed")
	}
	exit, value, _ = invokeCLI(t, context.Background(), text)
	if exit != ExitOK {
		t.Fatal("temporary body failed")
	}
	tempDir := filepath.Dir(value.Result.Body.Path)
	defer os.RemoveAll(tempDir)
	info, err = os.Stat(tempDir)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("temporary directory not private")
	}
	data, err := os.ReadFile(value.Result.Body.Path)
	if err != nil || len(data) != core.MaxBody {
		t.Fatal("temporary file not retained")
	}
	if os.Geteuid() != 0 {
		protected := t.TempDir()
		if err := os.Chmod(protected, 0500); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(protected, 0700)
		exit, value, _ = invokeCLI(t, context.Background(), text, "--out-dir", protected)
		if exit != ExitOutput || value.Error.Code != "OUTPUT_ERROR" {
			t.Fatal("unwritable directory not rejected")
		}
	}
}
```

- [x] **Step 4: 运行 `go test ./internal/cli -count=1`。** 预期：PASS。Writer 失败测试指定了临时输出目录，不留下无法通过 stdout 找到的默认临时文件。

- [x] **Step 5: 添加真实进程入口测试。** 在仓库根包创建 `main_cli_test.go`；实际测试覆盖 `--request` 与 `--curl` 文件输入、服务器就绪后的 SIGINT，以及浅深色渲染，使用隔离配置目录校验 workspace 不变。

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMainCLIDispatchAndRender(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dir := t.TempDir()
	binary := filepath.Join(dir, "curlgo")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %s %v", output, err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(configDir, "QingzhouAPI", "workspace.json")
	before, beforeErr := os.ReadFile(workspace)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(404)
		w.Write([]byte("missing"))
	}))
	defer srv.Close()
	request := filepath.Join(dir, "request.json")
	data, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "request": map[string]string{"url": srv.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(request, data, 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, binary, "run", "--request", request)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil || !json.Valid(stdout.Bytes()) || stderr.Len() != 0 {
		t.Fatalf("headless dispatch: %v %s %s", err, &stdout, &stderr)
	}
	after, afterErr := os.ReadFile(workspace)
	if (beforeErr == nil) != (afterErr == nil) || !bytes.Equal(before, after) {
		t.Fatal("CLI changed GUI workspace")
	}
	image := filepath.Join(dir, "preview.png")
	render := exec.CommandContext(ctx, binary, "-render", image, "-dark")
	if output, err := render.CombinedOutput(); err != nil {
		t.Fatalf("render: %s %v", output, err)
	}
	file, err := os.Open(image)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := png.DecodeConfig(file); err != nil {
		t.Fatal("render did not produce PNG", err)
	}
}
```

- [x] **Step 6: 运行完整验证。** 下列命令全部成功后才进入 skill 文档和交付；网络测试只使用本机服务器。

```sh
gofmt -w internal/core/headless_integration_test.go internal/cli/integration_test.go main_cli_test.go
go test -race ./internal/core ./internal/cli -count=1
CGO_ENABLED=0 go test ./... -count=1
go vet ./...
go tool mygo vet .
CGO_ENABLED=0 go build ./...
```

预期：测试和 race 全部 PASS；两个 vet 无诊断，build 成功。`-race` 不设 `CGO_ENABLED=0`，race detector 使用本机编译工具链；正常发布构建仍不依赖 cgo。

- [x] **Step 7: 提交验收测试与已验证的定向修复。** 先检查 `git diff --stat`，只 stage 计划内文件；下面命令覆盖新增验收测试。若前面修复了新执行器，明确另加对应文件路径。

```sh
git add internal/core/headless_integration_test.go internal/cli/integration_test.go main_cli_test.go
git commit -m "test(api-test): cover transport state safety and process dispatch"
```

## Task 9: 交付 api-test Skill 与使用说明

**Files:**

- Create: `.trae/skills/api-test/SKILL.md`
- Modify: `README.md`
- 不创建另一个 HTTP 客户端、不添加 shell 执行 curl 的脚本。

- [x] **Step 1: 构建供 Agent 使用的二进制，运行下列 skill 验收场景对应的测试。** 服务器和请求由测试创建，断言覆盖退出码、schemaVersion、body.mode 和错误码。

```sh
CGO_ENABLED=0 go build -o /tmp/curlgo-api-test .
go test ./internal/cli -run 'TestRunJSONAndCurl|TestInputCURLSafe|TestInputCURLReject|TestCLIIntegration' -v -count=1
```

预期：构建成功，列出的测试全部 PASS；技能示例使用 PATH 中的 `curlgo`，或这个 `/tmp/curlgo-api-test` 二进制。

| 场景 | 必须观察到的行为 |
|---|---|
| 本地 health JSON | `schemaVersion=1`，`ok=true`，读取业务字段 |
| 404 / 500 | 退出 0，但 Agent 根据 statusCode 判断接口结果 |
| shell 展开 / `@file` / `-F` curl | `INVALID_CURL` / 1，不执行子进程、不读取隐式文件 |
| 自签 HTTPS | 无 CA 时 `TLS_ERROR` / 2；显式 CA 后通过 |
| 登录态串联 | 显式 jar 两次调用通过，默认调用不继承 |
| 大 / 二进制正文 | 读取 body.path；检查 truncated；清理默认临时目录 |
| Cookie 写回与执行同时失败 | `OUTPUT_ERROR` / 3，message 含原执行错误 code |

- [x] **Step 2: 创建以下完整 `.trae/skills/api-test/SKILL.md`。**

````markdown
---
name: api-test
description: 用 CurlGo 调试、验证或冒烟测试本地与内网 HTTP API，安全执行 curl 文本，检查状态码、响应头、JSON、重定向与显式登录态。
---

# API Test with CurlGo

使用用户授权范围内的本地或内网 HTTP/HTTPS 接口。CurlGo 负责一次请求执行，Agent 根据状态码和响应正文判断业务结果。

## 必须遵守的安全规则

| 场景 | 把 curl 文本交给 shell | CurlGo run --curl |
|---|---|---|
| 未引用的 `;` / `\|` / `&` | 可改变命令结构 | 词法拒绝，引号内字面量保留 |
| `$()` / 反引号 / `$VAR` | 可展开执行 | 拒绝展开语法，单引号字面量保留 |
| curl `@file` / `-F` | 可读取上传本地文件 | 拒绝；只有请求 JSON 的显式 multipart filePath 可读取文件 |
| Token 暴露 | 参数可进入历史和进程列表 | 请求走文件或 stdin，不回显请求头；响应及 URL 仍可能含 Token |
| 自签 HTTPS | 由 curl 参数决定校验方式 | 显式 --ca-file，不提供 insecure |
| 状态复用 | 依赖 curl 参数和环境配置 | 显式 Cookie Jar 和代理，不读取 GUI 或系统代理配置 |

curl 文本必须作为数据文件或 stdin 交给 `curlgo run --curl`。禁止把它传给 `sh -c`、`eval`、命令替换或拼接 shell 命令。不通过 shell 展开 Token 或读取 curl 的隐式本地文件。

## 选择二进制

先用 `command -v curlgo` 查找已安装版本。在 CurlGo 仓库开发时，可执行 `CGO_ENABLED=0 go build -o /tmp/curlgo-api-test .`，后续用 `/tmp/curlgo-api-test run` 调用。

本技能的输入与输出契约都是 schemaVersion 1。收到其他版本时停止解析并说明版本不兼容，不猜测字段含义。

## 准备并执行请求

复杂请求优先生成权限 0600 的临时 JSON 文件，或通过 stdin 提交。包含凭证的内容不要放进命令行参数，也不要回显到对话。临时请求文件使用私有目录，调用结束后清理。

最小输入：

```json
{
  "schemaVersion": 1,
  "request": {
    "method": "GET",
    "url": "http://127.0.0.1:8080/health",
    "followRedirects": true,
    "timeoutSeconds": 30
  }
}
```

```sh
curlgo run --request request.json
curlgo run --request - < request.json
curlgo run --curl imported.curl
```

`--request` 与 `--curl` 互斥。JSON、curl 和 CA 文件分别最多 1 MiB。method、protocol、timeoutSeconds、followRedirects 省略时分别默认 GET、HTTP、30、true；显式 false 保留，0 秒拒绝。

环境变量只在 JSON 的 settings.environments / settings.environment 中显式提供，使用 `{{name}}` 替换；不会读取系统变量。全局 Header 来自 settings.globalHeaders，请求 Header 覆盖同名全局 Header。

需要 CA、登录态、代理或正文目录时显式指定：

```sh
curlgo run --request request.json \
  --ca-file ./ca.pem \
  --cookie-jar ./.curlgo-cookies.json \
  --proxy http://127.0.0.1:7890 \
  --out-dir ./test-output
```

自签 HTTPS 先获取正确 CA，再调用 `--ca-file`；不绕过证书验证。JSON CA / 代理与 CLI 参数冲突会被拒绝。系统 HTTP_PROXY、HTTPS_PROXY、NO_PROXY 不参与请求。

仅登录态串联时使用 `--cookie-jar`。jar 为本地明文 0600 文件，父目录须已存在；同一 jar 的不同进程调用要串行。执行失败仍可能更新 Cookie，写回失败为 OUTPUT_ERROR。

需要上传时用 JSON 的 bodyMode="multipart" / formParts，filePath 使用用户授权的绝对文件路径。curl 的 --form-string 可表示文本表单；`-F` 和隐式 @file 不可用。

## 读取并判断结果

stdout 是一个 JSON 文档：

- schemaVersion 必须为 1。
- ok=true 表示响应已按捕获策略读取。401/404/500 仍退出 0；检查 result.statusCode、headers 和正文判断业务是否成功。
- 非零退出码是工具层失败：1 输入、2 传输/取消/超时/正文读取、3 输出。先读 error.code，再确定修复路径。
- body.mode=inline 时读取 body.text；正文未格式化，bytes 是保留字节数。
- body.mode=file 时必须读取 body.path，encoding=binary 表示文件保存原始字节。不要假设正文在 stdout。
- truncated=true 表示达到 4 MiB 捕获上限。不能把截断的 JSON 当完整文档，也不能据此断言大响应完整性。
- --inline-body-bytes 默认为 262144，范围 0–4194304；0 让非空正文全部落盘。
- headers 保留多值，包括 Set-Cookie；响应头、正文和 URL 可能含秘密，汇报前只提取必要信息。
- timing 的 dnsMs/connectMs/tlsMs 是完成阶段耗时和，ttfbMs 从执行开始累计到最后响应的首字节，totalMs 到正文捕获结束。阶段可能重叠，不把它们相加解释 total。

正文文件权限为 0600；默认临时目录为 0700，CLI 保留给调用方读取。读取完成后清理自己拥有的临时正文目录。显式输出目录必须已存在，不删除用户已有目录或文件。

错误输出不保证有 result；BODY_READ_ERROR 可以保留部分结果。OUTPUT_ERROR 可以同时保留已收到响应，并在 message 中记录原执行错误 code。stdout 写入失败可能没有完整 JSON，此时结合退出 3 与固定 stderr 诊断判断，不继续当成功结果解析。

## 交付结论

报告接口地址中可公开的部分、状态码、必要业务字段及失败原因；保留复现输入路径供用户检查。区分工具成功与业务成功。不要在报告中粘贴 Token、Cookie 或完整凭证正文。

v1 只执行单个 HTTP/HTTPS 请求；批量断言、headless SSE/WebSocket 和 GUI 收藏回放不属于本技能。
````

- [x] **Step 3: 更新 README 的 CLI 说明与已过时能力描述。** 将以下段落插入「操作」前：

````markdown
## Agent 与命令行

同一个二进制支持 `curlgo run`，无需打开 GUI。提供 schemaVersion 1 的请求 JSON，或将 curl 文本作为数据文件输入：

```sh
curlgo run --request request.json
curlgo run --curl imported.curl
```

stdout 输出一个 JSON 结果。HTTP 404/500 仍退出 0，业务结果由调用方根据 statusCode 和正文判断；输入、执行、输出错误分别退出 1、2、3。小文本正文内联，大正文和二进制写入 0600 文件，必须读取 body.path；正文最多捕获 4 MiB，并标记 truncated。

通过 --ca-file、--cookie-jar、--proxy 显式配置状态。CLI 不读取 GUI workspace 或系统代理变量。curl 文本不执行 shell、不展开变量、不读取隐式 @file；自签 HTTPS 使用 CA，不提供 insecure。响应头、正文与 URL 仍可能含凭证，分享结果前检查。

Agent 的调用规范见 [.trae/skills/api-test/SKILL.md](.trae/skills/api-test/SKILL.md)。
````

同一 README 中按下表替换旧描述，避免 CLI 发布后仍声称当前版本缺少已经实现的 GUI 能力：

| 位置 | 写入内容 |
|---|---|
| 开头 MyGo 版本 | `基于 MyGo 0.7.1 原生 UI` |
| 可以做什么：Cookie | `Cookie：遵循域名、路径、Secure、过期与公共后缀规则；GUI 登录态跨启动持久化，CLI 通过显式 Cookie Jar 复用。` |
| 可以做什么：导入 | `curl 文本、Postman v2.1 和 Bruno 集合导入；支持预览及跳过不支持项，不执行导入脚本。` |
| 可以做什么：网络与正文 | 增加 `自定义 CA，HTTP/HTTPS/SOCKS5 代理与绕过列表，显式 multipart 文件上传；JSON/XML/HTML/CSS 等响应格式化与高亮。` |
| 操作中的退出清除 Cookie | `GUI 收到 Set-Cookie 后自动保存，跨启动恢复，可手动清空。` |
| workspace.json 内容 | `保存草稿、收藏、历史、环境配置及 Cookie 元数据` |
| 首版边界：CA / 代理 | `默认校验 TLS、直连目标，不读取系统代理；支持显式 CA 和代理配置，尚无 OAuth 授权与客户端证书面板。` |
| 首版边界：multipart | `支持 JSON 显式 multipart 与文件上传；curl --form-string 支持文本表单，-F 与隐式文件读取仍拒绝。尚无 --data-urlencode 与脚本。` |
| 首版边界：Cookie | 删除 `Cookie 暂不跨启动持久化`，保留团队协作等边界 |
| 开发版本 | `需要 Go 1.27.3` |

- [x] **Step 4: 校验 skill 与文档。** 执行 `rg -n 'schemaVersion|Cookie|insecure|@file|body.path|timing|OUTPUT_ERROR' .trae/skills/api-test/SKILL.md README.md`，核对规则与 spec §5–11 一致；执行 `git diff --check`，预期无空白错误。用 Task 8 的进程测试确认 README 两个命令形态确实被支持。
- [x] **Step 5: 提交。**

```sh
git add .trae/skills/api-test/SKILL.md README.md
git commit -m "docs(api-test): add agent skill and CLI usage"
```

## Spec 覆盖与完成标准

| Spec 要求 | 实施与验证 |
|---|---|
| §4 GUI 前分发、同一二进制、零新依赖 | Task 7 main；Task 8 真实进程 / render / build |
| §5 输入互斥、1 MiB、严格版本/字段/文档、默认值 | Task 5 输入；Task 7 flag；Task 5/7 测试 |
| §5 JSON 变量/全局 Header/multipart、HTTP 协议限制 | Task 5 Resolve；Task 8 multipart；Task 5 输入测试 |
| §6 版本化 envelope、原始字节、多值 Header、无请求回显 | Task 3/6；Task 3 Header 测试；Task 8 secret 测试 |
| §6 内联/文件/截断/字节数与自动解压 | Task 3/6；Task 8 4 MiB、gzip、临时目录测试 |
| §6 timing 口径、重定向、失败、并发、Header 上限 | Task 2/3；Task 2 逻辑时钟/race；Task 8 网络测试 |
| §7 强类型 HTTPError / Unwrap / 未定义变量 | Task 1/3/6；Task 1/5/6 测试 |
| §8 CA、显式代理/绕过、无系统代理、无 GUI workspace | Task 4/7；Task 8 CA、代理、workspace 快照 |
| §8 Cookie 2 MiB/version/0600/原子/损坏保护/失败写回 | Task 4；Task 4 原件测试；Task 8 状态与输出失败 |
| §9 每个错误码与退出码、stdout 写失败 | Task 5/6/7；Task 6 全映射；Task 8 Writer 失败 |
| §10 安全解析、不提供 insecure、唯一正文与权限 | Task 5/6/9；Task 5 恶意 curl；Task 8 文件 |
| §11 Skill 触发、操作、安全对照、业务判断 | Task 9 全文及场景验收 |
| §13 既有 GUI 回归与四项验收命令 | Task 8 全套 test/vet/mygo vet/build，另加核心 race |

实施完成条件：九个任务全部勾选；上述验证命令有新鲜成功输出；新增 skill 可被 TRAE 发现；spec v1 字段/错误码无漂移；已有 GUI 单测与 `-render` 正常。此计划不包含 push、PR、合并、发布或替换当前运行的 GUI。

## 实施验收记录（2026-10-29）

- 九项任务均完成独立规范审查和代码质量审查；未增加依赖，GUI 执行器保持原有链路。
- `go test -race ./internal/core ./internal/cli -count=1`、`CGO_ENABLED=0 go test ./... -count=1`、`go vet ./...`、`go tool mygo vet .`、`CGO_ENABLED=0 go build ./...` 全部通过。
- 真实进程覆盖 JSON / curl 文件输入、HTTP 404 退出 0、SIGINT 返回 `CANCELED` / 2、隔离 GUI 配置不变、浅深色 1200×820 PNG 渲染。
- `/tmp/curlgo-api-test` 已构建，并用本地 HTTP 服务器完成 JSON / curl 文件输入冒烟；schemaVersion、退出码和原始正文符合契约。
- 项目 skill 位于 `.trae/skills/api-test/SKILL.md`，frontmatter、目录名、示例与正文格式验证通过。
