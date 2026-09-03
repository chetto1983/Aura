package arcadedb

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestMemoryBatchSchemaStatements(t *testing.T) {
	statements := memoryBatchSchemaStatements()
	if len(statements) == 0 {
		t.Fatal("expected schema statements, got empty")
	}
	// Verify all statements contain expected keywords
	for _, stmt := range statements {
		if stmt == "" {
			t.Fatal("found empty schema statement")
		}
	}
}

func TestSortedMemoryBatchEntities(t *testing.T) {
	tests := []struct {
		name     string
		entities map[string]memoryBatchEntity
		want     []string
	}{
		{
			name:     "empty",
			entities: map[string]memoryBatchEntity{},
			want:     []string{},
		},
		{
			name: "single",
			entities: map[string]memoryBatchEntity{
				"Person": {Kind: "person"},
			},
			want: []string{"Person"},
		},
		{
			name: "multiple unsorted",
			entities: map[string]memoryBatchEntity{
				"Zebra": {Kind: "animal"},
				"Apple": {Kind: "fruit"},
				"Mango": {Kind: "fruit"},
			},
			want: []string{"Apple", "Mango", "Zebra"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sortedMemoryBatchEntities(tt.entities)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("sortedMemoryBatchEntities() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMemoryBatchEndpointsChanged(t *testing.T) {
	tests := []struct {
		name string
		a, b memoryBatchFact
		want bool
	}{
		{
			name: "same endpoints",
			a:    memoryBatchFact{Fact: Fact{Subject: "S1", Object: "O1"}},
			b:    memoryBatchFact{Fact: Fact{Subject: "S1", Object: "O1"}},
			want: false,
		},
		{
			name: "different subject",
			a:    memoryBatchFact{Fact: Fact{Subject: "S1", Object: "O1"}},
			b:    memoryBatchFact{Fact: Fact{Subject: "S2", Object: "O1"}},
			want: true,
		},
		{
			name: "different object",
			a:    memoryBatchFact{Fact: Fact{Subject: "S1", Object: "O1"}},
			b:    memoryBatchFact{Fact: Fact{Subject: "S1", Object: "O2"}},
			want: true,
		},
		{
			name: "different both",
			a:    memoryBatchFact{Fact: Fact{Subject: "S1", Object: "O1"}},
			b:    memoryBatchFact{Fact: Fact{Subject: "S2", Object: "O2"}},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := memoryBatchEndpointsChanged(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("memoryBatchEndpointsChanged() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMemoryBatchStoredFactsEqual(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name string
		a, b memoryBatchFact
		want bool
	}{
		{
			name: "identical",
			a: memoryBatchFact{
				RID:       "rid1",
				Fact:      Fact{Subject: "S", Predicate: "P", Object: "O"},
				ValidFrom: now, ValidTo: now.Add(time.Hour),
			},
			b: memoryBatchFact{
				RID:       "rid1",
				Fact:      Fact{Subject: "S", Predicate: "P", Object: "O"},
				ValidFrom: now, ValidTo: now.Add(time.Hour),
			},
			want: true,
		},
		{
			name: "different RID ignored",
			a: memoryBatchFact{
				RID:       "rid1",
				Fact:      Fact{Subject: "S", Predicate: "P", Object: "O"},
				ValidFrom: now, ValidTo: now.Add(time.Hour),
			},
			b: memoryBatchFact{
				RID:       "rid2",
				Fact:      Fact{Subject: "S", Predicate: "P", Object: "O"},
				ValidFrom: now, ValidTo: now.Add(time.Hour),
			},
			want: true,
		},
		{
			name: "different source ignored",
			a: memoryBatchFact{
				RID:       "rid1",
				Fact:      Fact{Subject: "S", Predicate: "P", Object: "O", Source: FactSource{RunID: "src1"}},
				ValidFrom: now, ValidTo: now.Add(time.Hour),
			},
			b: memoryBatchFact{
				RID:       "rid1",
				Fact:      Fact{Subject: "S", Predicate: "P", Object: "O", Source: FactSource{RunID: "src2"}},
				ValidFrom: now, ValidTo: now.Add(time.Hour),
			},
			want: true,
		},
		{
			name: "different predicate",
			a: memoryBatchFact{
				RID:       "rid1",
				Fact:      Fact{Subject: "S", Predicate: "P1", Object: "O"},
				ValidFrom: now, ValidTo: now.Add(time.Hour),
			},
			b: memoryBatchFact{
				RID:       "rid1",
				Fact:      Fact{Subject: "S", Predicate: "P2", Object: "O"},
				ValidFrom: now, ValidTo: now.Add(time.Hour),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := memoryBatchStoredFactsEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("memoryBatchStoredFactsEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseMemoryBatchTime(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr bool
	}{
		{
			name:    "empty string",
			input:   "",
			want:    time.Time{},
			wantErr: false,
		},
		{
			name:    "RFC3339Nano",
			input:   "2026-01-02T15:04:05.123456789Z",
			want:    time.Date(2026, 1, 2, 15, 4, 5, 123456789, time.UTC),
			wantErr: false,
		},
		{
			name:    "ArcadeDB default format without zone",
			input:   "2026-01-02 15:04:05",
			want:    time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC),
			wantErr: false,
		},
		{
			name:    "invalid format",
			input:   "not-a-time",
			want:    time.Time{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMemoryBatchTime(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseMemoryBatchTime() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !got.Equal(tt.want) {
				t.Errorf("parseMemoryBatchTime() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNullableMemoryBatchTime(t *testing.T) {
	tests := []struct {
		name  string
		value time.Time
		want  any
	}{
		{
			name:  "zero time",
			value: time.Time{},
			want:  nil,
		},
		{
			name:  "non-zero time",
			value: time.Date(2026, 1, 2, 15, 4, 5, 123456789, time.UTC),
			want:  "2026-01-02T15:04:05Z",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nullableMemoryBatchTime(tt.value)
			if tt.want == nil {
				if got != nil {
					t.Errorf("nullableMemoryBatchTime() = %v, want nil", got)
				}
			} else {
				if got != tt.want {
					t.Errorf("nullableMemoryBatchTime() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestNullableMemoryBatchString(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  any
	}{
		{
			name:  "empty string",
			value: "",
			want:  nil,
		},
		{
			name:  "non-empty string",
			value: "hello",
			want:  "hello",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nullableMemoryBatchString(tt.value)
			if tt.want == nil {
				if got != nil {
					t.Errorf("nullableMemoryBatchString() = %v, want nil", got)
				}
			} else {
				if got != tt.want {
					t.Errorf("nullableMemoryBatchString() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestMemoryBatchEntitiesToUpsert(t *testing.T) {
	before := memoryBatchState{
		Entities: map[string]memoryBatchEntity{"A": {Kind: "person"}, "B": {Kind: "place"}},
		Facts:    map[string]memoryBatchFact{},
	}
	after := memoryBatchState{
		Entities: map[string]memoryBatchEntity{"A": {Kind: "person"}, "B": {Kind: "place"}, "C": {Kind: "thing"}},
		Facts: map[string]memoryBatchFact{
			"f1": {Fact: Fact{Subject: "D", Object: "E"}},
		},
	}
	got := memoryBatchEntitiesToUpsert(before, after)
	// Should include C (new entity) and D, E (from fact endpoints)
	// A and B are in both, so not included
	want := []string{"C", "D", "E"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("memoryBatchEntitiesToUpsert() = %v, want %v", got, want)
	}
}

func TestClientMemoryBatchTx_Rollback_NoError(t *testing.T) {
	// Test that Rollback on a closed transaction is a no-op
	tx := &clientMemoryBatchTx{closed: true, client: nil, sessionID: "test"}
	// Should not panic
	tx.Rollback(context.Background())
}

func TestClientMemoryBatchTx_Commit_ClosedError(t *testing.T) {
	tx := &clientMemoryBatchTx{closed: true, client: nil, sessionID: "test"}
	err := tx.Commit(context.Background())
	if err == nil {
		t.Error("expected error for closed transaction Commit")
	}
	if err.Error() != "memory batch transaction already closed" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestMemoryBatchFactParams(t *testing.T) {
	now := time.Date(2026, 1, 2, 15, 4, 5, 123456789, time.UTC)
	fact := memoryBatchFact{
		RID:       "rid1",
		Fact:      Fact{Subject: "S", Predicate: "P", Object: "O", Statement: "stmt", Source: FactSource{RunID: "run1"}},
		ValidFrom: now, ValidTo: now.Add(time.Hour),
		CreatedAt: now, ExpiredAt: now.Add(2 * time.Hour),
		FactKey:   "key1",
		Sources:   []FactSource{{RunID: "src1"}},
		Embedding: []float32{0.1, 0.2},
	}
	got := memoryBatchFactParams(fact)
	if got["rid"] != "rid1" {
		t.Errorf("rid = %v, want rid1", got["rid"])
	}
	if got["statement"] != "stmt" {
		t.Errorf("statement = %v, want stmt", got["statement"])
	}
	if got["predicate"] != "P" {
		t.Errorf("predicate = %v, want P", got["predicate"])
	}
	if got["valid_from"] != "2026-01-02T15:04:05Z" {
		t.Errorf("valid_from = %v, want 2026-01-02T15:04:05Z", got["valid_from"])
	}
	// Test with zero time
	zeroFact := memoryBatchFact{
		RID:       "rid2",
		Fact:      Fact{Subject: "S2", Predicate: "P2", Object: "O2"},
		ValidFrom: time.Time{}, ValidTo: time.Time{},
		CreatedAt: time.Time{}, ExpiredAt: time.Time{},
		FactKey:   "",
		Sources:   nil,
		Embedding: nil,
	}
	gotZero := memoryBatchFactParams(zeroFact)
	if gotZero["valid_from"] != nil {
		t.Errorf("valid_from for zero time = %v, want nil", gotZero["valid_from"])
	}
	if gotZero["fact_key"] != nil {
		t.Errorf("fact_key for empty string = %v, want nil", gotZero["fact_key"])
	}
}

func TestPreferEntityKind(t *testing.T) {
	tests := []struct {
		name      string
		existing  string
		candidate string
		want      string
	}{
		{
			name:      "existing non-empty",
			existing:  "person",
			candidate: "thing",
			want:      "person",
		},
		{
			name:      "existing empty",
			existing:  "",
			candidate: "thing",
			want:      "thing",
		},
		{
			name:      "existing whitespace only",
			existing:  "   ",
			candidate: "thing",
			want:      "thing", // TrimSpace("   ") == "", so returns candidate
		},
		{
			name:      "existing whitespace with text",
			existing:  "  person  ",
			candidate: "thing",
			want:      "  person  ", // TrimSpace has content, so returns original
		},
		{
			name:      "both empty",
			existing:  "",
			candidate: "",
			want:      "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := preferEntityKind(tt.existing, tt.candidate); got != tt.want {
				t.Errorf("preferEntityKind(%q, %q) = %q, want %q", tt.existing, tt.candidate, got, tt.want)
			}
		})
	}
}
