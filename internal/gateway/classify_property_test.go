package gateway

import (
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/scoring"
	"pgregory.net/rapid"
)

// TestClassifyNonEnumeratedNeverSafe is the saturate-upward invariant (D-02, threat
// T-35-B): for ANY action string the model supplies that is not a live enumerated
// skill/task action, classify saturates to scoring.Risky — it never slips through as
// Safe. rapid explores arbitrary unicode/garbage action values; enumerated actions are
// the table test's job, so they are skipped here.
func TestClassifyNonEnumeratedNeverSafe(t *testing.T) {
	skill := skillSpec()
	task := taskSpec()
	skillActions := actionEnum(t, skill)
	taskActions := actionEnum(t, task)

	rapid.Check(t, func(rt *rapid.T) {
		action := rapid.String().Draw(rt, "action")

		raw, err := json.Marshal(map[string]string{"action": action})
		if err != nil {
			rt.Fatalf("marshal action %q: %v", action, err)
		}

		if !skillActions[action] {
			if got := classify(skill, raw); got != scoring.Risky {
				rt.Fatalf("classify(skill, non-enumerated %q) = %q, want Risky", action, got)
			}
		}
		if !taskActions[action] {
			if got := classify(task, raw); got != scoring.Risky {
				rt.Fatalf("classify(task, non-enumerated %q) = %q, want Risky", action, got)
			}
		}
	})
}

// TestClassifyParseFailNeverSafe proves a malformed argument blob (here: a truncated,
// therefore unparseable, JSON object built from any action bytes) always saturates to
// Risky for every multiplexed tool — a parse failure must NEVER downgrade risk.
func TestClassifyParseFailNeverSafe(t *testing.T) {
	skill := skillSpec()
	task := taskSpec()
	swarm := swarmSpec()

	rapid.Check(t, func(rt *rapid.T) {
		action := rapid.String().Draw(rt, "action")
		raw, err := json.Marshal(map[string]string{"action": action})
		if err != nil {
			rt.Fatalf("marshal: %v", err)
		}
		// Drop the closing brace to guarantee an unparseable object regardless of the
		// drawn action contents.
		truncated := json.RawMessage(raw[:len(raw)-1])

		if got := classify(skill, truncated); got != scoring.Risky {
			rt.Fatalf("classify(skill, parse-fail) = %q, want Risky", got)
		}
		if got := classify(task, truncated); got != scoring.Risky {
			rt.Fatalf("classify(task, parse-fail) = %q, want Risky", got)
		}
		// swarm_spawn ignores its args entirely — the tier never moves, garbage included.
		if got := classify(swarm, truncated); got != scoring.Normal {
			rt.Fatalf("classify(swarm_spawn, parse-fail) = %q, want Normal", got)
		}
	})
}
