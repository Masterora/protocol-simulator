"""Regenerate app icons from frontend/icon.svg (requires resvg-py and Pillow)."""
from io import BytesIO
from pathlib import Path

from PIL import Image
import resvg_py


ROOT = Path(__file__).resolve().parents[1]
SIZES = (16, 24, 32, 48, 64, 96, 128, 256)


def main():
    source = (ROOT / 'frontend/icon.svg').read_text()
    png = resvg_py.svg_to_bytes(svg_string=source, width=1024, height=1024)
    image = Image.open(BytesIO(png)).convert('RGBA')
    assert image.size == (1024, 1024)
    assert image.getpixel((0, 0))[3] == 0, 'Icon corners must be transparent'
    image.save(ROOT / 'build/appicon.png')
    image.save(ROOT / 'build/windows/icon.ico', sizes=[(n, n) for n in SIZES])
    with Image.open(ROOT / 'build/windows/icon.ico') as ico:
        assert ico.ico.sizes() == {(n, n) for n in SIZES}
    print('Updated 1024px PNG and eight-size Windows ICO')


if __name__ == '__main__':
    main()
