package conversations

import (
	"strings"
	"testing"
	"time"
)

// validRolloutStateFixture returns a state that satisfies validateRolloutState;
// individual fields are mutated per table case to exercise each rejection branch.
func validRolloutStateFixture() RolloutState {
	return RolloutState{
		ScopeID: "deployment-scope", Stage: "disabled", StageStartedAt: time.Now().UTC(),
		EvaluatorVersion: "eval-v1", ScorerVersion: "score-v1", ConfigVersion: "config-v1", CorpusVersion: "corpus-v1",
		StratumSnapshots: []byte(`{}`), FailureWindow: []byte(`{}`), LatencyWindow: []byte(`{}`), RestoreWindow: []byte(`{}`),
		ActiveConfig: []byte(`{"mode":"disabled"}`), LastKnownGoodConfig: []byte(`{"mode":"disabled"}`), LastKnownGoodPolicy: []byte(`{}`),
	}
}

func TestValidateRolloutStateRejectionBranches(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*RolloutState)
		wantSub string
	}{
		{"missing scope", func(s *RolloutState) { s.ScopeID = "" }, "scope, stage, and versions are required"},
		{"missing stage", func(s *RolloutState) { s.Stage = "" }, "scope, stage, and versions are required"},
		{"missing evaluator version", func(s *RolloutState) { s.EvaluatorVersion = "" }, "scope, stage, and versions are required"},
		{"missing scorer version", func(s *RolloutState) { s.ScorerVersion = "" }, "scope, stage, and versions are required"},
		{"missing config version", func(s *RolloutState) { s.ConfigVersion = "" }, "scope, stage, and versions are required"},
		{"missing corpus version", func(s *RolloutState) { s.CorpusVersion = "" }, "scope, stage, and versions are required"},
		{"stratum snapshots not JSON object", func(s *RolloutState) { s.StratumSnapshots = []byte(`[]`) }, "stratum snapshots"},
		{"failure window malformed JSON", func(s *RolloutState) { s.FailureWindow = []byte(`{`) }, "failure window"},
		{"latency window empty", func(s *RolloutState) { s.LatencyWindow = nil }, "latency window"},
		{"restore window scalar", func(s *RolloutState) { s.RestoreWindow = []byte(`"x"`) }, "restore window"},
		{"active config not object", func(s *RolloutState) { s.ActiveConfig = []byte(`42`) }, "active config"},
		{"last-known-good config malformed", func(s *RolloutState) { s.LastKnownGoodConfig = []byte(`{bad}`) }, "last-known-good config"},
		{"last-known-good policy not object", func(s *RolloutState) { s.LastKnownGoodPolicy = []byte(`null`) }, "last-known-good policy"},
		{"valid state passes", func(s *RolloutState) {}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := validRolloutStateFixture()
			tc.mutate(&state)
			err := validateRolloutState(state)
			if tc.wantSub == "" {
				if err != nil {
					t.Fatalf("validateRolloutState() unexpected err=%v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("validateRolloutState() err=%v, want substring %q", err, tc.wantSub)
			}
		})
	}
}

func validRolloutEvidenceFixture(scope string) RolloutEvidence {
	return RolloutEvidence{
		ScopeID: scope, Digest: strings.Repeat("a", 64),
		EvaluatorVersion: "eval-v1", ScorerVersion: "score-v1", ConfigVersion: "config-v1", CorpusVersion: "corpus-v1",
		Snapshot: []byte(`{"quality":"pass"}`),
	}
}

func TestValidateEvidenceRejectionBranches(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*RolloutEvidence)
		wantSub string
	}{
		{"scope mismatch", func(e *RolloutEvidence) { e.ScopeID = "other-scope" }, "scope or SHA-256 digest is invalid"},
		{"digest too short", func(e *RolloutEvidence) { e.Digest = strings.Repeat("a", 63) }, "scope or SHA-256 digest is invalid"},
		{"digest uppercase hex rejected", func(e *RolloutEvidence) { e.Digest = strings.Repeat("A", 64) }, "scope or SHA-256 digest is invalid"},
		{"digest non-hex rejected", func(e *RolloutEvidence) { e.Digest = strings.Repeat("z", 64) }, "scope or SHA-256 digest is invalid"},
		{"missing evaluator version", func(e *RolloutEvidence) { e.EvaluatorVersion = "" }, "versions are required"},
		{"missing scorer version", func(e *RolloutEvidence) { e.ScorerVersion = "" }, "versions are required"},
		{"missing config version", func(e *RolloutEvidence) { e.ConfigVersion = "" }, "versions are required"},
		{"missing corpus version", func(e *RolloutEvidence) { e.CorpusVersion = "" }, "versions are required"},
		{"snapshot not JSON object", func(e *RolloutEvidence) { e.Snapshot = []byte(`[1,2]`) }, "must be a JSON object"},
		{"valid evidence passes", func(e *RolloutEvidence) {}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evidence := validRolloutEvidenceFixture("scope-x")
			tc.mutate(&evidence)
			err := validateEvidence("scope-x", evidence)
			if tc.wantSub == "" {
				if err != nil {
					t.Fatalf("validateEvidence() unexpected err=%v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("validateEvidence() err=%v, want substring %q", err, tc.wantSub)
			}
		})
	}
}

func TestValidateReasonAcceptsLocaleNeutralCodesOnly(t *testing.T) {
	for _, tc := range []struct {
		code string
		ok   bool
	}{
		{"quality_gate_passed", true},
		{"restore_rate_exceeded", true},
		{"a1_2", true},
		{"", false},
		{"Has-Dash", false},
		{"has space", false},
		{"UPPERCASE", false},
		{"emoji🎉", false},
	} {
		t.Run(tc.code, func(t *testing.T) {
			err := validateReason(tc.code)
			if tc.ok && err != nil {
				t.Fatalf("validateReason(%q) unexpected err=%v", tc.code, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("validateReason(%q) accepted an invalid reason code", tc.code)
			}
		})
	}
}

func TestValidateJSONObjectBranches(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value []byte
		ok    bool
	}{
		{"empty", nil, false},
		{"blank", []byte("   "), false},
		{"malformed", []byte(`{"a":`), false},
		{"array", []byte(`[1,2,3]`), false},
		{"string scalar", []byte(`"hello"`), false},
		{"number scalar", []byte(`7`), false},
		{"null", []byte(`null`), false},
		{"empty object", []byte(`{}`), true},
		{"nested object", []byte(`{"a":{"b":1}}`), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateJSONObject(tc.value)
			if tc.ok && err != nil {
				t.Fatalf("validateJSONObject(%s) unexpected err=%v", tc.value, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("validateJSONObject(%s) accepted an invalid value", tc.value)
			}
		})
	}
}

func TestValidateTransitionRejectionBranches(t *testing.T) {
	validTransition := func() RolloutTransition {
		state := validRolloutStateFixture()
		state.Version = 2
		return RolloutTransition{ExpectedVersion: 2, State: state, Evidence: validRolloutEvidenceFixture(state.ScopeID), ReasonCode: "quality_gate_passed"}
	}
	t.Run("expected version below one", func(t *testing.T) {
		tr := validTransition()
		tr.ExpectedVersion = 0
		tr.State.Version = 0
		if err := validateTransition(tr); err == nil || !strings.Contains(err.Error(), "expected version must match state version") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("expected version mismatches state version", func(t *testing.T) {
		tr := validTransition()
		tr.ExpectedVersion = 3
		if err := validateTransition(tr); err == nil || !strings.Contains(err.Error(), "expected version must match state version") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("invalid reason code", func(t *testing.T) {
		tr := validTransition()
		tr.ReasonCode = "Not Valid"
		if err := validateTransition(tr); err == nil || !strings.Contains(err.Error(), "locale-neutral code") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("invalid state propagates", func(t *testing.T) {
		tr := validTransition()
		tr.State.Stage = ""
		if err := validateTransition(tr); err == nil || !strings.Contains(err.Error(), "scope, stage, and versions are required") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("invalid evidence propagates", func(t *testing.T) {
		tr := validTransition()
		tr.Evidence.ScopeID = "mismatched-scope"
		if err := validateTransition(tr); err == nil || !strings.Contains(err.Error(), "scope or SHA-256 digest is invalid") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("valid transition passes", func(t *testing.T) {
		if err := validateTransition(validTransition()); err != nil {
			t.Fatalf("unexpected err=%v", err)
		}
	})
}

func TestValidateRollbackRejectionBranches(t *testing.T) {
	validRollback := func() RolloutRollback {
		return RolloutRollback{ScopeID: "scope-x", ExpectedVersion: 1, Evidence: validRolloutEvidenceFixture("scope-x"), ReasonCode: "restore_rate_exceeded"}
	}
	t.Run("expected version not positive", func(t *testing.T) {
		rb := validRollback()
		rb.ExpectedVersion = 0
		if err := validateRollback(rb); err == nil || !strings.Contains(err.Error(), "expected version must be positive") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("invalid reason code", func(t *testing.T) {
		rb := validRollback()
		rb.ReasonCode = ""
		if err := validateRollback(rb); err == nil || !strings.Contains(err.Error(), "locale-neutral code") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("invalid evidence propagates", func(t *testing.T) {
		rb := validRollback()
		rb.Evidence.Digest = "short"
		if err := validateRollback(rb); err == nil || !strings.Contains(err.Error(), "scope or SHA-256 digest is invalid") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("valid rollback passes", func(t *testing.T) {
		if err := validateRollback(validRollback()); err != nil {
			t.Fatalf("unexpected err=%v", err)
		}
	})
}
