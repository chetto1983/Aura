#!/usr/bin/env bash
set -euo pipefail

# Prints, comma-separated, every custom build tag the tracked Go files use, for
# `golangci-lint run --build-tags`. A run without tags never compiles a file behind one: on
# 2026-11-26 the tagged files carried 45 findings the untagged run could not see. Platform names,
# race, ignore and Go versions are not test tiers. Derived from the tree, so a new tier is linted
# the day its first file lands.
platform="$(go tool dist list | tr '/' '\n' | sort -u)"
tags="$(git ls-files '*.go' \
	| xargs -r grep -h '^//go:build ' \
	| sed 's#^//go:build ##' \
	| tr -c 'A-Za-z0-9_.\n' '\n' \
	| grep -Ev '^$|^go1(\.[0-9]+)*$|^(race|ignore|cgo|unix|gc|gccgo)$' \
	| grep -Fxv -f <(printf '%s\n' "$platform") \
	| sort -u \
	| paste -sd, -)"
[ -n "$tags" ] || { echo "go_build_tags: the tracked Go files use no custom build tag" >&2; exit 1; }
printf '%s\n' "$tags"
