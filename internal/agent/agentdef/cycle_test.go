package agentdef

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRegistry_DetectCycle(t *testing.T) {
	reg, err := NewRegistry(context.Background(), &Loader{BuiltinFS: fstest.MapFS{
		"a/agent.json": {Data: []byte(`{"id":"a","tier":"chat","when_to_use":"a","prompt":"a","subagents":[{"id":"b"}]}`)},
		"b/agent.json": {Data: []byte(`{"id":"b","tier":"worker","when_to_use":"b","prompt":"b","subagents":[{"id":"a"}]}`)},
	}}, &Validator{})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	cycle := reg.DetectCycle("a")
	if got := strings.Join(cycle, " -> "); got != "a -> b -> a" {
		t.Fatalf("cycle = %q", got)
	}
}
