package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// stubTool is a minimal tool for registry-filtering assertions.
type stubTool struct{ name string }

func (s stubTool) Spec() Spec { return Spec{Name: s.name, Summary: s.name, Description: s.name} }
func (stubTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}

// TestWithout asserts the promoted helper drops the named tool while the parent
// keeps it (the parent registry is never mutated — immutable per run).
func TestWithout(t *testing.T) {
	parent := NewRegistry()
	parent.Register(stubTool{name: "swarm_spawn"})
	parent.Register(stubTool{name: "text_response"})
	parent.Register(stubTool{name: "web_search"})

	child := Without(parent, "swarm_spawn")

	if _, ok := child.Get("swarm_spawn"); ok {
		t.Error("child registry must NOT contain swarm_spawn")
	}
	if _, ok := child.Get("text_response"); !ok {
		t.Error("child registry must retain text_response")
	}
	if _, ok := child.Get("web_search"); !ok {
		t.Error("child registry must retain web_search")
	}
	if _, ok := parent.Get("swarm_spawn"); !ok {
		t.Error("parent registry must STILL contain swarm_spawn (no mutation)")
	}
	if len(child.All()) != 2 || len(parent.All()) != 3 {
		t.Errorf("child=%d parent=%d, want child=2 parent=3", len(child.All()), len(parent.All()))
	}
}

// TestRegisterDistinctNamesSucceeds asserts the normal path: registering tools
// with distinct names keeps every one of them.
func TestRegisterDistinctNamesSucceeds(t *testing.T) {
	r := NewRegistry()
	r.Register(stubTool{name: "alpha"})
	r.Register(stubTool{name: "beta"})
	if len(r.All()) != 2 {
		t.Fatalf("want 2 tools, got %d", len(r.All()))
	}
}

// TestRegisterDuplicateNamePanics asserts B-14: a duplicate name is a startup
// programming error (a name collision silently shadowing a tool) and must FAIL
// LOUD — like net/http's duplicate-route panic — not be swallowed by a silent
// overwrite.
func TestRegisterDuplicateNamePanics(t *testing.T) {
	r := NewRegistry()
	r.Register(stubTool{name: "dup"})

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("a duplicate tool-name registration must panic, not silently overwrite")
		}
		if msg := fmt.Sprint(rec); !strings.Contains(msg, "dup") {
			t.Fatalf("panic message should name the colliding tool, got %q", msg)
		}
	}()
	r.Register(stubTool{name: "dup"})
}

// TestWithoutMultipleNames asserts dropping several names at once and that an
// absent name is a harmless no-op (no panic, nothing dropped).
func TestWithoutMultipleNames(t *testing.T) {
	parent := NewRegistry()
	parent.Register(stubTool{name: "a"})
	parent.Register(stubTool{name: "b"})
	parent.Register(stubTool{name: "c"})

	child := Without(parent, "a", "c", "not_present")
	if len(child.All()) != 1 {
		t.Fatalf("want 1 remaining tool, got %d", len(child.All()))
	}
	if _, ok := child.Get("b"); !ok {
		t.Error("child must retain b")
	}
}
