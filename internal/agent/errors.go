package agent

import "errors"

// ErrBudgetExhausted is the exported sentinel for callers that inspect agent
// termination OUTSIDE the Event stream (D-04). Inside a Run the canonical signal
// is an explicit Event (Actions.Escalate=true + StateDelta termination_reason);
// this sentinel exists only so Phase 3/9 consumers can do errors.Is(err,
// agent.ErrBudgetExhausted) when a budget limit surfaces through the error slot.
var ErrBudgetExhausted = errors.New("agent budget exhausted")
