#!/usr/bin/env bash
# The ingest sidecar's image contract.
#
# It asserts the three capabilities the pipeline cannot run without, because each
# of them fails QUIETLY rather than loudly when absent:
#
#   - cocoindex with auto_refresh, which is the only way a non-live source becomes
#     live (`cocoindex update -L` on such a source prints "Watching for changes"
#     and exits 0 while watching nothing).
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
assert cocoindex.__version__.startswith('1.0.'), cocoindex.__version__
assert 'auto_refresh' in cocoindex.__all__, 'auto_refresh missing from the public API'
assert 'extractous' not in sys.modules, 'extractous must not be importable beside iscc-tika'
print('python ok: cocoindex', cocoindex.__version__)
"

docker run --rm --entrypoint sh "$img" -lc \
  'command -v soffice >/dev/null || { echo "soffice missing" >&2; exit 1; }; echo "soffice ok"'

echo "ok: ingest image contract"
