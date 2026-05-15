package attempts

import (
	"context"

	tools "github.com/aura/aura/internal/agent/tools/registry"
)

// CheckBudget reports whether dispatching (toolName) again for the given
// outcome class is within the retry budget for this run.
//
// budget=0 is treated as limit=1: any prior failure of this class blocks a
// subsequent dispatch. budget>0 allows up to budget prior failures before
// refusing (i.e. the (budget+1)th attempt is refused).
//
// On repo error CheckBudget fails open — returning (true, "") — so a storage
// hiccup never silently blocks a legitimately-needed tool call.
func CheckBudget(ctx context.Context, repo Repo, runID, toolName string, outcome tools.Outcome, budget int) (bool, string) {
	limit := budget
	if limit <= 0 {
		limit = 1
	}
	count, err := repo.CountOutcome(ctx, runID, toolName, outcome)
	if err != nil {
		return true, "" // fail open on repo error
	}
	if count >= limit {
		return false, "retry_budget_exhausted_for_class:" + outcome.String()
	}
	return true, ""
}
