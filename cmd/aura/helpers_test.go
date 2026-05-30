package main

import (
	"bytes"
	"os"
	"testing"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns whatever
// it wrote. Used to assert on commands that print to stdout and return normally
// (they os.Exit only on error).
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	_ = w.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(r)
	return out.String()
}
