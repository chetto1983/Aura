package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadToolOutput_BadArgs covers read_tool_output's malformed-JSON args branch.
// It asserts the SPECIFIC "args" error, not merely "an error": removing this guard
// lets execution fall through to a later guard (tool_call_id required), which still
// returns *an* error — so a bare err!=nil check would not kill that mutant. The
// exact-message assertion does (mutation discipline, zero production change).
func TestReadToolOutput_BadArgs(t *testing.T) {
	ctx := ctxWithRunDir("sess-ba", "call-ba", t.TempDir())
	_, err := (ReadToolOutput{}).Execute(ctx, []byte(`{not json}`))
	if err == nil {
		t.Fatal("want error on malformed read_tool_output args")
	}
	if !strings.Contains(err.Error(), "read_tool_output args:") {
		t.Fatalf("want the malformed-args error, got %q", err.Error())
	}
}

// TestReadToolOutput_EmptyID covers the required-tool_call_id branch. Asserts the
// SPECIFIC "tool_call_id is required" message: without the exact check, removing
// this guard falls through to sidecarPath, which also errors on the empty id — a
// bare err!=nil would pass on the mutant. The message assertion kills it.
func TestReadToolOutput_EmptyID(t *testing.T) {
	ctx := ctxWithRunDir("sess-eid", "call-eid", t.TempDir())
	_, err := (ReadToolOutput{}).Execute(ctx, []byte(`{"tool_call_id":""}`))
	if err == nil {
		t.Fatal("want error when tool_call_id is empty")
	}
	if !strings.Contains(err.Error(), "tool_call_id is required") {
		t.Fatalf("want the required-id error, got %q", err.Error())
	}
}

// TestReadToolOutput_MissingContext covers the missing-tool-call-context branch:
// Execute is called with a bare context (no WithToolCallContext). Asserts the
// SPECIFIC "missing tool-call context" message so the guard's removal (which falls
// through to a zero-value ctx and a downstream validateID error) is killed.
func TestReadToolOutput_MissingContext(t *testing.T) {
	_, err := (ReadToolOutput{}).Execute(context.Background(), []byte(`{"tool_call_id":"x"}`))
	if err == nil {
		t.Fatal("want error when the tool-call context is absent")
	}
	if !strings.Contains(err.Error(), "missing tool-call context") {
		t.Fatalf("want the missing-context error, got %q", err.Error())
	}
}

// TestReadToolOutput_SidecarReadError covers read_tool_output's non-NotExist read
// error branch: the sidecar PATH exists but is a directory, so os.ReadFile fails
// with something other than ErrNotExist. Asserts the SPECIFIC "read sidecar" wrap
// so the IsNotExist-branch removal (which would mis-route this to the no-output
// message) is killed.
func TestReadToolOutput_SidecarReadError(t *testing.T) {
	runDir := t.TempDir()
	const sess, call = "sess-rde", "call-rde"
	// Create <runDir>/conversations/<sess>/<call>.result as a DIRECTORY.
	dir := filepath.Join(runDir, "conversations", sess, call+".result")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := ctxWithRunDir(sess, call, runDir)
	_, err := (ReadToolOutput{}).Execute(ctx, []byte(`{"tool_call_id":"`+call+`"}`))
	if err == nil {
		t.Fatal("want a read error when the sidecar path is a directory")
	}
	if !strings.Contains(err.Error(), "read sidecar") {
		t.Fatalf("want the non-NotExist read-sidecar error, got %q", err.Error())
	}
}

// TestTextResponse_BadArgs covers text_response's malformed-JSON args branch.
func TestTextResponse_BadArgs(t *testing.T) {
	if _, err := (TextResponse{}).Execute(context.Background(), []byte(`{not json}`)); err == nil {
		t.Fatal("want error on malformed text_response args")
	}
}

// TestTextResponse_EmptyText covers the required-text branch.
func TestTextResponse_EmptyText(t *testing.T) {
	if _, err := (TextResponse{}).Execute(context.Background(), []byte(`{"text":""}`)); err == nil {
		t.Fatal("want error when text is empty")
	}
}

// TestNewResult_MissingContext covers NewResult's missing-tool-call-context branch.
func TestNewResult_MissingContext(t *testing.T) {
	if _, err := NewResult(context.Background(), "anything"); err == nil {
		t.Fatal("want error when WithToolCallContext was not called")
	}
}

// TestValidateID_Traversal covers validateID's `..` traversal branch (distinct
// from the path-separator branch the existing traversal test also drives) via a
// spill whose tool_call_id contains `..`.
func TestValidateID_Traversal(t *testing.T) {
	// A large content forces NewResult onto the sidecarPath -> validateID path.
	big := strings.Repeat("x", testCap*2)
	ctx := ctxWithRunDir("sess-tr", "..", t.TempDir())
	if _, err := NewResult(ctx, big); err == nil {
		t.Fatal("want error: a tool_call_id containing '..' must be rejected before any write")
	}
}

// TestValidateID_Empty covers validateID's empty-id branch via a spill whose
// session_id is empty (forcing the >cap output onto the sidecarPath -> validateID).
func TestValidateID_Empty(t *testing.T) {
	big := strings.Repeat("y", testCap*2)
	ctx := ctxWithRunDir("", "call-empty", t.TempDir())
	if _, err := NewResult(ctx, big); err == nil {
		t.Fatal("want error: an empty session_id must be rejected by validateID")
	}
}

// TestRenderText covers Registry.RenderText (the human-readable manifest used by
// the `aura tools` listing / boot logs), exercising both the deferred and active
// rendering branches.
func TestRenderText(t *testing.T) {
	r := NewRegistry()
	r.Register(TextResponse{}) // active (non-deferred)
	r.Register(deferredTool{}) // deferred
	out := r.RenderText()
	if !strings.Contains(out, "[active]") {
		t.Errorf("RenderText missing the [active] marker for a non-deferred tool:\n%s", out)
	}
	if !strings.Contains(out, "[deferred]") {
		t.Errorf("RenderText missing the [deferred] marker for a deferred tool:\n%s", out)
	}
	if !strings.Contains(out, "text_response") || !strings.Contains(out, "deferred_demo") {
		t.Errorf("RenderText missing a tool name:\n%s", out)
	}
}

// deferredTool is a minimal Deferred:true tool so RenderText's [deferred] branch
// has a row to render.
type deferredTool struct{}

func (deferredTool) Spec() Spec {
	return Spec{
		Name:        "deferred_demo",
		Summary:     "A deferred demo tool.",
		Description: "A long description that stays hidden until tool_search loads it.",
		Parameters:  []byte(`{"type":"object"}`),
		Deferred:    true,
	}
}

func (deferredTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}
