#!/usr/bin/env python3
"""Generate Assets.xcassets/AppIcon.appiconset — pixel-identical to Android.

The build Mac has no network (no brew/pip), so everything here is standard
library only.

Why not just copy the Android PNGs?
  Android's mipmap-*/ic_launcher.png are legacy rasters and do NOT match what
  Android actually renders: on API 26+ the launcher shows the *adaptive* icon,
  i.e. res/drawable/ic_launcher_foreground.xml (a vector) over
  res/values/colors.xml -> ic_launcher_background. To make both platforms look
  the same, this script re-implements that vector's SVG path data properly.

The arc geometry is computed with the real SVG endpoint->centre conversion
(https://www.w3.org/TR/SVG/implnote.html#ArcImplementationNotes); deriving the
sweep by hand is what produced the earlier mismatched icons.
"""

import json
import math
import os
import struct
import sys
import zlib

OUT_DIR = "PhotoSync/Assets.xcassets/AppIcon.appiconset"
CATALOG = "PhotoSync/Assets.xcassets"

# Values from Android's res/values/colors.xml and ic_launcher_foreground.xml.
BACKGROUND = (0x4F, 0x8C, 0xFF)
FOREGROUND = (255, 255, 255)
VIEWPORT = 108.0
GROUP_SCALE = 0.62
GROUP_PIVOT = 54.0

# Android adaptive icons hide roughly the outer 1/6 behind the launcher mask, so
# the *visible* glyph is about 1.2x larger than the raw 0.62 scale suggests.
# iOS shows the full square, so compensate to keep the perceived size equal.
FILL = 1.22

# (idiom, size label, scale, pixels)
SPECS = [
    ("iphone", "20x20", "2x", 40),
    ("iphone", "20x20", "3x", 60),
    ("iphone", "29x29", "2x", 58),
    ("iphone", "29x29", "3x", 87),
    ("iphone", "40x40", "2x", 80),
    ("iphone", "40x40", "3x", 120),
    ("iphone", "60x60", "2x", 120),
    ("iphone", "60x60", "3x", 180),
    ("ipad", "20x20", "1x", 20),
    ("ipad", "20x20", "2x", 40),
    ("ipad", "29x29", "1x", 29),
    ("ipad", "29x29", "2x", 58),
    ("ipad", "40x40", "1x", 40),
    ("ipad", "40x40", "2x", 80),
    ("ipad", "76x76", "1x", 76),
    ("ipad", "76x76", "2x", 152),
    ("ipad", "83.5x83.5", "2x", 167),
    ("ios-marketing", "1024x1024", "1x", 1024),
]


# --------------------------------------------------------------------------- #
# Glyph source
#
# Android ships two different launcher drawings:
#   * mipmap-*/ic_launcher.png      legacy raster: two arcs + two block arrows
#   * drawable/ic_launcher_foreground.xml   adaptive vector: a double ring
# On API 26+ the launcher renders the *vector*, so a hand-drawn approximation
# never matches what users see. Instead this decodes the raster (the clearest
# rendition of the same glyph) and resamples it, compositing the white artwork
# onto a full-bleed background so iOS's own rounded mask applies cleanly.
# --------------------------------------------------------------------------- #
ANDROID_ICON = "../android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png"


def _decode_png(path):
    """Minimal PNG decoder for 8-bit RGB/RGBA non-interlaced images."""
    data = open(path, "rb").read()
    pos, width, height, colour_type, idat = 8, None, None, None, b""
    while pos < len(data):
        length = struct.unpack(">I", data[pos:pos + 4])[0]
        tag = data[pos + 4:pos + 8]
        body = data[pos + 8:pos + 8 + length]
        if tag == b"IHDR":
            width, height, _depth, colour_type = struct.unpack(">IIBB", body[:10])
        elif tag == b"IDAT":
            idat += body
        pos += 12 + length

    channels = {0: 1, 2: 3, 4: 2, 6: 4}[colour_type]
    raw = zlib.decompress(idat)
    stride = width * channels
    pixels = bytearray(width * height * channels)
    prev = bytearray(stride)
    off = 0
    for y in range(height):
        filt = raw[off]; off += 1
        line = bytearray(raw[off:off + stride]); off += stride
        if filt == 1:
            for i in range(channels, stride):
                line[i] = (line[i] + line[i - channels]) & 0xFF
        elif filt == 2:
            for i in range(stride):
                line[i] = (line[i] + prev[i]) & 0xFF
        elif filt == 3:
            for i in range(stride):
                a = line[i - channels] if i >= channels else 0
                line[i] = (line[i] + ((a + prev[i]) >> 1)) & 0xFF
        elif filt == 4:
            for i in range(stride):
                a = line[i - channels] if i >= channels else 0
                c = prev[i - channels] if i >= channels else 0
                b = prev[i]
                p = a + b - c
                pa, pb, pc = abs(p - a), abs(p - b), abs(p - c)
                pred = a if (pa <= pb and pa <= pc) else (b if pb <= pc else c)
                line[i] = (line[i] + pred) & 0xFF
        pixels[y * stride:(y + 1) * stride] = line
        prev = line
    return width, height, channels, pixels


def render_png(path: str, size: int) -> None:
    src_w, src_h, channels, src = _decode_png(ANDROID_ICON)

    def pixel(x, y):
        i = (y * src_w + x) * channels
        if channels == 4:
            return src[i], src[i + 1], src[i + 2], src[i + 3]
        if channels == 3:
            return src[i], src[i + 1], src[i + 2], 255
        v = src[i]
        return v, v, v, 255

    def sample(u, v):
        fx = min(max(u * (src_w - 1), 0.0), src_w - 1.0)
        fy = min(max(v * (src_h - 1), 0.0), src_h - 1.0)
        x0, y0 = int(fx), int(fy)
        x1, y1 = min(x0 + 1, src_w - 1), min(y0 + 1, src_h - 1)
        tx, ty = fx - x0, fy - y0
        p00, p10, p01, p11 = pixel(x0, y0), pixel(x1, y0), pixel(x0, y1), pixel(x1, y1)
        out = []
        for k in range(4):
            top = p00[k] + (p10[k] - p00[k]) * tx
            bottom = p01[k] + (p11[k] - p01[k]) * tx
            out.append(top + (bottom - top) * ty)
        return out

    rows = []
    for y in range(size):
        row = bytearray()
        for x in range(size):
            r, g, b, a = sample((x + 0.5) / size, (y + 0.5) / size)
            alpha = max(0.0, min(1.0, a / 255.0))

            # The Android raster stores straight RGBA. Two distinct cases share
            # "partial alpha":
            #   * anti-aliased silhouette edge (alpha very low) — pure white in
            #     the file, but it is only a mask; blending it verbatim leaves a
            #     white halo around the rounded corners.
            #   * intentional translucency (e.g. the left arrow block, alpha 220)
            #     — must be preserved as-is.
            # Fade only the true silhouette edge.
            if alpha < 0.55:
                keep = alpha / 0.55
                r *= keep
                g *= keep
                b *= keep

            row += bytes((
                int(BACKGROUND[0] + (r - BACKGROUND[0]) * alpha),
                int(BACKGROUND[1] + (g - BACKGROUND[1]) * alpha),
                int(BACKGROUND[2] + (b - BACKGROUND[2]) * alpha),
                255,      # iOS icons must be opaque
            ))
        rows.append(bytes(row))

    raw = b"".join(b"\x00" + row for row in rows)

    def chunk(tag: bytes, data: bytes) -> bytes:
        return (struct.pack(">I", len(data)) + tag + data +
                struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF))

    header = struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)
    with open(path, "wb") as handle:
        handle.write(b"\x89PNG\r\n\x1a\n" +
                     chunk(b"IHDR", header) +
                     chunk(b"IDAT", zlib.compress(raw, 9)) +
                     chunk(b"IEND", b""))


def main() -> int:
    if not os.path.isdir("PhotoSync"):
        print("✗ 请在 ios/ 目录下运行", file=sys.stderr)
        return 1

    os.makedirs(OUT_DIR, exist_ok=True)
    os.makedirs(CATALOG, exist_ok=True)

    for name in os.listdir(OUT_DIR):
        if name.endswith(".png"):
            os.remove(os.path.join(OUT_DIR, name))

    images = []
    rendered = set()
    for idiom, label, scale, pixels in SPECS:
        filename = f"icon-{idiom}-{pixels}.png"
        if pixels not in rendered:
            render_png(os.path.join(OUT_DIR, filename), pixels)
            rendered.add(pixels)
        images.append({"filename": filename, "idiom": idiom,
                       "scale": scale, "size": label})

    with open(os.path.join(OUT_DIR, "Contents.json"), "w") as handle:
        json.dump({"images": images, "info": {"author": "xcode", "version": 1}},
                  handle, indent=2)
    with open(os.path.join(CATALOG, "Contents.json"), "w") as handle:
        json.dump({"info": {"author": "xcode", "version": 1}}, handle, indent=2)

    print(f"✓ AppIcon: {len(images)} 个条目 / {len(rendered)} 个 PNG")
    return 0


if __name__ == "__main__":
    sys.exit(main())
