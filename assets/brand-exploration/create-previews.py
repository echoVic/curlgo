"""Build editable identity concepts; production brand assets are separate."""

from pathlib import Path
import subprocess
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parent

CONCEPTS = [
    {
        "id": "A", "name": "双向回路", "subtitle": "Request. Response. Yours.",
        "color": "#087F78", "accent": "#42E3B4", "secondary": "#F1FFF9",
        "tile": "#123B40", "tile_end": "#082D35",
        "paths": [
            "M222 48 270 96 222 144V120H140Q94 120 94 160Q94 200 140 200H170L218 248H140Q46 248 46 160Q46 72 140 72H222Z",
            "M292 200H252V168L204 216 252 264V232H292Z",
        ],
        "description": "把发出的请求与返回的响应，收进一个 C。",
        "tone": "克制 · 清晰 · 由你掌控",
    },
    {
        "id": "B", "name": "卷曲 C", "subtitle": "A small tool. A clear signal.",
        "color": "#5145DF", "accent": "#5145DF", "secondary": "#8C7CF4",
        "tile": "#FAFAFF", "tile_end": "#E7E9F7",
        "paths": [
            "M270 78 230 122C213 104 191 94 166 94C127 94 96 122 96 160C96 198 127 226 166 226C191 226 213 216 230 198L270 242C243 272 207 289 165 289C90 289 30 234 30 160C30 86 90 31 165 31C207 31 244 48 270 78Z",
            "M230 122 270 78 270 156Z",
        ],
        "description": "一笔卷起的 C，呼应 curl 与轻巧的工具感。",
        "tone": "轻量 · 原生 · 易于辨认",
    },
    {
        "id": "C", "name": "快速交换", "subtitle": "Send it. See it. Move on.",
        "color": "#E66B44", "accent": "#FF976F", "secondary": "#FFF1D9",
        "tile": "#302C2B", "tile_end": "#1D1D20",
        "paths": [
            "M40 64H212V32L280 100 212 168V136H40Q28 136 28 124V76Q28 64 40 64Z",
            "M280 184H108V152L40 220 108 288V256H280Q292 256 292 244V196Q292 184 280 184Z",
        ],
        "description": "两个相向的箭头，让发送与查看一眼可见。",
        "tone": "直接 · 利落 · 行动感",
    },
]


def mark(c, colors=None):
    colors = colors or [c["color"], c["color"]]
    return "".join(f'<path d="{p}" fill="{colors[i % len(colors)]}"/>'
                   for i, p in enumerate(c["paths"]))


def svg(body, width, height, viewbox=None):
    box = viewbox or f"0 0 {width} {height}"
    return f'<svg xmlns="http://www.w3.org/2000/svg" width="{width}" height="{height}" viewBox="{box}">{body}</svg>\n'


def tile(c, suffix=""):
    key = f'tile{c["id"]}{suffix}'
    return f'''<defs>
      <linearGradient id="{key}" x1="0" y1="0" x2="0.8" y2="1">
        <stop stop-color="{c['tile']}"/><stop offset="1" stop-color="{c['tile_end']}"/>
      </linearGradient>
    </defs>
    <rect x="80" y="80" width="864" height="864" rx="200" fill="url(#{key})"/>
    <rect x="81" y="81" width="862" height="862" rx="199" fill="none" stroke="#FFFFFF" stroke-opacity=".13" stroke-width="2"/>
    <g transform="translate(188 192) scale(2)">{mark(c, [c['accent'], c['secondary']])}</g>'''


def text(x, y, value, size=20, color="#182C31", weight=400, extra=""):
    return f'<text x="{x}" y="{y}" font-family="Helvetica Neue, Arial, PingFang SC, sans-serif" font-size="{size}" fill="{color}" font-weight="{weight}" {extra}>{value}</text>'


def save(path, content, png=True):
    path.write_text(content)
    ET.fromstring(content)
    if png:
        subprocess.run(["rsvg-convert", str(path), "-o", str(path.with_suffix(".png"))], check=True)


for c in CONCEPTS:
    directory = ROOT / c["id"]
    directory.mkdir(exist_ok=True)
    save(directory / "mark.svg", svg(mark(c), 320, 320))
    save(directory / "mark-mono.svg", svg(mark(c, ["#182C31"]), 320, 320))
    save(directory / "icon.svg", svg(tile(c), 1024, 1024))
    save(directory / "wordmark.svg", svg(
        f'<g transform="translate(0 2) scale(.27)">{mark(c)}</g>'
        + text(106, 67, "CurlGo", 64, weight=600, extra='letter-spacing="-2"'), 345, 92))
    for size in [16, 32, 64, 128, 256, 512]:
        subprocess.run(["rsvg-convert", "-w", str(size), "-h", str(size),
                        str(directory / "icon.svg"), "-o", str(directory / f"icon-{size}.png")], check=True)

body = '<rect width="1800" height="1240" fill="#F4F5F2"/>'
body += text(72, 88, "CurlGo", 46, weight=600, extra='letter-spacing="-1.5"')
body += text(252, 86, "IDENTITY EXPLORATIONS / 01", 16, color="#667578", extra='letter-spacing="2"')
body += text(72, 139, "你的 API 工具，由你做主。", 24, color="#627175")
body += text(1728, 88, "2026", 16, color="#667578", extra='text-anchor="end"')

for i, c in enumerate(CONCEPTS):
    x = 72 + i * 564
    body += f'<rect x="{x}" y="192" width="528" height="940" rx="22" fill="#FFFFFF"/>'
    body += text(x + 32, 248, c["id"], 23, color=c["color"], weight=600)
    body += text(x + 78, 248, c["name"], 24, weight=500)
    if i == 0:
        body += f'<rect x="{x+408}" y="224" width="86" height="30" rx="15" fill="#E8F6F0"/>'
        body += text(x + 451, 244, "推荐", 14, color="#087F78", extra='text-anchor="middle"')
    body += f'<g transform="translate({x+53} 300) scale(.31)">{mark(c)}</g>'
    body += text(x + 171, 374, "CurlGo", 64, weight=600, extra='letter-spacing="-2"')
    body += text(x + 32, 422, c["subtitle"], 17, color="#788588")
    body += f'<rect x="{x+32}" y="462" width="464" height="344" rx="14" fill="#F3F5F5"/>'
    body += f'<g transform="translate({x+100} 470) scale(.32)">{tile(c, "large")}</g>'
    body += text(x + 32, 850, "MONO", 12, color="#7A888A", extra='letter-spacing="1.5"')
    body += f'<g transform="translate({x+30} 868) scale(.25)">{mark(c, ["#182C31"])}</g>'
    body += text(x + 148, 850, "DOCK / SMALL SIZES", 12, color="#7A888A", extra='letter-spacing="1.5"')
    for j, size in enumerate([16, 32, 64]):
        tx = x + [150, 228, 330][j]
        ty = 905 - size / 2
        body += f'<g transform="translate({tx} {ty}) scale({size/1024})">{tile(c, str(size))}</g>'
        body += text(tx + size/2, 963, str(size), 12, color="#899598", extra='text-anchor="middle"')
    body += f'<path d="M{x+32} 991H{x+496}" stroke="#EDF0ED"/>'
    body += text(x + 32, 1030, c["description"], 18, color="#56676C")
    body += text(x + 32, 1080, c["tone"], 17, color=c["color"])

body += text(72, 1194, "LOGO  /  DESKTOP ICON  /  MONOCHROME  /  16–64 PX", 13, color="#778588", extra='letter-spacing="1.8"')
body += text(1728, 1194, "Editable vector concepts", 14, color="#778588", extra='text-anchor="end"')
save(ROOT / "comparison.svg", svg(body, 1800, 1240))
print(ROOT / "comparison.png")
