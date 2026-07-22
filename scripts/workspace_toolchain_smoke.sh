#!/usr/bin/env bash
# scripts/workspace_toolchain_smoke.sh — run inside the aura container
set -euo pipefail
node -e "require('docx'); console.log('docx-js ok')"
python3 -c "import docx, openpyxl, pandas; print('py docx/openpyxl/pandas ok')"
for t in pandoc file xxd; do command -v "$t" >/dev/null || { echo "MISSING $t" >&2; exit 1; }; done
echo "toolchain smoke: ok"
