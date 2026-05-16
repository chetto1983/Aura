package swarm

import (
	"errors"
	"fmt"
)

// NodeSpec defines a bounded read-only subagent dispatch unit for the Phase 8
// first-slice fanout primitive. RiskTier must be "read_only" in this slice.
// MaxIterations <= 10 and BudgetSecs <= 300 enforce the mini-PC CPU budget.
type NodeSpec struct {
	Goal          string
	Instruction   string
	ToolAllowlist []string
	MaxIterations int
	MaxToolCalls  int
	BudgetSecs    int
	OutputSchema  string // optional JSON schema for structured output
	RiskTier      string // "read_only" only in this slice; "write_proposal" deferred
	ParentRunID   string
	AssignmentID  string
}

// Validate returns an error if the spec violates any constraint for the
// read-only fanout slice. Call before dispatching to catch mis-configurations
// early without hitting the hub.
func (n NodeSpec) Validate() error {
	if n.Goal == "" {
		return errors.New("nodespec: Goal is required")
	}
	if n.RiskTier != "read_only" {
		return fmt.Errorf("nodespec: RiskTier %q not allowed in this slice (must be read_only)", n.RiskTier)
	}
	if n.MaxIterations > 10 {
		return fmt.Errorf("nodespec: MaxIterations %d exceeds maximum of 10", n.MaxIterations)
	}
	if n.BudgetSecs > 300 {
		return fmt.Errorf("nodespec: BudgetSecs %d exceeds maximum of 300 (mini-PC CPU budget)", n.BudgetSecs)
	}
	return nil
}
