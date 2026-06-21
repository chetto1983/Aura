#!/usr/bin/env bash
# End-to-end driver for the Aura FunctionGemma fine-tuning spike.
#
# Usage:
#   ./scripts/run_pipeline.sh            # tools -> synth -> (harvest if AURA_DB_URL) -> dataset
#   ./scripts/run_pipeline.sh --train    # also LoRA-train and run the pre/post eval
#
# Requires: OPENROUTER_API_KEY for synth; AURA_DB_URL (optional) for harvest.
set -euo pipefail

cd "$(dirname "$0")/.."
PY="${PY:-python}"
CONFIG="${CONFIG:-config/pipeline.yaml}"

echo "==> [1/5] exporting Aura tool surface"
( cd .. && go run ./finetune/exporter -out finetune/data/aura_tools.json )

echo "==> [2/5] synthetic generation (teacher distillation)"
"$PY" -m aura_finetune.synth --config "$CONFIG"

if [[ -n "${AURA_DB_URL:-}" ]]; then
  echo "==> [3/5] harvesting real traces from Postgres"
  "$PY" -m aura_finetune.harvest --config "$CONFIG"
else
  echo "==> [3/5] skipping harvest (AURA_DB_URL unset) — synthetic-only dataset"
fi

echo "==> [4/5] building dataset"
"$PY" -m aura_finetune.build_dataset --config "$CONFIG"

if [[ "${1:-}" == "--train" ]]; then
  echo "==> [5/5] measuring BASE model (before)"
  "$PY" -m aura_finetune.evaluate --config "$CONFIG" --base || true
  echo "==> training LoRA"
  "$PY" -m aura_finetune.train --config "$CONFIG"
  echo "==> measuring FINE-TUNED model (after)"
  "$PY" -m aura_finetune.evaluate --config "$CONFIG" --adapter data/out/lora
else
  echo "==> [5/5] data ready. Re-run with --train to fine-tune + evaluate."
fi
