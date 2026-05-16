package swarm

import (
	"errors"
	"fmt"
)

// NodeSpec defines a bounded subagent dispatch unit for the Phase 8 fanout
// primitive. RiskTier must be "read_only" or "write_proposal".
// MaxIterations <= 10 and BudgetSecs <= 300 enforce the mini-PC CPU budget.
type NodeSpec struct {
	Goal          string
	Instruction   string
	ToolAllowlist []string
	MaxIterations int
	MaxToolCalls  int
	BudgetSecs    int
	OutputSchema  string // optional JSON schema for structured output
	RiskTier      string // "read_only" | "write_proposal"
	ParentRunID   string
	AssignmentID  string
}

// Validate returns an error if the spec violates any constraint.
// Call before dispatching to catch mis-configurations early without hitting
// the hub.
func (n NodeSpec) Validate() error {
	if n.Goal == "" {
		return errors.New("nodespec: Goal is required")
	}
	switch n.RiskTier {
	case "read_only":
		// no allowlist enforcement for read_only
	case "write_proposal":
		if err := EnforceWriteProposalAllowlist(n.ToolAllowlist); err != nil {
			return err
		}
	default:
		return fmt.Errorf("nodespec: RiskTier %q not allowed (must be read_only or write_proposal)", n.RiskTier)
	}
	if n.MaxIterations > 10 {
		return fmt.Errorf("nodespec: MaxIterations %d exceeds maximum of 10", n.MaxIterations)
	}
	if n.BudgetSecs > 300 {
		return fmt.Errorf("nodespec: BudgetSecs %d exceeds maximum of 300 (mini-PC CPU budget)", n.BudgetSecs)
	}
	return nil
}
