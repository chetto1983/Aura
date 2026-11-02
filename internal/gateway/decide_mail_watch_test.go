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
		"status": scoring.Safe, "list_rules": scoring.Safe, "list_labels": scoring.Safe, "list_decisions": scoring.Safe,
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

var teachingCalls = map[string]string{
	"add_rule": `{"action":"add_rule","key":"a@b.it","tier":"later"}`,
	"correct":  `{"action":"correct","account_id":"work","item_id":"m1","tier":"later"}`,
}

// A rule filing a sender to later hides that person's mail from the digest, so an instruction
// injected into a job's reading must not be able to write one where nobody would see it.
func TestAHeadlessRunCannotTeachTheWatch(t *testing.T) {
	t.Parallel()
	spec := mailWatchSpec()
	for name, args := range teachingCalls {
		raw := json.RawMessage(args)
		store := &fakeStore{}
		g := New(config.ProfileSingleUserHardened, store)
		g.SetOperationRegistry(acquiringOperations())

		v, err := g.Decide(injectionOperationContext(t, spec, raw, name), spec, raw, testKey())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if v.Decision != Deny || v.Tier != scoring.Normal || !v.Refusal || !strings.Contains(v.Reason, "do not retry") {
			t.Fatalf("%s: verdict = %+v", name, v)
		}
		if len(store.reserves()) != 0 {
			t.Fatalf("%s: a refused action reached the reservation", name)
		}
		facts := store.calls()
		if len(facts) != 1 || facts[0].Meta["reason"] != "needs_person" {
			t.Fatalf("%s: deny facts = %+v", name, facts)
		}
	}
}

// "Di queste non avvisarmi più" stays one sentence: a person in the conversation gets no prompt,
// on either strict profile, and the call is reserved like any other write.
func TestALiveChatTeachesTheWatchWithNoPrompt(t *testing.T) {
	t.Parallel()
	spec := mailWatchSpec()
	for _, profile := range []config.RuntimeProfile{config.ProfileSingleUserHardened, config.ProfileServerProduction} {
		for name, args := range teachingCalls {
			raw := json.RawMessage(args)
			store := &fakeStore{}
			g := New(profile, store)
			g.SetOperationRegistry(acquiringOperations())

			ctx := WithResponder(injectionOperationContext(t, spec, raw, name))
			v, err := g.Decide(ctx, spec, raw, testKey())
			if err != nil {
				t.Fatalf("%s %s: %v", profile, name, err)
			}
			if v.Decision != Allow || v.ApprovalRequest != nil || v.Tier != scoring.Normal {
				t.Fatalf("%s %s: verdict = %+v", profile, name, v)
			}
			if len(store.reserves()) != 1 {
				t.Fatalf("%s %s: reservations = %d, want 1", profile, name, len(store.reserves()))
			}
		}
	}
}

// runChild builds a worker's context from the turn that spawned it, so the worker inherits the
// responder marker and still has nobody on it: its brief says it cannot see the user.
func TestASwarmWorkerInALiveChatCannotTeachTheWatch(t *testing.T) {
	t.Parallel()
	spec := mailWatchSpec()
	for name, args := range teachingCalls {
		raw := json.RawMessage(args)
		store := &fakeStore{}
		g := New(config.ProfileSingleUserHardened, store)
		g.SetOperationRegistry(acquiringOperations())

		ctx := WithDelegatedDispatch(WithResponder(injectionOperationContext(t, spec, raw, name)))
		v, err := g.Decide(ctx, spec, raw, testKey())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if v.Decision != Deny || !v.Refusal {
			t.Fatalf("%s: verdict = %+v", name, v)
		}
		if len(store.reserves()) != 0 {
			t.Fatalf("%s: a refused action reached the reservation", name)
		}
	}
}

func TestAHeadlessRunKeepsTheWatchsOtherVerbs(t *testing.T) {
	t.Parallel()
	spec := mailWatchSpec()
	for name, args := range map[string]string{
		"status":         `{"action":"status"}`,
		"list_rules":     `{"action":"list_rules"}`,
		"list_labels":    `{"action":"list_labels"}`,
		"list_decisions": `{"action":"list_decisions","account_id":"work"}`,
		"enable":         `{"action":"enable","account_id":"work"}`,
	} {
		raw := json.RawMessage(args)
		g := New(config.ProfileSingleUserHardened, &fakeStore{})
		g.SetOperationRegistry(acquiringOperations())

		v, err := g.Decide(injectionOperationContext(t, spec, raw, name), spec, raw, testKey())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if v.Decision != Allow {
			t.Fatalf("%s: verdict = %+v", name, v)
		}
	}
}

func TestOnlyTheWatchsTeachingVerbsNeedAPerson(t *testing.T) {
	t.Parallel()
	for subject, want := range map[grantSubject]bool{
		{Tool: "mail_watch", Action: "add_rule"}: true,
		{Tool: "mail_watch", Action: "correct"}:  true,
		{Tool: "mail_watch", Action: "enable"}:   false,
		{Tool: "mail_watch", Action: "unknown"}:  false,
		{Tool: "mail_watch"}:                     false,
		{Tool: "task", Action: "correct"}:        false,
	} {
		if got := needsPerson(subject); got != want {
			t.Errorf("needsPerson(%+v) = %v, want %v", subject, got, want)
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
