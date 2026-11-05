# 可选的 API 测试技能

CurlGo 桌面端不会安装或更新 coding agent 技能、修改 agent 配置、注册 SessionStart hook、自动批准信任或配置远程 MCP。这里的集成由你单独安装。

## 入口与运行时指南

采用稳定入口与运行时指南分离的方式：Agent 安装的 `SKILL.md` 先通过 `curlgo help` 的 `help.usage` 确认支持，再要求执行 `curlgo skill`，完整使用指南和参考资料嵌入 CLI 可执行文件。安装目录不再保存操作文档副本。

| 组件 | 版本来源 | 升级方式 |
| --- | --- | --- |
| 桌面客户端 | `mygo.json` 的 `version` | 桌面更新器或手动安装客户端 |
| 独立 CLI + 完整指南 | `internal/cli/VERSION` | 替换独立 CLI，指南随二进制一起升级 |
| Agent 入口 skill | `skills/api-test/SKILL.md` 的 `metadata.version` | 原技能安装工具；只在入口或安装方式变化时更新 |

CLI 使用 `./cmd/curlgo` 单独构建，复用同一套 HTTP 核心，不包含桌面界面。桌面程序也支持同样的 CLI 子命令，但桌面更新器不负责更新你另外安装的独立 CLI。入口 skill 不是二进制安装器，升级它不会升级 CLI。

```sh
curlgo skill
curlgo skill workflows
curlgo skill cli-reference
curlgo skill assertions
curlgo skill response-extraction
curlgo skill project-secrets
```

这些命令输出 Markdown，不发送请求、不读取 stdin 或工作区、不写文件。输出必须以 `[curlgo:skill] end of guide` 结束；缺少标记时应保存到私有文件并分段读取，不使用截断的指南执行请求。`help`、预检和请求仍使用 JSON 协议 v1。

升级或切换可执行文件后重新读取主指南。多份 CLI 共存时，使用明确的可执行文件路径读取指南并执行后续请求，避免读取新版指南却调用旧版 CLI。若帮助中没有 `curlgo skill [topic]`，不要尝试该子命令（旧桌面程序可能打开 GUI）；按照[安装与旧版本说明](../skills/api-test/references/install.md)更新 CLI，而不是继续使用缓存文档。

## 安装与范围

在想使用技能的项目目录运行：

```sh
npx skills add echoVic/curlgo --skill api-test -a claude-code -a codex
```

这是第三方 skills CLI。执行前可以先查看 `npx skills --help`；在安装交互中确认目标 agent、项目或全局范围、安装路径。具体文件或链接位置由 CLI 版本和你的选择决定，CurlGo 不覆盖该安装器的行为。不要在需要全局安装时省略对范围的确认。

该入口需要支持 `skill` 命令的 CurlGo 可执行文件。也可以从可信的 CurlGo 源码根目录构建独立 CLI：

```sh
CGO_ENABLED=0 go build -o /tmp/curlgo-api-test ./cmd/curlgo
/tmp/curlgo-api-test skill
```

随后所有命令使用同一条 `/tmp/curlgo-api-test` 路径。构建得到的是这份源码对应的版本，不保证是最新版；缺少二进制和源码时应询问位置。

## 查看与移除

使用同一 skills CLI 的 `list` 和 `remove` 命令查看、移除技能；具体参数以当前版本的 `npx skills --help` 为准，并选择与安装时一致的范围。移除前确认条目为 CurlGo 的 `api-test`，避免影响同名技能。

如果选择了手动复制方式，只删除自己添加的技能目录或链接。卸载 CurlGo 桌面应用不会删除独立安装的技能。

## 数据边界

CLI 按显式输入执行请求，不加载桌面工作区、Cookie 或钥匙串。技能要求仅使用获授权的接口、操作、凭据与上传文件，并将敏感输入和输出保存在私有文件中。

Agent 读取到的请求、响应仍受该 agent 与模型服务的数据处理方式约束。安装技能不构成对任意接口请求、文件上传或凭据使用的授权。
