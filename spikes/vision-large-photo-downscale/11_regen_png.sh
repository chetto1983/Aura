#!/bin/bash
# Re-make only the PNG (no noise, under the 25 MiB upload ceiling); a PNG's decode memory follows its size, not its bytes.
set -eu
S=/tmp/vision-spike
$S/venv/bin/python -I -c '
import sys; sys.path.insert(0, "/mnt/d/Aura/spikes/vision-large-photo-downscale")
import gen_images as g
from pathlib import Path
g.photo(8000, 6000, 0, 4).save(Path("/tmp/vision-spike/img/p48.png"), compress_level=6)'
ls -la $S/img
