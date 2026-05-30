#!/usr/bin/env bash
# Format the given Go files in place with gofmt. Resolves gofmt through the Go
# toolchain because GOROOT/bin is not always on the hook shell's PATH (the `go`
# wrapper can live elsewhere). Called by lefthook pre-commit with {staged_files};
# safe to run standalone: scripts/gofmt-staged.sh file1.go file2.go ...
set -euo pipefail

[ "$#" -eq 0 ] && exit 0

GOFMT="$(go env GOROOT)/bin/gofmt"
[ -x "$GOFMT" ] || GOFMT="gofmt" # fall back to PATH if the toolchain layout differs

"$GOFMT" -w "$@"
