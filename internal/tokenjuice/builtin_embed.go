package tokenjuice

import "embed"

//go:embed builtin/*.json
var builtinFS embed.FS
