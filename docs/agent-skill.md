# 可选的 API 测试技能

CurlGo 桌面端不会安装或更新 coding agent 技能、修改 agent 配置、注册 SessionStart hook、自动批准信任或配置远程 MCP。这里的集成由你单独安装。

## 安装与范围

在想使用技能的项目目录运行：

```sh
npx skills add echoVic/curlgo --skill api-test -a claude-code -a codex
```

这是第三方 skills CLI。执行前可以先查看 `npx skills --help`；在安装交互中确认目标 agent、项目或全局范围、安装路径。具体文件或链接位置由 CLI 版本和你的选择决定，CurlGo 不覆盖该安装器的行为。不要在需要全局安装时省略对范围的确认。

该技能需要 CurlGo 可执行文件。若未找到可执行文件，技能允许 agent 从可信的 CurlGo 源码构建 `/tmp/curlgo-api-test`；缺少二进制和源码时应询问位置。技能不包含桌面端安装器。

## 查看与移除

使用同一 skills CLI 的 `list` 和 `remove` 命令查看、移除技能；具体参数以当前版本的 `npx skills --help` 为准，并选择与安装时一致的范围。移除前确认条目为 CurlGo 的 `api-test`，避免影响同名技能。

如果选择了手动复制方式，只删除自己添加的技能目录或链接。卸载 CurlGo 桌面应用不会删除独立安装的技能。

## 数据边界

CLI 按显式输入执行请求，不加载桌面工作区、Cookie 或钥匙串。技能要求仅使用获授权的接口、操作、凭据与上传文件，并将敏感输入和输出保存在私有文件中。

Agent 读取到的请求、响应仍受该 agent 与模型服务的数据处理方式约束。安装技能不构成对任意接口请求、文件上传或凭据使用的授权。
