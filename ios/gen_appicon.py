#!/usr/bin/env python3
"""Generate Assets.xcassets/AppIcon.appiconset without Xcode or PIL.

The build Mac has no network (no brew/pip), so the icon set is rendered with the
standard library only.

Important: Xcode 15 rejects `idiom: universal` entries for anything except the
1024pt App Store icon — using it makes actool emit
    "The app icon set AppIcon has N unassigned children"
and no Assets.car is produced, which shows up as a blank home-screen icon.
This script therefore emits explicit iphone / ipad / ios-marketing entries.
"""

import json
import math
import os
import struct
import sys
import zlib

OUT_DIR = "PhotoSync/Assets.xcassets/AppIcon.appiconset"
CATALOG = "PhotoSync/Assets.xcassets"

# (idiom, size label, scale, pixel size)
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


ANDROID_ICON = "../android/app/src/main/res/mipmap-xxxhdpi/ic_launcher.png"
FG = (255, 255, 255)


def _render_vector(path: str, size: int, bg: tuple) -> None:
    """Fallback: draw the same two-arc glyph without the Android asset.

    Only used when android/ is not present next to ios/. The arcs are simple
    circles with round caps plus triangular arrow heads, matching the launcher.
    """
    import math as _math

    cx = cy = size / 2.0
    radius = size * 0.30
    half = size * 0.045

    def arc(angle: float, start: float, sweep: float) -> bool:
        return (angle - start) % 360 <= sweep

    rows = []
    for y in range(size):
        row = bytearray()
        for x in range(size):
            px, py = x + 0.5, y + 0.5
            dx, dy = px - cx, py - cy
            dist = _math.hypot(dx, dy)
            ang = (_math.degrees(_math.atan2(dy, dx)) + 360) % 360
            colour = bg
            if abs(dist - radius) <= half:
                if arc(ang, 200, 130):
                    colour = FG
                elif arc(ang, 20, 130):
                    colour = tuple(int(b + (f - b) * 0.6) for b, f in zip(bg, FG))
            row += bytes((*colour, 255))
        rows.append(bytes(row))

    raw = b"".join(b"\x00" + row for row in rows)

    def chunk(tag, data):
        return (struct.pack(">I", len(data)) + tag + data +
                struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF))

    header = struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)
    with open(path, "wb") as handle:
        handle.write(b"\x89PNG\r\n\x1a\n" +
                     chunk(b"IHDR", header) +
                     chunk(b"IDAT", zlib.compress(raw, 9)) +
                     chunk(b"IEND", b""))


def _decode_png(path: str):
    """Minimal PNG decoder: 8-bit RGB/RGBA, non-interlaced (all we generate)."""
    data = open(path, "rb").read()
    pos = 8
    width = height = colour_type = None
    idat = b""
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
    out = bytearray(width * height * channels)
    prev = bytearray(stride)
    off = 0
    for y in range(height):
        filter_type = raw[off]
        off += 1
        line = bytearray(raw[off:off + stride])
        off += stride
        if filter_type == 1:
            for i in range(channels, stride):
                line[i] = (line[i] + line[i - channels]) & 0xFF
        elif filter_type == 2:
            for i in range(stride):
                line[i] = (line[i] + prev[i]) & 0xFF
        elif filter_type == 3:
            for i in range(stride):
                a = line[i - channels] if i >= channels else 0
                line[i] = (line[i] + ((a + prev[i]) >> 1)) & 0xFF
        elif filter_type == 4:
            for i in range(stride):
                a = line[i - channels] if i >= channels else 0
                c = prev[i - channels] if i >= channels else 0
                b = prev[i]
                p = a + b - c
                pa, pb, pc = abs(p - a), abs(p - b), abs(p - c)
                pr = a if (pa <= pb and pa <= pc) else (b if pb <= pc else c)
                line[i] = (line[i] + pr) & 0xFF
        out[y * stride:(y + 1) * stride] = line
        prev = line

    return width, height, channels, out


def render_png(path: str, size: int) -> None:
    """Produce an App Store icon from the Android launcher artwork.

    iOS requires a fully opaque, unrounded square (the system applies the mask
    itself), while the Android asset already has rounded corners and
    transparency. So the Android glyph is composited onto the brand blue and
    scaled with bilinear sampling.
    """
    BG = (0x4F, 0x8C, 0xFF)
    if not os.path.isfile(ANDROID_ICON):
        _render_vector(path, size, BG)
        return

    src_w, src_h, channels, src = _decode_png(ANDROID_ICON)

    def sample(u: float, v: float):
        """Bilinear sample of the Android bitmap at normalised (u, v)."""
        fx = min(max(u * (src_w - 1), 0.0), src_w - 1.0)
        fy = min(max(v * (src_h - 1), 0.0), src_h - 1.0)
        x0, y0 = int(fx), int(fy)
        x1, y1 = min(x0 + 1, src_w - 1), min(y0 + 1, src_h - 1)
        tx, ty = fx - x0, fy - y0

        def pixel(x, y):
            i = (y * src_w + x) * channels
            if channels == 4:
                return src[i], src[i + 1], src[i + 2], src[i + 3]
            if channels == 3:
                return src[i], src[i + 1], src[i + 2], 255
            return src[i], src[i], src[i], 255

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
            alpha = a / 255.0
            row += bytes((
                int(BG[0] + (r - BG[0]) * alpha),
                int(BG[1] + (g - BG[1]) * alpha),
                int(BG[2] + (b - BG[2]) * alpha),
                255,          # iOS icons must be opaque
            ))
        rows.append(bytes(row))

    raw = b"".join(b"\x00" + row for row in rows)

    def chunk(tag, data):
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
    rendered = {}
    for idiom, label, scale, pixels in SPECS:
        filename = f"icon-{idiom}-{pixels}.png"
        if pixels not in rendered:
            render_png(os.path.join(OUT_DIR, filename), pixels)
            rendered[pixels] = filename
        images.append({
            "filename": filename,
            "idiom": idiom,
            "scale": scale,
            "size": label,
        })

    with open(os.path.join(OUT_DIR, "Contents.json"), "w") as handle:
        json.dump({"images": images, "info": {"author": "xcode", "version": 1}},
                  handle, indent=2)
    with open(os.path.join(CATALOG, "Contents.json"), "w") as handle:
        json.dump({"info": {"author": "xcode", "version": 1}}, handle, indent=2)

    print(f"✓ AppIcon: {len(images)} 个条目 / {len(rendered)} 个 PNG")
    return 0


if __name__ == "__main__":
    sys.exit(main())
