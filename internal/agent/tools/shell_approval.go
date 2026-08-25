package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	pathpkg "path"
	"strings"
	"sync"
)

// ShellApprovals stores one-shot shell command approvals keyed by conversation
// session and command digest. Consume deletes the approval so a retry can run once.
type ShellApprovals struct {
	mu       sync.Mutex
	approved map[string]struct{}
	pending  map[string]ShellApprovalChallenge
}

// ShellApprovalChallenge is one pending approval prompt: the command as it will
// run, and the exact Question the operator was shown. ApproveChallenge re-checks
// that Question, so the text the operator agreed to is bound to the command that
// executes.
type ShellApprovalChallenge struct {
	Command  string
	Cwd      string
	Digest   string
	Question string
}

// NewShellApprovals returns an empty approval store with both maps initialized.
func NewShellApprovals() *ShellApprovals {
	return &ShellApprovals{
		approved: map[string]struct{}{},
		pending:  map[string]ShellApprovalChallenge{},
	}
}

// ShellApprovalDigest normalizes cwd before hashing (AG-018) so cosmetic variants (/tmp vs /tmp/)
// yield one digest and an approved command is not re-prompted on retry. An empty cwd stays empty
// (Clean would yield ".").
//
// path.Clean, not filepath.Clean: the cwd this hashes is the directory the command runs in, which is
// a POSIX path INSIDE the box. filepath is the host's path package and on a Windows dev host it
// rewrites "/workspace/sub" to "\workspace\sub" — host path semantics applied to a box path, the
// same category of mistake the one-path collapse exists to remove.
func ShellApprovalDigest(command, cwd string) string {
	if strings.TrimSpace(cwd) != "" {
		cwd = pathpkg.Clean(cwd)
	}
	sum := sha256.Sum256([]byte(cwd + "\x00" + command))
	return hex.EncodeToString(sum[:])
}

// Approve grants a one-shot approval for the command digest and clears any
// pending challenge for it. A nil receiver or empty key is a no-op, so callers
// on a build with approvals disabled need no nil check.
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

// Consume redeems an approval, reporting whether one was present. It deletes the
// approval on success: approvals are one-shot, so a second run of the same
// command prompts again.
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

// CreateChallenge records a pending approval for the command and returns the
// challenge to put to the operator. The challenge is returned even on a nil
// receiver so the caller can always render a prompt.
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

// PendingChallenge returns the outstanding challenge for a digest, if any.
func (a *ShellApprovals) PendingChallenge(sessionID, digest string) (ShellApprovalChallenge, bool) {
	if a == nil || sessionID == "" || digest == "" {
		return ShellApprovalChallenge{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	challenge, ok := a.pending[shellApprovalKey(sessionID, digest)]
	return challenge, ok
}

// ApproveChallenge promotes a pending challenge to an approval, but only if
// question matches the text stored when the challenge was created. That equality
// check is the anti-swap guard: it stops an approval collected for one prompt
// from authorizing a command the operator never saw.
func (a *ShellApprovals) ApproveChallenge(sessionID, digest, question string) error {
	if a == nil || sessionID == "" || digest == "" {
		return fmt.Errorf("shell approval challenge %q not found", digest)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	key := shellApprovalKey(sessionID, digest)
	challenge, ok := a.pending[key]
	if !ok {
		return fmt.Errorf("shell approval challenge %q not found", digest)
	}
	if question != challenge.Question {
		return fmt.Errorf("shell approval challenge %q question mismatch", digest)
	}
	if a.approved == nil {
		a.approved = map[string]struct{}{}
	}
	a.approved[key] = struct{}{}
	delete(a.pending, key)
	return nil
}

// DiscardChallenge removes one unresolved challenge without creating an approval.
// Approval expiry calls it only after the durable pause claim has won its race.
func (a *ShellApprovals) DiscardChallenge(sessionID, digest string) {
	if a == nil || sessionID == "" || digest == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.pending, shellApprovalKey(sessionID, digest))
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

// Evict reclaims a finished session's approval ledger (SessionEvictor, R-41). The
// maps are keyed by sessionID+"\x00"+digest, so every entry under the session
// prefix is dropped. An unknown session id is a no-op; concurrency-safe under a.mu.
func (a *ShellApprovals) Evict(sessionID string) {
	if a == nil || sessionID == "" {
		return
	}
	prefix := sessionID + "\x00"
	a.mu.Lock()
	defer a.mu.Unlock()
	for k := range a.approved {
		if strings.HasPrefix(k, prefix) {
			delete(a.approved, k)
		}
	}
	for k := range a.pending {
		if strings.HasPrefix(k, prefix) {
			delete(a.pending, k)
		}
	}
}
