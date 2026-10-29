# CurlGo CLI 易用性与发送前检查

- 日期：2026-10-29
- 状态：已完成
- 来源：用户要求“根据现有功能继续完善”
- 基线：`3472353`，包含现有单请求 CLI 和请求认证
- 分支：`feat/cli-usability`；使用普通分支，不使用 git worktree

## 目标与选择

桌面端已具有参数、认证、收藏分组、集合导入导出；单请求 CLI 已能安全执行 HTTP，但缺少帮助入口、发送前检查和足够具体的输入诊断。

本轮优先完善现有 CLI。相比新增桌面功能，这些改进直接服务于刚交付的 Agent 调试链路；相比集合 runner，它们沿用已有单请求结果契约，改动范围更小。

## 契约与兼容性

消费者为开发者及项目 `api-test` skill。CLI 保持离线、无遥测、无新依赖；GUI 无参数启动和 `-render` 入口保持原有行为。

| 改动 | 分类 | 行为 |
|---|---|---|
| `help`、`help run`、`-h`、`--help` | 新增操作 | 在 GUI 初始化前输出一个 schema v1 JSON 帮助文档，退出 0 |
| `run -h`、`run --help` | 新增选项 | 输出相同命令帮助，不读输入、不访问配置、不发请求 |
| `run --dry-run` | 新增选项 | 检查输入、显式配置及 multipart 文件；输出 validation，不发送 HTTP |
| 正常 `run` | 兼容 | 仍输出 result/error；404/500 仍退出 0 |
| 输入诊断 message | 非机器契约文案更新 | 保留 code 与退出码，不回显底层错误或用户值 |
| envelope 的 help/validation | 可选字段 | 仅相应新操作输出；普通请求不增加这两个字段 |
| 输入与文件格式 | 兼容 | schemaVersion=1、Cookie version=1、GUI workspace 不变 |

已有严格解析器只需在调用新操作时读取 help/validation；现有 run 响应字段没有变化。不新增重试，POST 等操作仍由用户决定是否再次执行。

## 帮助与参数

支持：

```sh
curlgo help
curlgo help run
curlgo --help
curlgo -h
curlgo run --help
curlgo run -h
```

帮助包含 usage、选项说明与默认值、HTTP/HTTPS 协议、Bearer/Basic/API Key 认证能力、输入/捕获限制，以及 0–3 的退出码说明。选项从实际 FlagSet 生成，避免帮助与解析器漂移。

help 不接受未知主题或额外位置参数。run --help 的选项语法必须有效；语法错误仍为 INVALID_USAGE / 1。语法有效时，帮助优先于请求源、配置和正文阈值的执行约束，不触碰传入文件或 stdin。

帮助输出使用已有 emit，写失败退出 3，仅向 stderr 写固定诊断。无参数或现有 GUI 渲染参数不被 CLI 分发器接管。

## dry-run

```sh
curlgo run --request request.json --dry-run
curlgo run --curl imported.curl --dry-run
```

输入与普通 run 共享严格解码、Resolve、认证、环境、全局 Header、CA 与代理冲突检查以及 Cookie 文件加载校验。仅检查本地准备工作：

1. JSON/curl 仍限 1 MiB，使用同一 schema 与默认值。
2. 只支持 HTTP/HTTPS；变量和认证在内存中解析。
3. CA 与代理检查复用 configureEngine；Cookie 只读校验，不恢复/保存持久化更新。
4. multipart 复用 openMultipart 的文件准备逻辑，验证普通文件、可打开、文件名和表单合法性，然后立即关闭；不读取或上传内容。
5. 显式 out-dir 检查已存在且为目录；缺失 Cookie 文件的父目录须存在且为目录。此处不创建目录、不尝试写入，不能保证后续写权限、磁盘空间或持久化成功。
6. ctx 已取消/超时返回 CANCELED/TIMEOUT / 2，不打开 multipart 文件。局部文件准备完成后再检查 ctx。
7. 不发 HTTP/DNS/TLS/代理连接，不执行 shell，不调用 DoHTTP、SaveCookieFile 或正文落盘。

成功输出示意：

```json
{
  "schemaVersion": 1,
  "ok": true,
  "validation": {
    "dryRun": true,
    "method": "GET",
    "protocol": "HTTP",
    "bodyMode": "",
    "headerCount": 0,
    "formPartCount": 0,
    "timeoutSeconds": 30,
    "followRedirects": true
  }
}
```

validation 不包含 URL、Header 值、正文、认证字段、环境值或文件路径。它表示本地准备检查通过，不表示端点可达、证书可信链成功、登录有效或业务成功。

失败仍使用 error：输入/配置失败退出 1；multipart 文件准备失败为 REQUEST_BUILD_ERROR / 1；取消/超时退出 2；显式输出目录或 Cookie 父目录不可用为 OUTPUT_ERROR / 3。没有 result、timing 或正文文件。

## 安全诊断

保留已有错误码。固定文案区分：

- 输入读取失败和超过 1 MiB。
- JSON 外层解码、schemaVersion、缺失 request、request 字段及不支持协议。
- 未定义变量、请求约束及不支持的 curl。
- CA 读取/大小、内容冲突、无效 PEM。
- 无效命令行代理、代理冲突、JSON 代理或 bypass 无效。
- Cookie 文件读取、损坏或不支持格式。
- 参数缺失、互斥、额外位置参数、阈值超界。

不以中文/英文错误字符串匹配分类。大小限制可用私有 sentinel 配合 errors.Is；核心已有类型继续用 errors.As。禁止回显 JSON 字段名、URL、路径、代理凭证、变量名、Token 或底层 Error()。

## 文件职责

| 文件 | 职责 |
|---|---|
| `internal/cli/options.go` | 参数注册、帮助开关、dry-run 开关、互斥与范围校验 |
| `internal/cli/help.go` | FlagSet 帮助模型及 CLI 分发 |
| `internal/cli/validate.go` | 预检与安全 validation 元数据 |
| `internal/cli/run.go` | 共用输入/配置后选择预检或真实执行 |
| `internal/cli/input.go` | 固定、具体的输入诊断和有界读 |
| `internal/cli/output.go` | 可选 help/validation 字段；保留正常结果字段 |
| `internal/core/multipart.go` | 复用现有准备器检查文件并关闭 |
| `main.go` | GUI 前的 CLI 分发，保持 signal 与 render |
| 对应测试、README、api-test skill | 验证与操作说明 |

## 验收

- 帮助不消费 stdin，不打开指向不存在文件的输入/CA/Cookie。
- 帮助选项与 FlagSet 名称、默认值一致，JSON 仅一个文档；stdout 失败退出 3。
- dry-run 使用本机 HTTP/HTTPS 监听器及不可达代理，断言请求/连接数为 0。
- Cookie、输入和显式输出目录的内容及权限保持不变；默认临时目录没有新增正文。
- 覆盖 JSON/curl、变量、三种认证、multipart 缺失/目录/普通文件、CA/代理冲突、取消、输出目录及 Cookie 父目录缺失。
- 错误与 validation 中不出现预先植入的敏感标记；正常 404/500 与鉴权请求回归通过。
- 实际二进制验证根帮助、run 帮助、dry-run、普通 HTTP、SIGINT、浅深色 render。
- `go test -race ./internal/core ./internal/cli -count=1`、`CGO_ENABLED=0 go test ./... -count=1`、`go vet ./...`、`go tool mygo vet .`、`CGO_ENABLED=0 go build ./...`。

本轮不包含集合执行、断言 DSL、HTTP 自动重试、headless 流协议、发布或替换运行中的 GUI。

## 完成证据

帮助、无网络预检与安全诊断分别提交为 `e4e315e`、`8882d06`、`e949673`；各项均通过规格与质量双审查，整体源码审查通过。README 与项目 api-test skill 已同步帮助、预检、当前鉴权及诊断契约，文档双审查通过。

精确提交源码副本通过全量测试、core/CLI 竞态检测、Go vet、MyGo vet 与无 cgo 构建。实际二进制覆盖 6 种帮助入口、JSON/curl 零请求零写入预检、HTTP 404 退出 0 和安全版本诊断；root 验收覆盖 SIGINT 与浅深色渲染。8 个 JSON 块、9 个行内 JSON、项目技能格式及本地链接已校验，13 组文档请求/鉴权示例预检通过。完整执行记录见[实施计划](../plans/2026-10-29-cli-usability.md)。
