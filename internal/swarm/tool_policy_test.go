package swarm

import "testing"

// TestToolPolicyReadOnlyPassthrough verifies that read_only NodeSpec
// validation does not enforce any ToolAllowlist restrictions.
func TestToolPolicyReadOnlyPassthrough(t *testing.T) {
	spec := NodeSpec{
		Goal:          "search the web",
		RiskTier:      "read_only",
		ToolAllowlist: []string{"any_tool", "wiki_page", "source"},
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("read_only spec with arbitrary tool names should pass, got: %v", err)
	}
}

// TestToolPolicyWriteProposalValid verifies a write_proposal spec with
// propose_patch + read-only tools is accepted.
func TestToolPolicyWriteProposalValid(t *testing.T) {
	err := EnforceWriteProposalAllowlist([]string{"propose_patch", "web_search", "search_memory"})
	if err != nil {
		t.Fatalf("expected valid write_proposal allowlist to pass, got: %v", err)
	}
}

// TestToolPolicyWriteProposalMissingProposePatch verifies that a
// write_proposal allowlist without propose_patch is rejected.
func TestToolPolicyWriteProposalMissingProposePatch(t *testing.T) {
	err := EnforceWriteProposalAllowlist([]string{"web_search", "search_memory"})
	if err == nil {
		t.Fatal("expected error for write_proposal allowlist missing propose_patch, got nil")
	}
}

// TestToolPolicyWriteProposalContainsDirectWrite verifies that a
// write_proposal allowlist containing a direct-write tool (wiki_page) is
// rejected.
func TestToolPolicyWriteProposalContainsDirectWrite(t *testing.T) {
	err := EnforceWriteProposalAllowlist([]string{"propose_patch", "wiki_page"})
	if err == nil {
		t.Fatal("expected error for write_proposal allowlist containing wiki_page, got nil")
	}
}

// TestToolPolicyWriteProposalEmptyAllowlist verifies that an empty allowlist
// is rejected for write_proposal.
func TestToolPolicyWriteProposalEmptyAllowlist(t *testing.T) {
	err := EnforceWriteProposalAllowlist([]string{})
	if err == nil {
		t.Fatal("expected error for empty write_proposal allowlist, got nil")
	}
}
