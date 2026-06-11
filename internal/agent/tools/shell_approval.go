package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
)

// ShellApprovals stores one-shot shell command approvals keyed by conversation
// session and command digest. Consume deletes the approval so a retry can run once.
type ShellApprovals struct {
	mu       sync.Mutex
	approved map[string]struct{}
	pending  map[string]ShellApprovalChallenge
}

type ShellApprovalChallenge struct {
	Command  string
	Cwd      string
	Digest   string
	Question string
}

func NewShellApprovals() *ShellApprovals {
	return &ShellApprovals{
		approved: map[string]struct{}{},
		pending:  map[string]ShellApprovalChallenge{},
	}
}

func ShellApprovalDigest(command, cwd string) string {
	sum := sha256.Sum256([]byte(cwd + "\x00" + command))
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
	key := shellApprovalKey(sessionID, digest)
	a.approved[key] = struct{}{}
	delete(a.pending, key)
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

func (a *ShellApprovals) CreateChallenge(sessionID, command, cwd string) ShellApprovalChallenge {
	digest := ShellApprovalDigest(command, cwd)
	challenge := ShellApprovalChallenge{
		Command:  command,
		Cwd:      cwd,
		Digest:   digest,
		Question: shellApprovalQuestion(command, cwd, digest),
	}
	if a == nil || sessionID == "" || digest == "" {
		return challenge
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending == nil {
		a.pending = map[string]ShellApprovalChallenge{}
	}
	a.pending[shellApprovalKey(sessionID, digest)] = challenge
	return challenge
}

func (a *ShellApprovals) PendingChallenge(sessionID, digest string) (ShellApprovalChallenge, bool) {
	if a == nil || sessionID == "" || digest == "" {
		return ShellApprovalChallenge{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	challenge, ok := a.pending[shellApprovalKey(sessionID, digest)]
	return challenge, ok
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
	challenge := s.Approvals.CreateChallenge(sessionID, command, cwd)
	res := shellApprovalRequiredResult(challenge)
	return &res, nil
}

func shellApprovalRequiredResult(challenge ShellApprovalChallenge) ToolResult {
	payload := map[string]string{
		"error":          "shell_approval_required",
		"command_sha256": challenge.Digest,
		"command":        challenge.Command,
		"cwd":            challenge.Cwd,
		"question":       challenge.Question,
		"message": "This shell command matches AURA_SHELL_DESTRUCTIVE_PATTERNS. " +
			"Call ask_user with kind=\"approval\", question exactly equal to the question field, " +
			"and resume_context={\"type\":\"shell_exec_approval\",\"command_sha256\":\"" + challenge.Digest + "\"}. " +
			"Retry the exact command only after the user accepts.",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(`{"error":"shell_approval_required"}`)
	}
	return ToolResult{Preview: string(raw), Bytes: len(raw)}
}

func shellApprovalQuestion(command, cwd, digest string) string {
	return fmt.Sprintf("Approve shell_exec command?\ncwd: %s\ncommand:\n%s\nsha256: %s", cwd, command, digest)
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
