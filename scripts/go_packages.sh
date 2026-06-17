#!/usr/bin/env bash
set -euo pipefail

# Go's ./... follows Go examples inside local frontend dependencies when
# web/node_modules exists. Keep Go gates on Aura-owned package directories only.
module="$(go list -m)"
go list ./... | awk -v mod="$module" '
	$0 == mod { print "."; next }
	index($0, mod "/") == 1 {
		rel = substr($0, length(mod) + 2)
		if (rel !~ /^web\/node_modules(\/|$)/) {
			print "./" rel
		}
	}
'
