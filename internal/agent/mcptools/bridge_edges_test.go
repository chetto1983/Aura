package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// TestConfiguredMCPCallTimeout covers timeout.go's parse branches: unset →
// default, a valid float → scaled duration, "0" → 0 (no deadline), and an
// invalid/negative value → error.
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
	t.Run("minus one is rejected", func(t *testing.T) {
		t.Setenv(envMCPCallTimeoutSec, "-1")
		if _, err := configuredMCPCallTimeout(); err == nil {
			t.Fatal("-1 must not permit an unbounded MCP call")
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

// TestBridge_BadTimeoutEnvFailsBeforeListTools covers boot-time timeout
// validation: an unparseable AURA_MCP_CALL_TIMEOUT_SEC fails before the mount
// even lists tools.
func TestBridge_BadTimeoutEnvFailsBeforeListTools(t *testing.T) {
	t.Setenv(envMCPCallTimeoutSec, "garbage")
	srv, _ := newInMemoryMounted(t, sandboxTools()...)

	got, err := Bridge(context.Background(), "sb", srv)
	if err == nil {
		t.Fatal("a bad timeout-config env must fail Bridge")
	}
	if got != nil {
		t.Fatalf("Bridge should not return tools on timeout config failure, got %v", got)
	}
}

// TestBridge_TimeoutMinusOneFailsBeforeListTools covers the Amendment #100
// finite-deadline contract: the former infinite timeout is rejected at mount.
func TestBridge_TimeoutMinusOneFailsBeforeListTools(t *testing.T) {
	t.Setenv(envMCPCallTimeoutSec, "-1")
	srv, _ := newInMemoryMounted(t, sandboxTools()...)
	got, err := Bridge(context.Background(), "sb", srv)
	if err == nil || got != nil {
		t.Fatalf("Bridge(-1) = (%v, %v), want nil tools and validation error", got, err)
	}
}

// TestBridgedTool_Execute_NilArgsSkipsUnmarshal covers Execute's len(raw)==0
// branch: a nil/empty raw payload calls the tool with nil args and never
// attempts a JSON unmarshal.
func TestBridgedTool_Execute_NilArgsSkipsUnmarshal(t *testing.T) {
	srv, _ := newInMemoryMounted(t, sandboxTools()...)
	got, _ := Bridge(context.Background(), "sb", srv)
	ctx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)

	res, err := got[0].Execute(ctx, nil)
	if err != nil {
		t.Fatalf("Execute with nil args: %v", err)
	}
	if !strings.HasPrefix(res.Preview, "sandbox_exec:") {
		t.Fatalf("preview = %q, want routed to sandbox_exec", res.Preview)
	}
}

// TestBridgedTool_Execute_MissingToolCallContextIsGoError covers newResult's
// NewResult error branch: when Execute runs without a tool-call context,
// NewResult returns its "missing tool-call context" Go error and Execute
// surfaces it (not as inline content).
func TestBridgedTool_Execute_MissingToolCallContextIsGoError(t *testing.T) {
	srv, _ := newInMemoryMounted(t, sandboxTools()...)
	got, _ := Bridge(context.Background(), "sb", srv)

	_, err := got[0].Execute(context.Background(), json.RawMessage(`{"container_id":"abc"}`))
	if err == nil {
		t.Fatal("a missing tool-call context must surface as a Go error from NewResult")
	}
	if !strings.Contains(err.Error(), "tool-call context") {
		t.Fatalf("error should name the missing tool-call context, got %v", err)
	}
}

// TestSchemaJSON_NilFallsBackToEmptyObject covers schemaJSON's own fallback: a
// nil *sdkmcp.Tool.InputSchema — the "server sent nothing" shape a real fixture
// cannot produce (AddTool panics on a nil schema) — falls back to the
// empty-object schema.
func TestSchemaJSON_NilFallsBackToEmptyObject(t *testing.T) {
	got := schemaJSON(&sdkmcp.Tool{Name: "x", InputSchema: nil})
	if string(got) != `{"type":"object"}` {
		t.Fatalf("schemaJSON(nil InputSchema) = %s, want the empty-object fallback", got)
	}
}

// TestSchemaJSON_RoundTripsNestedSchema covers the real shape-change from the
// deleted mcp.ToolDef's json.RawMessage InputSchema to the SDK's `any`: a nested
// schema round-trips byte-equivalent through json.Marshal.
func TestSchemaJSON_RoundTripsNestedSchema(t *testing.T) {
	nested := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "search text"},
		},
		"required": []any{"query"},
	}
	got := schemaJSON(&sdkmcp.Tool{Name: "x", InputSchema: nested})
	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("schemaJSON output is not valid JSON: %v", err)
	}
	props, _ := parsed["properties"].(map[string]any)
	if props == nil {
		t.Fatalf("round-tripped schema lost properties: %s", got)
	}
	query, _ := props["query"].(map[string]any)
	if query["description"] != "search text" {
		t.Fatalf("round-tripped schema lost the nested description: %s", got)
	}
}

// TestCapSchemaDescriptions_InvalidJSONFallsBackToEmptyObject covers
// capSchemaDescriptions' unmarshal-failure branch directly: a malformed schema
// must NOT be forwarded raw — the bridge falls back to the safe empty-object
// schema rather than carrying server bytes.
func TestCapSchemaDescriptions_InvalidJSONFallsBackToEmptyObject(t *testing.T) {
	got := capSchemaDescriptions(json.RawMessage(`{not json`))
	if string(got) != `{"type":"object"}` {
		t.Fatalf("malformed schema should fall back to empty-object, got %s", got)
	}
}

// TestCapDescriptions_DepthGuard covers capDescriptions' depth bound: a schema
// nested deeper than the guard (16) stops recursing without panicking and the
// resulting parameters stay valid JSON. The deep "description" past the guard is
// left untouched (not truncated), which is the documented stack-safety tradeoff.
func TestCapDescriptions_DepthGuard(t *testing.T) {
	const depth = 40
	longDesc := strings.Repeat("Z", maxMCPArgDescBytes+200)
	var sb strings.Builder
	sb.WriteString(`{"type":"object"`)
	for range depth {
		sb.WriteString(`,"x":{"type":"object"`)
	}
	sb.WriteString(`,"description":"` + longDesc + `"`)
	for range depth {
		sb.WriteString(`}`)
	}
	sb.WriteString(`}`)
	schema := json.RawMessage(sb.String())
	if !json.Valid(schema) {
		t.Fatalf("test fixture is not valid JSON: %s", schema[:60])
	}
	got := capSchemaDescriptions(schema)
	if !json.Valid(got) {
		t.Fatalf("parameters not valid JSON after deep walk: %s", got)
	}
}

// TestCapDescriptions_ShallowDescriptionStillCapped is the positive control for
// the depth guard: a description ABOVE the guard depth is still truncated,
// proving the guard didn't short-circuit the whole walk.
func TestCapDescriptions_ShallowDescriptionStillCapped(t *testing.T) {
	longDesc := strings.Repeat("Q", maxMCPArgDescBytes+200)
	schema := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string","description":"` + longDesc + `"}}}`)
	got := capSchemaDescriptions(schema)
	var parsed struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(got, &parsed); err != nil {
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

func TestCapSchemaDescriptions_OverLargeSchemaFallsBackToEmptyObject(t *testing.T) {
	huge := json.RawMessage(`{"type":"object","description":"` + strings.Repeat("x", maxMCPSchemaBytes) + `"}`)
	got := capSchemaDescriptions(huge)
	if string(got) != `{"type":"object"}` {
		t.Fatalf("oversized schema should fall back to empty-object, got %s", got)
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
	got := capSchemaDescriptions(json.RawMessage(schemaBytes))
	if string(got) != `{"type":"object"}` {
		t.Fatalf("wide schema should fall back to empty-object, got %s", got)
	}
}

func TestCapMCPErrorContent(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	longErr := errors.New(strings.Repeat("E", maxMCPErrorPreviewBytes*2))
	server.AddTool(sandboxTools()[0], func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return nil, longErr
	})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	srv := NewMountedServer("fixture", nil)
	session, err := connectClient(ctx, clientTransport, mcpSessionOptionsFor(srv))
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	srv.Attach(session)

	got, _ := Bridge(ctx, "sb", srv)
	callCtx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), maxMCPErrorPreviewBytes*4)

	_, execErr := got[0].Execute(callCtx, json.RawMessage(`{}`))
	if execErr == nil {
		t.Fatal("Execute returned nil error")
	}
	if len(execErr.Error()) > maxMCPErrorPreviewBytes {
		t.Fatalf("error exceeded cap: len=%d cap=%d", len(execErr.Error()), maxMCPErrorPreviewBytes)
	}
	if !strings.HasSuffix(execErr.Error(), mcpErrorTruncated) {
		t.Fatalf("capped error should carry truncation marker, got suffix %.40q", execErr.Error()[max(len(execErr.Error())-40, 0):])
	}
}

func TestFrameMCPSummary_HugeRequiredHintTruncatesHint(t *testing.T) {
	required := make([]string, 0, 200)
	for range 200 {
		required = append(required, "arg_name_"+strings.Repeat("x", 8))
	}
	reqJSON, err := json.Marshal(required)
	if err != nil {
		t.Fatalf("marshal required: %v", err)
	}
	schema := json.RawMessage(`{"type":"object","required":` + string(reqJSON) + `}`)
	var schemaMap map[string]any
	if err := json.Unmarshal(schema, &schemaMap); err != nil {
		t.Fatalf("unmarshal schema fixture: %v", err)
	}

	spec := specFromToolDef("x", &sdkmcp.Tool{
		Name:        "manyargs",
		Description: "Tool with a huge required list.",
		InputSchema: schemaMap,
	})
	if len(spec.Summary) > maxMCPSummaryBytes {
		t.Fatalf("summary must stay within the cap, got %d bytes (cap %d)", len(spec.Summary), maxMCPSummaryBytes)
	}
	if strings.Contains(strings.ToLower(spec.Summary), "untrusted") {
		t.Fatalf("summary must not carry distrust framing, got %.80q", spec.Summary)
	}
	if !strings.Contains(spec.Summary, "summary truncated") {
		t.Fatalf("over-budget summary should carry the truncation marker, got %.80q", spec.Summary)
	}
	if !strings.Contains(spec.Summary, "Required args:") {
		t.Fatalf("the (truncated) hint should still begin with the required-args label, got %.80q", spec.Summary)
	}
}

// TestFrameMCPSummary_BudgetTruncatesSummaryKeepsHint covers frameMCPSummary's
// primary truncation branch (budget >= 0): a long description with a small
// required hint truncates the SUMMARY while keeping the hint intact.
func TestFrameMCPSummary_BudgetTruncatesSummaryKeepsHint(t *testing.T) {
	longDesc := strings.Repeat("d", 5000)
	spec := specFromToolDef("x", &sdkmcp.Tool{
		Name:        "longdesc",
		Description: longDesc,
		InputSchema: map[string]any{"type": "object", "required": []any{"target"}},
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

// TestTruncateUTF8Bytes covers truncateUTF8Bytes' edge branches: maxBytes<=0 →
// "", len(s)<=maxBytes → s unchanged, and a multibyte cut backs off to a valid
// boundary.
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
