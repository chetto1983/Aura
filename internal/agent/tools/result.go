package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/secret"
)

// ToolCallContext carries the per-Execute identifiers the spillover helper needs
// to place a sidecar file. The agent injects it (via WithToolCallContext) before
// dispatching each tool, so individual tools never reimplement spillover (D-25).
type toolCallContextKey struct{}

type toolCallContext struct {
	sessionID  string
	toolCallID string
	sidecarID  string
	runDir     string
	cap        int
}

// WithToolCallContext returns a ctx carrying the ids + run dir + preview cap the
// spillover helper reads. The agent calls this before each Tool.Execute (D-25).
func WithToolCallContext(ctx context.Context, sessionID, toolCallID, runDir string, previewCap int) context.Context {
	return context.WithValue(ctx, toolCallContextKey{}, toolCallContext{
		sessionID:  sessionID,
		toolCallID: toolCallID,
		sidecarID:  newSidecarID(toolCallID),
		runDir:     runDir,
		cap:        previewCap,
	})
}

func newSidecarID(toolCallID string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return toolCallID + "-" + hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("%s-%x", toolCallID, time.Now().UnixNano())
}

func toolCallCtx(ctx context.Context) (toolCallContext, bool) {
	v, ok := ctx.Value(toolCallContextKey{}).(toolCallContext)
	return v, ok
}

// requestIDCtxKey carries the per-turn request_id (UUIDv7) down to execTool so the
// gateway PEP can build its originating-conversation-keyed ReservationKey. It is set
// ONCE at the top of the agent run loop (WithRequestID); WithToolCallContext — built
// per tool inside runTool, where the request_id is not in scope — deliberately does not.
type requestIDCtxKey struct{}

// WithRequestID returns a ctx carrying the per-turn request_id. The agent sets it once
// per turn so the gateway ReservationKey (conversation + request + tool_call) is
// buildable in execTool without threading the id through every dispatch signature.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDCtxKey{}, requestID)
}

// RequestIDFromContext returns the per-turn request_id set by WithRequestID, or "".
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDCtxKey{}).(string)
	return v
}

// SessionIDFromContext returns the conversation/session id set by
// WithToolCallContext, or "" when no tool-call context is present.
//
// RED stub for Plan 49-08: Task 1's failing contract is committed before the
// implementation so the normal repository-wide vet hook still has a compiling
// public symbol to inspect.
func SessionIDFromContext(context.Context) string {
	return ""
}

// delegatedDispatchCtxKey marks a dispatch whose events no Runner observes --
// set ONLY by internal/swarm's runChild for a worker's own InvocationContext,
// absent everywhere else (a parent turn always flows through a Runner).
//
// This lives here, not in internal/gateway where it originated (delegated
// reservation-closing, 791dcd7e0), because internal/gateway imports
// internal/agent/mcptools (classify.go), and D-10 needs the SAME marker
// readable from internal/agent/mcptools too (to pick the actor's WriterRole
// for a host-derived memory-fact provenance header) -- importing
// internal/gateway from internal/agent/mcptools would cycle back. Both
// packages already depend on internal/agent/tools, so the primitive moved to
// the one place both can reach without inventing a second marker meaning the
// same thing (CLAUDE.md: never duplicate). internal/gateway.WithDelegatedDispatch
// and its unexported reader now call straight through to these two functions,
// so internal/swarm's call site is untouched.
type delegatedDispatchCtxKey struct{}

// WithDelegatedDispatch marks ctx as a delegated (worker) dispatch.
func WithDelegatedDispatch(ctx context.Context) context.Context {
	return context.WithValue(ctx, delegatedDispatchCtxKey{}, true)
}

// IsDelegatedDispatch reports whether ctx was marked by WithDelegatedDispatch.
func IsDelegatedDispatch(ctx context.Context) bool {
	marked, _ := ctx.Value(delegatedDispatchCtxKey{}).(bool)
	return marked
}

// ToolCallIDFromContext returns the tool_call_id set by WithToolCallContext, or "".
func ToolCallIDFromContext(ctx context.Context) string {
	tc, ok := toolCallCtx(ctx)
	if !ok {
		return ""
	}
	return tc.toolCallID
}

// validateID rejects any id outside the sidecar grammar before it is joined into
// a path. The run_dir + "conversations/" prefix is fixed and never
// model-controlled; the model/agent-supplied segments are opaque-id shaped.
func validateID(kind, id string) error {
	if id == "" {
		return fmt.Errorf("%s is empty", kind)
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		ok := (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '_' || c == '-'
		if !ok {
			return fmt.Errorf("%s %q contains invalid character %q", kind, id, c)
		}
	}
	return nil
}

// sidecarPath builds the validated path for a sidecar spill id. It rejects ids
// outside the allowlist BEFORE filepath.Join so a malicious id can never escape
// the fixed <run_dir>/conversations/ prefix (T-03-07).
func sidecarPath(runDir, sessionID, spillID string) (string, error) {
	if err := validateID("session_id", sessionID); err != nil {
		return "", err
	}
	if err := validateID("tool_call_id", spillID); err != nil {
		return "", err
	}
	return filepath.Join(runDir, "conversations", sessionID, spillID+".result"), nil
}

// truncatePreview returns content truncated to at most capBytes, backed off to a
// UTF-8 rune boundary so a multi-byte rune is never split.
func truncatePreview(content string, capBytes int) string {
	if capBytes <= 0 {
		return ""
	}
	if len(content) <= capBytes {
		return content
	}
	cut := capBytes
	for cut > 0 && !utf8.RuneStart(content[cut]) {
		cut--
	}
	return content[:cut]
}

// NewResult applies the shared cap → preview → (maybe) sidecar spillover (D-25).
// It reads the session_id, tool_call_id, run_dir, and preview cap from the ctx
// the agent injected via WithToolCallContext. Small outputs (≤cap) become a
// preview-only result with no disk write; large outputs are truncated on a rune
// boundary, get a read_tool_output footer pointer, and have their FULL bytes
// written to <run_dir>/conversations/<session_id>/<opaque-spill-id>.result. A
// sidecar write failure degrades clean: the preview carries a "full output
// unavailable" note and NO error is returned, so the turn continues (D-29).
func NewResult(ctx context.Context, content string) (ToolResult, error) {
	tc, ok := toolCallCtx(ctx)
	if !ok {
		return ToolResult{}, fmt.Errorf("tools.NewResult: missing tool-call context (call WithToolCallContext first)")
	}
	content = secret.RedactConfigured(content)
	total := len(content)
	if total <= tc.cap {
		return retainedResult(tc, content, total), nil
	}

	preview := truncatePreview(content, tc.cap)
	shown := len(preview)
	spillID := tc.sidecarID
	if spillID == "" {
		spillID = tc.toolCallID
	}
	footer := fmt.Sprintf(
		"\n\n[output truncated: showing bytes 0-%d of %d; read more via read_tool_output(tool_call_id=%q, offset=%d, limit=%d)]",
		shown, total, spillID, shown, defaultReadLimit,
	)

	path, err := sidecarPath(tc.runDir, tc.sessionID, spillID)
	if err != nil {
		// Traversal-shaped id: reject before any write. This is a real error
		// (T-03-07) — the caller must not have let a malformed id through.
		return ToolResult{}, fmt.Errorf("tools.NewResult: %w", err)
	}

	if werr := writeSidecar(path, content); werr != nil {
		// Degrade clean (D-29): keep the preview, note the failure, no error.
		return ToolResult{
			Preview:   preview + fmt.Sprintf("\n\n[full output unavailable: %s]", werr),
			Bytes:     total,
			Truncated: true,
		}, nil
	}

	return ToolResult{
		Preview:   preview + footer,
		FullPath:  path,
		Bytes:     total,
		Truncated: true,
	}, nil
}

// NewResultReservingTail behaves like NewResult, but treats footer as
// always-visible content. It truncates body first, then appends footer, so shell
// status, stderr tails, and structured exit-code metadata cannot be sliced off by
// the preview cap.
func NewResultReservingTail(ctx context.Context, body, footer string) (ToolResult, error) {
	tc, ok := toolCallCtx(ctx)
	if !ok {
		return ToolResult{}, fmt.Errorf("tools.NewResultReservingTail: missing tool-call context (call WithToolCallContext first)")
	}
	body = secret.RedactConfigured(body)
	footer = secret.RedactConfigured(footer)
	content := body + footer
	total := len(content)
	if total <= tc.cap {
		return ToolResult{Preview: content, Bytes: total, Truncated: false}, nil
	}

	bodyCap := max(tc.cap-len(footer), 0)
	bodyPreview := truncatePreview(body, bodyCap)
	shown := len(bodyPreview)
	preview := bodyPreview + footer
	spillID := tc.sidecarID
	if spillID == "" {
		spillID = tc.toolCallID
	}
	truncFooter := fmt.Sprintf(
		"\n\n[output truncated: showing body bytes 0-%d of %d plus reserved footer; read more via read_tool_output(tool_call_id=%q, offset=%d, limit=%d)]",
		shown, len(body), spillID, shown, defaultReadLimit,
	)

	path, err := sidecarPath(tc.runDir, tc.sessionID, spillID)
	if err != nil {
		return ToolResult{}, fmt.Errorf("tools.NewResultReservingTail: %w", err)
	}

	if werr := writeSidecar(path, content); werr != nil {
		return ToolResult{
			Preview:   preview + fmt.Sprintf("\n\n[full output unavailable: %s]", werr),
			Bytes:     total,
			Truncated: true,
		}, nil
	}

	return ToolResult{
		Preview:   preview + truncFooter,
		FullPath:  path,
		Bytes:     total,
		Truncated: true,
	}, nil
}

// writeSidecar persists the full content to path, creating the per-session dir
// lazily on first persist. The dir is 0o750, not 0o755: it holds spilled tool
// output, which can carry whatever the tool read, so group-read is the widest
// access it ever needs and world-read is not justified (gosec G301).
func writeSidecar(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// spillRetainFloorBytes is the size above which a result gets a sidecar EVEN WHEN IT FITS
// under the preview cap.
//
// The two thresholds answer different questions and were the same number by accident. The
// cap answers "how much does the model see NOW" and is measured (median result 233 bytes,
// p90 6.2 KB): paging should be the exception. The floor below answers "how much does the
// conversation keep PAYING for this later", and until it existed the answer was forever --
// the context ladder may only evict a tool result it can page back, so a 27.5 KB result
// that fit under the 30 KB cap stayed verbatim in every subsequent request for the life of
// the conversation. Measured on the live deployment 2026-08-16: five tool turns, none
// sidecar-backed, one of them 27,515 bytes, and the window filled with tool traffic.
//
// A file write for a minority of calls buys the ladder the right to reclaim them.
const spillRetainFloorBytes = 8000

// retainedFooterMarker announces a result that is COMPLETE in context and also on disk. It
// is deliberately different wording from the truncation footer: nothing was cut here, and
// telling the model to "read more" when it already has everything would earn a pointless
// round trip.
const retainedFooterMarker = "[full output also retained:"

// retainedResult returns a result that fits under the cap, spilling it to the sidecar when
// it is large enough that the ladder will one day want to evict it. A sidecar failure is
// not an error: the content is intact in context, and the only thing lost is the ladder's
// later option to reclaim it.
func retainedResult(tc toolCallContext, content string, total int) ToolResult {
	if total <= spillRetainFloorBytes {
		return ToolResult{Preview: content, Bytes: total, Truncated: false}
	}
	spillID := tc.sidecarID
	if spillID == "" {
		spillID = tc.toolCallID
	}
	path, err := sidecarPath(tc.runDir, tc.sessionID, spillID)
	if err != nil {
		return ToolResult{Preview: content, Bytes: total, Truncated: false}
	}
	if werr := writeSidecar(path, content); werr != nil {
		return ToolResult{Preview: content, Bytes: total, Truncated: false}
	}
	footer := fmt.Sprintf(
		"\n\n%s %d bytes, page it back with read_tool_output(tool_call_id=%q)]",
		retainedFooterMarker, total, spillID)
	return ToolResult{Preview: content + footer, FullPath: path, Bytes: total, Truncated: false}
}
