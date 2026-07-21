package main

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/onboarding"
)

type recordedCall struct {
	tool string
	args map[string]any
}

// fakeStore builds a memoryProfileStore with a recording call seam and a fixed clock.
func fakeStore() (*memoryProfileStore, *[]recordedCall) {
	var calls []recordedCall
	m := &memoryProfileStore{
		call: func(_ context.Context, tool string, args map[string]any) (string, error) {
			calls = append(calls, recordedCall{tool: tool, args: args})
			return `{"stored":true}`, nil
		},
		now: func() time.Time { return time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC) },
	}
	return m, &calls
}

func TestStoreConfirmedScopesEveryCallAndEndsWithSentinel(t *testing.T) {
	m, calls := fakeStore()
	err := m.StoreConfirmed(context.Background(), "id-uuid", onboarding.Answers{
		Name: "Davide", Company: "PmSync", Lang: "it",
	}, "# Agent.md draft")
	if err != nil {
		t.Fatalf("StoreConfirmed: %v", err)
	}
	if len(*calls) == 0 {
		t.Fatal("no memory calls recorded")
	}
	// EVERY call must carry user_identifier = identityID (the fail-open scope guard).
	for _, c := range *calls {
		if c.args["user_identifier"] != "id-uuid" {
			t.Errorf("call %q missing user_identifier=id-uuid: %#v", c.tool, c.args)
		}
	}
	// entities-first, then the raw-draft message, then the completed sentinel LAST.
	if (*calls)[0].tool != "memory_add_entity" {
		t.Errorf("first call = %q, want memory_add_entity", (*calls)[0].tool)
	}
	last := (*calls)[len(*calls)-1]
	if last.tool != "memory_add_fact" || last.args["predicate"] != onboarding.PredicateOnboardingCompleted {
		t.Errorf("last call = %q pred=%v, want completed sentinel", last.tool, last.args["predicate"])
	}
	if last.args["subject"] != "id-uuid" {
		t.Errorf("sentinel subject = %v, want identity UUID", last.args["subject"])
	}
	if !containsToolCall(*calls, "memory_store_message") {
		t.Error("raw draft was not stored as a message safety net")
	}
}

func TestStoreSkippedWritesOnlySkippedSentinel(t *testing.T) {
	m, calls := fakeStore()
	if err := m.StoreSkipped(context.Background(), "id-uuid"); err != nil {
		t.Fatalf("StoreSkipped: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("StoreSkipped calls = %d, want 1", len(*calls))
	}
	c := (*calls)[0]
	if c.tool != "memory_add_fact" || c.args["predicate"] != onboarding.PredicateOnboardingSkipped || c.args["user_identifier"] != "id-uuid" {
		t.Errorf("skip sentinel = %#v", c.args)
	}
}

func TestStoreConfirmedEmptyIdentityRefused(t *testing.T) {
	m, _ := fakeStore()
	if err := m.StoreConfirmed(context.Background(), "", onboarding.Answers{Name: "x"}, ""); err == nil {
		t.Fatal("StoreConfirmed with empty identity must error, not hit global scope")
	}
}

func TestStatusScansSentinelPredicates(t *testing.T) {
	cases := []struct {
		name         string
		facts        string
		wantC, wantS bool
		expectErr    bool
	}{
		{"completed", `{"facts":[{"predicate":"onboarding_completed"}]}`, true, false, false},
		{"skipped", `{"facts":[{"predicate":"onboarding_skipped"}]}`, false, true, false},
		{"none", `{"facts":[{"predicate":"role"}]}`, false, false, false},
		{"empty", `{"facts":[]}`, false, false, false},
		{"server-error", `{"error":"boom"}`, false, false, true},
		{"bad-json", `not json`, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &memoryProfileStore{
				call: func(_ context.Context, _ string, args map[string]any) (string, error) {
					if args["user_identifier"] != "id" {
						t.Errorf("status read missing user_identifier scope: %#v", args)
					}
					return tc.facts, nil
				},
				now: time.Now,
			}
			st, err := m.Status(context.Background(), "id")
			if tc.expectErr != (err != nil) {
				t.Fatalf("err = %v, expectErr = %v", err, tc.expectErr)
			}
			if st.Completed != tc.wantC || st.Skipped != tc.wantS {
				t.Errorf("state = %+v, want completed=%v skipped=%v", st, tc.wantC, tc.wantS)
			}
		})
	}
}

func containsToolCall(calls []recordedCall, tool string) bool {
	for _, c := range calls {
		if c.tool == tool {
			return true
		}
	}
	return false
}
