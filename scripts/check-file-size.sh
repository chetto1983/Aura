#!/usr/bin/env bash
# check-file-size.sh — enforces the 600-LOC cap on non-test, non-generated Go files.
#
# Rules:
#   • Files in .file-size-baseline.txt: FAIL if current LOC > baseline LOC (they grew).
#   • Files NOT in baseline: FAIL if LOC > 600.
#   • Files with a "// file-size-exempt: <reason>" comment on any of the first 10 lines
#     are skipped entirely (use sparingly — split the file instead).
#   • *_test.go, *_gen.go, vendor/, and web/dist/ are always excluded.
#
# Usage:
#   ./scripts/check-file-size.sh [baseline-file]   (default: .file-size-baseline.txt)
#
# Exit code: 0 = all green, 1 = violations found.

set -euo pipefail

BASELINE_FILE="${1:-.file-size-baseline.txt}"
LOC_CAP=600
FAIL=0
CHECKED=0

declare -A BASELINE

if [[ -f "$BASELINE_FILE" ]]; then
    while read -r count path; do
        # Skip comment lines
        [[ "$count" == \#* ]] && continue
        BASELINE["$path"]="$count"
    done < "$BASELINE_FILE"
fi

while IFS= read -r file; do
    rel="${file#./}"

    # Exclusions
    [[ "$rel" == *_test.go ]]   && continue
    [[ "$rel" == *_gen.go ]]    && continue
    [[ "$rel" == vendor/* ]]    && continue
    [[ "$rel" == web/dist/* ]]  && continue
    [[ "$rel" == .claude/* ]]   && continue   # Claude Code state dir; worktrees are agent scratch space

    # Exempt marker in the first 10 lines
    if head -n 10 "$file" 2>/dev/null | grep -q "// file-size-exempt:"; then
        continue
    fi

    count=$(wc -l < "$file")
    CHECKED=$((CHECKED + 1))

    if [[ -v "BASELINE[$rel]" ]]; then
        baseline_count="${BASELINE[$rel]}"
        if (( count > baseline_count )); then
            echo "FAIL [baseline-grew]  $rel: ${count} LOC  (baseline: ${baseline_count})"
            FAIL=1
        fi
    else
        if (( count > LOC_CAP )); then
            echo "FAIL [new-violation]  $rel: ${count} LOC  (cap: ${LOC_CAP})"
            FAIL=1
        fi
    fi
done < <(find . -name "*.go" -type f | sort)

if (( FAIL == 1 )); then
    echo ""
    echo "File-size check FAILED (checked ${CHECKED} files)."
    echo "Fix options:"
    echo "  • Split the file into smaller focused files (preferred)."
    echo "  • Add '// file-size-exempt: <reason>' on one of the first 10 lines (use sparingly)."
    exit 1
fi

echo "File-size check PASSED (checked ${CHECKED} files, cap: ${LOC_CAP} LOC)."
