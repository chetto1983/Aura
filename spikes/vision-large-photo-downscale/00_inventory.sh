#!/bin/bash
# Inventory: images on this host, the pinned PhotoCraft CLI's surface, the JPEG libraries in the aura images.
export PATH="$HOME/.local/bin:$HOME/go/bin:/usr/local/go/bin:$PATH"
set -u
echo "== docker images"; docker images --format '{{.Repository}}:{{.Tag}} {{.Size}} {{.CreatedSince}}' | grep -E 'aura(-sandbox)?:edge'
IMG=ghcr.io/chetto1983/aura-sandbox:edge
echo "== photocraft in $IMG"
docker run --rm --entrypoint sh "$IMG" -c 'ls -la /usr/local/bin/photocraft-cli; photocraft-cli --version; echo; photocraft-cli --help; echo; ls /usr/local/share/licenses/photocraft/; file /usr/local/bin/photocraft-cli 2>/dev/null; ldd /usr/local/bin/photocraft-cli' 2>&1 | head -120
for img in ghcr.io/chetto1983/aura:edge $(docker images --format "{{.Repository}}:{{.Tag}}" | grep -m1 ingest); do
  echo "== jpeg libs in $img"
  docker run --rm --entrypoint sh "$img" -c 'ls /usr/lib/x86_64-linux-gnu/ | grep -i -E "jpeg|png|vips|magick" ; command -v djpeg cjpeg convert vips python3; dpkg -l | grep -i -E "jpeg|pillow" ; python3 -c "import PIL; print(\"PIL\", PIL.__version__)"' 2>&1 | head -30
done
