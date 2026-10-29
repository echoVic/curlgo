# 响应变量提取

每个 HTTP 请求可以保存一组提取规则，将 JSON 字段或响应头写入变量，供下一条请求中的 `{{name}}` 使用。桌面端和 CLI 使用同一个提取器，不执行脚本。

## 桌面端

在请求的「提取」页添加规则，填写目标变量名，选择 JSON 正文或响应头，再填写选择器。

例如登录响应为 `{"data":{"access_token":"…"}}`，将目标变量设为 `token`，来源设为 JSON 正文，选择器填 `/data/access_token`。下一条请求可在「鉴权」页选择 Bearer Token，填写 `{{token}}`。

自动提取需要传输成功、完整读取响应、HTTP 状态为 2xx，且全部启用的[响应断言](assertions.md)通过。变量写入发送时选定的环境，覆盖同名变量并保留其他变量。全部启用的提取规则成功后才一次性写入；任何规则失败都保留原有变量及收到的 HTTP 响应。响应截断、读取失败、超时、取消和非 2xx 均不写入变量。

收到响应后可以修改规则，再点击「从当前响应提取」。此操作使用当前请求最近一次成功的 2xx 响应，重新验证当前断言规则，再写入当前选定的环境；切换请求会清空可提取的响应。完整空正文的 204 可提取响应头。

规则随草稿、收藏、历史及复制的请求保存，应用重启后恢复。CurlGo 原生集合保留规则；Postman 导出会提示规则无法保留，不生成替代脚本。响应正文仅保存在运行中的应用内存，不写入历史或集合。提取后的环境值按现有环境配置以本地明文保存，界面反馈只显示环境和变量数量。

## 规则格式

规则位于 `request.extractors`：

```json
[
  {
    "variable": "token",
    "source": "json",
    "selector": "/data/access_token"
  },
  {
    "variable": "request_id",
    "source": "header",
    "selector": "X-Request-Id"
  }
]
```

`id` 为可选的编辑标识，`disabled: true` 保留规则但不执行。每个请求最多 100 条规则；启用规则的变量名不可重复，格式为 `[A-Za-z_][A-Za-z0-9_.-]*`，最多 256 字节。选择器最多 4096 字节。禁用规则允许保留未完成的编辑内容，但仍检查编码、大小和 ID。

JSON 选择器使用 RFC 6901 JSON Pointer，不使用 JSONPath：

| 选择器 | 含义 |
|---|---|
| `/data/access_token` | `data` 对象中的 `access_token` |
| `/items/0/id` | `items` 数组首项中的 `id` |
| `/a~1b/~0` | 对象键 `a/b` 中的键 `~` |
| `/` | 根对象的空字符串键 |
| 空字符串 | 根值 |

仅提取字符串、数字和布尔值。数字保留原始精度和表示，布尔值转为 `true` / `false`，空字符串有效。不存在的字段、`null`、对象或数组会失败；数组索引必须为规范非负整数，不接受 `01`、`-` 或负数。JSON 必须为完整的单个 UTF-8 文档，提取不依赖 Content-Type。

响应头名称不区分大小写，但必须恰好有一个值；不存在或有多个值时失败。不会拆解 `Set-Cookie` 或任选重复头的第一个值。单次提取的变量名和值合计最多 1 MiB。

## CLI

`curlgo run --request login.json` 支持相同规则，输入与输出继续使用 schema v1：

```json
{
  "schemaVersion": 1,
  "request": {
    "method": "POST",
    "url": "http://127.0.0.1:8080/login",
    "extractors": [
      {"variable": "token", "source": "json", "selector": "/data/access_token"}
    ]
  }
}
```

成功后，结果包含：

```json
{
  "schemaVersion": 1,
  "ok": true,
  "result": {
    "statusCode": 200,
    "extractedVariables": {"token": "example-token"}
  }
}
```

以上只展示结果的相关字段；实际输出仍包含 URL、响应头、正文及 timing。即使正文按内联阈值写入文件，提取也从捕获的原始字节执行。显式配置的提取值原样返回，可能包含凭证，应与原始响应一样保存在私有文件中，不回显到报告。

下一条调用需要将结果中的变量显式合入输入的 `settings.environments`，再用 `settings.environment` 选择该环境。CLI 不加载桌面工作区、不修改输入文件或隐式环境文件，每次只执行一个请求。

断言失败时跳过提取，CLI 返回 `ASSERTION_FAILED` / 退出 4，并保留响应及断言报告。断言通过后的 2xx 响应提取失败时，CLI 返回 `ok: false`、`error.code: EXTRACTION_ERROR`，退出码 2；已捕获的响应仍在 `result` 中，不返回部分 `extractedVariables`。错误消息不包含提取值或响应内容。输出或 Cookie 保存失败仍优先返回 `OUTPUT_ERROR` 和退出码 3。

无启用提取规则或非 2xx 时跳过提取；若也没有启用断言，保持原有退出码策略：例如完整捕获的 HTTP 401 仍可为 `ok: true`、退出码 0。必须继续检查 HTTP 状态和业务结果。
