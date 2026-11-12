# Changelog

## 未发布

- 新增「问题反馈」入口：应用菜单与设置页可直接打开 GitHub Issues。
- 升级 MyGo 至 0.8.3。
- 修复历史列表中同一请求的多条记录共用一个列表标识的问题，点击或聚焦不再可能落到另一条记录上。
- 修复空闲时按「停止请求」快捷键（默认 Esc）会让当前响应无法再「验证当前响应」「从当前响应提取」的问题。
- 「环境与全局请求头」页不再允许把当前编辑的变量保存到另一个已存在的环境名下，避免覆盖该环境；保存到当前环境或新名称不受影响。
- 请求头与超时输入恢复有效后，它们留下的错误提示会自动消失，不影响其他提示。
- CLI：显式空值 `--out-dir ""` 在普通执行中与 `--dry-run` 一致，在发送请求前以 `OUTPUT_ERROR`（退出码 3）失败；省略该参数仍使用私有临时目录。
- Agent 指南（`curlgo skill cli-reference`）新增 curl 导入支持的参数清单。
- Bruno 集合导入时每个请求文件只解析一次。
- 清理：移除不再使用的品牌探索素材、旧版宣传片合成及其素材、已完成的设计计划文档。

## 0.2.0 - 2026-11-06

- 新增可自定义的键盘快捷键：新请求、收藏、发送、聚焦地址栏、停止与打开设置均可在「设置 → 快捷键」录制新组合、逐行或全部恢复默认，修改即时生效并随工作区保存；支持 Windows/Linux 的 Ctrl 与 macOS 的 ⌘ 提示，菜单「设置…」加速键会跟随配置更新。
- 修复 Windows 上快捷键提示固定显示 ⌘ 的问题；地址栏等输入框获得焦点时，发送/停止等快捷键现在也能触发；发送按钮不再截断 Windows 与自定义组合的标签。
- 修复 Windows 上 curl 导入页「导入请求 / 从剪贴板粘贴」按钮点击无响应的问题（文本区域命中区域与按钮行重叠）。
- Windows 兼容性修复：`go test ./...` 现在可在 Windows 11 通过（测试夹具的 POSIX 权限/路径/TMPDIR/symlink/.exe 等假设按平台处理），`scripts/native_qa_fixture.py` 显式以 UTF-8 写文件，`scripts/test_api_skill.py` 的 POSIX 权限断言在 Windows 自动跳过。
- 提供 Windows x64 / ARM64 安装程序与免安装 ZIP（未签名，附在 GitHub Release）；macOS 继续提供 Developer ID 签名并公证的 DMG。

## CLI 0.1.0 - 2026-11-06

- Add `curlgo skill [topic]` to read the complete API Test guide and references embedded in the executable.
- Replace the separately installed Agent skill with a stable bootstrap that reads instructions from the actual CLI.
- Add a standalone `./cmd/curlgo` build, with its CLI/guide version tracked independently from desktop packaging.

## 0.1.1 - 2026-11-04

- Add an application settings page with the current version, appearance, a manual update check, and preferences for background checks and automatic downloads.
- Add native update notifications with release notes and install/relaunch actions. Updates are verified with a dedicated Ed25519 public key before installation. Background checks run every four hours; automatic downloads are off by default.
- Introduce the original double-loop CurlGo logo and desktop icon.
- Refresh the product walkthrough with Chinese narration and an original soundtrack.

Users of 0.1.0 must install 0.1.1 from the DMG once: 0.1.0 did not include an updater. Future signed releases can be installed through the new updater. The desktop app does not install agent skills or modify coding-agent configuration.

## 0.1.0 - 2026-11-04

First public release of CurlGo, a local native API client.

- Send HTTP/HTTPS requests, receive SSE events, and exchange WebSocket messages.
- Import curl text, Postman Collection v2.1, Bruno collections, and CurlGo collections; preview imports before saving. Collection scripts are not executed.
- Configure environments, Bearer/Basic/API Key authentication, explicit proxies, private CAs, and multipart uploads.
- Check declarative response assertions and extract values for subsequent requests.
- Keep drafts, favorites, history, and Cookies locally. Store explicitly configured secret variables in an encrypted vault with its master key in the macOS Keychain.
- Run individual HTTP requests through the JSON CLI, with help and a local dry-run mode. Install the optional `api-test` agent skill yourself.
- Distribute a Developer ID signed and notarized macOS Apple Silicon DMG.

The first binary release targets macOS Apple Silicon. It does not include team collaboration, collection script execution, a collection runner, or automatic updates. Ordinary workspace values remain plaintext; the encrypted vault only protects values explicitly saved as secrets.
