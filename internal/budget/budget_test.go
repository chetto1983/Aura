package budget

import (
	"log/slog"
	"os"
	"testing"

	"github.com/aura/aura/internal/llm"
)

func newTestTracker(soft, hard, inputPerM, outputPerM float64) *Tracker {
	return NewTracker(Config{
		SoftBudget:           soft,
		HardBudget:           hard,
		InputCostPerMTokens:  inputPerM,
		OutputCostPerMTokens: outputPerM,
	}, slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn})))
}

func TestRecordUsage(t *testing.T) {
	tr := newTestTracker(10.0, 20.0, 1.00, 5.00)
	tr.RecordUsage(llm.TokenUsage{PromptTokens: 1_000_000, CompletionTokens: 100_000, TotalTokens: 1_100_000})

	if tr.TotalTokens() != 1_100_000 {
		t.Errorf("TotalTokens = %d, want 1100000", tr.TotalTokens())
	}
	if tr.TotalCost() != 1.5 {
		t.Errorf("TotalCost = %.4f, want 1.5", tr.TotalCost())
	}
}

func TestRecordUsageAccumulates(t *testing.T) {
	tr := newTestTracker(10.0, 20.0, 1.00, 5.00)
	tr.RecordUsage(llm.TokenUsage{PromptTokens: 500_000, CompletionTokens: 100_000, TotalTokens: 600_000})
	tr.RecordUsage(llm.TokenUsage{PromptTokens: 500_000, TotalTokens: 500_000})

	if tr.TotalTokens() != 1_100_000 {
		t.Errorf("TotalTokens = %d, want 1100000", tr.TotalTokens())
	}
	if tr.TotalCost() != 1.5 {
		t.Errorf("TotalCost = %.4f, want 1.5", tr.TotalCost())
	}
}

func TestSoftBudgetWarning(t *testing.T) {
	tr := newTestTracker(1.0, 5.0, 1.00, 1.00)
	tr.RecordUsage(llm.TokenUsage{PromptTokens: 1_000_000, TotalTokens: 1_000_000})

	if !tr.IsSoftBudgetExceeded() {
		t.Error("soft budget should be exceeded")
	}
	if tr.IsHardBudgetExceeded() {
		t.Error("hard budget should not be exceeded at soft budget level")
	}
}

func TestHardBudgetExceeded(t *testing.T) {
	tr := newTestTracker(1.0, 2.0, 1.00, 1.00)
	tr.RecordUsage(llm.TokenUsage{PromptTokens: 2_000_000, TotalTokens: 2_000_000})

	if !tr.IsHardBudgetExceeded() {
		t.Error("hard budget should be exceeded")
	}
}

func TestCanAfford(t *testing.T) {
	tr := newTestTracker(10.0, 5.0, 1.00, 1.00)
	tr.RecordUsage(llm.TokenUsage{PromptTokens: 2_000_000, TotalTokens: 2_000_000})

	if !tr.CanAfford(2_000_000, 0) {
		t.Error("CanAfford should return true when cost stays under hard budget")
	}
	if tr.CanAfford(4_000_000, 0) {
		t.Error("CanAfford should return false when cost exceeds hard budget")
	}
}

func TestCanAffordNoHardBudget(t *testing.T) {
	tr := newTestTracker(10.0, 0, 1.00, 1.00)
	if !tr.CanAfford(999999, 0) {
		t.Error("CanAfford should return true when no hard budget is set")
	}
}

func TestPredictCost(t *testing.T) {
	tr := newTestTracker(0, 0, 1.00, 5.00)
	cost := tr.PredictCost(1_000_000, 100_000)
	if cost != 1.5 {
		t.Errorf("PredictCost = %.4f, want 1.5", cost)
	}
}

func TestStatus(t *testing.T) {
	tr := newTestTracker(1.0, 5.0, 1.00, 5.00)
	tr.RecordUsage(llm.TokenUsage{PromptTokens: 500_000, CompletionTokens: 100_000, TotalTokens: 600_000})

	status := tr.Status()
	if status.TotalTokens != 600_000 {
		t.Errorf("Status.TotalTokens = %d, want 600000", status.TotalTokens)
	}
	if status.PromptTokens != 500_000 || status.CompletionTokens != 100_000 {
		t.Errorf("Status tokens = prompt %d completion %d", status.PromptTokens, status.CompletionTokens)
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
	if status.InputPerMTokens != 1.00 || status.OutputPerMTokens != 5.00 {
		t.Errorf("prices = input %.4f output %.4f", status.InputPerMTokens, status.OutputPerMTokens)
	}
}

func TestDefaultCostPerMillionTokens(t *testing.T) {
	tr := NewTracker(Config{
		SoftBudget: 10.0,
		HardBudget: 20.0,
	}, slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn})))

	tr.RecordUsage(llm.TokenUsage{PromptTokens: 1000, CompletionTokens: 100, TotalTokens: 1100})
	if tr.TotalCost() <= 0 {
		t.Errorf("TotalCost = %.6f, want > 0 with default cost", tr.TotalCost())
	}
}

func TestLegacyCostPerTokenCompatibility(t *testing.T) {
	tr := NewTracker(Config{
		LegacyCostPerTokenUSD: 0.000001,
	}, slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn})))

	if cost := tr.PredictCost(1_000_000, 0); cost != 1.0 {
		t.Errorf("legacy cost = %.4f, want 1.0", cost)
	}
}

func TestShouldNotifySoftBudgetOnce(t *testing.T) {
	tr := newTestTracker(1.0, 5.0, 1.00, 1.00)
	tr.RecordUsage(llm.TokenUsage{PromptTokens: 1_000_000, TotalTokens: 1_000_000})

	if !tr.ShouldNotifySoftBudget() {
		t.Error("ShouldNotifySoftBudget should return true on first call after soft budget hit")
	}
	if tr.ShouldNotifySoftBudget() {
		t.Error("ShouldNotifySoftBudget should return false on second call (already notified)")
	}
}

func TestShouldNotifySoftBudgetNotHit(t *testing.T) {
	tr := newTestTracker(1.0, 5.0, 1.00, 1.00)
	if tr.ShouldNotifySoftBudget() {
		t.Error("ShouldNotifySoftBudget should return false when soft budget not exceeded")
	}
}
