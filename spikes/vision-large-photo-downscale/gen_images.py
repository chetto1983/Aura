"""Synthetic test photos: RGB gradients plus Gaussian noise, the method vision_downscale.go's
table was measured with. Run: python gen_images.py <outdir>. Pillow + numpy, seed fixed."""

import sys
from pathlib import Path

import numpy as np
from PIL import Image


def photo(w: int, h: int, sigma: float, seed: int) -> Image.Image:
    rng = np.random.default_rng(seed)
    x = np.linspace(0, 255, w, dtype=np.float32)
    y = np.linspace(0, 255, h, dtype=np.float32)[:, None]
    out = np.empty((h, w, 3), dtype=np.uint8)
    for top in range(0, h, 512):
        rows = y[top : top + 512]
        base = np.stack(
            [np.broadcast_to(x, (rows.shape[0], w)), np.broadcast_to(rows, (rows.shape[0], w)), (x + rows) / 2],
            axis=-1,
        )
        noisy = base + rng.normal(0, sigma, base.shape).astype(np.float32)
        out[top : top + 512] = np.clip(noisy, 0, 255).astype(np.uint8)
    return Image.fromarray(out, "RGB")


def main() -> None:
    d = Path(sys.argv[1])
    d.mkdir(parents=True, exist_ok=True)
    jpeg = dict(quality=92, subsampling=2)  # 4:2:0, what phone cameras write
    big = photo(8064, 6048, 12, 1)
    big.save(d / "p48_baseline.jpg", progressive=False, **jpeg)
    big.save(d / "p48_progressive.jpg", progressive=True, **jpeg)
    big.save(d / "p48_444_progressive.jpg", progressive=True, quality=92, subsampling=0)
    del big
    photo(6000, 4000, 12, 2).save(d / "p24_progressive.jpg", progressive=True, **jpeg)
    photo(4032, 3024, 12, 3).save(d / "p12_control.jpg", progressive=False, **jpeg)
    # No noise keeps the PNG under the 25 MiB image upload ceiling (AURA_ASSET_MAX_IMAGE_BYTES).
    photo(8000, 6000, 0, 4).save(d / "p48.png", compress_level=6)


if __name__ == "__main__":
    main()
