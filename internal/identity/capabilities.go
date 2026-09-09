// Package identity — capability declarations (RBAC-02).
//
// This file is the ONLY place a capability_grants name is declared as a Go
// literal (RBAC-02). Every other file that used to declare its own copy of
// one of these strings now references the constant here instead — see
// cmd/aura/serve_webui_routes.go, internal/agui/onboarding_provision.go,
// internal/agui/share_api.go, internal/agent/tools/shell_bg_owner.go and
// internal/agent/tools/skill_manage.go.
package identity

import "slices"

// CapIdentityCreate permits minting a new identity through the onboarding
// wizard (POST /api/onboarding/provision and its start leg).
const CapIdentityCreate = "identity.create"

// CapIdentityDelete permits removing an identity and its owned data. New in
// this phase (D-01): the wildcard used to imply it silently; now it must be
// held explicitly.
const CapIdentityDelete = "identity.delete"

// CapAgentRun permits driving the agent loop (POST /agent/run).
const CapAgentRun = "agent.run"

// CapGovernanceRead permits reading the governance board — scheduler and
// audit rows that can reveal cross-identity operational metadata.
const CapGovernanceRead = "governance.read"

// CapGovernanceWrite permits every governance WRITE surface: MCP server
// config mutation, skill install, background-shell admin recovery, and
// model-settings changes. Strictly stronger than CapGovernanceRead.
const CapGovernanceWrite = "governance.write"

// CapSharePublic permits minting a public share link for the caller's own
// conversation. Per-user, off-by-default — not an admin-only capability.
const CapSharePublic = "share.public"

// administrative is the source-of-truth order for Administrative(); All()
// concatenates this then userSet, so the order is stable across runs
// (byte-identical bootstrap grant + audit array).
var administrative = []string{CapIdentityCreate, CapIdentityDelete}

// userSet is the source-of-truth order for UserSet() — the four capabilities
// every identity holds from provisioning.
var userSet = []string{CapAgentRun, CapGovernanceRead, CapGovernanceWrite, CapSharePublic}

// Administrative returns the two capabilities the operator-tier identity
// holds explicitly: identity.create and identity.delete. Returns a fresh
// copy each call so a caller cannot mutate the declared set.
func Administrative() []string {
	out := make([]string, len(administrative))
	copy(out, administrative)
	return out
}

// UserSet returns the four capabilities every identity holds from
// provisioning. Returns a fresh copy each call.
func UserSet() []string {
	out := make([]string, len(userSet))
	copy(out, userSet)
	return out
}

// All returns the full declared capability set — Administrative() followed
// by UserSet(), in that source order. Returns a fresh copy each call.
func All() []string {
	out := make([]string, 0, len(administrative)+len(userSet))
	out = append(out, administrative...)
	out = append(out, userSet...)
	return out
}

// IsAdministrative reports whether capability is one of the two
// administrative names, by exact string match — never a prefix match, never
// case-folded.
func IsAdministrative(capability string) bool {
	return slices.Contains(administrative, capability)
}
