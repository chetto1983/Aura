package mcptools

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/obs"
)

// elicitation.go implements SEP-2322 server-initiated elicitation for the
// surface plan 45.1-06 recorded: decline-and-surface. The handler ALWAYS
// declines, but delivers the ask to the operator so an otherwise-invisible
// refusal becomes visible.
//
// This closes no security hole. With no handler registered the SDK already
// refuses on Aura's behalf — c.elicit returns
// &jsonrpc.Error{Code: CodeInvalidParams, Message: "client does not support
// elicitation"} (go-sdk@v1.7.0 mcp/client.go:862-864) and does NOT auto-fulfil.
// What registering a handler buys is protocol compliance and operator sight:
// today a server that needs input is refused and nobody ever learns why, so a
// legitimate multi-round-trip tool simply cannot work.
//
// Aura writes the handler body and nothing else. clientMultiRoundTripMiddleware
// (go-sdk@v1.7.0 mcp/mrtr.go:73-115) already owns the fulfil-and-retry loop, and
// MRTR is enabled by default when ClientOptions.MultiRoundTrip is nil.

// The three action strings the protocol defines (go-sdk@v1.7.0 mcp/protocol.go,
// ElicitResult.Action). Anything else a consent surface returns is treated as
// decline: an unrecognised action is not a permissive one.
const (
	elicitActionAccept  = "accept"
	elicitActionDecline = "decline"
	elicitActionCancel  = "cancel"
)

// elicitModeURL is the one mode Aura refuses without consulting the operator.
// Opening a server-supplied URL that the operator reads as Aura-sanctioned is a
// phishing primitive (T-45.1-31), so the URL is never rendered anywhere — not to
// the operator, not to a log. Hermes declined it for the same reason
// (tools/mcp_tool.py:1720-1731).
const elicitModeURL = "url"

const envMCPElicitationTimeoutSec = "AURA_MCP_ELICITATION_TIMEOUT_SEC"

// defaultElicitationTimeout matches hermes' reference default and the value
// recorded in 45.1-06-SUMMARY.md.
//
// A configured value <= 0 DISABLES elicitation — the handler declines
// immediately. It does NOT mean "wait forever", which is the reading a future
// reader will assume and the dangerous one: an unbounded handler holds the
// in-flight CallTool, and with it the agent turn, until the server gives up.
const defaultElicitationTimeout = 300 * time.Second

// maxElicitationTypeBytes caps the schema-declared type name. Types are short
// JSON-schema keywords; anything longer is a server padding a field it knows
// reaches a human.
const maxElicitationTypeBytes = 32

// The boundary reuses the MCP call counter with its own operation value rather
// than registering a second instrument: obs exposes emission ONLY through
// Boundary, whose outcome is derived from the error (nil→success,
// context.Canceled→canceled, context.DeadlineExceeded→timeout, else error). The
// finer action — url_declined, no_surface, unrecognised — is carried in the
// structured log instead. Widening obs' public surface for one counter would be
// a bespoke addition to a shared package, and the catalog's finite sets are
// precisely what keeps a server-supplied string out of a metric dimension.
var mcpElicitationBoundary = obs.NewGlobalBoundary("github.com/chetto1983/aura/internal/agent/mcptools", obs.BoundaryConfig{
	Operation: "mcp_elicitation", ToolClass: obs.ToolClassMCP, Transport: "in_process",
	Count: obs.MCPCallsID, Duration: obs.MCPDurationID,
})

// ElicitationField is one top-level property of a server's requested schema,
// already bounded and ready to render. Nested objects are not represented: the
// protocol allows only top-level properties (go-sdk@v1.7.0 mcp/protocol.go,
// ElicitParams.RequestedSchema: "Only top-level properties are allowed, without
// nesting").
type ElicitationField struct {
	Name        string
	Type        string
	Required    bool
	Description string
}

// ElicitationRequest is the operator-facing projection of *sdkmcp.ElicitParams:
// the asking server, a byte-capped message, and a rendered field list.
//
// The consent surface receives THIS, never the SDK params. That is deliberate
// and load-bearing: a composition-root implementation cannot reach the raw
// RequestedSchema or the URL even by mistake, so the phishing and flood surfaces
// are closed by the type rather than by everyone downstream remembering.
type ElicitationRequest struct {
	Server  string
	Message string
	Fields  []ElicitationField
}

// ElicitationConsent is the seam the composition root implements. It is declared
// here, consumer-side, so internal/agent/mcptools keeps no dependency on
// internal/runner or internal/channels — either would be an import cycle.
//
// Returning anything other than "accept" yields a non-accept result. Returning
// an error, panicking, or never returning all yield decline or cancel: there is
// no path to accept that does not pass through an operator decision.
type ElicitationConsent interface {
	AskOperator(ctx context.Context, req ElicitationRequest) (action string, content map[string]any, err error)
}

// NewElicitationHandler builds the closure mcp.SessionOptions.Elicitation takes.
//
// It returns (*ElicitResult, nil) in EVERY case and never a non-nil error. An
// error returned from here propagates through fulfillInputRequests
// (go-sdk@v1.7.0 mcp/mrtr.go:233-253) and fails the whole CallTool with an
// opaque message; a decline instead hands the server a protocol answer it can
// respond to gracefully.
func NewElicitationHandler(server string, consent ElicitationConsent) func(context.Context, *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error) {
	return func(ctx context.Context, req *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error) {
		ctx, end := mcpElicitationBoundary.Start(ctx)
		action, content, observed := elicit(ctx, server, consent, req)
		end.End(observed)
		return &sdkmcp.ElicitResult{Action: action, Content: content}, nil
	}
}

// elicit is the decision, split from the handler so every branch returns
// directly and the boundary is closed exactly once. The error it returns is for
// OBSERVABILITY ONLY — the handler never forwards it to the SDK.
func elicit(ctx context.Context, server string, consent ElicitationConsent, req *sdkmcp.ElicitRequest) (string, map[string]any, error) {
	if req == nil || req.Params == nil {
		slog.Warn("mcp elicitation without params", "server", server, "action", elicitActionDecline)
		return elicitActionDecline, nil, nil
	}
	params := req.Params

	if strings.EqualFold(strings.TrimSpace(params.Mode), elicitModeURL) {
		// The URL is deliberately absent from this record. T-45.1-31.
		slog.Info("mcp elicitation declined: url mode is opt-out", "server", server, "action", elicitActionDecline)
		return elicitActionDecline, nil, nil
	}

	timeout := configuredElicitationTimeout()
	if timeout <= 0 {
		slog.Info("mcp elicitation declined: disabled by configuration",
			"server", server, "env", envMCPElicitationTimeoutSec, "action", elicitActionDecline)
		return elicitActionDecline, nil, nil
	}

	if consent == nil {
		slog.Warn("mcp elicitation declined: no consent surface wired",
			"server", server, "action", elicitActionDecline)
		return elicitActionDecline, nil, nil
	}

	askCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	action, content, err := askOperatorBounded(askCtx, consent, ElicitationRequest{
		Server:  server,
		Message: truncateUTF8Bytes(params.Message, maxMCPSummaryBytes),
		Fields:  summariseElicitationSchema(params.RequestedSchema),
	})
	switch {
	case err != nil && askCtx.Err() != nil:
		// The deadline (or a cancelled parent) won, not the surface.
		slog.Warn("mcp elicitation cancelled", "server", server, "action", elicitActionCancel, "err", err)
		return elicitActionCancel, nil, askCtx.Err()
	case err != nil:
		slog.Warn("mcp elicitation declined: consent surface failed",
			"server", server, "action", elicitActionDecline, "err", err)
		return elicitActionDecline, nil, err
	}

	switch action {
	case elicitActionAccept:
		return elicitActionAccept, content, nil
	case elicitActionCancel:
		return elicitActionCancel, nil, nil
	case elicitActionDecline:
		return elicitActionDecline, nil, nil
	default:
		slog.Warn("mcp elicitation declined: consent surface returned an unrecognised action",
			"server", server, "returned", truncateUTF8Bytes(action, maxElicitationTypeBytes), "action", elicitActionDecline)
		return elicitActionDecline, nil, nil
	}
}

// askOperatorBounded runs the consent surface on its own goroutine so a surface
// that ignores ctx — or never returns at all — cannot hold the in-flight agent
// turn open. The result channel is buffered so that goroutine can always finish
// and exit even after this function has already returned on the deadline.
//
// A panicking surface is recovered here rather than in the caller, because the
// panic happens on the goroutine's stack and would otherwise take the process
// down instead of declining.
func askOperatorBounded(ctx context.Context, consent ElicitationConsent, req ElicitationRequest) (string, map[string]any, error) {
	type outcome struct {
		action  string
		content map[string]any
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- outcome{err: &elicitationPanicError{value: r}}
			}
		}()
		action, content, err := consent.AskOperator(ctx, req)
		done <- outcome{action: action, content: content, err: err}
	}()

	select {
	case got := <-done:
		return got.action, got.content, got.err
	case <-ctx.Done():
		return "", nil, ctx.Err()
	}
}

// elicitationPanicError carries a recovered panic as an error so the single
// error path above handles it like any other surface failure.
type elicitationPanicError struct{ value any }

func (e *elicitationPanicError) Error() string {
	return "elicitation consent surface panicked"
}

// configuredElicitationTimeout reads AURA_MCP_ELICITATION_TIMEOUT_SEC, mirroring
// configuredMCPCallTimeout's convention in timeout.go.
//
// A value <= 0 returns 0, which the handler reads as DISABLED, not infinite.
// An unparseable value falls back to the default rather than failing the mount:
// a malformed display knob must not stop a server from being usable.
func configuredElicitationTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv(envMCPElicitationTimeoutSec))
	if raw == "" {
		return defaultElicitationTimeout
	}
	sec, err := strconv.Atoi(raw)
	if err != nil {
		slog.Warn("ignoring malformed elicitation timeout",
			"env", envMCPElicitationTimeoutSec, "value", truncateUTF8Bytes(raw, maxElicitationTypeBytes))
		return defaultElicitationTimeout
	}
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

// summariseElicitationSchema renders a server's requested schema down to its
// top-level fields, sorted by name.
//
// Sorted because Go map iteration is random and an operator prompt whose fields
// reorder on every ask is a prompt nobody can read twice. Anything unparseable,
// non-object, or over the shared maxMCPSchemaBytes cap yields no fields rather
// than an error — the operator still gets a message naming the server.
func summariseElicitationSchema(raw any) []ElicitationField {
	if raw == nil {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil || len(encoded) > maxMCPSchemaBytes {
		return nil
	}
	var schema struct {
		Properties map[string]struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(encoded, &schema); err != nil || len(schema.Properties) == 0 {
		return nil
	}
	fields := make([]ElicitationField, 0, len(schema.Properties))
	for name, prop := range schema.Properties {
		fields = append(fields, ElicitationField{
			Name:        truncateUTF8Bytes(name, maxMCPArgDescBytes),
			Type:        truncateUTF8Bytes(prop.Type, maxElicitationTypeBytes),
			Required:    slices.Contains(schema.Required, name),
			Description: truncateUTF8Bytes(prop.Description, maxMCPArgDescBytes),
		})
	}
	slices.SortFunc(fields, func(a, b ElicitationField) int { return strings.Compare(a.Name, b.Name) })
	return fields
}
