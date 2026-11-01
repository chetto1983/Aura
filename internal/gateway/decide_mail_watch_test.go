package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/scoring"
)

func TestMailWatchActionsCarryTheirTiers(t *testing.T) {
	t.Parallel()
	for action, want := range map[string]scoring.RiskTier{
		"status": scoring.Safe, "list_rules": scoring.Safe, "list_labels": scoring.Safe,
		"enable": scoring.Normal, "correct": scoring.Normal, "add_rule": scoring.Normal,
		"disable": scoring.Destructive, "remove_rule": scoring.Destructive,
		"forget_everything": scoring.Risky,
	} {
		if got := classify(mailWatchSpec(), json.RawMessage(`{"action":"`+action+`"}`)); got != want {
			t.Errorf("%s = %s, want %s", action, got, want)
		}
	}
	if got := classify(mailWatchSpec(), json.RawMessage(`not json`)); got != scoring.Risky {
		t.Errorf("malformed = %s, want risky", got)
	}
}

// A scheduled job has no responder, so mail it reads cannot talk it into switching the watch off
// or taking a rule back.
func TestAHeadlessRunCannotTurnTheWatchOffOrRemoveARule(t *testing.T) {
	t.Parallel()
	spec := mailWatchSpec()
	for name, args := range map[string]string{
		"disable":     `{"action":"disable","account_id":"work"}`,
		"remove_rule": `{"action":"remove_rule","rule_id":"r1"}`,
	} {
		raw := json.RawMessage(args)
		store := &fakeStore{}
		g := New(config.ProfileSingleUserHardened, store)
		g.SetOperationRegistry(acquiringOperations())

		v, err := g.Decide(injectionOperationContext(t, spec, raw, name), spec, raw, testKey())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if v.Decision != Deny || v.Tier != scoring.Destructive || !strings.Contains(v.Reason, "no interactive approver") {
			t.Fatalf("%s: verdict = %+v", name, v)
		}
		if len(store.reserves()) != 0 {
			t.Fatalf("%s: a denied action reached the reservation", name)
		}
	}
}

func TestAHeadlessRunMayReadTheWatch(t *testing.T) {
	t.Parallel()
	spec := mailWatchSpec()
	raw := json.RawMessage(`{"action":"status"}`)
	g := New(config.ProfileSingleUserHardened, &fakeStore{})
	g.SetOperationRegistry(acquiringOperations())

	v, err := g.Decide(injectionOperationContext(t, spec, raw, "read"), spec, raw, testKey())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if v.Decision != Allow {
		t.Fatalf("verdict = %+v", v)
	}
}
