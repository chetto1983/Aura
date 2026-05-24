package governance

import (
	"fmt"
	"sync"
)

// Per-class default budget caps. Production-validated values; operators tune
// via AURA_TOOL_BUDGET_<CLASS> env vars or the runtime settings table.
const (
	DefaultCapWeb       = 3
	DefaultCapWiki      = 20
	DefaultCapExec      = 2
	DefaultCapSource    = 8
	DefaultCapScheduler = 5
	DefaultCapAskUser   = 3
	DefaultCapDefault   = 10
)

// Tool class name constants returned by ToolClass.
const (
	ClassWeb       = "web"
	ClassWiki      = "wiki"
	ClassExec      = "exec"
	ClassSource    = "source"
	ClassScheduler = "scheduler"
	ClassAskUser   = "ask_user"
	ClassDefault   = "default"
)

// defaultCaps returns the hardcoded default per-class cap map.
func defaultCaps() map[string]int {
	return map[string]int{
		ClassWeb:       DefaultCapWeb,
		ClassWiki:      DefaultCapWiki,
		ClassExec:      DefaultCapExec,
		ClassSource:    DefaultCapSource,
		ClassScheduler: DefaultCapScheduler,
		ClassAskUser:   DefaultCapAskUser,
		ClassDefault:   DefaultCapDefault,
	}
}

// ToolClass maps a tool name to its budget class. The mapping is coarse by
// design — operators tune caps at class granularity, not per tool name.
// Unknown names map to ClassDefault.
func ToolClass(name string) string {
	switch name {
	case "web_search", "web_fetch", "web":
		return ClassWeb
	case "search", "search_memory", "read_memory", "list_memory", "forget_memory":
		return ClassWiki
	case "execute_code", "execute_bash", "execute_shell":
		return ClassExec
	case "source", "read_source", "store_source", "ingest_source", "ocr_source":
		return ClassSource
	case "task", "list_tasks", "cancel_task":
		return ClassScheduler
	case "ask_user":
		return ClassAskUser
	}
	// schedule_* prefix — matches any schedule tool variant
	if len(name) > 9 && name[:9] == "schedule_" {
		return ClassScheduler
	}
	return ClassDefault
}

// BudgetTracker enforces per-turn per-class tool call caps. One tracker per
// agent turn; per-turn reset is achieved by constructing a fresh tracker for
// each RunTask invocation.
//
// Safe for concurrent use: parallel tool dispatches in the same iteration
// may call Check simultaneously.
type BudgetTracker struct {
	mu     sync.Mutex
	counts map[string]int
	caps   map[string]int
}

// NewBudgetTracker returns a tracker with the supplied caps merged over the
// hardcoded defaults. Keys in caps with values outside [1, 100] are ignored
// (fail-soft: bad parse leaves the default in place). Nil caps uses all defaults.
func NewBudgetTracker(caps map[string]int) *BudgetTracker {
	merged := defaultCaps()
	for k, v := range caps {
		if v >= 1 && v <= 100 {
			merged[k] = v
		}
	}
	return &BudgetTracker{
		counts: make(map[string]int, len(merged)),
		caps:   merged,
	}
}

// Check atomically increments the class counter and returns an error when the
// cap is exceeded. The toolName argument is included in the error for context.
// Returns nil for calls within budget.
func (b *BudgetTracker) Check(class, toolName string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.counts[class]++
	cap := b.caps[class]
	if cap <= 0 {
		cap = b.caps[ClassDefault]
	}
	if b.counts[class] > cap {
		return fmt.Errorf("%s budget exhausted (%d/%d). Use a different tool class or finalize with what you have",
			class, b.counts[class], cap)
	}
	return nil
}
