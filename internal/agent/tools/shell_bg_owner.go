package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
)

// Background-job identity & authority (MUSR-03 / D-17/D-18). Pre-Phase-36 the
// registry minted sequential sh_%d ids with no owner, so any caller could poll or
// kill any shell_id (trivially guessable → cross-session takeover). This file mints
// unguessable crypto-random ids, binds each job to its (identity, session) owner at
// start, and gates poll/kill on owner-match OR an explicit admin capability.

// localOwnerID is the seeded `local` identity (migration 0004) — the CLI /
// no-principal fallback owner. The CLI always runs as `local` (D-25); a background
// job started without an authenticated principal is owned by, and reachable by, `local`.
const localOwnerID = "00000000-0000-0000-0000-000000000001"

// adminShellCapability grants cross-session poll/kill recovery (D-18). It reuses the
// existing capability_grants seam — governance.write — per the RESEARCH OQ resolution
// (no net-new settings.model.write); the seeded `local` admin holds it (0026).
const adminShellCapability = "governance.write"

// capabilityChecker is the narrow consumer-side seam (D-A2-02 "accept interfaces")
// the poll/kill authority path needs for the D-18 admin exemption; *identity.Store
// satisfies it structurally. It stays nil in the current composition root (owner-only,
// fail-closed) and is wired to the identity store when the multi-user surface goes
// live — a zero-rework swap (D-04). Nil ⇒ no admin exemption (foreign callers denied).
type capabilityChecker interface {
	HasCapability(ctx context.Context, identityID, capability string) (bool, error)
}

// newBackgroundShellID mints an unguessable 128-bit crypto-random hex id, replacing
// the pre-MUSR-03 sequential sh_%d. A crypto/rand failure is fatal to the start (no
// guessable fallback is ever minted — fail closed).
func newBackgroundShellID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("background shell id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

// ownerFromContext resolves the caller principal: the authenticated identity from
// identityctx, or the `local` identity when no principal is scoped (CLI / bare ctx).
func ownerFromContext(ctx context.Context) string {
	if id := identityctx.IdentityID(ctx); id != "" {
		return id
	}
	return localOwnerID
}

// authorize is the poll/kill decision: allow iff the caller owns the job
// ((identity, session) both match) OR the caller holds the admin capability (D-18).
func (s *bgShell) authorize(callerIdentity, callerSession string, hasAdmin bool) bool {
	if hasAdmin {
		return true
	}
	return callerIdentity == s.ownerID && callerSession == s.sessionID
}

// authorizeCaller resolves the ctx caller's (identity, session) and consults the admin
// capability ONLY when the caller does not own the job (avoids an unnecessary store hit
// on the common owner path). caps == nil ⇒ owner-only (fail-closed).
func authorizeCaller(ctx context.Context, caps capabilityChecker, sh *bgShell) bool {
	callerIdentity := ownerFromContext(ctx)
	callerSession := shellSessionKey(ctx)
	if callerIdentity == sh.ownerID && callerSession == sh.sessionID {
		return true
	}
	hasAdmin := false
	if caps != nil {
		if ok, err := caps.HasCapability(ctx, callerIdentity, adminShellCapability); err == nil {
			hasAdmin = ok
		}
	}
	return sh.authorize(callerIdentity, callerSession, hasAdmin)
}

// ShellPoll reads new output from a background shell_exec job — Claude Code's
// BashOutput. Non-mutating: it only reads accumulated output + status.
type ShellPoll struct {
	Shells *BackgroundShells
	// Caps is the optional admin-capability seam for the D-18 cross-session recovery
	// path; nil ⇒ owner-only (foreign callers get the not-found shape).
	Caps capabilityChecker
}

type shellPollArgs struct {
	ShellID string `json:"shell_id"`
	Filter  string `json:"filter"`
}

func (p *ShellPoll) Spec() Spec {
	return Spec{
		Name:    "shell_poll",
		Summary: "Read new output from a background shell_exec job.",
		Description: "Retrieve output from a running or finished background shell started by shell_exec with \"background\": true. " +
			"Returns ONLY the output produced since your last poll, plus a final [aura_shell_bg {...}] footer with the shell_id and status (running | exited:<code> | killed). " +
			"Pass an optional regex 'filter' to keep only matching lines. Poll again to follow a long job; stop it with shell_kill.",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "shell_id": {"type": "string", "description": "The id returned by the background shell_exec call."},
    "filter": {"type": "string", "description": "Optional regular expression; only output lines matching it are returned."}
  },
  "required": ["shell_id"]
}`),
		Deferred: true,
	}
}

// Evict forwards a conversation-end signal to the shared background-shell
// registry, reclaiming finished shells (SessionEvictor, AG-015). ShellPoll is a
// registry tool, so the Runner's evict loop reaches the process-scoped registry
// through it; the prune is by completion, not by session.
func (p *ShellPoll) Evict(sessionID string) {
	if p == nil || p.Shells == nil {
		return
	}
	p.Shells.Evict(sessionID)
}

func (p *ShellPoll) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	if p.Shells == nil {
		return ToolResult{}, fmt.Errorf("shell_poll: background shells are not available in this context")
	}
	var a shellPollArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("shell_poll args: %w", err)
	}
	if strings.TrimSpace(a.ShellID) == "" {
		return ToolResult{}, fmt.Errorf("shell_poll: shell_id is required")
	}
	sh, ok := p.Shells.get(a.ShellID)
	if !ok || !authorizeCaller(ctx, p.Caps, sh) {
		// D-06 read = 404: a foreign job is indistinguishable from a missing one, so a
		// caller cannot probe another session's shell_ids for existence.
		return ToolResult{}, fmt.Errorf("shell_poll: unknown shell_id %q", a.ShellID)
	}
	var filter *regexp.Regexp
	if strings.TrimSpace(a.Filter) != "" {
		re, err := regexp.Compile(a.Filter)
		if err != nil {
			return ToolResult{}, fmt.Errorf("shell_poll: invalid filter regex: %w", err)
		}
		filter = re
	}
	chunk, status := sh.snapshot(filter)
	ageMS := sh.age(time.Now()).Milliseconds() // MUSR-04 age metric (now - startedAt)
	// AG-015: reclaim OTHER finished shells on this poll (not the one being polled —
	// the model may re-poll it for a final read), so a long-lived daemon does not
	// accumulate finished-but-unpolled buffers until the next start.
	p.Shells.pruneFinishedExcept(a.ShellID)
	body := chunk
	if strings.TrimSpace(body) == "" {
		body = "[no new output]"
	}
	body = redactModelPreview(body)
	footer, _ := json.Marshal(map[string]any{"shell_id": a.ShellID, "status": status, "age_ms": ageMS})
	rendered := body + "\n[aura_shell_bg " + string(footer) + "]"
	res, err := NewResult(ctx, rendered)
	if err != nil {
		return ToolResult{}, err
	}
	res.Meta = &ToolResultMeta{"shell_id": a.ShellID, "status": status, "age_ms": ageMS}
	return res, nil
}

// ShellKill terminates a background shell_exec job — Claude Code's KillBash.
type ShellKill struct {
	Shells *BackgroundShells
	// Caps is the optional admin-capability seam for the D-18 cross-session recovery
	// path; nil ⇒ owner-only (a foreign kill is refused).
	Caps capabilityChecker
}

type shellKillArgs struct {
	ShellID string `json:"shell_id"`
}

func (k *ShellKill) Spec() Spec {
	return Spec{
		Name:    "shell_kill",
		Summary: "Terminate a background shell_exec job.",
		Description: "Kill a running background shell started by shell_exec (Claude Code's KillBash). " +
			"Takes the shell_id; returns success even if the job already finished (idempotent).",
		Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "shell_id": {"type": "string", "description": "The id of the background shell to terminate."}
  },
  "required": ["shell_id"]
}`),
		Deferred: true,
		Mutating: true,
	}
}

func (k *ShellKill) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	if k.Shells == nil {
		return ToolResult{}, fmt.Errorf("shell_kill: background shells are not available in this context")
	}
	var a shellKillArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("shell_kill args: %w", err)
	}
	if strings.TrimSpace(a.ShellID) == "" {
		return ToolResult{}, fmt.Errorf("shell_kill: shell_id is required")
	}
	sh, ok := k.Shells.get(a.ShellID)
	if !ok {
		return ToolResult{}, fmt.Errorf("shell_kill: unknown shell_id %q", a.ShellID)
	}
	if !authorizeCaller(ctx, k.Caps, sh) {
		// D-06 mutate = 403: a known-foreign kill is refused (not hidden) — the caller
		// is told it is not authorized rather than that the job is missing.
		return ToolResult{}, fmt.Errorf("shell_kill: not authorized to terminate shell_id %q", a.ShellID)
	}
	if err := k.Shells.kill(a.ShellID); err != nil {
		return ToolResult{}, err
	}
	return NewResult(ctx, fmt.Sprintf("shell %s terminated", a.ShellID))
}
