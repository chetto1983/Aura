//go:build db_integration && race

package adaptive

// snapshotTimeoutScale stretches the wall-clock bounds asserted by the capacity tests when
// the race detector is on. See race_scale_norace_test.go for why.
const snapshotTimeoutScale = 5
