package mcptools

import "sort"

// MultiplexedMCPToolNamesForTest hands the external test package in this directory
// (bridge_multiplex_classifier_test.go) the keys of the unexported multiplexedMCPTools
// table. It is compiled into test binaries only: production has no enumerator of
// the table.
func MultiplexedMCPToolNamesForTest() []string {
	names := make([]string, 0, len(multiplexedMCPTools))
	for name := range multiplexedMCPTools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
