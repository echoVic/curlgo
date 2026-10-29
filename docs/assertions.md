# 声明式响应断言

HTTP 请求可以保存状态码、响应头、JSON 字段和正文断言。桌面端与 CLI 使用同一套规则，返回每条启用规则的结果，不执行脚本。

## 桌面端

在请求的「断言」页添加规则，选择来源和操作，填写选择器及期望值。状态码默认要求 `200`；JSON 字段的期望值需要写成 JSON 字面量，如 `"ok"`、`200`、`true`、`null`。

传输成功并完整读取响应后，客户端自动验证发送时的规则。「断言结果」页显示通过数量及每条规则的结果。失败不会丢弃状态码、响应头或正文。截断响应的所有启用断言均失败；读取失败、超时或取消不产生断言报告。

收到完整响应后，可以编辑规则，再点「验证当前响应」。手动验证使用当前请求最近一次完整响应，不写入环境。编辑规则会清空旧报告；切换请求会清空响应和报告。断言适用于任意 HTTP 状态，因此可以要求错误场景返回 `401` 或 `500`。

响应变量提取要求 HTTP 2xx、完整响应、全部启用断言通过，以及全部提取规则成功。自动提取写入发送时的环境；「从当前响应提取」会重新检查当前断言规则，再写入当前环境。任一断言失败均不覆盖原有环境变量。Cookie 仍按正常接收及保存流程更新。

规则随草稿、收藏、历史及复制的请求保存，重启后恢复；报告和响应正文只保存在内存中。CurlGo 原生集合保留规则，Postman 导出会提示断言无法保留，不生成替代脚本。

## 规则格式

规则位于 `request.assertions`：

```json
[
  {
    "id": "status",
    "name": "返回成功状态",
    "source": "status",
    "operator": "equals",
    "expected": "200"
  },
  {
    "name": "业务成功",
    "source": "json",
    "operator": "equals",
    "selector": "/ok",
    "expected": "true"
  },
  {
    "source": "header",
    "operator": "contains",
    "selector": "Content-Type",
    "expected": "application/json"
  },
  {
    "source": "body",
    "operator": "contains",
    "expected": "ready"
  }
]
```

`id` 和 `name` 是可选的标识及名称；`disabled: true` 保留规则但不执行。每个请求最多 100 条规则；ID 和名称最多 256 字节，选择器最多 4096 字节，期望值最多 65536 字节，所有规则字段内容合计最多 1 MiB。禁用规则可以保留未完成的编辑内容，但仍检查大小、UTF-8 编码、名称控制字符及唯一 ID。启用规则只适用于 HTTP。

| 来源 | 操作 | 选择器 | 期望值 |
|---|---|---|---|
| `status` | `equals` | 省略或空字符串 | 100–599 的规范十进制状态码，如 `"200"` |
| `header` | `equals` / `contains` / `exists` | 合法响应头名称，不区分大小写 | 相等或包含时为普通文本 |
| `json` | `equals` / `contains` / `exists` | RFC 6901 JSON Pointer | 相等或包含时为包含 JSON 字面量的字符串 |
| `body` | `equals` / `contains` | 省略或空字符串 | 普通 UTF-8 文本，空字符串有效 |

选择器和期望值按字面使用，不展开 `{{变量}}`，不支持正则或 JSONPath。`exists` 忽略期望值，便于切换操作时保留编辑内容。

JSON Pointer 如 `/items/0/id` 选择数组首项的 `id`；`~1` 表示 `/`，`~0` 表示 `~`。空字符串选择根值，`/` 选择根对象的空字符串键。数组索引须为规范非负整数，不接受 `01`、`-` 或负数。

JSON 相等比较支持字符串、数字、布尔值和 `null`，保留类型，不将数字与字符串互相转换。数字精确比较，`1`、`1.0`、`1e0` 相等，大于 2⁵³ 的整数不发生浮点舍入。对象和数组只支持存在判断；包含操作要求实际值和期望值都是 JSON 字符串。存在判断接受已存在的 `null`、空值、对象或数组，但缺失字段不通过。响应必须是完整的单个 UTF-8 JSON 文档，与 Content-Type 无关。

| 想比较的 JSON 值 | 界面中填写 | 请求配置中的 `expected` |
|---|---|---|
| 字符串 `ok` | `"ok"` | `"\"ok\""` |
| 数字 200 | `200` | `"200"` |
| 布尔值 true | `true` | `"true"` |
| 空值 null | `null` | `"null"` |

响应头相等和包含比较要求恰好一个值；多个值会失败，不任取第一项。存在判断只要求至少一个值，空字符串和重复值有效。正文按原始捕获字节对应的 UTF-8 文本比较，不比较界面格式化后的缩进；无效 UTF-8 不通过。

## CLI

将规则加入现有 schema v1 请求输入：

```json
{
  "schemaVersion": 1,
  "request": {
    "url": "http://127.0.0.1:8080/health",
    "assertions": [
      {"name": "状态", "source": "status", "operator": "equals", "expected": "200"},
      {"name": "业务", "source": "json", "operator": "equals", "selector": "/ok", "expected": "true"}
    ]
  }
}
```

运行 `curlgo run --request health.json`。有启用规则且传输成功时，`result.assertions` 返回报告；无规则或全部禁用时省略该字段。即使正文写入文件，仍从捕获的原始字节验证。

以下只展示失败结果的相关字段，实际结果还包含 URL、响应头、正文和 timing：

```json
{
  "schemaVersion": 1,
  "ok": false,
  "error": {
    "code": "ASSERTION_FAILED",
    "message": "Response assertions failed."
  },
  "result": {
    "statusCode": 200,
    "assertions": {
      "passed": false,
      "results": [
        {"index": 0, "name": "状态", "passed": true, "code": "PASSED", "message": "断言通过"},
        {"index": 1, "name": "业务", "passed": false, "code": "MISMATCH", "message": "值与期望不符"}
      ]
    }
  }
}
```

断言失败退出 **4**，错误码为 `ASSERTION_FAILED`，保留全部启用规则结果及捕获的响应，省略 `extractedVariables`。通过后才按原有 2xx 规则执行提取；提取失败仍为 `EXTRACTION_ERROR` / 退出 2。输出或 Cookie 保存失败优先为 `OUTPUT_ERROR` / 退出 3。

无启用断言时保持原有 HTTP 状态策略：完整捕获的 401 或 500 仍可退出 0。配置断言也只验证所列规则；状态及其他业务字段仍需按任务要求检查。

`--dry-run` 校验规则配置，不发送请求、不验证响应、不返回断言报告，成功仅表示本地准备通过。CLI 不修改输入文件或桌面环境。下一条请求的环境变量需由调用方显式合入输入。

## 结果代码

每条结果的 `index` 是规则在配置中的零基位置，禁用项不产生结果但保留其位置。机器判定使用 `passed` 和稳定 `code`，不匹配说明文字。

| 代码 | 含义 |
|---|---|
| `PASSED` | 规则通过 |
| `MISMATCH` | 值与期望不符 |
| `MISSING_VALUE` | 字段或响应头不存在 |
| `INVALID_JSON` | 响应不是完整有效的 UTF-8 JSON |
| `NON_SCALAR` | 相等或包含比较选中了对象或数组 |
| `TYPE_MISMATCH` | JSON 值类型不符，或包含比较选中了非字符串标量 |
| `AMBIGUOUS_HEADER` | 相等或包含比较遇到多个响应头值 |
| `TRUNCATED_RESPONSE` | 响应截断，无法验证 |
| `INVALID_BODY` | 正文不是有效的 UTF-8 文本 |

配置无效在发送前拒绝，不将其包装成响应断言失败。报告只包含规则标识、名称和固定说明，不回显实际值、期望值或选择器。名称、原始响应和配置中的期望值仍可能含秘密；配置使用现有本地明文存储规则。
