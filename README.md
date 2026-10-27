# 轻舟 API

MyGo 0.5 原生离线 API 客户端。打开即用，无需账户或先建项目，应用自身不连接云端服务。

## 可以做什么

- HTTP / HTTPS 请求：方法、URL、请求头、原始正文、JSON 格式化响应、响应头、耗时、取消与超时。
- SSE：支持 GET / POST，持续接收事件、多行 data、event / id，随时停止。
- WebSocket：ws / wss、握手请求头、文本双向消息、二进制消息的 base64 展示。
- Cookie：同一会话共享 Cookie Jar，遵循域名、路径、Secure、过期及公共后缀规则；查看当前地址适用值或清空。
- 多个环境、全局请求头，URL / Header / 正文使用 `{{name}}` 引用变量；请求同名 Header 覆盖全局配置。
- curl 文本导入与复制导出，收藏请求，最近 100 条历史恢复，上次草稿自动恢复。
- 浅色 / 深色主题，键盘操作，原生可调整的请求与响应分区。

## 操作

直接输入完整地址发送，或点「导入 curl」。环境与全局请求头在左下角配置，环境值使用 JSON 字符串对象。Cookie 收到 `Set-Cookie` 后自动保存，退出后清除。收藏与历史可在左侧切换。

| 快捷键 | 功能 |
|---|---|
| ⌘ N | 新请求 |
| ⌘ L | 聚焦地址 |
| ⌘ Enter | 发送 / 停止 |
| ⌘ S | 收藏 / 更新收藏 |
| Esc | 停止连接 |

## 本地存储

macOS 数据保存在 `~/Library/Application Support/QingzhouAPI/`；其他平台使用 Go 的用户配置目录。`workspace.json` 保存草稿、收藏、历史和环境配置；`workspace.json.bak` 保留上一份有效内容。后台合并保存、同步文件、原子替换，关窗前完成保存。损坏原件会保留，并显示恢复提示；无法恢复时进入临时会话，避免覆盖原件。

数据为本地明文，目录权限 0700、文件权限 0600。分享或提交数据文件前请检查请求凭证与环境值。历史不保存响应正文。客户端无遥测、自动更新或云同步。

## 首版边界

- 默认校验 TLS 证书，直连目标，不读取系统代理；尚无自定义 CA、代理、OAuth 授权、客户端证书面板。
- 请求正文及单条流事件 / WebSocket 消息最多 1 MiB；HTTP 响应最多展示 4 MiB，超出提示截断。流消息保留最近 256 KiB。
- HTTP 超时限制整次请求；SSE / WebSocket 超时限制连接建立阶段。SSE 首版不自动重连，避免重复提交 POST。
- curl 支持常用 `--request`、`--header`、`--data`、`--data-raw`、`--data-binary`、`--json`、`--user`、`--cookie`、`--oauth2-bearer`、`--url`、`--location`、`--head`、`--max-time`。不执行命令、不展开系统变量、不读 `@file`；未知参数明确拒绝。暂不支持 multipart、文件上传、`--data-urlencode` 和脚本。
- 不包括团队协作、插件、GraphQL 专用面板、gRPC；Cookie 暂不跨启动持久化。
- Mac 为首要平台。Linux 内存渲染和协议测试不代表已完成 Mac 真机验收；启动耗时、常驻内存与签名需真机验证。

## 开发

需要 Go 1.27.1，MyGo 不依赖 cgo。网络执行与持久化逻辑在 `internal/core`，原生交互状态在 `app.go`，视图在 `view.go`。

```sh
CGO_ENABLED=0 go run .
CGO_ENABLED=0 go test -p=2 -parallel=2 ./...
go tool mygo vet .
go tool mygo build -platform darwin/arm64 -skip-dmg
```

开发用的原生渲染可通过 `-render <file.png>` 和可选 `-dark` 参数生成，不读取用户数据、不发送网络请求。

## 依赖

MyGo 负责原生 UI，Go 标准库负责 HTTP 与文件持久化，coder/websocket 处理 WebSocket 协议，x/net/publicsuffix 支持 Cookie 公共后缀校验。依赖许可随应用包附带。
