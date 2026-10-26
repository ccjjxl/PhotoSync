#!/usr/bin/env python3
"""Regenerate every launcher icon from assets/app-icon.svg.

    python3 assets/gen_icons.py

Requirements (development machines only; the generated PNGs are committed):
  * librsvg  (rsvg-convert)
  * Pillow   (pip install pillow)

Outputs:
  * assets/app-icon-1024.png                         full-bleed master (iOS)
  * android/app/src/main/res/mipmap-*/ic_launcher.png rounded legacy icons
  * ios/PhotoSync/Assets.xcassets/AppIcon.appiconset/*.png
"""

import subprocess
import sys
from pathlib import Path

try:
    from PIL import Image, ImageDraw
except ImportError:  # reported nicely in main()
    Image = ImageDraw = None

ROOT = Path(__file__).resolve().parent.parent
SVG = ROOT / "assets" / "app-icon.svg"
MASTER = ROOT / "assets" / "app-icon-1024.png"
ANDROID_RES = ROOT / "android/app/src/main/res"

DENSITIES = {
    "mdpi": 48,
    "hdpi": 72,
    "xhdpi": 96,
    "xxhdpi": 144,
    "xxxhdpi": 192,
}

# Matches the rounded-square shape used by iOS / the provided artwork.
CORNER_RATIO = 0.22
SUPERSAMPLE = 4


def render_master():
    subprocess.run(
        ["rsvg-convert", "-w", "1024", "-h", "1024", str(SVG), "-o", str(MASTER)],
        check=True,
    )
    image = Image.open(MASTER).convert("RGB")
    image.save(MASTER, optimize=True)
    return image.convert("RGBA")


def render_android(master) -> None:
    for density, size in DENSITIES.items():
        big = size * SUPERSAMPLE
        image = master.resize((big, big), Image.LANCZOS)

        mask = Image.new("L", (big, big), 0)
        ImageDraw.Draw(mask).rounded_rectangle(
            (0, 0, big - 1, big - 1),
            radius=round(big * CORNER_RATIO),
            fill=255,
        )
        image.putalpha(mask)

        out = ANDROID_RES / f"mipmap-{density}" / "ic_launcher.png"
        image.resize((size, size), Image.LANCZOS).save(out, optimize=True)
        print(f"  ✓ {out.relative_to(ROOT)} ({size}×{size})")


def main() -> int:
    if Image is None:
        print("✗ 需要 Pillow: pip install pillow", file=sys.stderr)
        return 1

    print(f"→ 渲染 {SVG.relative_to(ROOT)}", flush=True)
    master = render_master()

    print("→ 生成 Android mipmap")
    render_android(master)

    print("→ 生成 iOS AppIcon", flush=True)
    subprocess.run([sys.executable, "gen_appicon.py"], cwd=ROOT / "ios", check=True)

    print("✓ 全部图标已更新")
    return 0


if __name__ == "__main__":
    sys.exit(main())
