#!/bin/bash
# The one-shot surface of the pinned PhotoCraft CLI, read from its own --help.
IMG=ghcr.io/chetto1983/aura-sandbox:edge
docker run --rm --entrypoint sh "$IMG" -c '
for c in convert info run batch serve; do echo "===== $c"; photocraft-cli $c --help; done
echo "===== commands (size/export/resize)"; photocraft-cli commands 2>&1 | grep -i -E "size|resiz|scale|export|flatten|sample" | head -40
echo "===== NOTICE"; cat /usr/local/share/licenses/photocraft/NOTICE
echo "===== ATTRIBUTION head"; head -60 /usr/local/share/licenses/photocraft/ATTRIBUTION.md
' 2>&1
