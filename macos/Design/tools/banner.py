#!/usr/bin/env python3
"""Generates the dot rows of install.sh's banner from the app icon.

Renders appicon/A-port-pit.svg without its background (Quick Look + sips,
both part of macOS), averages it down to a grid and prints one line per row:
a 6-digit hex colour per dot, "." where the icon is transparent. Paste the
output into BANNER_ROWS in install.sh:

  python3 macos/Design/tools/banner.py [COLUMNS] [ROWS]     (default 15 19)
"""
import os
import re
import struct
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
SVG = os.path.join(HERE, "..", "appicon", "A-port-pit.svg")
SIZE = 512
KEY = (255, 0, 255)  # stands in for "transparent" in the render


def render(tmp):
    svg = open(SVG).read()
    svg = re.sub(r'<rect width="1024" height="1024"[^>]*/>', '<rect width="1024" height="1024" fill="#ff00ff"/>', svg)
    svg = svg.replace(' filter="url(#shadow)"', "")  # a shadow would bleed into the key colour
    src = os.path.join(tmp, "icon.svg")
    open(src, "w").write(svg)
    subprocess.run(["qlmanage", "-t", "-s", str(SIZE), "-o", tmp, src], check=True, capture_output=True)
    bmp = os.path.join(tmp, "icon.bmp")
    subprocess.run(["sips", "-s", "format", "bmp", src + ".png", "--out", bmp], check=True, capture_output=True)
    return open(bmp, "rb").read()


def pixels(d):
    off, = struct.unpack("<I", d[10:14])
    w, h = struct.unpack("<ii", d[18:26])
    bp = struct.unpack("<H", d[28:30])[0] // 8
    top, h = h < 0, abs(h)
    row = (w * bp + 3) & ~3

    def px(x, y):
        i = off + (y if top else h - 1 - y) * row + x * bp
        return d[i + 2], d[i + 1], d[i]
    return w, h, px


def is_key(c):
    return c[0] > 200 and c[2] > 200 and c[1] < 90


def main():
    cols = int(sys.argv[1]) if len(sys.argv) > 1 else 15
    rows = int(sys.argv[2]) if len(sys.argv) > 2 else 19
    with tempfile.TemporaryDirectory() as tmp:
        w, h, px = pixels(render(tmp))
    ys = [y for y in range(h) if any(not is_key(px(x, y)) for x in range(0, w, 4))]
    xs = [x for x in range(w) if any(not is_key(px(x, y)) for y in range(0, h, 4))]
    # Symmetric around the centre: the icon is, and rounding shouldn't skew it.
    half = max(w / 2 - min(xs), max(xs) + 1 - w / 2)
    x0, x1, y0, y1 = w / 2 - half, w / 2 + half, min(ys), max(ys) + 1
    for gy in range(rows):
        out = []
        for gx in range(cols):
            ax0, ax1 = x0 + (x1 - x0) * gx / cols, x0 + (x1 - x0) * (gx + 1) / cols
            ay0, ay1 = y0 + (y1 - y0) * gy / rows, y0 + (y1 - y0) * (gy + 1) / rows
            n, acc = 0, []
            for y in range(int(ay0), max(int(ay1), int(ay0) + 1)):
                for x in range(int(ax0), max(int(ax1), int(ax0) + 1)):
                    n += 1
                    c = px(x, y)
                    if not is_key(c):
                        acc.append(c)
            if len(acc) < n / 2:
                out.append(".")
            else:
                out.append("%02x%02x%02x" % tuple(sum(c[i] for c in acc) // len(acc) for i in range(3)))
        print(" ".join(out))


if __name__ == "__main__":
    main()
