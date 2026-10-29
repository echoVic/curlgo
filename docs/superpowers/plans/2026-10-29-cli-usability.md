# CLI Usability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完成机器可读帮助、无网络 dry-run 和安全、具体的诊断。

**Architecture:** 沿用 v1 Run/Resolve/Engine；共用参数注册生成帮助，预检只做本地准备。通过可选 help/validation 保持普通 run envelope 不变。

**Tech Stack:** Go 1.27.3 标准库；无新增依赖。

---

执行依据：[CLI 易用性设计](../specs/2026-10-29-cli-usability-design.md)。用户已要求继续完善；沿用此前执行方式，不额外等待流程确认。普通分支 `feat/cli-usability`；不使用 worktree。每项 fresh implementer → spec review → quality review；仅 leader 提交，禁止把未跟踪 HTML 纳入提交。

## Task 1: 帮助与参数

Files: create `internal/cli/options.go`、`help.go`、`help_test.go`；modify `run.go`、`output.go`、`main.go`、`run_test.go`。

- [x] 写失败测试：根 help/help run/-h/--help、Run 的 -h/--help；拒绝未知主题、额外参数与无效语法；帮助不读 stdin/输入/CA/Cookie；writer 失败退出 3。原来把 --help 当 INVALID_USAGE 的用例改为帮助契约。
- [x] `go test ./internal/cli -run 'TestHelp|TestDispatch|TestRunHelp' -count=1`，观察新增接口缺失或帮助仍返回 1。
- [x] 提取共用参数注册及解析，加入 help/h 的 bool 选项。dry-run 在 Task 2 实现执行分支时再注册，避免中间提交中的预检参数实际发送请求。普通互斥、阈值边界保持不变。实现 Dispatch 和帮助 JSON，可选 envelope.Help，仅帮助时输出。帮助选项从 FlagSet VisitAll/UnquoteUsage 获取，使用 core.MaxInput/MaxBody。

接口：

```go
func Dispatch(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) (ExitCode, bool)
```

main 在 GUI 前分发；无参数与渲染参数返回 handled=false；run 继续使用 signal context。

- [x] `gofmt` 修改文件；`go test ./internal/cli -count=1` 与现有进程验收通过（精确暂存源码副本，隔离并行开发）。
- [x] 双审查后，精确暂存并提交 `feat(cli): add machine-readable help and shared options`（`e4e315e`）。

## Task 2: 无网络预检

Files: create `internal/cli/validate.go`、`validate_test.go`、`internal/core/multipart_validation_test.go`；modify `run.go`、`output.go`、`internal/core/multipart.go`、`main_cli_test.go`。

- [x] 写失败测试：JSON/curl dry-run 返回 validation；HTTP/HTTPS 请求数为零、代理无连接；敏感标记不回显，目录/Cookie 无写入；变量/认证、文件、配置失败与取消错误码正确。
- [x] `go test ./internal/cli -run 'TestDryRun' -count=1`，观察返回真实 HTTP 结果或 dry-run 未实现。
- [x] 新增 `core.ValidateMultipartFiles(parts []FormPart) error`，用 openMultipart 和固定合法 boundary 准备并关闭，不读取内容；普通 run 的 openMultipart 不变。dry-run 共用 decodeInput/configureEngine/LoadCookieFile，检查本地目录及 context，返回安全元数据；禁止调用 DoHTTP/SaveCookieFile/formatResult。

只包含以下 validation 字段：

```go
type validationValue struct {
    DryRun bool
    Method, Protocol string
    BodyMode core.BodyMode
    HeaderCount, FormPartCount, TimeoutSeconds int
    FollowRedirects bool
}
```

JSON 字段使用 lowerCamelCase；error、退出码及不输出 URL/凭证的规则以 spec 为准。

- [x] `go test ./internal/core ./internal/cli -count=1`；root 真实进程补测根帮助、run 帮助、dry-run、不触碰隔离 GUI workspace，保持普通请求、SIGINT、浅深色 render（工作区与精确暂存源码副本均通过）。
- [x] 双审查后，精确提交 `feat(cli): validate requests without network or state writes`（`8882d06`）。

## Task 3: 安全诊断

Files: modify `internal/cli/input.go`、`options.go`、`run.go`、相关 `_test.go`；必要时私有 sentinel 放 `errors.go`。

- [x] 写失败测试：输入大小与读取失败文案不同；schema 缺失/版本、request 缺失/字段、协议错误有固定具体提示；CA 读取/冲突/PEM、代理 URL/冲突/bypass、Cookie 格式有固定提示；所有植入敏感标记均不回显。
- [x] `go test ./internal/cli -run 'TestDiagnostic' -count=1`，观察原 generic 文案导致失败。
- [x] 用 errors.Is 的私有输入上限 sentinel 分类大小错误；保留错误码/退出码，不匹配字符串。按 spec 给出固定文案，绝不输出原错误、字段名或用户值。更新原测试中对 generic message 的断言为对应固定文案/安全语义，保留错误码与隐私断言。
- [x] `go test ./internal/cli -count=1`，验证普通 run、预检与帮助一致（精确暂存源码副本再次通过）。
- [x] 双审查后提交 `fix(cli): explain input and configuration failures safely`（`e949673`）。

## Task 4: 文档与最终验收

Files: modify `README.md`、`.trae/skills/api-test/SKILL.md`、本计划。

- [x] 更新帮助与 dry-run 示例、预检不表示网络/业务成功、目录写权限的限制；补齐当前 Bearer/Basic/API Key JSON 认证说明。skill 仍为 schema v1、项目作用域、合法 frontmatter，不添加第二个客户端。8 个 JSON 块、9 个行内 JSON 及本地链接校验通过，13 组非上传请求/鉴权示例经实际二进制预检通过。
- [x] 运行（精确提交源码副本全部通过；工作区首次运行因并行新增的断言测试尚缺类型而中断）：

```sh
go test -race ./internal/core ./internal/cli -count=1
CGO_ENABLED=0 go test ./... -count=1
go vet ./...
go tool mygo vet .
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go build -o /tmp/curlgo-api-test .
git diff --check
```

- [x] 用实际二进制验证 6 种 help 入口、JSON/curl dry-run（零网络请求、零正文/Cookie 写入）、JSON/curl 本地 404 正常请求，以及安全版本诊断；请求和输出仅用临时私有目录，测试资源已清理。全量 root 验收同时覆盖 SIGINT 与浅深色 render。
- [x] 文档规格与质量双审查、最终整体源码审查及 plan checkbox 审计通过；精确暂存本轮文档、设计与计划，提交 `docs(cli): document preflight and current authentication`。并行新增的断言代码与文档保留在工作区，不纳入本轮提交。

每次提交前检查精确 staged diff；已有用户 HTML 不提交。按已加载的 agent-pr-review 记录审查结果和本地 staged-tree receipt，再提交。最终保留普通分支，不 push/PR/发布。
