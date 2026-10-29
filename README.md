# CurlGo

轻量离线 API 客户端，基于 MyGo 0.7.1 原生 UI。打开即用，无需账户或先建项目，应用自身不连接云端服务。

## 可以做什么

- HTTP / HTTPS 请求：方法、URL、可编辑查询参数、请求头、原始正文与 multipart 文件上传；响应头、耗时、取消与超时，JSON / XML / HTML / CSS 响应格式化。
- SSE：支持 GET / POST，持续接收事件、多行 data、event / id，随时停止。
- WebSocket：ws / wss、握手请求头、文本双向消息、二进制消息的 base64 展示。
- Cookie：GUI 共享并持久化 Cookie Jar，重启后恢复，遵循域名、路径、Secure、过期及公共后缀规则；查看当前地址适用值或清空。CLI 跨调用共享需显式指定 Cookie 文件。
- 自定义 CA，HTTP / HTTPS / SOCKS5 代理与绕过列表；默认校验证书、直连，不自动读取系统代理。
- 多个环境、全局请求头，URL / Header / 正文使用 `{{name}}` 引用变量；请求同名 Header 覆盖全局配置。
- 鉴权面板：Bearer、Basic 和 API Key（Header / Query）；响应变量提取：JSON Pointer 或响应头，完整 2xx 后原子写入环境，供后续请求引用。
- curl 文本导入与复制导出，收藏请求，最近 100 条历史恢复，上次草稿自动恢复。
- 收藏分组；Postman v2.1、Bruno 和原生 CurlGo 集合导入预览，CurlGo / Postman 集合导出；不执行集合脚本。
- 浅色 / 深色主题，键盘操作，原生可调整的请求与响应分区。

## Agent 与 CLI

`curlgo run` 不启动 GUI，每次执行一个 HTTP / HTTPS 请求，向 stdout 输出一个 `schemaVersion: 1` JSON 文档。先用 `command -v curlgo` 检查可执行文件；不存在时在仓库根目录构建同一应用，以下命令的 `curlgo` 均替换为 `/tmp/curlgo-api-test`：

```sh
CGO_ENABLED=0 go build -o /tmp/curlgo-api-test .
```

本地服务已在 8080 端口运行时，`request.json` 最小示例：

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

`imported.curl` 可包含 `curl --location 'http://127.0.0.1:8080/health'`。选择文件或 stdin 输入，`--request` 与 `--curl` 互斥：

```sh
curlgo run --request request.json
curlgo run --request - <request.json
curlgo run --curl imported.curl
curlgo run --curl - <imported.curl
```

输入与输出均使用 schema v1。JSON 缺省为 GET / HTTP、30 秒超时、跟随重定向；显式 `false` 保留，超时 0 拒绝。curl 未指定 `-L` 时不跟随重定向。退出码 0 / `ok: true` 表示响应捕获、已配置的提取及输出成功，不代表业务成功，HTTP 401 / 404 / 500 也可退出 0；须检查 `result.statusCode` 与正文。退出码 1 为输入或配置错误，2 为传输、取消、超时、正文读取或响应提取失败，3 为输出或 Cookie 保存失败。

小文本原样放在 `result.body.text`；二进制或超过默认 262144 字节内联阈值的正文保存到 `body.path`，须读取该文件。`--inline-body-bytes` 范围 0–4194304，0 让所有非空正文写文件；捕获最多 4 MiB，`truncated` 为真时不能声称响应完整或直接解析不完整 JSON。文件 0600，默认临时目录 0700，保留供调用方读取与清理；`--out-dir` 须为已有目录，不应删除用户目录或无关文件。`timing` 为毫秒，DNS / 连接 / TLS 累计已完成阶段，TTFB 从执行开始到最后响应首字节，total 到捕获结束；阶段可重叠，不可相加，正文 bytes 为自动解压后的应用字节而非网络流量。

CLI 不加载 GUI 工作区。变量来自 JSON `settings.environments` / `environment`，而非 OS 环境；CA、Cookie、代理分别通过 `--ca-file`、`--cookie-jar`、`--proxy` 显式提供，不读取 `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY`。凭证使用 0600 私有文件或 stdin，不放命令行参数；curl 只解析文本，不执行 shell 或隐式 `@file` 读取。响应头、正文和 URL 仍可能含秘密。安全输入、授权 multipart、串行登录链、错误与清理规则见项目技能 [api-test](.trae/skills/api-test/SKILL.md)；v1 不含断言 DSL、集合运行器或无界面 SSE / WebSocket。

## 操作

直接输入完整地址发送，或点「导入 curl / 集合」；集合先预览再加入收藏，查询参数在「参数」页编辑。环境、全局请求头、CA 与代理在左下角配置，环境值使用 JSON 字符串对象。Cookie 收到 `Set-Cookie` 后自动保存，重启后恢复，可在 Cookie 页清空。收藏与历史可在左侧切换，收藏支持分组与集合导出。

在「提取」页配置变量名及 `/data/access_token` 等路径，成功后可在下一条请求的鉴权中引用 `{{token}}`。规则、CLI 的 `result.extractedVariables` 契约及集合兼容性见[响应变量提取](docs/response-extraction.md)。

| 快捷键 | 功能 |
|---|---|
| ⌘ N | 新请求 |
| ⌘ L | 聚焦地址 |
| ⌘ Enter | 发送 / 停止 |
| ⌘ S | 收藏 / 更新收藏 |
| Esc | 停止连接 |

## 本地存储

GUI 的 macOS 数据保存在 `~/Library/Application Support/QingzhouAPI/`；其他平台使用 Go 的用户配置目录。CurlGo 保留初版数据目录名称，已有草稿、收藏与历史可继续读取。`workspace.json` 保存草稿、收藏与分组、历史、环境配置和 Cookie；`workspace.json.bak` 保留上一份有效内容。后台合并保存、同步文件、原子替换，关窗前完成保存。损坏原件会保留，并显示恢复提示；无法恢复时进入临时会话，避免覆盖原件。

数据为本地明文，目录权限 0700、文件权限 0600。分享或提交数据文件前请检查请求凭证与环境值。历史不保存响应正文。客户端无遥测、自动更新或云同步。

## 当前边界

- 默认校验 TLS 证书，直连目标，不读取系统代理；暂无 OAuth 授权流程或客户端证书面板。
- 原始请求正文及单条流事件 / WebSocket 消息最多 1 MiB；multipart 文件流式上传，不受原始正文大小限制。HTTP 响应最多捕获 4 MiB，超出提示截断。流消息保留最近 256 KiB。
- HTTP 超时限制整次请求；SSE / WebSocket 超时限制连接建立阶段。SSE 首版不自动重连，避免重复提交 POST。
- curl 支持常用 `--request`、`--header`、`--data`、`--data-raw`、`--data-binary`、`--json`、`--user`、`--cookie`、`--oauth2-bearer`、`--url`、`--location`、`--head`、`--max-time` 和文本 `--form-string`。不执行命令、不展开系统变量、不读隐式 `@file`；`-F` / `--form`、`--data-urlencode` 和未知参数明确拒绝。上传文件需在 GUI 选择，或使用获授权的 CLI JSON multipart 绝对路径。
- 不包括团队协作、插件、GraphQL 专用面板、gRPC 或脚本执行。
- Mac 为首要平台。Linux 内存渲染和协议测试不代表已完成 Mac 真机验收；启动耗时、常驻内存与签名需真机验证。

## 开发

需要 Go 1.27.3，MyGo 不依赖 cgo。网络执行与持久化逻辑在 `internal/core`，原生交互状态在 `app.go`，视图在 `view.go`。

```sh
CGO_ENABLED=0 go run .
CGO_ENABLED=0 go test -p=2 -parallel=2 ./...
go tool mygo vet .
go tool mygo build -platform darwin/arm64 -skip-dmg
```

开发用的原生渲染可通过 `-render <file.png>` 和可选 `-dark` 参数生成，不读取用户数据、不发送网络请求。

## 依赖

MyGo 负责原生 UI，Go 标准库负责 HTTP 与文件持久化，coder/websocket 处理 WebSocket 协议，x/net/publicsuffix 支持 Cookie 公共后缀校验。依赖许可随应用包附带。
