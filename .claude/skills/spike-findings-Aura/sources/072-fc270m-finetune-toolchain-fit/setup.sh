#!/usr/bin/env bash
# Spike 072 — provision the Unsloth finetune toolchain in WSL.
#
# Host: WSL Ubuntu 26.04, system Python 3.14 (too new for torch/unsloth wheels),
# uv 0.11 present, CUDA via WSL passthrough (RTX A2000 4 GB). uv pins Python 3.12
# (the newest with stable torch+unsloth wheels) into a local venv.
#
# Run: bash setup.sh   (tee a log; this is multi-GB / multi-minute)
set -euo pipefail
DIR="$HOME/spike-072-fc270m-finetune"
mkdir -p "$DIR" && cd "$DIR"

uv venv --python 3.12 .venv
# shellcheck disable=SC1091
source .venv/bin/activate

# CUDA torch first so uv resolves the GPU build (auto = detect from nvidia-smi),
# then unsloth on top of it.
uv pip install --torch-backend=auto torch
uv pip install unsloth unsloth_zoo

python - <<'PY'
import torch, unsloth
print("torch", torch.__version__, "cuda_available", torch.cuda.is_available())
print("device", torch.cuda.get_device_name(0) if torch.cuda.is_available() else "CPU")
print("unsloth", unsloth.__version__)
PY
echo "SETUP-OK"
