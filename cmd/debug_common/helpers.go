// Package debugcommon provides shared scenario/assert helpers for Aura debug harnesses.
package debugcommon

import (
	"fmt"
	"os"
)

// Harness is a lightweight harness for debug smoke scripts.
type Harness struct {
	name string
}

// New returns a Harness tagged with the given harness name (used as a prefix in error output).
func New(name string) *Harness {
	return &Harness{name: name}
}

// Fail prints a prefixed error message and exits with status 1.
func (h *Harness) Fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, h.name+": "+format+"\n", args...)
	os.Exit(1)
}

// MustEqual asserts got == want (by fmt.Sprintf) and calls Fail if not.
func (h *Harness) MustEqual(label string, got, want any) {
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		h.Fail("  %s = %v, want %v", label, got, want)
	}
}

// Scenario runs fn, printing the scenario name and pass/fail result.
func (h *Harness) Scenario(name string, fn func() error) {
	fmt.Printf("→ %s\n", name)
	if err := fn(); err != nil {
		h.Fail("  FAIL: %v", err)
	}
	fmt.Println("  ok")
}
