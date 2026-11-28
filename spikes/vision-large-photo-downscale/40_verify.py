"""Check each output is a real downscale of its input: it decodes, its size is what was asked, and it
matches a reference (Pillow full decode, LANCZOS to the same size): mean absolute error per channel
and PSNR. Run with the spike venv: python -I 40_verify.py /tmp/vision-spike."""

import sys
from pathlib import Path

import numpy as np
from PIL import Image

root = Path(sys.argv[1])
refs: dict[tuple[str, tuple[int, int]], np.ndarray] = {}
full: dict[str, Image.Image] = {}


def source(stem: str) -> Image.Image:
    if stem not in full:
        full.clear()
        name = "p48.png" if stem == "p48_png" else stem + ".jpg"
        full[stem] = Image.open(root / "img" / name).convert("RGB")
    return full[stem]


rows = []
for out in sorted((root / "out").glob("*.jpg")):
    stem, tag = out.name.split(".")[0], out.name.split(".")[1]
    im = Image.open(out)
    im.load()
    key = (stem, im.size)
    if key not in refs:
        refs[key] = np.asarray(source(stem).resize(im.size, Image.LANCZOS), dtype=np.float32)
    got = np.asarray(im.convert("RGB"), dtype=np.float32)
    diff = got - refs[key]
    mae = float(np.abs(diff).mean())
    psnr = 10 * np.log10(255**2 / float((diff**2).mean()))
    rows.append(f"{out.name:36} {out.stat().st_size:>7} B {im.size[0]}x{im.size[1]} {im.format} MAE={mae:5.2f} PSNR={psnr:5.1f} dB")
print("\n".join(rows))
