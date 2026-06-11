package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
)

// ShellApprovals stores one-shot shell command approvals keyed by conversation
// session and command digest. Consume deletes the approval so a retry can run once.
type ShellApprovals struct {
	mu       sync.Mutex
	approved map[string]struct{}
}

func NewShellApprovals() *ShellApprovals {
	return &ShellApprovals{approved: map[string]struct{}{}}
}

func ShellApprovalDigest(command, cwd string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(cwd) + "\x00" + strings.TrimSpace(command)))
	return hex.EncodeToString(sum[:])
}

func (a *ShellApprovals) Approve(sessionID, digest string) {
	if a == nil || sessionID == "" || digest == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.approved == nil {
		a.approved = map[string]struct{}{}
	}
	a.approved[shellApprovalKey(sessionID, digest)] = struct{}{}
}

func (a *ShellApprovals) Consume(sessionID, digest string) bool {
	if a == nil || sessionID == "" || digest == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	key := shellApprovalKey(sessionID, digest)
	if _, ok := a.approved[key]; !ok {
		return false
	}
	delete(a.approved, key)
	return true
}

func (s *ShellExec) requireShellApproval(ctx context.Context, command, cwd string) (*ToolResult, error) {
	destructive, err := destructiveShellMatch(command)
	if err != nil {
		return nil, err
	}
	if !destructive {
		return nil, nil
	}
	sessionID := shellSessionKey(ctx)
	digest := ShellApprovalDigest(command, cwd)
	if s.Approvals.Consume(sessionID, digest) {
		return nil, nil
	}
	res := shellApprovalRequiredResult(digest)
	return &res, nil
}

func shellApprovalRequiredResult(digest string) ToolResult {
	payload := map[string]string{
		"error":          "shell_approval_required",
		"command_sha256": digest,
		"message": "This shell command matches AURA_SHELL_DESTRUCTIVE_PATTERNS. " +
			"Ask the user for approval with ask_user(kind=approval, resume_context={\"type\":\"shell_exec_approval\",\"command_sha256\":\"" + digest + "\"}), then retry the exact command after acceptance.",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(`{"error":"shell_approval_required"}`)
	}
	return ToolResult{Preview: string(raw), Bytes: len(raw)}
}

// shellSessionKey scopes shell state per conversation/session: the session id from
// WithToolCallContext, "" for bare-ctx callers (unit tests, one-shot CLIs).
func shellSessionKey(ctx context.Context) string {
	if tc, ok := toolCallCtx(ctx); ok {
		return tc.sessionID
	}
	return ""
}

func shellApprovalKey(sessionID, digest string) string {
	return sessionID + "\x00" + digest
}
