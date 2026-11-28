#!/bin/bash
# What the measuring needs: a peak-memory reading inside a 768 MiB container, the image.imageSize parameters.
export PATH="$HOME/.local/bin:$HOME/go/bin:/usr/local/go/bin:$PATH"
IMG=ghcr.io/chetto1983/aura-sandbox:edge
docker run --rm --memory 768m --entrypoint sh "$IMG" -c '
echo "time: $(command -v time) $(ls /usr/bin/time 2>&1)"; cat /sys/fs/cgroup/memory.max /sys/fs/cgroup/memory.peak 2>&1; nproc
photocraft-cli commands --help
photocraft-cli commands 2>&1 | grep -E "^image\.imageSize" ; photocraft-cli commands --json 2>/dev/null | python3 -c "import json,sys; d=json.load(sys.stdin); [print(json.dumps(c)) for c in (d if isinstance(d,list) else d.get(\"commands\",[])) if \"imageSize\" in json.dumps(c)]" 2>&1 | head -5
' 2>&1
echo "== WSL tools"; command -v python3 uv go; go version; python3 -c "import PIL" 2>&1; nproc; free -m
