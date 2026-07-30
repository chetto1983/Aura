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
		return ToolResult{Preview: content, Bytes: total, Truncated: false}, nil
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

	bodyCap := tc.cap - len(footer)
	if bodyCap < 0 {
		bodyCap = 0
	}
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
// lazily on first persist.
func writeSidecar(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
