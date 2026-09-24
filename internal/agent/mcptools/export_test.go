package mcptools

import (
	"maps"
	"slices"
)

// MultiplexedMCPToolNamesForTest hands the external test package in this directory
// (bridge_multiplex_classifier_test.go) the keys of the unexported multiplexedMCPTools
// table. It is compiled into test binaries only: production has no enumerator of
// the table.
func MultiplexedMCPToolNamesForTest() []string {
	return slices.Sorted(maps.Keys(multiplexedMCPTools))
}
