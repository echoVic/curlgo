"""Export selected SVG identity assets. Requires rsvg-convert and Pillow.

Desktop icon source: assets/icon.svg. This script never reads concept assets.
"""

from pathlib import Path
import shutil
import subprocess

from PIL import Image

ROOT = Path(__file__).resolve().parents[1]
BRAND = ROOT / "assets" / "brand"


def render(source, destination, size=None):
    command = ["rsvg-convert", str(source), "-o", str(destination)]
    if size is not None:
        command += ["-w", str(size), "-h", str(size)]
    subprocess.run(command, check=True)


render(ROOT / "assets/icon.svg", ROOT / "assets/icon.png")
for name in ["logo", "logo-mono", "logo-dark", "logo-white", "wordmark"]:
    render(BRAND / f"{name}.svg", BRAND / f"{name}.png")

for size in [16, 32, 64, 128, 256, 512, 1024]:
    render(ROOT / "assets/icon.svg", BRAND / f"icon-{size}.png", size)

with Image.open(ROOT / "assets/icon.png") as icon:
    icon.save(BRAND / "icon.ico", format="ICO", sizes=[(s, s) for s in [16, 32, 48, 64, 128, 256]])
    icon.save(BRAND / "icon.icns", format="ICNS")

shutil.copy2(ROOT / "assets/icon.png", ROOT / "marketing/video/public/icon.png")
print("Exported selected CurlGo identity and synchronized Remotion icon.")
