#!/usr/bin/env bash
# The ingest sidecar's image contract.
#
# It asserts the four capabilities the pipeline cannot run without, because each
# of them fails QUIETLY rather than loudly when absent:
#
#   - cocoindex with auto_refresh, which is the only way a non-live source becomes
#     live (`cocoindex update -L` on such a source prints "Watching for changes"
#     and exits 0 while watching nothing).
#   - cocoindex.connectors.amazon_s3 actually importable. A bare `cocoindex==1.0.19`
#     pin raises ImportError ("aiobotocore is required ... install cocoindex[amazon_s3]")
#     with no earlier symptom -- the image builds clean, and the failure only
#     surfaces the first time app.py touches the S3 source. The bucket IS the
#     source this whole rewrite reconciles against, so this must never regress
#     silently.
#   - iscc-tika, and ALONE: importing extractous beside it loads a second GraalVM
#     native image and the collision surfaces as
#     `NoSuchMethodError ... TesseractOCRConfig.setSkipOcr` on whichever loads second.
#   - soffice, without which a legacy .doc/.xls/.ppt has no route into Aura.
#
# And one absence: pip. The service never runs it, and its vendored msgpack, urllib3 and
# setuptools were the image's HIGH findings on 2026-10-26 (issue #189).
set -euo pipefail

img="${AURA_INGEST_IMAGE:-aura-ingest:local}"
# The image must carry the cocoindex the requirements pin, whatever Dependabot moved it to: a
# hard-coded "1.0." went stale at the 1.2.0 bump (2026-11-16) and ran nowhere to notice.
requirements="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/docker/aura-ingest/requirements.txt"
cocoindex_pin="$(sed -n 's/^cocoindex\[[^]]*\]==\([0-9][0-9.]*\)$/\1/p' "$requirements")"
[ -n "$cocoindex_pin" ] || { echo "no cocoindex==<version> pin in $requirements" >&2; exit 1; }

# --entrypoint is required: the image's ENTRYPOINT is `python -m ingest.app`, so a
# bare `docker run <img> python -c …` APPENDS to it instead of replacing it.
docker run --rm -e COCOINDEX_PIN="$cocoindex_pin" --entrypoint python "$img" -c "
import os, sys
import cocoindex, iscc_tika
from cocoindex.connectors import amazon_s3
assert cocoindex.__version__ == os.environ['COCOINDEX_PIN'], (cocoindex.__version__, os.environ['COCOINDEX_PIN'])
assert 'auto_refresh' in cocoindex.__all__, 'auto_refresh missing from the public API'
assert 'extractous' not in sys.modules, 'extractous must not be importable beside iscc-tika'
assert amazon_s3.list_objects, 'amazon_s3 connector did not import cleanly'
print('python ok: cocoindex', cocoindex.__version__)
"

docker run --rm --entrypoint sh "$img" -lc '
  command -v soffice >/dev/null || { echo "soffice missing" >&2; exit 1; }
  command -v pdftoppm >/dev/null || { echo "pdftoppm missing" >&2; exit 1; }
  if python -m pip --version >/dev/null 2>&1; then echo "pip is still in the image" >&2; exit 1; fi
  echo "soffice + pdftoppm ok, no pip"
'

echo "ok: ingest image contract"
