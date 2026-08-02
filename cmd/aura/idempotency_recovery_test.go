package main

import (
	"bytes"
	"reflect"
	"testing"
)

func TestEveryMutatingCLICommandUsesTheSubprocessExecutor(t *testing.T) {
	t.Parallel()
	for command, meta := range cliMutationCommands {
		if reflect.ValueOf(meta.Execute).Pointer() !=
			reflect.ValueOf(executeCLIChild).Pointer() {
			t.Fatalf("%q unexpectedly changed its subprocess executor", command)
		}
	}
}

func TestCanonicalCLIReplayBodyRejectsAmbiguousEnvelope(t *testing.T) {
	t.Parallel()
	canonical := []byte(
		`{"exit_code":0,"stdout":"sealed\n"}`,
	)
	normalized, ok := canonicalCLIReplayBody([]byte(
		"{\n  \"stdout\": \"sealed\\n\",\n  \"exit_code\": 0\n}",
	))
	if !ok || !bytes.Equal(normalized, canonical) {
		t.Fatalf("normalized replay = %q/%t, want %q/true", normalized, ok, canonical)
	}
	tests := []struct {
		name string
		body string
	}{
		{name: "duplicate", body: `{"exit_code":0,"exit_code":0}`},
		{name: "unknown", body: `{"exit_code":0,"unknown":true}`},
		{name: "missing exit code", body: `{"stdout":"sealed\n"}`},
		{name: "trailing", body: `{"exit_code":0} {}`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, ok := canonicalCLIReplayBody(
				[]byte(testCase.body),
			); ok {
				t.Fatalf("accepted ambiguous replay %s", testCase.name)
			}
		})
	}
}
