#!/bin/bash
# Generate the test images under /tmp/vision-spike/img with Pillow in a throwaway uv venv.
export PATH="$HOME/.local/bin:$HOME/go/bin:/usr/local/go/bin:$PATH"
set -eu
S=/tmp/vision-spike; mkdir -p $S/img
[ -x $S/venv/bin/python ] || uv venv -q $S/venv
uv pip install -q --python $S/venv/bin/python pillow numpy
$S/venv/bin/python -c 'import PIL, numpy; print("Pillow", PIL.__version__, "numpy", numpy.__version__)'
/usr/bin/time -v $S/venv/bin/python -I /mnt/d/Aura/spikes/vision-large-photo-downscale/gen_images.py $S/img 2>&1 | grep -E "Maximum resident|Elapsed"
$S/venv/bin/python -I - <<'PY'
from pathlib import Path
from PIL import Image
for p in sorted(Path("/tmp/vision-spike/img").iterdir()):
    im = Image.open(p)
    print(f"{p.name:22} {p.stat().st_size:>11} B  {im.size[0]}x{im.size[1]}  {im.format} {im.mode}  progressive={bool(im.info.get('progressive') or im.info.get('progression'))}")
PY
