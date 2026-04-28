package budget

import (
	"log/slog"
	"os"
	"testing"
)

func newTestTracker(soft, hard, costPerToken float64) *Tracker {
	return NewTracker(Config{
		SoftBudget:   soft,
		HardBudget:   hard,
		CostPerToken: costPerToken,
	}, slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn})))
}

func TestRecordUsage(t *testing.T) {
	tr := newTestTracker(10.0, 20.0, 0.001)
	tr.RecordUsage(1000)

	if tr.TotalTokens() != 1000 {
		t.Errorf("TotalTokens = %d, want 1000", tr.TotalTokens())
	}
	if tr.TotalCost() != 1.0 {
		t.Errorf("TotalCost = %.4f, want 1.0", tr.TotalCost())
	}
}

func TestRecordUsageAccumulates(t *testing.T) {
	tr := newTestTracker(10.0, 20.0, 0.001)
	tr.RecordUsage(1000)
	tr.RecordUsage(500)

	if tr.TotalTokens() != 1500 {
		t.Errorf("TotalTokens = %d, want 1500", tr.TotalTokens())
	}
	if tr.TotalCost() != 1.5 {
		t.Errorf("TotalCost = %.4f, want 1.5", tr.TotalCost())
	}
}

func TestSoftBudgetWarning(t *testing.T) {
	tr := newTestTracker(1.0, 5.0, 0.001)
	tr.RecordUsage(1000) // cost = 1.0, hits soft budget

	if !tr.IsSoftBudgetExceeded() {
		t.Error("soft budget should be exceeded")
	}
	// Should not be hard-exceeded yet
	if tr.IsHardBudgetExceeded() {
		t.Error("hard budget should not be exceeded at soft budget level")
	}
}

func TestHardBudgetExceeded(t *testing.T) {
	tr := newTestTracker(1.0, 2.0, 0.001)
	tr.RecordUsage(2000) // cost = 2.0, hits hard budget

	if !tr.IsHardBudgetExceeded() {
		t.Error("hard budget should be exceeded")
	}
}

func TestCanAfford(t *testing.T) {
	tr := newTestTracker(10.0, 5.0, 0.001)
	tr.RecordUsage(2000) // cost = 2.0

	// Can afford 2000 more tokens? cost = 2.0 + 2.0 = 4.0 < 5.0 → yes
	if !tr.CanAfford(2000, 0) {
		t.Error("CanAfford should return true when cost stays under hard budget")
	}

	// Can afford 4000 more tokens? cost = 2.0 + 4.0 = 6.0 > 5.0 → no
	if tr.CanAfford(4000, 0) {
		t.Error("CanAfford should return false when cost exceeds hard budget")
	}
}

func TestCanAffordNoHardBudget(t *testing.T) {
	tr := newTestTracker(10.0, 0, 0.001) // no hard budget
	if !tr.CanAfford(999999, 0) {
		t.Error("CanAfford should return true when no hard budget is set")
	}
}

func TestPredictCost(t *testing.T) {
	tr := newTestTracker(0, 0, 0.001)
	cost := tr.PredictCost(1000, 500)
	if cost != 1.5 {
		t.Errorf("PredictCost = %.4f, want 1.5", cost)
	}
}

func TestStatus(t *testing.T) {
	tr := newTestTracker(1.0, 5.0, 0.001)
	tr.RecordUsage(1000)

	status := tr.Status()
	if status.TotalTokens != 1000 {
		t.Errorf("Status.TotalTokens = %d, want 1000", status.TotalTokens)
	}
	if status.TotalCost != 1.0 {
		t.Errorf("Status.TotalCost = %.4f, want 1.0", status.TotalCost)
	}
	if status.SoftBudget != 1.0 {
		t.Errorf("Status.SoftBudget = %.4f, want 1.0", status.SoftBudget)
	}
	if status.HardBudget != 5.0 {
		t.Errorf("Status.HardBudget = %.4f, want 5.0", status.HardBudget)
	}
	if !status.SoftBudgetExceeded {
		t.Error("Status.SoftBudgetExceeded should be true after hitting soft budget")
	}
	if status.BudgetExceeded {
		t.Error("Status.BudgetExceeded should be false when only soft budget is hit")
	}
}

func TestDefaultCostPerToken(t *testing.T) {
	tr := NewTracker(Config{
		SoftBudget:   10.0,
		HardBudget:   20.0,
		CostPerToken: 0, // should use default
	}, slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn})))

	// Default is $0.01/1K tokens = 0.00001 per token
	tr.RecordUsage(1000)
	if tr.TotalCost() <= 0 {
		t.Errorf("TotalCost = %.6f, want > 0 with default cost", tr.TotalCost())
	}
}

func TestShouldNotifySoftBudgetOnce(t *testing.T) {
	tr := newTestTracker(1.0, 5.0, 0.001)
	tr.RecordUsage(1000) // cost = 1.0, hits soft budget

	if !tr.ShouldNotifySoftBudget() {
		t.Error("ShouldNotifySoftBudget should return true on first call after soft budget hit")
	}
	if tr.ShouldNotifySoftBudget() {
		t.Error("ShouldNotifySoftBudget should return false on second call (already notified)")
	}
}

func TestShouldNotifySoftBudgetNotHit(t *testing.T) {
	tr := newTestTracker(1.0, 5.0, 0.001)
	// Don't record enough usage to hit soft budget
	if tr.ShouldNotifySoftBudget() {
		t.Error("ShouldNotifySoftBudget should return false when soft budget not exceeded")
	}
}
