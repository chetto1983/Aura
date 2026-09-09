// Package identity — capability declarations (RBAC-02).
//
// TDD RED scaffold: signatures exist so capabilities_test.go compiles, but
// the bodies are intentionally incomplete. Task 1's GREEN commit fills them
// in for real.
package identity

// CapIdentityCreate permits minting a new identity through the onboarding
// wizard (POST /api/onboarding/provision and its start leg).
const CapIdentityCreate = "identity.create"

// CapIdentityDelete permits removing an identity and its owned data.
const CapIdentityDelete = "identity.delete"

// CapAgentRun permits driving the agent loop (POST /agent/run).
const CapAgentRun = "agent.run"

// CapGovernanceRead permits reading the governance board.
const CapGovernanceRead = "governance.read"

// CapGovernanceWrite permits every governance WRITE surface.
const CapGovernanceWrite = "governance.write"

// CapSharePublic permits minting a public share link.
const CapSharePublic = "share.public"

// Administrative is a RED scaffold: it must return exactly
// [CapIdentityCreate, CapIdentityDelete] once implemented.
func Administrative() []string { return nil }

// UserSet is a RED scaffold: it must return exactly [CapAgentRun,
// CapGovernanceRead, CapGovernanceWrite, CapSharePublic] once implemented.
func UserSet() []string { return nil }

// All is a RED scaffold: it must return Administrative() ++ UserSet() once
// implemented.
func All() []string { return nil }

// IsAdministrative is a RED scaffold: it must exact-match against
// Administrative() once implemented.
func IsAdministrative(capability string) bool { return false }
