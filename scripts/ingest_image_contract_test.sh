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
set -euo pipefail

img="${AURA_INGEST_IMAGE:-aura-ingest:local}"

# --entrypoint is required: the image's ENTRYPOINT is `python -m ingest.app`, so a
# bare `docker run <img> python -c …` APPENDS to it instead of replacing it.
docker run --rm --entrypoint python "$img" -c "
import sys
import cocoindex, iscc_tika
from cocoindex.connectors import amazon_s3
assert cocoindex.__version__.startswith('1.0.'), cocoindex.__version__
assert 'auto_refresh' in cocoindex.__all__, 'auto_refresh missing from the public API'
assert 'extractous' not in sys.modules, 'extractous must not be importable beside iscc-tika'
assert amazon_s3.list_objects, 'amazon_s3 connector did not import cleanly'
print('python ok: cocoindex', cocoindex.__version__)
"

docker run --rm --entrypoint sh "$img" -lc \
  'command -v soffice >/dev/null || { echo "soffice missing" >&2; exit 1; }; echo "soffice ok"'

echo "ok: ingest image contract"
