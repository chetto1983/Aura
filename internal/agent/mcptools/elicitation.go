package mcptools

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/obs"
	"github.com/chetto1983/aura/internal/redact"
)

// elicitation.go answers SEP-2322 server-initiated elicitation. A form a mounted
// server asks for inside a cockpit run is put to that run's operator
// (elicitation_route.go), and the call waits, with its clocks held, for the
// answer. Anything Aura cannot place in one run falls back to
// decline-and-surface: the ElicitationConsent the composition root wires declines
// and tells the operator on their channel.
//
// Aura writes the handler body and nothing else. The SDK owns the multi-round-trip
// loop (go-sdk@v1.8.0 mcp/mrtr.go:73-118), the classic elicitation/create request,
// and the schema checks made before and after the handler (mcp/client.go:869-920).

// elicitModeURL is the one mode Aura refuses without consulting anyone. Opening a
// server-supplied URL that the operator reads as Aura-sanctioned is a phishing
// primitive (T-45.1-31), so the URL is never rendered anywhere, not even in a log.
// Hermes declines it for the same reason (NousResearch/hermes-agent@7b761da2d
// tools/mcp_tool_sampling.py:292-295).
const elicitModeURL = "url"

const envMCPElicitationTimeoutSec = "AURA_MCP_ELICITATION_TIMEOUT_SEC"

// defaultElicitationTimeout matches Hermes' default (tools/mcp_tool_sampling.py:262
// at the same commit) and the value recorded in 45.1-06-SUMMARY.md.
//
// A configured value <= 0 DISABLES elicitation: the handler declines at once. It
// does NOT mean "wait forever", the reading a future reader will assume and the
// dangerous one: an unbounded wait holds the call, and with it the turn, until the
// server gives up.
const defaultElicitationTimeout = 300 * time.Second

// maxLoggedValueBytes caps a server- or surface-supplied value that reaches a log
// line: an unrecognised action, a malformed timeout.
const maxLoggedValueBytes = 32

// maxLoggedErrorBytes caps the error on the resolved line. A refusal's error is
// FromSchema's, and it quotes server-supplied names and options.
const maxLoggedErrorBytes = 256

// The boundary reuses the MCP call counter with its own operation value rather
// than registering a second instrument: obs exposes emission ONLY through
// Boundary, whose outcome is derived from the error (nil→success,
// context.Canceled→canceled, context.DeadlineExceeded→timeout, else error). The
// action and the reason ride the structured log line instead, which keeps a
// server-supplied string out of a metric dimension.
var mcpElicitationBoundary = obs.NewGlobalBoundary("github.com/chetto1983/aura/internal/agent/mcptools", obs.BoundaryConfig{
	Operation: "mcp_elicitation", ToolClass: obs.ToolClassMCP, Transport: "in_process",
	Count: obs.MCPCallsID, Duration: obs.MCPDurationID,
})

// ElicitationConsent is the fallback for a request no run can be asked: the
// composition root's decline-and-surface. It is declared here, consumer-side, so
// this package imports neither internal/runner nor internal/channels.
//
// It receives the bounded elicit.Question, never the SDK params, so a
// composition-root implementation cannot reach the raw schema or a URL even by
// mistake. Returning anything other than "accept" yields a non-accept result, and
// an error, a panic or a surface that never returns all decline or cancel.
type ElicitationConsent interface {
	AskOperator(ctx context.Context, q elicit.Question) (action string, content map[string]any, err error)
}

// elicitOutcome is what the handler answers and records. err is for
// observability only: the SDK never sees it.
type elicitOutcome struct {
	action  string
	content map[string]any
	fields  int
	reason  string
	err     error
}

// NewElicitationHandler builds the closure mcp.SessionOptions.Elicitation takes.
//
// It returns (*ElicitResult, nil) in EVERY case. An error returned from here
// propagates through fulfillInputRequests (go-sdk@v1.8.0 mcp/mrtr.go:273-305) and
// fails the whole CallTool with an opaque message, where a decline hands the
// server a protocol answer it can respond to.
func NewElicitationHandler(server string, consent ElicitationConsent) func(context.Context, *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error) {
	return func(ctx context.Context, req *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error) {
		ctx, end := mcpElicitationBoundary.Start(ctx)
		out := decideElicitation(ctx, server, consent, req)
		end.End(out.err)
		level := slog.LevelInfo
		if out.err != nil {
			level = slog.LevelWarn
		}
		// The operator's values never reach a log: the action, the server and the
		// field count do.
		slog.Log(ctx, level, "mcp elicitation resolved", "server", redact.Line(server),
			"action", out.action, "fields", out.fields, "reason", out.reason, "err", loggedError(out.err))
		return &sdkmcp.ElicitResult{Action: out.action, Content: out.content}, nil
	}
}

func decideElicitation(ctx context.Context, server string, consent ElicitationConsent, req *sdkmcp.ElicitRequest) elicitOutcome {
	if req == nil || req.Params == nil {
		return elicitOutcome{action: elicit.ActionDecline, reason: "no params"}
	}
	params := req.Params
	if strings.EqualFold(strings.TrimSpace(params.Mode), elicitModeURL) {
		// The URL is deliberately absent from every record. T-45.1-31.
		return elicitOutcome{action: elicit.ActionDecline, reason: "url mode is refused"}
	}
	timeout := configuredElicitationTimeout()
	if timeout <= 0 {
		return elicitOutcome{action: elicit.ActionDecline, reason: "disabled by " + envMCPElicitationTimeoutSec}
	}
	// A server past its open questions is declined without a card or a channel
	// message: telling the operator once per request would be the flood itself.
	done, ok := inFlight.ask(req.Session)
	if !ok {
		return elicitOutcome{action: elicit.ActionDecline, reason: "too many open questions on the session"}
	}
	defer done()

	r := routeFor(ctx, req.Session)
	q, err := questionFor(server, r.tool, params)
	switch {
	case err != nil:
		out := refuse(ctx, r, consent, q, elicit.RefusalUnrenderable, timeout)
		out.err = err
		return out
	case r.mixed:
		return refuse(ctx, r, consent, q, elicit.RefusalAmbiguousRun, timeout)
	case r.asker != nil:
		return askRun(ctx, r, q, timeout)
	default:
		return askFallback(r.fallbackContext(ctx), consent, q, timeout)
	}
}

func questionFor(server, tool string, params *sdkmcp.ElicitParams) (elicit.Question, error) {
	schema, err := elicit.DecodeSchema(params.RequestedSchema)
	if err != nil {
		return elicit.Question{Server: server, Tool: tool}, err
	}
	return elicit.FromSchema(server, tool, params.Message, schema)
}

// answered maps an operator's decision onto the protocol. Content rides only an
// accept, and an action the protocol does not define declines: an unrecognised
// action is not a permissive one.
func answered(a elicit.Answer, fields int) elicitOutcome {
	switch a.Action {
	case elicit.ActionAccept:
		return elicitOutcome{action: elicit.ActionAccept, content: a.Content, fields: fields, reason: "answered"}
	case elicit.ActionDecline, elicit.ActionCancel:
		return elicitOutcome{action: a.Action, fields: fields, reason: "answered"}
	default:
		return elicitOutcome{action: elicit.ActionDecline, fields: fields,
			reason: "unrecognised action " + redact.Line(strconv.Quote(truncateUTF8Bytes(a.Action, maxLoggedValueBytes)))}
	}
}

// loggedError is err as the resolved line records it, redacted and capped like
// every other server-supplied string in this file.
func loggedError(err error) string {
	if err == nil {
		return ""
	}
	return redact.Line(truncateUTF8Bytes(err.Error(), maxLoggedErrorBytes))
}

// configuredElicitationTimeout reads AURA_MCP_ELICITATION_TIMEOUT_SEC, mirroring
// configuredMCPCallTimeout's convention in timeout.go.
//
// A value <= 0 returns 0, which the handler reads as DISABLED, not infinite. An
// unparseable value falls back to the default rather than failing the mount: a
// malformed knob must not stop a server from being usable.
func configuredElicitationTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv(envMCPElicitationTimeoutSec))
	if raw == "" {
		return defaultElicitationTimeout
	}
	sec, err := strconv.Atoi(raw)
	if err != nil {
		slog.Warn("ignoring malformed elicitation timeout",
			"env", envMCPElicitationTimeoutSec, "value", truncateUTF8Bytes(raw, maxLoggedValueBytes))
		return defaultElicitationTimeout
	}
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}
