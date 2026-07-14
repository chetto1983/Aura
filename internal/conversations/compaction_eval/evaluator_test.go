package compaction_eval

import (
	"encoding/json"
	"math"
	"testing"
)

// evaluator.go shipped with the durable rollout runtime chain (42-09) but no
// dedicated unit test — the 42-10 mutation gate (docs/conversation-compaction.md)
// needs Evaluate's own contract pinned, not just exercised incidentally through
// the corpus census test's single happy-path call. [Rule 2 - missing critical:
// a blocking CI mutation gate over an untested pure seal is a correctness gap.]

func validInput() Input {
	return Input{
		ScopeID:          "scope-1",
		EligibleAttempts: 10,
		EvaluatorVersion: "v1",
		ScorerVersion:    "v1",
		ConfigVersion:    "v1",
		CorpusVersion:    "v1",
		Eligibility:      map[string]int64{"semantic_eligible": 10},
		Censors:          map[string]int64{"none": 10},
		Strata:           map[string]int64{"chat": 10},
		Gates:            Gates{L0Retention: 1, ToolPendingRetention: 0.99},
	}
}

func TestEvaluate_RejectsMissingRequiredFields(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Input)
	}{
		{"empty scope id", func(in *Input) { in.ScopeID = "" }},
		{"empty evaluator version", func(in *Input) { in.EvaluatorVersion = "" }},
		{"empty scorer version", func(in *Input) { in.ScorerVersion = "" }},
		{"empty config version", func(in *Input) { in.ConfigVersion = "" }},
		{"empty corpus version", func(in *Input) { in.CorpusVersion = "" }},
		{"negative eligible attempts", func(in *Input) { in.EligibleAttempts = -1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := validInput()
			c.mutate(&in)
			if _, err := Evaluate(in); err == nil {
				t.Fatalf("Evaluate(%s) = nil error, want rejection", c.name)
			}
		})
	}
}

func TestEvaluate_AcceptsCompleteInput(t *testing.T) {
	dec, err := Evaluate(validInput())
	if err != nil {
		t.Fatalf("Evaluate(valid): %v", err)
	}
	if dec.ScopeID != "scope-1" {
		t.Errorf("ScopeID = %q, want scope-1", dec.ScopeID)
	}
	if dec.EligibleAttempts != 10 {
		t.Errorf("EligibleAttempts = %d, want 10", dec.EligibleAttempts)
	}
	if len(dec.EvidenceDigest) != 64 {
		t.Errorf("EvidenceDigest = %q, want a 64-char hex sha256", dec.EvidenceDigest)
	}
	if dec.Gates != validInput().Gates {
		t.Errorf("Gates = %+v, want passthrough of input gates", dec.Gates)
	}
	if len(dec.Snapshot) == 0 {
		t.Fatal("Snapshot is empty")
	}
	var roundTrip Input
	if err := json.Unmarshal(dec.Snapshot, &roundTrip); err != nil {
		t.Fatalf("Snapshot does not round-trip as JSON: %v", err)
	}
	if roundTrip.ScopeID != "scope-1" {
		t.Errorf("Snapshot round-trip ScopeID = %q, want scope-1", roundTrip.ScopeID)
	}
}

func TestEvaluate_DigestIsDeterministicAndSensitiveToInput(t *testing.T) {
	dec1, err := Evaluate(validInput())
	if err != nil {
		t.Fatal(err)
	}
	dec2, err := Evaluate(validInput())
	if err != nil {
		t.Fatal(err)
	}
	if dec1.EvidenceDigest != dec2.EvidenceDigest {
		t.Errorf("digest not deterministic across identical inputs: %q != %q", dec1.EvidenceDigest, dec2.EvidenceDigest)
	}

	in3 := validInput()
	in3.EligibleAttempts = 999
	dec3, err := Evaluate(in3)
	if err != nil {
		t.Fatal(err)
	}
	if dec3.EvidenceDigest == dec1.EvidenceDigest {
		t.Error("digest unchanged despite a different EligibleAttempts value")
	}
}

func TestEvaluate_ClonesEligibilityCensorsStrataIndependently(t *testing.T) {
	in := validInput()
	dec, err := Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	// Mutate the ORIGINAL input maps after Evaluate; Decision must be unaffected.
	in.Eligibility["semantic_eligible"] = 999
	in.Censors["none"] = 999
	in.Strata["chat"] = 999
	if dec.Eligibility["semantic_eligible"] != 10 {
		t.Errorf("Decision.Eligibility aliased the input map: got %d, want 10", dec.Eligibility["semantic_eligible"])
	}
	if dec.Censors["none"] != 10 {
		t.Errorf("Decision.Censors aliased the input map: got %d, want 10", dec.Censors["none"])
	}
	if dec.Strata["chat"] != 10 {
		t.Errorf("Decision.Strata aliased the input map: got %d, want 10", dec.Strata["chat"])
	}

	// Mutate the DECISION's maps; a second Evaluate on fresh input must be unaffected.
	dec.Eligibility["semantic_eligible"] = 111
	dec2, err := Evaluate(validInput())
	if err != nil {
		t.Fatal(err)
	}
	if dec2.Eligibility["semantic_eligible"] != 10 {
		t.Errorf("cross-call map aliasing: got %d, want 10", dec2.Eligibility["semantic_eligible"])
	}
}

// TestEvaluate_ZeroEligibleAttemptsIsAccepted pins the exact "< 0" boundary
// (not "<= 0"): zero eligible attempts is a valid, unrejected observation.
func TestEvaluate_ZeroEligibleAttemptsIsAccepted(t *testing.T) {
	in := validInput()
	in.EligibleAttempts = 0
	dec, err := Evaluate(in)
	if err != nil {
		t.Fatalf("Evaluate(EligibleAttempts=0): %v, want acceptance", err)
	}
	if dec.EligibleAttempts != 0 {
		t.Errorf("EligibleAttempts = %d, want 0", dec.EligibleAttempts)
	}
}

// TestEvaluate_PropagatesMarshalError forces json.Marshal to fail (NaN has no
// JSON representation) and pins that Evaluate propagates the error rather than
// sealing a Decision over a truncated/empty snapshot.
func TestEvaluate_PropagatesMarshalError(t *testing.T) {
	in := validInput()
	in.Gates.L0Retention = math.NaN()
	if _, err := Evaluate(in); err == nil {
		t.Fatal("Evaluate with a NaN gate value = nil error, want the json.Marshal error propagated")
	}
}

func TestEvaluate_NilMapsCloneToEmptyNonNilMaps(t *testing.T) {
	in := validInput()
	in.Eligibility = nil
	in.Censors = nil
	in.Strata = nil
	dec, err := Evaluate(in)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Eligibility == nil || dec.Censors == nil || dec.Strata == nil {
		t.Fatal("nil input maps must clone to non-nil empty maps")
	}
	if len(dec.Eligibility) != 0 || len(dec.Censors) != 0 || len(dec.Strata) != 0 {
		t.Fatal("nil input maps must clone to EMPTY maps")
	}
}
