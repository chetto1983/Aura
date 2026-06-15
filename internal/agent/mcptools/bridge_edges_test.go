package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

// TestConfiguredMCPCallTimeout covers timeout.go's parse branches: unset → default,
// a valid float → scaled duration, "0" → 0 (no deadline), and an invalid/negative
// value → error.
func TestConfiguredMCPCallTimeout(t *testing.T) {
	t.Run("unset returns default", func(t *testing.T) {
		t.Setenv(envMCPCallTimeoutSec, "")
		d, err := configuredMCPCallTimeout()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d != defaultMCPCallTimeout {
			t.Fatalf("unset should return default %v, got %v", defaultMCPCallTimeout, d)
		}
	})
	t.Run("valid float scales to duration", func(t *testing.T) {
		t.Setenv(envMCPCallTimeoutSec, "1.5")
		d, err := configuredMCPCallTimeout()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d != 1500*time.Millisecond {
			t.Fatalf("1.5 should be 1.5s, got %v", d)
		}
	})
	t.Run("zero returns default", func(t *testing.T) {
		t.Setenv(envMCPCallTimeoutSec, "0")
		d, err := configuredMCPCallTimeout()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d != defaultMCPCallTimeout {
			t.Fatalf("0 should return default %v, got %v", defaultMCPCallTimeout, d)
		}
	})
	t.Run("minus one disables the deadline", func(t *testing.T) {
		t.Setenv(envMCPCallTimeoutSec, "-1")
		d, err := configuredMCPCallTimeout()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d != 0 {
			t.Fatalf("-1 should disable the deadline (return 0), got %v", d)
		}
	})
	t.Run("non-numeric is an error", func(t *testing.T) {
		t.Setenv(envMCPCallTimeoutSec, "not-a-number")
		if _, err := configuredMCPCallTimeout(); err == nil {
			t.Fatal("a non-numeric value must be an error")
		}
	})
	t.Run("less than minus one is an error", func(t *testing.T) {
		t.Setenv(envMCPCallTimeoutSec, "-5")
		if _, err := configuredMCPCallTimeout(); err == nil {
			t.Fatal("a value less than -1 must be an error")
		}
	})
}

// TestBridge_BadTimeoutEnvFailsBeforeListTools covers boot-time timeout validation:
// an unparseable AURA_MCP_CALL_TIMEOUT_SEC fails before tools/list, and the server
// is never reached.
func TestBridge_BadTimeoutEnvFailsBeforeListTools(t *testing.T) {
	t.Setenv(envMCPCallTimeoutSec, "garbage")
	srv := &fakeServer{defs: sandboxDefs(), callText: "unused"}

	got, err := Bridge(context.Background(), "sb", srv)
	if err == nil {
		t.Fatal("a bad timeout-config env must fail Bridge")
	}
	if got != nil {
		t.Fatalf("Bridge should not return tools on timeout config failure, got %v", got)
	}
	if srv.lastName != "" {
		t.Fatalf("the server must not be called when timeout config fails, got call to %q", srv.lastName)
	}
}

// TestBridgedTool_Execute_NoDeadlineWhenTimeoutMinusOne covers Execute's
// timeout==0 branch: with the deadline explicitly disabled the call still routes
// through and returns the scripted text (no WithTimeout wrapper is applied).
func TestBridgedTool_Execute_NoDeadlineWhenTimeoutMinusOne(t *testing.T) {
	t.Setenv(envMCPCallTimeoutSec, "-1")
	var sawDeadline bool
	srv := &fakeServer{defs: sandboxDefs(), callText: "ok"}
	srv.CallToolFunc = func(ctx context.Context, _ string, _ map[string]any) (string, error) {
		_, sawDeadline = ctx.Deadline()
		return "ok", nil
	}
	got, _ := Bridge(context.Background(), "sb", srv)
	ctx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)

	res, err := got[0].Execute(ctx, json.RawMessage(`{"container_id":"abc"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Preview != "ok" {
		t.Fatalf("preview = %q, want ok", res.Preview)
	}
	if sawDeadline {
		t.Fatal("timeout=-1 must NOT install a context deadline")
	}
}

// TestBridgedTool_Execute_NilArgsSkipsUnmarshal covers Execute's len(raw)==0 branch:
// a nil/empty raw payload calls the tool with nil args and never attempts a JSON
// unmarshal.
func TestBridgedTool_Execute_NilArgsSkipsUnmarshal(t *testing.T) {
	srv := &fakeServer{defs: sandboxDefs(), callText: "pong"}
	got, _ := Bridge(context.Background(), "sb", srv)
	ctx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)

	res, err := got[0].Execute(ctx, nil)
	if err != nil {
		t.Fatalf("Execute with nil args: %v", err)
	}
	if res.Preview != "pong" {
		t.Fatalf("preview = %q, want pong", res.Preview)
	}
	if srv.lastArgs != nil {
		t.Fatalf("empty raw payload should forward nil args, got %+v", srv.lastArgs)
	}
}

// TestBridgedTool_Execute_MissingToolCallContextIsGoError covers newUntrustedResult's
// NewResult error branch: when Execute runs without a tool-call context, NewResult
// returns its "missing tool-call context" Go error and Execute surfaces it (not as
// inline content).
func TestBridgedTool_Execute_MissingToolCallContextIsGoError(t *testing.T) {
	srv := &fakeServer{defs: sandboxDefs(), callText: "ok"}
	got, _ := Bridge(context.Background(), "sb", srv)

	// No WithToolCallContext on this ctx → NewResult fails inside newUntrustedResult.
	_, err := got[0].Execute(context.Background(), json.RawMessage(`{"container_id":"abc"}`))
	if err == nil {
		t.Fatal("a missing tool-call context must surface as a Go error from NewResult")
	}
	if !strings.Contains(err.Error(), "tool-call context") {
		t.Fatalf("error should name the missing tool-call context, got %v", err)
	}
}

// TestCapSchemaDescriptions_InvalidJSONFallsBackToEmptyObject covers capSchemaDescriptions'
// unmarshal-failure branch: a malformed inputSchema must NOT be forwarded raw — the
// bridge falls back to the safe empty-object schema rather than carrying server bytes.
func TestCapSchemaDescriptions_InvalidJSONFallsBackToEmptyObject(t *testing.T) {
	// A non-whitespace, non-"null" but malformed schema reaches capSchemaDescriptions.
	srv := &fakeServer{defs: []mcp.ToolDef{
		{Name: "broken", Description: "Broken schema.", InputSchema: json.RawMessage(`{not json`)},
	}}
	got, err := Bridge(context.Background(), "srv", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	if string(got[0].Spec().Parameters) != `{"type":"object"}` {
		t.Fatalf("malformed schema should fall back to empty-object, got %s", got[0].Spec().Parameters)
	}
}

// TestCapDescriptions_DepthGuard covers capDescriptions' depth bound: a schema
// nested deeper than the guard (16) stops recursing without panicking and the
// resulting parameters stay valid JSON. The deep "description" past the guard is
// left untouched (not truncated), which is the documented stack-safety tradeoff.
func TestCapDescriptions_DepthGuard(t *testing.T) {
	// Build a 40-level-deep nested object whose innermost node holds a long description.
	const depth = 40
	longDesc := strings.Repeat("Z", maxMCPArgDescBytes+200)
	var sb strings.Builder
	sb.WriteString(`{"type":"object"`)
	for i := 0; i < depth; i++ {
		sb.WriteString(`,"x":{"type":"object"`)
	}
	sb.WriteString(`,"description":"` + longDesc + `"`)
	for i := 0; i < depth; i++ {
		sb.WriteString(`}`)
	}
	sb.WriteString(`}`)
	schema := json.RawMessage(sb.String())
	if !json.Valid(schema) {
		t.Fatalf("test fixture is not valid JSON: %s", schema[:60])
	}

	srv := &fakeServer{defs: []mcp.ToolDef{
		{Name: "deep", Description: "Deeply nested.", InputSchema: schema},
	}}
	got, err := Bridge(context.Background(), "srv", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	// The guard must not panic and must leave valid JSON.
	if !json.Valid(got[0].Spec().Parameters) {
		t.Fatalf("parameters not valid JSON after deep walk: %s", got[0].Spec().Parameters)
	}
}

// TestCapDescriptions_ShallowDescriptionStillCapped is the positive control for the
// depth guard: a description ABOVE the guard depth is still truncated, proving the
// guard didn't short-circuit the whole walk.
func TestCapDescriptions_ShallowDescriptionStillCapped(t *testing.T) {
	longDesc := strings.Repeat("Q", maxMCPArgDescBytes+200)
	schema := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string","description":"` + longDesc + `"}}}`)
	srv := &fakeServer{defs: []mcp.ToolDef{
		{Name: "search", Description: "Search.", InputSchema: schema},
	}}
	got, err := Bridge(context.Background(), "srv", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	var parsed struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(got[0].Spec().Parameters, &parsed); err != nil {
		t.Fatalf("parameters not valid JSON: %v", err)
	}
	desc := parsed.Properties["q"].Description
	if len(desc) > maxMCPArgDescBytes+len(mcpArgDescTruncated) {
		t.Fatalf("shallow description not capped: %d bytes", len(desc))
	}
	if !strings.HasSuffix(desc, mcpArgDescTruncated) {
		t.Fatalf("capped description should carry the truncation marker, got %q", desc[len(desc)-20:])
	}
}

// TestFrameMCPSummary_HugeRequiredHintTruncatesHint covers frameMCPSummary's
// hint-overflow branch (budget < 0 → fall through to truncating the hint itself):
// a tool with a required-args list long enough to blow the summary budget on its
// own must still produce a bounded, framed summary that keeps the untrusted prefix.
func TestCapSchemaDescriptions_OverLargeSchemaFallsBackToEmptyObject(t *testing.T) {
	huge := json.RawMessage(`{"type":"object","description":"` + strings.Repeat("x", maxMCPSchemaBytes) + `"}`)
	srv := &fakeServer{defs: []mcp.ToolDef{
		{Name: "huge", Description: "Huge schema.", InputSchema: huge},
	}}
	got, err := Bridge(context.Background(), "srv", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	if string(got[0].Spec().Parameters) != `{"type":"object"}` {
		t.Fatalf("oversized schema should fall back to empty-object, got %s", got[0].Spec().Parameters)
	}
}

func TestCapSchemaDescriptions_TooManyPropertiesFallsBackToEmptyObject(t *testing.T) {
	props := make(map[string]any, maxMCPSchemaProperties+1)
	for i := 0; i <= maxMCPSchemaProperties; i++ {
		props["field_"+strconv.Itoa(i)] = map[string]any{"type": "string"}
	}
	schemaBytes, err := json.Marshal(map[string]any{"type": "object", "properties": props})
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	srv := &fakeServer{defs: []mcp.ToolDef{
		{Name: "wide", Description: "Wide schema.", InputSchema: json.RawMessage(schemaBytes)},
	}}
	got, err := Bridge(context.Background(), "srv", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	if string(got[0].Spec().Parameters) != `{"type":"object"}` {
		t.Fatalf("wide schema should fall back to empty-object, got %s", got[0].Spec().Parameters)
	}
}

func TestCapMCPErrorContent(t *testing.T) {
	srv := &fakeServer{defs: sandboxDefs(), callErr: errors.New(strings.Repeat("E", maxMCPErrorPreviewBytes*2))}
	got, err := Bridge(context.Background(), "sb", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	ctx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), maxMCPErrorPreviewBytes*4)

	res, err := got[0].Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Preview) > maxMCPErrorPreviewBytes {
		t.Fatalf("error preview exceeded cap: len=%d cap=%d", len(res.Preview), maxMCPErrorPreviewBytes)
	}
	if !strings.HasSuffix(res.Preview, mcpErrorTruncated) {
		t.Fatalf("capped error should carry truncation marker, got suffix %.40q", res.Preview[len(res.Preview)-40:])
	}
}

func TestFrameMCPSummary_HugeRequiredHintTruncatesHint(t *testing.T) {
	// Build a required list whose rendered hint exceeds the summary budget (~713
	// bytes after the fixed prefix+marker), forcing the budget<0 path.
	required := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		required = append(required, "arg_name_"+strings.Repeat("x", 8))
	}
	reqJSON, err := json.Marshal(required)
	if err != nil {
		t.Fatalf("marshal required: %v", err)
	}
	schema := json.RawMessage(`{"type":"object","required":` + string(reqJSON) + `}`)

	spec := specFromToolDef("x", mcp.ToolDef{
		Name:        "manyargs",
		Description: "Tool with a huge required list.",
		InputSchema: schema,
	})
	if len(spec.Summary) > maxMCPSummaryBytes {
		t.Fatalf("summary must stay within the cap, got %d bytes (cap %d)", len(spec.Summary), maxMCPSummaryBytes)
	}
	if !strings.Contains(spec.Summary, "untrusted MCP server summary data:") {
		t.Fatalf("framed summary must keep the untrusted prefix, got %.80q", spec.Summary)
	}
	if !strings.Contains(spec.Summary, "Required args:") {
		t.Fatalf("the (truncated) hint should still begin with the required-args label, got %.80q", spec.Summary)
	}
}

// TestFrameMCPSummary_BudgetTruncatesSummaryKeepsHint covers frameMCPSummary's
// primary truncation branch (budget >= 0): a long description with a small required
// hint truncates the SUMMARY (carrying the [summary truncated] marker) while keeping
// the hint intact.
func TestFrameMCPSummary_BudgetTruncatesSummaryKeepsHint(t *testing.T) {
	longDesc := strings.Repeat("d", 5000)
	schema := json.RawMessage(`{"type":"object","required":["target"]}`)
	spec := specFromToolDef("x", mcp.ToolDef{
		Name:        "longdesc",
		Description: longDesc,
		InputSchema: schema,
	})
	if len(spec.Summary) > maxMCPSummaryBytes {
		t.Fatalf("summary must stay within the cap, got %d bytes", len(spec.Summary))
	}
	if !strings.Contains(spec.Summary, "[summary truncated]") {
		t.Fatalf("a long summary should carry the truncation marker, got %.120q", spec.Summary)
	}
	if !strings.Contains(spec.Summary, "Required args: target.") {
		t.Fatalf("the required-args hint must survive summary truncation, got %.120q", spec.Summary)
	}
}

// TestTruncateUTF8Bytes covers truncateUTF8Bytes' edge branches: maxBytes<=0 → "",
// len(s)<=maxBytes → s unchanged, and a multibyte cut backs off to a valid boundary.
func TestTruncateUTF8Bytes(t *testing.T) {
	if got := truncateUTF8Bytes("hello", 0); got != "" {
		t.Fatalf("maxBytes=0 should yield empty, got %q", got)
	}
	if got := truncateUTF8Bytes("hello", -1); got != "" {
		t.Fatalf("negative maxBytes should yield empty, got %q", got)
	}
	if got := truncateUTF8Bytes("hi", 10); got != "hi" {
		t.Fatalf("len(s)<=maxBytes should return s unchanged, got %q", got)
	}
	// "é" is 2 bytes; cutting at 1 byte must back off to "".
	if got := truncateUTF8Bytes("é", 1); got != "" {
		t.Fatalf("a mid-rune cut should back off to a valid boundary, got %q", got)
	}
}
