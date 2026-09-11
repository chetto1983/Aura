#!/usr/bin/env bash
set -euo pipefail

# Go's ./... walks every directory under the module, tracked or not: Go examples inside local
# frontend dependencies when web/node_modules exists, and local scratch such as artifacts/.
# Measured 2026-09-11: a gitignored benchmark client failed `make lint` on this checkout while
# CI, which checks out only tracked files, never saw it. Keep Go gates on the packages git
# tracks Go files in.
module="$(go list -m)"
tracked="$(git ls-files '*.go' | sed -E 's#/[^/]+$##; s#^[^/]+\.go$#.#' | sort -u)"
[ -n "$tracked" ] || { echo "go_packages: git lists no tracked Go files" >&2; exit 1; }
go list ./... \
	| awk -v mod="$module" '$0 == mod { print "."; next } index($0, mod "/") == 1 { print substr($0, length(mod) + 2) }' \
	| grep -Fx -f <(printf '%s\n' "$tracked") \
	| sed -E '/^\.$/!s#^#./#'
