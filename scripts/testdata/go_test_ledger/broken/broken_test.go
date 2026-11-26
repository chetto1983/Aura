package broken

import "testing"

// Does not compile on purpose: go test reports the error as build-output events.
func TestBroken(t *testing.T) { var n int = "not an int"; _ = n }
