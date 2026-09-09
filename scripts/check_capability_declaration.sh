#!/usr/bin/env bash
# check_capability_declaration.sh — RBAC-02: every capability_grants name Aura enforces is
# declared in internal/identity/capabilities.go and NOWHERE else. Modeled on
# scripts/check_ci_go_packages.sh's argument handling, failure-output style, and exit
# discipline; the assertion logic below is unrelated (it counts declarations, that script
# scans for a banned token) and is written fresh here.
#
# Two halves, and both matter:
#   1. No second declaration — a capability-name string literal outside
#      internal/identity/capabilities.go, in a non-test Go file under cmd/ or internal/,
#      fails the build. Offending file:line entries are printed sorted, so a rerun diffs
#      byte-identical.
#   2. The declaration exists — internal/identity/capabilities.go must itself declare at
#      least six names. Without this half, deleting the declaration file would make half 1
#      pass vacuously (zero names to scan for = zero matches = a "clean" scan that is
#      actually just blind). An empty result must never read as a pass.
#
# The name list is DERIVED from capabilities.go itself, never hard-coded here — a
# hard-coded list in the checker would itself be a second declaration point, the exact
# thing this check exists to forbid. Comment lines are stripped before counting (a bare
# grep -c over a name would count this file's own header prose and capabilities.go's doc
# comments, which self-invalidates the empty-scan check below).
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

DECL_FILE="internal/identity/capabilities.go"

if [[ ! -f "$DECL_FILE" ]]; then
  echo "check_capability_declaration: declaration file '$DECL_FILE' not found" >&2
  exit 2
fi

# Half 2 first: derive the declared names. Only a `const Cap... = "name"` line on a
# NON-comment line counts as a declaration (grep -v strips comment lines before the
# anchored const-declaration match runs, so a doc comment mentioning a capability name in
# prose is never mistaken for a declaration).
declared_lines="$(grep -v '^\s*//' "$DECL_FILE" | grep -oE '^const Cap[A-Za-z0-9_]+ = "[a-z][a-z0-9._-]{0,63}"' || true)"
declared_names="$(printf '%s\n' "$declared_lines" | grep -oE '"[a-z][a-z0-9._-]{0,63}"$' | tr -d '"' | sort -u || true)"
declared_count="$(printf '%s\n' "$declared_names" | grep -c . || true)"

if [[ "$declared_count" -lt 6 ]]; then
  echo "check_capability_declaration: FAIL — $DECL_FILE declares $declared_count capability name(s), want at least 6 (an empty or shrunk declaration file must fail, never pass)" >&2
  exit 1
fi

echo "check_capability_declaration: $DECL_FILE declares $declared_count capability names: $(printf '%s' "$declared_names" | tr '\n' ' ')"

# Half 1: scan every non-test Go file under cmd/ and internal/, excluding the declaration
# file itself, for a QUOTED occurrence of one of the derived names. A quoted literal is the
# thing RBAC-02 forbids — every legitimate reference goes through the exported const
# instead (identity.CapAgentRun, etc.), never a re-spelled string.
offenders=""
while IFS= read -r name; do
  [[ -z "$name" ]] && continue
  matches="$(grep -rnF "\"$name\"" --include='*.go' cmd/ internal/ 2>/dev/null \
    | grep -v '_test\.go:' \
    | grep -vF "$DECL_FILE:" || true)"
  if [[ -n "$matches" ]]; then
    offenders="${offenders}${matches}"$'\n'
  fi
done <<< "$declared_names"

# Sorted (LC_ALL=C for a locale-independent, byte-identical rerun diff) and de-duplicated.
offenders="$(printf '%s' "$offenders" | grep -v '^$' | LC_ALL=C sort -u || true)"

if [[ -n "$offenders" ]]; then
  {
    echo "check_capability_declaration: FAIL — a capability name is declared outside $DECL_FILE:"
    printf '%s\n' "$offenders"
  } >&2
  exit 1
fi

echo "check_capability_declaration: PASS — no second declaration outside $DECL_FILE"
