package tools

import (
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// Shared harness for the box-routed tools. These lived in fs_test.go until the fs_* tools were
// replaced by the read_file/write_file/patch/search_files port; they are used by tests across the
// package (evict, shell_bg, the cap suite), so they belong in a file named after what they are
// rather than after a tool that no longer exists.
//
// A canned box response is what a real call sees — the tools have one filesystem now, the
// caller's box — and it pins the exact command composed, which an end-to-end container run could
// only observe indirectly. The docker_integration tier that runs a real box contributes ZERO
// coverage (CLAUDE.md), so anything provable without a daemon has to be proved here.

// boxRespondingWith answers every exec with stdout and records the calls.
func boxRespondingWith(stdout string) *fakeBox {
	return &fakeBox{respond: func(string) usersandbox.ExecResult {
		return usersandbox.ExecResult{Stdout: []byte(stdout)}
	}}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
