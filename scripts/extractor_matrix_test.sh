#!/usr/bin/env bash
# The extractor's format matrix, scored on the two failures that matter.
#
# HARD failure: the format cannot be opened at all. MarkItDown raises
# UnsupportedFormatException on .doc/.ppt/.odt/.ods/.odp -- every legacy Office
# format and the whole OpenDocument family.
#
# SILENT failure: the format opens and returns plausible text in which the canary
# sentence no longer appears verbatim. This is the one that costs a corpus: a
# justified PDF reflowed with double spaces yields MORE characters than a clean
# extraction (96,063 vs 92,891 measured) while the lexical leg of hybrid retrieval
# stops matching, and nothing anywhere reports an error.
set -euo pipefail

img="${AURA_INGEST_IMAGE:-aura-ingest:local}"
fx="${1:-scripts/fixtures/document_pipeline_e2e}"
abs="$(cd "$fx" && { pwd -W 2>/dev/null || pwd; })"

MSYS_NO_PATHCONV=1 docker run --rm -i -v "${abs}:/fx:ro" --entrypoint python "$img" - <<'PY'
import pathlib, subprocess, sys, tempfile, time
from iscc_tika import Extractor

CANARY = "La sovranita appartiene al popolo"
LEGACY = {".doc": "docx", ".xls": "xlsx", ".ppt": "pptx"}

# The extractor ships a string-length cap and truncates to it in silence -- the
# same class of failure as everything else in this file. Set it explicitly and
# far above any fixture so a short result means a short document, never a cap.
_ex = Extractor()
_ex.set_extract_string_max_length(50_000_000)

def extract_text(path: str) -> str:
    # Returns (text, metadata). The metadata dict carries Tika's own provenance --
    # Content-Type, page count, author, producer -- which the pipeline will want
    # later; here only the text is scored.
    text, _metadata = _ex.extract_file_to_string(path)
    return text

def normalise(path: pathlib.Path, outdir: str) -> pathlib.Path:
    target = LEGACY[path.suffix.lower()]
    subprocess.run(["soffice", "--headless", "--convert-to", target,
                    "--outdir", outdir, str(path)],
                   check=True, capture_output=True, timeout=300)
    out = pathlib.Path(outdir) / (path.stem + "." + target)
    if not out.exists():
        raise RuntimeError(f"soffice produced no {target}")
    return out

# Two families, two ground truths. The prose files carry the CANARY sentence; the
# spreadsheets carry an order table and never contained that sentence, so asserting
# it there would be the test lying about the extractor rather than the reverse.
PROSE = {".pdf", ".docx", ".doc", ".odt", ".rtf", ".pptx", ".ppt", ".odp"}
SHEET = {".xlsx", ".xls", ".ods", ".csv"}
SHEET_TOKENS = ("Codice", "Descrizione", "A9A26924")

files = sorted(p for p in pathlib.Path("/fx").iterdir() if p.suffix.lower() in PROSE | SHEET)
hard, silent = [], []
print(f"{'formato':10s} {'esito':6s} {'caratteri':>10s} {'secondi':>8s}  verità")
for p in files:
    t0 = time.perf_counter()
    try:
        if p.suffix.lower() in LEGACY:
            with tempfile.TemporaryDirectory() as tmp:
                text = extract_text(str(normalise(p, tmp)))
        else:
            text = extract_text(str(p))
    except Exception as exc:
        hard.append((p.suffix, type(exc).__name__, str(exc)[:70]))
        print(f"{p.suffix:10s} {'FAIL':6s} {'-':>10s} {'-':>8s}  {type(exc).__name__}")
        continue
    dt = time.perf_counter() - t0
    if p.suffix.lower() in PROSE:
        # Collapsed first (are the WORDS there?), then exact (did spacing survive?).
        # The gap between the two answers IS the reflow bug: text a human still
        # reads, that an exact-phrase search can no longer find.
        found = CANARY in " ".join(text.split())
        verbatim = CANARY in text
        if not found:
            silent.append((p.suffix, "canarino assente"))
        elif not verbatim:
            silent.append((p.suffix, "canarino trovato solo dopo aver compresso gli spazi — testo riflusso"))
        mark = "canarino ok" if verbatim else ("RIFLUSSO" if found else "ASSENTE")
    else:
        missing = [t for t in SHEET_TOKENS if t not in text]
        if missing:
            silent.append((p.suffix, "celle mancanti: " + ", ".join(missing)))
        mark = "tabella ok" if not missing else "CELLE MANCANTI"
    print(f"{p.suffix:10s} {'ok':6s} {len(text):10d} {dt:8.2f}  {mark}")

print()
if hard:
    print("FALLIMENTI DURI:")
    for s, kind, msg in hard:
        print(f"  {s}: {kind}: {msg}")
if silent:
    print("FALLIMENTI SILENZIOSI (il testo esce, la frase non si trova):")
    for s, why in silent:
        print(f"  {s}: {why}")
if hard or silent:
    sys.exit(1)
print(f"ok: {len(files)} formati estratti, canarino verbatim in tutti")
PY
