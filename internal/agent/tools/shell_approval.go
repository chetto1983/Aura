package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	pathpkg "path"
	"strings"
)

// ShellApprovals stores one-shot shell command approvals keyed by conversation session and
// command digest, over an ApprovalStore of its own. Every method is nil-receiver safe: a
// shell_exec with no store fails closed by asking every time, and never panics.
//
// The store is a field and not an embedded value: a method promoted through a nil
// *ShellApprovals would dereference it.
type ShellApprovals struct {
	store ApprovalStore[ShellCommand]
}

// ShellCommand is what a shell approval names: the command as it will run, in its cwd.
type ShellCommand struct {
	Command string
	Cwd     string
}

// ShellApprovalChallenge is one pending approval prompt: the command as it will run, and
// the exact Question the operator was shown. ApproveChallenge re-checks that Question, so
// the text the operator agreed to is bound to the command that executes.
type ShellApprovalChallenge struct {
	Command  string
	Cwd      string
	Digest   string
	Question string
}

// NewShellApprovals returns an empty approval store.
func NewShellApprovals() *ShellApprovals {
	return &ShellApprovals{}
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

func (a *ShellApprovals) approvals() *ApprovalStore[ShellCommand] {
	if a == nil {
		return nil
	}
	return &a.store
}

// Approve grants a one-shot approval for the command digest and clears any pending
// challenge for it.
func (a *ShellApprovals) Approve(sessionID, digest string) {
	a.approvals().Approve(sessionID, digest)
}

// Consume redeems an approval, reporting whether one was present. Approvals are one-shot,
// so a second run of the same command prompts again.
func (a *ShellApprovals) Consume(sessionID, digest string) bool {
	return a.approvals().Consume(sessionID, digest)
}

// CreateChallenge records a pending approval for the command and returns the challenge to
// put to the operator, even when nothing could be recorded.
func (a *ShellApprovals) CreateChallenge(sessionID, command, cwd string) ShellApprovalChallenge {
	digest := ShellApprovalDigest(command, cwd)
	return shellChallenge(a.approvals().CreateChallenge(sessionID, digest,
		shellApprovalQuestion(command, cwd, digest), ShellCommand{Command: command, Cwd: cwd}))
}

// PendingChallenge returns the outstanding challenge for a digest, if any.
func (a *ShellApprovals) PendingChallenge(sessionID, digest string) (ShellApprovalChallenge, bool) {
	challenge, ok := a.approvals().PendingChallenge(sessionID, digest)
	return shellChallenge(challenge), ok
}

// ApproveChallenge promotes a pending challenge to an approval, but only if question
// matches the text stored when the challenge was created.
func (a *ShellApprovals) ApproveChallenge(sessionID, digest, question string) error {
	return a.approvals().ApproveChallenge(sessionID, digest, question)
}

// DiscardChallenge removes one unresolved challenge without creating an approval.
func (a *ShellApprovals) DiscardChallenge(sessionID, digest string) {
	a.approvals().DiscardChallenge(sessionID, digest)
}

// Evict reclaims a finished session's approval ledger (SessionEvictor, R-41).
func (a *ShellApprovals) Evict(sessionID string) {
	a.approvals().Evict(sessionID)
}

func shellChallenge(c ApprovalChallenge[ShellCommand]) ShellApprovalChallenge {
	return ShellApprovalChallenge{Command: c.Data.Command, Cwd: c.Data.Cwd, Digest: c.Digest, Question: c.Question}
}

func (s *ShellExec) requireShellApproval(ctx context.Context, command, cwd string) (*ToolResult, error) {
	destructive, err := destructiveShellMatch(command)
	if err != nil {
		return nil, err
	}
	if !destructive {
		return nil, nil
	}
	sessionID := SessionIDFromContext(ctx)
	digest := ShellApprovalDigest(command, cwd)
	if s.Approvals.Consume(sessionID, digest) {
		return nil, nil
	}
	challenge := s.Approvals.CreateChallenge(sessionID, command, cwd)
	res := shellApprovalRequiredResult(challenge)
	return &res, nil
}

// shellApprovalRequiredResult is Aura's own text, marked trusted so the model copies the
// question and resume_context without reading through an HTML-escaped envelope (spec
// 2026-10-12). The command and cwd are the model's own arguments, and json.Marshal's
// HTML-safe encoding keeps every <, > and & of them out of the payload.
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
	return ToolResult{
		Preview: string(raw), Bytes: len(raw),
		Provenance: &ToolResultProvenance{Source: "aura:shell_approval", Trust: TrustTrusted},
	}
}

func shellApprovalQuestion(command, cwd, digest string) string {
	return fmt.Sprintf("Approve shell_exec command?\ncwd: %s\ncommand:\n%s\nsha256: %s", cwd, command, digest)
}
