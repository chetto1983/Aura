package arcadedb

import (
	"strings"
	"testing"
	"time"
)

// TestStringActiveFactKey tests the pure active fact key string conversion
func TestStringActiveFactKey(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		key     string
		validTo time.Time
		want    string
	}{
		{
			name:    "active fact key",
			key:     "key-123",
			validTo: time.Time{}, // zero means still active
			want:    "key-123",
		},
		{
			name:    "active fact key with far future expiry",
			key:     "key-456",
			validTo: now.AddDate(1, 0, 0), // far in the future
			want:    "key-456",
		},
		{
			name:    "expired fact key",
			key:     "key-789",
			validTo: now.AddDate(0, -1, 0), // in the past
			want:    "",
		},
		{
			name:    "empty key",
			key:     "",
			validTo: time.Time{},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringActiveFactKey(tt.key, tt.validTo, now)
			if got != tt.want {
				t.Errorf("stringActiveFactKey(%q, %v, %v) = %q, want %q",
					tt.key, tt.validTo, now, got, tt.want)
			}
		})
	}
}

func TestApplyMemoryBatchForgetRemovesAnEntityNoFactHolds(t *testing.T) {
	state := memoryBatchState{
		Entities: map[string]memoryBatchEntity{"Paolo": {}, "Davide": {}, "Monica": {}},
		Facts: map[string]memoryBatchFact{"#1:0": {RID: "#1:0", Fact: Fact{
			Subject: "Davide", Predicate: "is_married_to", Object: "Monica",
		}}},
	}
	forget := func(filter ForgetFilter) (MemoryBatchOperationResult, error) {
		return applyMemoryBatchForget(state, MemoryBatchOperation{Type: MemoryBatchForget, Forget: &filter}, 0)
	}

	for _, filter := range []ForgetFilter{
		{Entity: "Nobody"},
		{Entity: "Paolo", KeepOrphans: true},
		{Subject: "Paolo"},
	} {
		if _, err := forget(filter); err == nil {
			t.Fatalf("forget %+v matched nothing and still succeeded", filter)
		}
	}
	got, err := forget(ForgetFilter{Entity: "Paolo"})
	if err != nil {
		t.Fatalf("forget the orphan: %v", err)
	}
	if got.Entities != 1 || got.Facts != 0 {
		t.Fatalf("result = %+v, want 1 entity and no facts", got)
	}
	if _, kept := state.Entities["Paolo"]; kept {
		t.Fatal("the orphan is still in the batch state")
	}
	if _, kept := state.Entities["Davide"]; !kept {
		t.Fatal("an entity a fact still holds was removed")
	}
}

// A narrower filter on a name that holds no facts still clears the name, as Forget does:
// with no facts there is nothing narrower to keep.
func TestApplyMemoryBatchForgetClearsAFactlessEntityWhateverTheNarrowing(t *testing.T) {
	for _, filter := range []ForgetFilter{
		{Entity: "Paolo", Predicate: "lives_in"},
		{Entity: "Paolo", SourceRunID: "run-9"},
	} {
		state := memoryBatchState{Entities: map[string]memoryBatchEntity{"Paolo": {}}, Facts: map[string]memoryBatchFact{}}
		got, err := applyMemoryBatchForget(state, MemoryBatchOperation{Type: MemoryBatchForget, Forget: &filter}, 0)
		if err != nil || got.Entities != 1 {
			t.Fatalf("forget %+v = %+v, %v; want the fact-less name removed", filter, got, err)
		}
	}
}

// A mention of a surviving fact holds the name; once that fact goes, so does the hold,
// and the name it held is pruned with the fact's endpoints.
func TestApplyMemoryBatchForgetKeepsANameAnotherFactStillMentions(t *testing.T) {
	state := memoryBatchState{
		Entities: map[string]memoryBatchEntity{"Acme": {}, "Monica": {}, "Davide": {}},
		Facts: map[string]memoryBatchFact{"#1:0": {RID: "#1:0", Fact: Fact{
			Subject: "Monica", Predicate: "met", Object: "Davide", Statement: "Monica met Davide at Acme.",
		}}},
		Mentions: []memoryBatchMention{{Source: "Monica", Target: "Acme", FactRID: "#1:0"}},
	}
	forget := func(filter ForgetFilter) (MemoryBatchOperationResult, error) {
		return applyMemoryBatchForget(state, MemoryBatchOperation{Type: MemoryBatchForget, Forget: &filter}, 0)
	}
	if _, err := forget(ForgetFilter{Entity: "Acme"}); err == nil || !strings.Contains(err.Error(), "still name it") {
		t.Fatalf("forget of a still-mentioned name = %v, want a refusal that says why", err)
	}
	got, err := forget(ForgetFilter{Subject: "Monica"})
	if err != nil {
		t.Fatalf("forget Monica's fact: %v", err)
	}
	if got.Entities != 3 {
		t.Fatalf("result = %+v, want Monica, Davide and the no-longer-mentioned Acme removed", got)
	}
}
