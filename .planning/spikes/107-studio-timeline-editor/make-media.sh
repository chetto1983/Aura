#!/usr/bin/env bash
# Copies the test media and one self-hosted font (Aura's own Atkinson Hyperlegible Next, OFL) into
# media/, which Vite serves as its publicDir. Nothing is downloaded.
set -euo pipefail
cd "$(dirname "$0")"
WEB=../../../web
mkdir -p media/fonts
cp "$WEB/e2e/fixtures/media-edit/clip.mp4" "$WEB/e2e/fixtures/media-edit/photo.png" media/
cp "$WEB/public/fonts/atkinson-hyperlegible-next-variable.woff2" media/fonts/atkinson.woff2
# One stylesheet per family, because VideoFlow's FontEmbedder inlines every rule of every registered
# stylesheet into each text layer's SVG. "Noto Sans" is VideoFlow's hard-coded default family: it is
# aliased to the same local file so the renderer never needs Google's copy.
for family in "Atkinson Hyperlegible Next:atkinson" "Noto Sans:noto-sans-alias"; do
  name=${family%%:*}; file=${family##*:}
  cat > "media/fonts/$file.css" <<CSS
@font-face {
  font-family: '$name';
  src: url('/fonts/atkinson.woff2') format('woff2');
  font-weight: 200 800;
  font-style: normal;
  font-display: block;
}
CSS
done
ls -la media media/fonts
