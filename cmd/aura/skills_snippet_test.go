package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/skills"
)

func TestSnippetExecTimeout(t *testing.T) {
	if os.Getenv("AURA_TEST_SNIPPET_SLEEP") == "1" {
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}

	t.Setenv("AURA_TEST_SNIPPET_SLEEP", "1")
	oldTimeout := snippetExecTimeout
	snippetExecTimeout = 25 * time.Millisecond
	t.Cleanup(func() { snippetExecTimeout = oldTimeout })

	_, _, _, err := runSnippetProcess(context.Background(), skills.SnippetUse{
		Interpreter: os.Args[0],
		HostPath:    "-test.run=^TestSnippetExecTimeout$",
	}, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runSnippetProcess timeout err = %v, want context deadline exceeded", err)
	}
}
