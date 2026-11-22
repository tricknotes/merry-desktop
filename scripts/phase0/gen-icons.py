"""Generate placeholder status icons (PNG) and an app icon (ICO) for the phase 0 spikes.

Uses only the standard library so it runs anywhere without extra packages.
"""
import struct
import sys
import zlib
from pathlib import Path

COLORS = {
    "stopped": (128, 128, 128),
    "starting": (230, 160, 20),
    "running": (40, 170, 80),
    "error": (210, 50, 50),
}
APP_COLOR = (50, 110, 200)


def png(size: int, rgb: tuple[int, int, int]) -> bytes:
    """Return a PNG of a filled, anti-aliased circle on a transparent background."""
    rows = []
    c = (size - 1) / 2
    r = size * 0.42
    for y in range(size):
        row = bytearray([0])  # filter type: none
        for x in range(size):
            d = ((x - c) ** 2 + (y - c) ** 2) ** 0.5
            a = max(0.0, min(1.0, r - d + 0.5))
            row += bytes((*rgb, int(a * 255)))
        rows.append(bytes(row))
    raw = b"".join(rows)

    def chunk(tag: bytes, data: bytes) -> bytes:
        return struct.pack(">I", len(data)) + tag + data + struct.pack(">I", zlib.crc32(tag + data))

    ihdr = struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr) + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b"")


def ico(images: list[tuple[int, bytes]]) -> bytes:
    """Return an ICO file that embeds the given PNG images."""
    header = struct.pack("<HHH", 0, 1, len(images))
    offset = 6 + 16 * len(images)
    entries, blobs = b"", b""
    for size, data in images:
        dim = 0 if size >= 256 else size
        entries += struct.pack("<BBBBHHII", dim, dim, 0, 0, 1, 32, len(data), offset)
        blobs += data
        offset += len(data)
    return header + entries + blobs


def main(out: Path) -> None:
    out.mkdir(parents=True, exist_ok=True)
    for name, rgb in COLORS.items():
        (out / f"tray-{name}.png").write_bytes(png(32, rgb))
    (out / "icon.png").write_bytes(png(256, APP_COLOR))
    (out / "icon.ico").write_bytes(ico([(s, png(s, APP_COLOR)) for s in (16, 32, 48, 256)]))


if __name__ == "__main__":
    main(Path(sys.argv[1]))
