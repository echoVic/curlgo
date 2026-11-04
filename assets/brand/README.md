# CurlGo 品牌资源

正式采用 A「双向回路」：C 形的请求路径向右发出，浅色箭头向左返回响应。

- `../icon.svg` 是桌面图标的源文件，`../icon.png` 是 MyGo 打包配置的输入。
- `logo.svg` 是浅色背景标识；`logo-dark.svg` 用于深色背景；`logo-white.svg` 与 `logo-mono.svg` 为单色版本。
- `wordmark.svg` 为横向组合，文字已转换为路径，不依赖系统字体。
- `header-icon.svg` 去掉桌面外边距，用于应用顶部的 32px 原生 SVG 标识。
- `icon.icns`、`icon.ico` 和 `icon-16.png` 至 `icon-1024.png` 为分发格式。

| 颜色 | 色值 | 用途 |
| --- | --- | --- |
| 深青 | `#123B40` → `#082D35` | 桌面图标底色 |
| 薄荷绿 | `#42E3B4` | 请求路径、深色背景标识 |
| 浅色 | `#F1FFF9` | 响应箭头 |
| 青绿 | `#087F78` | 浅色背景标识 |
| 墨色 | `#182C31` | 字标、单色标识 |

桌面图标使用 1024px 透明画布，居中 864px 圆角底板。独立 logo 周围至少留出一条主路径宽度的空间；避免改变两个箭头的相对位置和比例。

重新导出（Python 3、Pillow、rsvg-convert）：

```sh
python3 scripts/export_brand.py
```

图标源文件变更时，同步更新 `header-icon.svg` 的内部几何形状；它的 viewBox 为 `80 80 864 864`。字体转路径的正式字标已保存；无需再次读取探索方案或安装字体。
