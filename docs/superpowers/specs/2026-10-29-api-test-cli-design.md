# CurlGo API-Test CLI 与 Agent Skill 设计

- 日期：2026-10-29
- 状态：已批准，进入实施计划阶段（2026-10-29）
- 范围：第一版（v1）—— 单请求 headless 执行器 + Agent skill；不含断言 Runner、集合批量、SSE/WebSocket headless、GUI 改造

---

## 1. 背景与目标

CurlGo 目前是一个纯本地、无账号、原生 GUI 的 API 客户端，核心能力（HTTP/SSE/WebSocket、自定义 CA、Cookie 持久化、multipart 上传、Postman/Bruno 导入、代理、响应格式化）集中在 `internal/core`，与 MyGo UI 解耦。

近期 Agent 驱动开发成为常态。CurlGo 可以作为 Agent 的本地 API 执行底座：让 Agent 用一个**零依赖、离线、安全**的二进制执行请求、拿到结构化结果、再做判断，而不是把 curl 文本丢给 shell 执行。

第一版目标：

1. 在不影响现有 GUI 的前提下，提供 `curlgo run` headless 子命令，支持请求 JSON 与安全 curl 双输入。
2. 输出机器可读、可审计的 JSON 结果，含分阶段 timing、schemaVersion、正文落盘元数据。
3. 提供 `api-test` skill，规范 Agent 的安全调用方式，突出相对 shell-curl 与 Postman/Bruno MCP 的差异。

非目标（v2+ 再考虑）：断言 DSL、集合批量执行与报告、SSE/WebSocket headless、GUI 与 CLI HTTP 路径合并、MCP server。

---

## 2. 定位与差异点

### 2.1 竞品格局（按 Agent 可用性分三层）

**第一层：面向 Agent 的 MCP server**

- [Postman MCP](https://learning.postman.com/docs/developer/postman-api/postman-api-mcp-server/overview/)：官方提供 Agent 管理 Postman 资源的入口；本地运行 MCP server 不等于其所有工作流都可离线。
- [Bruno CLI](https://docs.usebruno.com/introduction/getting-started)：本地集合执行工具，已有围绕 CLI 的 MCP 集成；具体 MCP 发布日期不作为本设计的决策依据。
- Hoppscotch MCP：需进一步核实其本地执行、CA 和认证边界，本 spec 不据此认定功能缺失。
- shell 包装型 curl MCP：若实现将不可信文本传给 shell，则存在命令注入风险；直接传递 argv 的实现不具有同样的 shell 展开风险。

**第二层：Headless 测试 CLI（agent 间接使用）**

- [Hurl](https://hurl.dev/)：Rust + libcurl，支持文本请求、断言和报告，是单二进制 CLI 的直接竞品；分阶段 timing 本身不作为独有能力。
- Bruno CLI / Newman：`bru run --env dev` 跑集合，需 Node 或 Docker；面向集合回归而非单请求探查。
- Slumber：Rust/TUI + YAML，正在补 `--exit-status` 等 CI flag，无头模式是附属。

**第三层：同源桌面客户端**

- [Yaak](https://yaak.app/)：local-first、Git 工作流，是人用客户端的对标对象；本 spec 不依据未验证的 CLI 功能缺失建立优势。

### 2.2 CurlGo 差异化（按可信度排序）

1. **curl 文本作为数据输入**：自研词法分析器拒绝未引用的 shell 操作符和展开语法，不展开系统变量、不读 curl 的 `@file`。仅支持已实现的参数子集，未知参数明确拒绝；请求仍需处于用户授权范围。
2. **单 Go 二进制、零运行时、全离线**：MyGo 无 cgo，不依赖 npm/Node/Docker，内网开发机即装即用。
3. **为 Agent 设计的机器契约**：版本化 JSON envelope、语义化退出码、默认不回显请求头/正文。响应头、响应体和 URL 仍可能包含凭证，不能承诺结果完全脱敏。
4. **默认无状态、状态全部显式**（`--ca-file`、`--cookie-jar`、`--proxy`），连续调用可复现、可审计。
5. **GUI 与 CLI 共享核心能力**：v1 共享请求模型、解析、CA、代理和 Cookie 能力；GUI 收藏执行与结果的一键回放不在 v1 范围。
6. **内网命门能力已就位**：自定义 CA、Cookie 跨重启、multipart 流式、代理。

一句话定位：*"CurlGo is the zero-dependency, offline API runner your coding agent can safely drive — one binary, safe curl in, machine-readable result out."*

---

## 3. 第一版范围

| 项 | v1 | v2+ |
|---|---|---|
| 协议 | HTTP/HTTPS | SSE、WebSocket |
| 输入 | 请求 JSON、安全 curl | 集合批量、`.bru`/`.hurl` 导入执行 |
| 断言 | 无（由 agent 判断） | 状态码/Header/JSONPath 断言 |
| 状态 | 默认无状态，显式 CA/Cookie/Proxy | 环境选择器、集合级变量 |
| 正文交付 | 小正文内联，大/二进制落盘，可配置内联阈值 | 压缩 |
| GUI 影响 | 不改 `Engine.Run`，旁路新增 | GUI HTTP 分支复用 `DoHTTP` |
| 分发 | 同构二进制 `curlgo run` | MCP server（可选） |

---

## 4. 总体架构

三层，边界清晰：

```
┌──────────────────────────────────────────────────────────┐
│ Agent                                                     │
│   └─ skills/api-test/SKILL.md （操作规范）                  │
└────────────────────────┬─────────────────────────────────┘
                         │ 调用
┌────────────────────────▼─────────────────────────────────┐
│ internal/cli （薄适配）                                    │
│   run.go    : 子命令分发、参数、退出码                      │
│   input.go  : JSON / curl 解码与校验                        │
│   output.go : JSON envelope、正文内联/落盘、脱敏            │
└────────────────────────┬─────────────────────────────────┘
                         │ 调用
┌────────────────────────▼─────────────────────────────────┐
│ internal/core （核心，无 UI 依赖）                         │
│   headless_http.go : Engine.DoHTTP / HTTPResult / HTTPError│
│   cookie_file.go    : 版本化 Cookie Jar 读写                │
│   复用: model.Resolve, curl.ImportCURL, engine.            │
│         SetCACertificates, SetProxy, CookieSnapshot/...    │
└──────────────────────────────────────────────────────────┘
```

入口分发（`main.go`）：

- 无参数 → 启动 GUI（现状不变）。
- `run` 子命令 → 在 MyGo 初始化前分发到 `cli.Run`。
- `-render ...` → 保持现有 UI 渲染测试。

`main.go` 只做分发，不写业务逻辑。

---

## 5. CLI 命令与输入契约

### 5.1 命令

```sh
# 请求 JSON（Agent 生成）
curlgo run --request request.json
curlgo run --request - < request.json

# 安全 curl（不经过 shell）
curlgo run --curl imported.curl
curlgo run --curl - < imported.curl

# 可选状态与输出
curlgo run --request request.json \
  --ca-file ./ca.pem \
  --cookie-jar ./.curlgo-cookies.json \
  --proxy http://127.0.0.1:7890 \
  --out-dir ./test-output
```

参数：

| 参数 | 说明 |
|---|---|
| `--request <path|->` | 请求 JSON 文件；`-` 表示 stdin |
| `--curl <path|->` | curl 文本文件；`-` 表示 stdin |
| `--ca-file <path>` | PEM CA 证书文件，空则用系统信任 |
| `--cookie-jar <path>` | Cookie Jar 文件；不传则仅进程内保存 |
| `--proxy <url>` | 代理地址，复用 `core.ParseProxy` 校验 |
| `--out-dir <dir>` | 正文落盘目录；不传则用临时目录 |
| `--inline-body-bytes <n>` | 内联阈值，默认 262144（256 KiB） |

`--request` 与 `--curl` 互斥。
请求 JSON、curl 文本和 CA 文件分别限制为 1 MiB。内联阈值范围为 0–4 MiB；0 表示非空正文全部落盘。

### 5.2 请求 JSON envelope

```json
{
  "schemaVersion": 1,
  "request": {
    "name": "健康检查",
    "method": "GET",
    "url": "{{base_url}}/health",
    "headers": [{ "key": "Accept", "value": "application/json" }],
    "body": "",
    "bodyMode": "",
    "formParts": [],
    "followRedirects": true,
    "timeoutSeconds": 30
  },
  "settings": {
    "environment": "local",
    "environments": {
      "local": { "base_url": "http://127.0.0.1:8080" }
    },
    "globalHeaders": []
  }
}
```

约束：

- `request.protocol` 省略或为 `HTTP`；`SSE`/`WebSocket` 在 `run` 中明确拒绝。
- 输入 `schemaVersion` 必须为整数 `1`；未知字段、额外 JSON 文档和不支持的版本拒绝。省略 method、protocol、timeoutSeconds、followRedirects 时分别默认 GET、HTTP、30、true；显式 false 保留，显式 0 秒拒绝。
- `settings.caCertPEM` 与 `--ca-file` 同时提供且内容不一致时，报错（拒绝静默覆盖）。
- 变量替换、Header 合并、multipart 校验复用 `core.Resolve`。
- curl 输入只表达请求本身，不执行 shell、不展开系统变量、不读 `@file` 正文；环境变量场景走 JSON。
- CLI 不读取 GUI 的 `QingzhouAPI/workspace.json`。
- JSON 的 `settings.proxy` 可显式指定代理和绕过列表；`--proxy` 同时指定时，规范化 URL 必须一致，不能静默覆盖。不读取 HTTP_PROXY、HTTPS_PROXY 或 NO_PROXY 等系统环境配置。

---

## 6. 输出契约

stdout 永远输出一个 JSON envelope，字段稳定：

```json
{
  "schemaVersion": 1,
  "ok": true,
  "result": {
    "method": "GET",
    "url": "http://127.0.0.1:8080/health",
    "statusCode": 200,
    "status": "200 OK",
    "proto": "HTTP/1.1",
    "headers": {
      "Content-Type": ["application/json"],
      "Set-Cookie": ["session=...; Path=/; HttpOnly"]
    },
    "body": {
      "mode": "inline",
      "encoding": "utf-8",
      "contentType": "application/json",
      "bytes": 15,
      "text": "{\"status\":\"ok\"}",
      "truncated": false
    },
    "timing": {
      "dnsMs": 0.0,
      "connectMs": 0.3,
      "tlsMs": 0.0,
      "ttfbMs": 3.1,
      "totalMs": 3.42
    }
  }
}
```

### 6.1 字段规则

- `schemaVersion`：结果 envelope 版本，当前 `1`。
- `ok: true` 表示 HTTP 响应已按捕获策略成功读取；401/404/500 仍 `ok: true`、退出码 0。已收到响应头但正文读取失败时 `ok: false`、退出码 2；可附带部分 `result`。达到 4 MiB 捕获上限是明确截断，不是执行错误。
- `headers`：`map[string][]string`，保留重复与多值 Header。
- 默认**不回显请求头与请求正文**；结果回显最后一个已收到响应的 `resp.Request.Method` 和 `resp.Request.URL`。响应本身及 URL 可能包含敏感数据；错误 message 不直接输出含完整 URL 的 `url.Error`。
- 正文不做 JSON pretty-print，保留原始字节（与 GUI 展示模型解耦）。
- `body.mode`：
  - `inline`：小且为 UTF-8 文本正文，`text` 字段携带内容。
  - `file`：二进制或超过内联阈值，`path` 字段指向落盘文件；JSON 含 `contentType`、`bytes`、`truncated`。
- `body.truncated`：响应超过 `MaxBody`（4 MiB）时为 true，保存已接收部分。
- `body.bytes` 是保存的正文字节数，不是线上传输量或已知总响应长度；Go 自动解压时是解压后的原始应用正文，不代表 HTTP 线上的压缩字节。文件模式使用 `encoding: "binary"`，原样保存，不重新编码。
- `timing` 通过 `httptrace.ClientTrace` 采集，单位毫秒：DNS/connect/TLS 是本次调用所有完成阶段（包括重定向和拨号尝试）的耗时和；未发生的阶段为 0。TTFB 是从 `DoHTTP` 开始到最后一个已收到响应的首字节的累计耗时；total 从同一起点到正文读完、明确截断或失败。阶段可能重叠，不能相加推导 total。失败时保留已观测的阶段，不虚构未发生的首字节时间；没有响应的错误可省略 result。
- `httptrace` 回调可能并发且迟于正文返回，采集和快照必须同步。HTTP Transport 的响应头大小另限制为 256 KiB，保证 stdout 有界。

### 6.2 错误输出

```json
{
  "schemaVersion": 1,
  "ok": false,
  "error": {
    "code": "TIMEOUT",
    "message": "请求超时"
  }
}
```

错误码见 §9。

---

## 7. 核心接口

新增 `internal/core/headless_http.go`，旁路实现，不改 `Engine.Run`：

```go
type HTTPOptions struct {
    MaxBodyBytes int // <=0 用 MaxBody (4 MiB)
}

type HTTPTiming struct {
    DNS     time.Duration
    Connect time.Duration
    TLS     time.Duration
    TTFB    time.Duration
    Total   time.Duration
}

type HTTPResult struct {
    Method        string // 最后一个已收到响应的请求
    URL           string
    Proto         string
    StatusCode    int
    Status        string
    Headers       http.Header
    Body          []byte
    BodyTruncated bool
    Timing        HTTPTiming
}

type HTTPErrorCode string

const (
    HTTPErrInvalidRequest  HTTPErrorCode = "INVALID_REQUEST"
    HTTPErrRequestBuild    HTTPErrorCode = "REQUEST_BUILD_ERROR"
    HTTPErrRedirect        HTTPErrorCode = "REDIRECT_ERROR"
    HTTPErrTLS             HTTPErrorCode = "TLS_ERROR"
    HTTPErrConnect         HTTPErrorCode = "CONNECT_ERROR"
    HTTPErrTimeout         HTTPErrorCode = "TIMEOUT"
    HTTPErrCanceled        HTTPErrorCode = "CANCELED"
    HTTPErrBodyRead        HTTPErrorCode = "BODY_READ_ERROR"
)

type HTTPError struct {
    Code HTTPErrorCode
    Op   string
    Err  error
}

func (e *Engine) DoHTTP(
    ctx context.Context,
    request Request,
    options HTTPOptions,
) (HTTPResult, error)
```

约束：

- 入参必须已通过 `Resolve`，且协议为 HTTP/HTTPS；`DoHTTP` 不做变量替换、curl 导入、环境解析。
- 4xx/5xx 是正常 `HTTPResult`，不是 error。
- `Body` 保留原始字节。
- 错误通过 `HTTPError` wrap 原始错误，CLI 不匹配字符串，仅映射 `Code`。
- 复用 `Engine` 的 `client()`（含 CA、Proxy、Cookie Jar、重定向策略）。
- timing 用 `httptrace.WithClientTrace` 注入到请求上下文。
- 超时覆盖请求构建、连接、上传、重定向和正文读取；外部 context 取消映射为 `CANCELED`、退出码 2。
- `HTTPError` 实现 `Error()` 和 `Unwrap()`；错误码判断使用 `errors.As` / `errors.Is`。未定义变量需为 `Expand` 增加保留现有错误文案的强类型错误，不通过匹配中文字符串分类。

---

## 8. 状态管理

### 8.1 CA 证书

- `--ca-file` 指定的 PEM 通过 `Engine.SetCACertificates` 注入。
- JSON `settings.caCertPEM` 与 `--ca-file` 冲突时拒绝。
- 不传则用系统信任（`x509.SystemCertPool`）。

### 8.2 Cookie Jar

新增 `internal/core/cookie_file.go`，定义版本化格式：

```json
{
  "version": 1,
  "cookies": [
    {
      "url": "https://example.com/",
      "cookie": { "Name": "session", "Value": "example", "Path": "/" }
    }
  ]
}
```

- 文件权限 `0600`，原子写入（复用 `store.atomicWrite` 模式）。
- Cookie 文件读取限制为 2 MiB（含 envelope），拒绝损坏 JSON 和未知 version，不覆盖损坏原件；父目录须已存在。不同 CLI 进程对同一个 jar 的操作由调用方串行化，v1 不引入跨进程锁。
- 开始时若文件存在，用 `Engine.RestoreCookies` 载入。
- 请求结束后用 `Engine.CookieSnapshot` 写回。
- 请求执行失败后仍保存已收到的 Set-Cookie 更新；Cookie 写回失败是 `OUTPUT_ERROR`、退出码 3。已有 HTTP 结果可附带返回；不静默丢弃主执行错误。
- 未传 `--cookie-jar` 时不读写任何文件，Cookie 仅在本次进程内存中有效。

### 8.3 代理

- `--proxy` 通过 `core.ParseProxy` 校验，再 `Engine.SetProxy`。
- 复用现有 `ProxySettings` 与 `proxyError` 分类。
- 不传则直连（`Engine` 默认 `Proxy: nil`）。

### 8.4 环境与全局 Header

- 由 JSON `settings` 显式提供，不读 GUI workspace。
- 变量未定义时返回 `UNRESOLVED_VARIABLE`。

---

## 9. 错误码与退出码

退出码：

| 码 | 含义 |
|---|---|
| 0 | HTTP 响应按捕获策略成功读取（含 4xx/5xx、明确截断） |
| 1 | 输入错误：参数、JSON、curl、变量、请求约束 |
| 2 | 执行错误：DNS、连接、TLS、超时、正文读取 |
| 3 | 输出错误：目录不可写、落盘失败 |

错误码映射：

| code | 退出码 | 触发 |
|---|---|---|
| `INVALID_USAGE` | 1 | 参数缺失/互斥冲突 |
| `INVALID_INPUT` | 1 | JSON 格式错误、协议非 HTTP |
| `INVALID_CURL` | 1 | curl 解析失败 |
| `UNRESOLVED_VARIABLE` | 1 | `{{name}}` 未定义 |
| `INVALID_REQUEST` | 1 | `Resolve` 拒绝（Header 名无效、超时越界等） |
| `REQUEST_BUILD_ERROR` | 1 | 构建请求或读取显式 multipart 文件失败 |
| `REDIRECT_ERROR` | 2 | 重定向超过上限或重放请求失败 |
| `TLS_ERROR` | 2 | 证书校验失败 |
| `CONNECT_ERROR` | 2 | DNS/连接失败，含代理连接失败 |
| `TIMEOUT` | 2 | 上下文超时 |
| `CANCELED` | 2 | 外部 context 取消 |
| `BODY_READ_ERROR` | 2 | 响应体读取中断 |
| `OUTPUT_ERROR` | 3 | 正文落盘、Cookie 写回、stdout 写入失败 |

成功和错误 envelope 都必须带 `schemaVersion: 1`。stdout 写入失败时无法保证 JSON 已完整送达，此时退出 3 并向 stderr 写不含请求数据的固定诊断；不得继续追加第二个 JSON 文档。

---

## 10. 安全设计与对照

### 10.1 安全对照表（skill 必含）

| 场景 | 把 curl 文本交给 shell | **CurlGo `run --curl`** |
|---|---|---|
| 未引用的 `;` / `\|` / `&` | 可改变命令结构 | **词法拒绝**，引号内的字面量保留 |
| `$()` / 反引号 / `$VAR` | 可展开执行 | **拒绝展开语法**，单引号字面量保留 |
| curl `@file` / `-F` | 可读取上传本地文件 | **拒绝**；仅请求 JSON 的显式 multipart filePath 可读取文件 |
| Token 暴露 | 参数可进入历史和进程列表 | 输入走文件/stdin，**不回显请求头**；响应仍可能包含 Token |
| 自签 HTTPS | 由 curl 参数决定校验方式 | **显式 `--ca-file`，不提供 insecure** |
| 状态复用 | 取决于 curl 参数和环境配置 | **显式 Cookie Jar 和代理**，不读取 GUI 或系统代理配置 |

### 10.2 安全原则

1. curl 永远走 `core.SplitShell` + `ImportCURL`，绝不交给 `os/exec` 或 shell。
2. 默认无状态：不传 `--cookie-jar` 不读写 Cookie 文件，不传 `--ca-file` 用系统信任；大正文和二进制仍可按输出规则落盘。
3. 默认不回显请求敏感信息；如需调试，agent 应主动检查自己生成的请求 JSON，而非依赖输出回显。
4. 不提供 `--insecure`；自签证书必须显式 `--ca-file`。
5. 正文使用唯一文件名并显式设置 `0600`；默认临时目录 `0700`。显式输出目录须已存在，不改变其权限、不覆盖已有文件。默认临时正文需保留给 Agent 读取，并由调用方在读取完毕后清理。

---

## 11. api-test Skill 行为边界

`skills/api-test/SKILL.md` 是项目内的通用 Agent 能力，参考资料随包存放于 `references/`，客户端通过安装使用。它只做操作规范，不实现第二套 HTTP 客户端：

- 触发：调试/验证/冒烟测试本地或内网 HTTP API、执行 curl、检查状态码/响应头/JSON/重定向/登录态。
- 复杂请求优先生成临时 JSON 文件或 stdin，避免 secret 进命令行参数。
- curl 只能通过 `curlgo run --curl` 进入安全解析器，**绝不** `sh -c "$curl"`。
- 判断规则：
  - `ok=true` 只代表收到响应；业务成败需结合 `statusCode` 与 `body`。
  - 非零退出码才是工具层失败（输入/连接/TLS/超时/输出）。
- 自签 HTTPS 用 `--ca-file`；没有就先获取 CA，不绕过校验。
- 需登录态串联时才用 `--cookie-jar`，默认无状态。
- `body.mode=file` 时必须读 `path`，不能假设正文在 stdout。
- §10.1 安全对照表作为 skill 的强制规则前置。

---

## 12. 文件变更清单

| 文件 | 动作 | 说明 |
|---|---|---|
| `internal/core/headless_http.go` | 新增 | `DoHTTP`、`HTTPResult`、`HTTPError`、timing |
| `internal/core/cookie_file.go` | 新增 | 版本化 Cookie Jar 读写 |
| `internal/cli/run.go` | 新增 | 子命令分发、参数、退出码 |
| `internal/cli/input.go` | 新增 | JSON / curl 解码校验 |
| `internal/cli/output.go` | 新增 | JSON envelope、正文内联/落盘、脱敏 |
| `main.go` | 修改 | 增加 `run` 子命令分发 |
| `skills/api-test/SKILL.md` | 新增 | Agent 操作规范与安全对照表 |
| `internal/core/model.go` | 定向修改 | 未定义变量返回强类型错误，保留现有文案 |
| 对应 `_test.go` | 新增 | 见 §13 |

---

## 13. 测试策略与验收

### 13.1 核心层（`internal/core`）

- `DoHTTP` 返回原始字节，不格式化 JSON。
- 200/400/404/500 均正常返回，非 error。
- 重复/多值响应头保留。
- multipart、重定向跨域移除敏感头、自定义 CA、代理、超时、TLS 失败、正文截断。
- timing 验证 TLS、DNS、重定向、失败和并发回调；验证阶段口径和 total，不对墙钟时间设置易波动的上限。
- 现有 `Engine.Run` 与 GUI 测试不改，确认旁路无影响。

### 13.2 CLI 层（直接调用 `cli.Run`）

```go
func Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) ExitCode
```

覆盖：

- JSON 请求成功。
- curl stdin 成功。
- 404/500 退出码 0。
- 非法 JSON、非法 curl、未定义变量退出码 1。
- DNS/TLS/超时退出码 2。
- 输出目录不可写退出码 3。
- 小正文内联、大正文和二进制落盘。
- 两次调用通过 `--cookie-jar` 保持登录态。
- 不读取 GUI workspace。
- 结果不泄露 Authorization/Cookie 请求头。
- `schemaVersion` 字段存在且为 1。

### 13.3 验收命令

```sh
go test ./...
go vet ./...
go tool mygo vet .
go build ./...
```

---

## 14. 后续演进（v2+）

- GUI HTTP 分支复用 `DoHTTP`，统一执行路径。
- 内置断言：状态码、Header、JSONPath、响应时间阈值。
- 集合批量执行（复用 `CollectionImport`），输出汇总报告。
- SSE headless（JSON Lines 输出，事件数/时长上限）。
- WebSocket headless（脚本化消息序列）。
- 可选 MCP server（包装 `cli.Run`，不新增执行逻辑）。
