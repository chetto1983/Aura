package llm

import "testing"

func TestRuntimeVersionAdvancesOnReplace(t *testing.T) {
	rt := NewRuntime(nil, Config{Model: "a"})
	first := rt.Snapshot().Version
	rt.Replace(nil, Config{Model: "b"})
	second := rt.Snapshot()
	if second.Version != first+1 || second.Config.Model != "b" {
		t.Fatalf("after Replace: version %d model %q, want %d and b", second.Version, second.Config.Model, first+1)
	}
}
