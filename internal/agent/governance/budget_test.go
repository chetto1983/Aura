package governance

import (
	"strings"
	"testing"
)

// TestToolClass verifies the tool-name → class mapping for all 7 classes.
func TestToolClass(t *testing.T) {
	cases := []struct {
		name  string
		class string
	}{
		{"web_search", ClassWeb},
		{"web_fetch", ClassWeb},
		{"web", ClassWeb},
		{"search", ClassWiki},
		{"search_memory", ClassWiki},
		{"read_memory", ClassWiki},
		{"list_memory", ClassWiki},
		{"forget_memory", ClassWiki},
		{"execute_code", ClassExec},
		{"execute_bash", ClassExec},
		{"execute_shell", ClassExec},
		{"source", ClassSource},
		{"read_source", ClassSource},
		{"store_source", ClassSource},
		{"ingest_source", ClassSource},
		{"ocr_source", ClassSource},
		{"task", ClassScheduler},
		{"schedule_task", ClassScheduler},
		{"schedule_weekly_report", ClassScheduler},
		{"list_tasks", ClassScheduler},
		{"cancel_task", ClassScheduler},
		{"ask_user", ClassAskUser},
		{"foo_unknown", ClassDefault},
		{"create_xlsx", ClassDefault},
		{"wiki", ClassDefault},
	}
	for _, tc := range cases {
		got := ToolClass(tc.name)
		if got != tc.class {
			t.Errorf("ToolClass(%q) = %q, want %q", tc.name, got, tc.class)
		}
	}
}

// TestBudgetTrackerAllowsWithinCap verifies calls within budget return nil.
func TestBudgetTrackerAllowsWithinCap(t *testing.T) {
	b := NewBudgetTracker(nil)
	for range DefaultCapWeb {
		if err := b.Check(ClassWeb, "web_search"); err != nil {
			t.Fatalf("call within cap should succeed, got %v", err)
		}
	}
}

// TestBudgetTrackerBlocksAtCap verifies the (cap+1)th call returns a budget error.
func TestBudgetTrackerBlocksAtCap(t *testing.T) {
	b := NewBudgetTracker(nil)
	// Exhaust the cap.
	for range DefaultCapWeb {
		if err := b.Check(ClassWeb, "web_search"); err != nil {
			t.Fatalf("call within cap should succeed, got %v", err)
		}
	}
	// Next call must fail.
	err := b.Check(ClassWeb, "web_search")
	if err == nil {
		t.Fatal("call cap+1 should return budget error, got nil")
	}
	if !strings.Contains(err.Error(), "web budget exhausted") {
		t.Errorf("unexpected error message: %v", err)
	}
	if !strings.Contains(err.Error(), "Use a different tool class") {
		t.Errorf("error should mention alternative: %v", err)
	}
}

// TestBudgetTrackerCounterResetsPerTurn verifies that creating a new tracker
// (= new turn) resets all counts: two consecutive turns with cap-many calls
// each both succeed.
func TestBudgetTrackerCounterResetsPerTurn(t *testing.T) {
	// Turn 1: 3 web calls succeed.
	b1 := NewBudgetTracker(nil)
	for range DefaultCapWeb {
		if err := b1.Check(ClassWeb, "web_search"); err != nil {
			t.Fatalf("turn1 call failed: %v", err)
		}
	}
	// Turn 2: fresh tracker, same 3 calls succeed.
	b2 := NewBudgetTracker(nil)
	for range DefaultCapWeb {
		if err := b2.Check(ClassWeb, "web_search"); err != nil {
			t.Fatalf("turn2 call failed: %v", err)
		}
	}
}

// TestBudgetTrackerCustomCaps verifies override caps take effect.
func TestBudgetTrackerCustomCaps(t *testing.T) {
	b := NewBudgetTracker(map[string]int{ClassWeb: 10})
	// 10 calls succeed.
	for range 10 {
		if err := b.Check(ClassWeb, "web_search"); err != nil {
			t.Fatalf("call should succeed with cap=10, got %v", err)
		}
	}
	// 11th fails.
	if err := b.Check(ClassWeb, "web_search"); err == nil {
		t.Fatal("11th call should fail with cap=10")
	}
}

// TestBudgetTrackerInvalidCapsUseDefaults verifies out-of-range values are ignored.
func TestBudgetTrackerInvalidCapsUseDefaults(t *testing.T) {
	// 0 and 101 are outside [1,100] — must fall back to hardcoded default.
	b := NewBudgetTracker(map[string]int{ClassWeb: 0, ClassExec: 101})
	// DefaultCapWeb=3 should apply.
	for range DefaultCapWeb {
		if err := b.Check(ClassWeb, "web_search"); err != nil {
			t.Fatalf("call should succeed with default cap, got %v", err)
		}
	}
	if err := b.Check(ClassWeb, "web_search"); err == nil {
		t.Fatal("cap+1 call should fail")
	}
}

// TestBudgetTrackerDifferentClassesIndependent verifies class caps are tracked
// independently: exhausting web does not affect wiki.
func TestBudgetTrackerDifferentClassesIndependent(t *testing.T) {
	b := NewBudgetTracker(nil)
	// Exhaust web.
	for range DefaultCapWeb + 1 {
		_ = b.Check(ClassWeb, "web_search")
	}
	// Wiki still has budget.
	if err := b.Check(ClassWiki, "search_memory"); err != nil {
		t.Errorf("wiki should be unaffected by web exhaustion, got %v", err)
	}
}
