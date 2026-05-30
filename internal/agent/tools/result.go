package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ToolCallContext carries the per-Execute identifiers the spillover helper needs
// to place a sidecar file. The agent injects it (via WithToolCallContext) before
// dispatching each tool, so individual tools never reimplement spillover (D-25).
type toolCallContextKey struct{}

type toolCallContext struct {
	sessionID  string
	toolCallID string
	runDir     string
	cap        int
}

// WithToolCallContext returns a ctx carrying the ids + run dir + preview cap the
// spillover helper reads. The agent calls this before each Tool.Execute (D-25).
func WithToolCallContext(ctx context.Context, sessionID, toolCallID, runDir string, previewCap int) context.Context {
	return context.WithValue(ctx, toolCallContextKey{}, toolCallContext{
		sessionID:  sessionID,
		toolCallID: toolCallID,
		runDir:     runDir,
		cap:        previewCap,
	})
}

func toolCallCtx(ctx context.Context) (toolCallContext, bool) {
	v, ok := ctx.Value(toolCallContextKey{}).(toolCallContext)
	return v, ok
}

// validateID rejects any id that could escape <run_dir>/conversations/ once
// joined into a path: `..` traversal or an embedded path separator (T-03-07).
// The run_dir + "conversations/" prefix is fixed and never model-controlled; the
// only model/agent-supplied segments are session_id and tool_call_id, so those
// are the bytes that must be opaque-id shaped.
func validateID(kind, id string) error {
	if id == "" {
		return fmt.Errorf("%s is empty", kind)
	}
	if strings.Contains(id, "..") {
		return fmt.Errorf("%s %q contains %q", kind, id, "..")
	}
	for i := 0; i < len(id); i++ {
		if id[i] == '/' || id[i] == '\\' || os.IsPathSeparator(id[i]) {
			return fmt.Errorf("%s %q contains a path separator", kind, id)
		}
	}
	return nil
}

// sidecarPath builds the validated sidecar path for a tool call. It rejects
// traversal-shaped ids BEFORE filepath.Join so a malicious id can never escape
// the fixed <run_dir>/conversations/ prefix (T-03-07).
func sidecarPath(runDir, sessionID, toolCallID string) (string, error) {
	if err := validateID("session_id", sessionID); err != nil {
		return "", err
	}
	if err := validateID("tool_call_id", toolCallID); err != nil {
		return "", err
	}
	return filepath.Join(runDir, "conversations", sessionID, toolCallID+".result"), nil
}

// truncatePreview returns content truncated to at most capBytes, backed off to a
// UTF-8 rune boundary so a multi-byte rune is never split.
func truncatePreview(content string, capBytes int) string {
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
// written to <run_dir>/conversations/<session_id>/<tool_call_id>.result. A
// sidecar write failure degrades clean: the preview carries a "full output
// unavailable" note and NO error is returned, so the turn continues (D-29).
func NewResult(ctx context.Context, content string) (ToolResult, error) {
	tc, ok := toolCallCtx(ctx)
	if !ok {
		return ToolResult{}, fmt.Errorf("tools.NewResult: missing tool-call context (call WithToolCallContext first)")
	}
	total := len(content)
	if total <= tc.cap {
		return ToolResult{Preview: content, Bytes: total, Truncated: false}, nil
	}

	preview := truncatePreview(content, tc.cap)
	shown := len(preview)
	footer := fmt.Sprintf(
		"\n\n[output truncated: showing bytes 0-%d of %d; read more via read_tool_output(tool_call_id=%q, offset=%d, limit=2048)]",
		shown, total, tc.toolCallID, shown,
	)

	path, err := sidecarPath(tc.runDir, tc.sessionID, tc.toolCallID)
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

// writeSidecar persists the full content to path, creating the per-session dir
// lazily on first persist.
func writeSidecar(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
