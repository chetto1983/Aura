#!/usr/bin/env bash
# web_filter_backstop.sh — no-skip-as-green backstop for the path-filtered web CI
# jobs (WR-05). The web-lint/web-test jobs gate their real work
# on `dorny/paths-filter` reporting web == 'true'; when it reports 'false' the job
# would otherwise report SUCCESS having run nothing. This step runs in exactly that
# branch and FAILS if the diff for the event range actually touched a web path —
# i.e. the filter misfired (a glob edit, base-ref quirk, or rename dodging web/**)
# and a web change is about to merge with its gates silently skipped.
#
# It is deliberately fail-OPEN on an uncomputable range (shallow clone with the
# base SHA unfetchable, a brand-new branch's first push where BEFORE is all-zeros,
# etc.): the goal is to catch a CONFIRMED filter miss without ever red-falsing the
# pipeline on a range it cannot trust. Fail-CLOSED only on a verified web-path diff.
#
# Args:
#   $1 — base SHA (PR base.sha or push `before`)
#   $2 — head SHA (PR head.sha or push `sha`)
#
# Exit codes:
#   0 — no web path changed in the (computable) range, or the range is untrustworthy
#   1 — the filter said web==false but a web/** or internal/webui/dist/** file changed

set -euo pipefail

base="${1:-}"
head="${2:-}"
zero="0000000000000000000000000000000000000000"

if [ -z "${base}" ] || [ -z "${head}" ] || [ "${base}" = "${zero}" ]; then
  echo "web-filter-backstop: no trustworthy diff range (base='${base}' head='${head}') — skipping cross-check."
  exit 0
fi

# Best-effort: the web jobs use a shallow checkout, so the base/head objects may be
# absent. Fetch them; if the fetch fails, fail-open (do not break the pipeline).
if ! git cat-file -e "${base}^{commit}" 2>/dev/null; then
  git fetch --no-tags --depth=1 origin "${base}" 2>/dev/null || true
fi
if ! git cat-file -e "${head}^{commit}" 2>/dev/null; then
  git fetch --no-tags --depth=1 origin "${head}" 2>/dev/null || true
fi
if ! git cat-file -e "${base}^{commit}" 2>/dev/null || ! git cat-file -e "${head}^{commit}" 2>/dev/null; then
  echo "web-filter-backstop: base/head objects unfetchable in this shallow clone — skipping cross-check."
  exit 0
fi

changed="$(git diff --name-only "${base}" "${head}" -- 'web/**' 'internal/webui/dist/**' || true)"
if [ -n "${changed}" ]; then
  echo "NO-SKIP-AS-GREEN VIOLATION: paths-filter reported web==false, but these web paths changed:" >&2
  echo "${changed}" >&2
  echo "The web quality gates would merge SKIPPED. Fix the dorny/paths-filter globs." >&2
  exit 1
fi

echo "web-filter-backstop: confirmed no web/** or internal/webui/dist/** change in the range — skip is genuine."
