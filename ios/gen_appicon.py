#!/usr/bin/env python3
"""Generate Assets.xcassets/AppIcon.appiconset from the shared icon master.

The build Mac has no network (no brew/pip), so this file uses the standard
library only. The single source of truth is ``assets/app-icon.svg``; it is
rendered to ``assets/app-icon-1024.png`` by ``assets/gen_icons.py`` and this
script downsamples that master into every size iOS asks for.

Downsampling uses a 2x box pyramid plus area averaging instead of bilinear
sampling, so thin strokes in the photo glyph stay crisp at 20–180 px.
"""

import json
import math
import os
import struct
import sys
import zlib

OUT_DIR = "PhotoSync/Assets.xcassets/AppIcon.appiconset"
CATALOG = "PhotoSync/Assets.xcassets"
MASTER = "../assets/app-icon-1024.png"

# Fallback colour when the master carries transparency (it normally does not).
BACKGROUND = (0x3D, 0x8B, 0xE6)

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


def _to_rgb(width, height, channels, pixels):
    if channels == 3:
        return pixels
    out = bytearray(width * height * 3)
    for i in range(width * height):
        j = i * channels
        if channels == 4:
            alpha = pixels[j + 3]
            r, g, b = pixels[j], pixels[j + 1], pixels[j + 2]
        elif channels == 1:
            r = g = b = pixels[j]
            alpha = 255
        else:  # grayscale + alpha
            r = g = b = pixels[j]
            alpha = pixels[j + 1]
        k = i * 3
        out[k] = (r * alpha + BACKGROUND[0] * (255 - alpha) + 127) // 255
        out[k + 1] = (g * alpha + BACKGROUND[1] * (255 - alpha) + 127) // 255
        out[k + 2] = (b * alpha + BACKGROUND[2] * (255 - alpha) + 127) // 255
    return out


def _halve(width, height, pixels):
    nw, nh = width // 2, height // 2
    out = bytearray(nw * nh * 3)
    for y in range(nh):
        row0 = (y * 2) * width * 3
        row1 = row0 + width * 3
        for x in range(nw):
            i0 = row0 + x * 2 * 3
            i1 = i0 + 3
            j0 = row1 + x * 2 * 3
            j1 = j0 + 3
            o = (y * nw + x) * 3
            out[o] = (pixels[i0] + pixels[i1] + pixels[j0] + pixels[j1] + 2) >> 2
            out[o + 1] = (pixels[i0 + 1] + pixels[i1 + 1] +
                          pixels[j0 + 1] + pixels[j1 + 1] + 2) >> 2
            out[o + 2] = (pixels[i0 + 2] + pixels[i1 + 2] +
                          pixels[j0 + 2] + pixels[j1 + 2] + 2) >> 2
    return nw, nh, out


def _pyramid(width, height, pixels):
    levels = [(width, height, pixels)]
    while width >= 32 and height >= 32 and width % 2 == 0 and height % 2 == 0:
        width, height, pixels = _halve(width, height, pixels)
        levels.append((width, height, pixels))
    return levels


def _contributions(src_n, out_n):
    scale = src_n / out_n
    result = []
    for i in range(out_n):
        start, end = i * scale, (i + 1) * scale
        spans = []
        for j in range(int(math.floor(start)), int(math.ceil(end))):
            overlap = min(end, j + 1) - max(start, j)
            if overlap > 0:
                spans.append((j, overlap / scale))
        result.append(spans)
    return result


def _resample(level, size):
    sw, sh, src = level
    if sw == size and sh == size:
        return src

    xs = _contributions(sw, size)
    ys = _contributions(sh, size)
    out = bytearray(size * size * 3)

    for oy, yspans in enumerate(ys):
        for ox, xspans in enumerate(xs):
            r = g = b = 0.0
            for sy, wy in yspans:
                row = sy * sw * 3
                for sx, wx in xspans:
                    weight = wy * wx
                    i = row + sx * 3
                    r += src[i] * weight
                    g += src[i + 1] * weight
                    b += src[i + 2] * weight
            o = (oy * size + ox) * 3
            out[o] = int(r + 0.5)
            out[o + 1] = int(g + 0.5)
            out[o + 2] = int(b + 0.5)
    return out


def _write_png(path, size, rgb):
    raw = b"".join(b"\x00" + rgb[y * size * 3:(y + 1) * size * 3]
                   for y in range(size))

    def chunk(tag, data):
        return (struct.pack(">I", len(data)) + tag + data +
                struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF))

    # Colour type 2 (RGB, no alpha): App Store icons must be opaque.
    header = struct.pack(">IIBBBBB", size, size, 8, 2, 0, 0, 0)
    with open(path, "wb") as handle:
        handle.write(b"\x89PNG\r\n\x1a\n" +
                     chunk(b"IHDR", header) +
                     chunk(b"IDAT", zlib.compress(raw, 9)) +
                     chunk(b"IEND", b""))


def main():
    if not os.path.isdir("PhotoSync"):
        print("✗ 请在 ios/ 目录下运行", file=sys.stderr)
        return 1
    if not os.path.isfile(MASTER):
        print(f"✗ 找不到 {MASTER}，请先运行 python3 assets/gen_icons.py",
              file=sys.stderr)
        return 1

    width, height, channels, pixels = _decode_png(MASTER)
    rgb = _to_rgb(width, height, channels, pixels)
    levels = _pyramid(width, height, rgb)

    os.makedirs(OUT_DIR, exist_ok=True)
    os.makedirs(CATALOG, exist_ok=True)

    for name in os.listdir(OUT_DIR):
        if name.endswith(".png"):
            os.remove(os.path.join(OUT_DIR, name))

    images = []
    rendered = set()
    for idiom, label, scale, size in SPECS:
        filename = f"icon-{idiom}-{size}.png"
        if size not in rendered:
            level = next(item for item in levels if item[0] >= size)
            _write_png(os.path.join(OUT_DIR, filename), size,
                       _resample(level, size))
            rendered.add(size)
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
