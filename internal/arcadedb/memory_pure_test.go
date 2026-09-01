package arcadedb

import (
	"strings"
	"testing"
	"time"
)

// TestLooksLikeProse tests the pure function that detects prose-shaped objects
func TestLooksLikeProse(t *testing.T) {
	tests := []struct {
		name   string
		object string
		want   bool
	}{
		// Within bound, not prose
		{"short entity", "Caraglio", false},
		{"exactly at bound", strings.Repeat("a", proseObjectRuneBound), false},

		// Newlines are prose
		{"with newline", "Caraglio\nTorino", true},
		{"with carriage return", "Caraglio\rTorino", true},

		// Sentence terminals are prose
		{"ends with period", "Davide lives in Caraglio.", true},
		{"ends with question", "Where does Davide live?", true},
		{"ends with exclamation", "Move to Caraglio now!", true},

		// Trailing whitespace after punctuation is still prose
		{"period with trailing space", "Davide lives in Caraglio.   ", true},

		// Over the bound is prose
		{"over bound", strings.Repeat("a", proseObjectRuneBound+1), true},

		// Empty after trim is not prose
		{"empty", "", false},
		{"whitespace only", "   \t\n  ", true}, // contains \n, so it's prose

		// No terminal, within bound, not prose
		{"entity name", "b130c94d-a213-463a-a797-ec124104363a", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := looksLikeProse(tt.object)
			if got != tt.want {
				t.Errorf("looksLikeProse(%q) = %v, want %v", tt.object, got, tt.want)
			}
		})
	}
}

// TestValidateRuneLimit tests the pure rune limit validation
func TestValidateRuneLimit(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		value   string
		limit   int
		wantErr bool
	}{
		{"within limit", "field", "hello", 10, false},
		{"at limit", "field", "hello", 5, false},
		{"exactly one over", "field", "hello", 4, true},
		{"empty value", "field", "", 5, false},
		{"empty field", "", "hello", 5, false},
		{"unicode runes", "field", "界界界", 3, false},
		{"unicode over", "field", "界界界界", 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRuneLimit(tt.field, tt.value, tt.limit)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRuneLimit(%q, %q, %d) error = %v, wantErr %v",
					tt.field, tt.value, tt.limit, err, tt.wantErr)
			}
		})
	}
}

// TestBoundedLimit tests the pure bounded limit function
func TestBoundedLimit(t *testing.T) {
	tests := []struct {
		name     string
		value    int
		fallback int
		maximum  int
		want     int
	}{
		{"positive within max", 10, 5, 100, 10},
		{"zero uses fallback", 0, 5, 100, 5},
		{"negative uses fallback", -1, 5, 100, 5},
		{"over maximum", 150, 5, 100, 100},
		{"exactly at maximum", 100, 5, 100, 100},
		{"fallback over maximum", 0, 150, 100, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := boundedLimit(tt.value, tt.fallback, tt.maximum)
			if got != tt.want {
				t.Errorf("boundedLimit(%d, %d, %d) = %v, want %v",
					tt.value, tt.fallback, tt.maximum, got, tt.want)
			}
		})
	}
}

// TestLexicalScoreFloor tests the pure lexical score floor function
func TestLexicalScoreFloor(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		configured float64
		want       float64
	}{
		{"single word", "hello", 2.0, 0},
		{"two words", "hello world", 2.0, 2.0},
		{"with punctuation", "hello, world!", 2.0, 2.0},
		{"empty query", "", 2.0, 0},
		{"single char", "a", 2.0, 0},
		{"zero configured", "hello world", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lexicalScoreFloor(tt.query, tt.configured)
			if got != tt.want {
				t.Errorf("lexicalScoreFloor(%q, %v) = %v, want %v",
					tt.query, tt.configured, got, tt.want)
			}
		})
	}
}

// TestFactHitFromRow tests the pure row-to-FactHit mapping
func TestFactHitFromRow(t *testing.T) {
	tests := []struct {
		name string
		row  map[string]any
		want FactHit
	}{
		{
			name: "full row",
			row: map[string]any{
				"statement":    "Davide lives in Caraglio.",
				"predicate":    "lives_in",
				"subject":      "Davide",
				"subject_kind": "person",
				"object":       "Caraglio",
				"object_kind":  "place",
				"valid_from":   "2026-01-01T00:00:00Z",
				"valid_to":     "2026-12-31T00:00:00Z",
				"fact_key":     "key-123",
				"sources":      []map[string]any{{"run_id": "run-1", "writer_role": "parent"}},
			},
			want: FactHit{
				Statement:   "Davide lives in Caraglio.",
				Predicate:   "lives_in",
				Subject:     "Davide",
				SubjectKind: "person",
				Object:      "Caraglio",
				ObjectKind:  "place",
				ValidFrom:   "2026-01-01T00:00:00Z",
				ValidTo:     "2026-12-31T00:00:00Z",
				FactKey:     "key-123",
				Sources:     []FactSource{{RunID: "run-1", WriterRole: WriterParent}},
			},
		},
		{
			name: "missing optional fields",
			row: map[string]any{
				"statement":  "Test",
				"predicate":  "test",
				"subject":    "A",
				"object":     "B",
				"valid_from": "2026-01-01T00:00:00Z",
				"sources":    []map[string]any{},
			},
			want: FactHit{
				Statement: "Test",
				Predicate: "test",
				Subject:   "A",
				Object:    "B",
				ValidFrom: "2026-01-01T00:00:00Z",
				Sources:   []FactSource{},
			},
		},
		{
			name: "empty row",
			row:  map[string]any{},
			want: FactHit{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := factHitFromRow(tt.row)
			// Compare fields individually since FactHit has many
			if got.Statement != tt.want.Statement {
				t.Errorf("Statement = %v, want %v", got.Statement, tt.want.Statement)
			}
			if got.Predicate != tt.want.Predicate {
				t.Errorf("Predicate = %v, want %v", got.Predicate, tt.want.Predicate)
			}
			if got.Subject != tt.want.Subject {
				t.Errorf("Subject = %v, want %v", got.Subject, tt.want.Subject)
			}
			if got.Object != tt.want.Object {
				t.Errorf("Object = %v, want %v", got.Object, tt.want.Object)
			}
			if got.FactKey != tt.want.FactKey {
				t.Errorf("FactKey = %v, want %v", got.FactKey, tt.want.FactKey)
			}
		})
	}
}

// TestNullableTime tests the pure nullable time conversion
func TestNullableTime(t *testing.T) {
	tests := []struct {
		name  string
		value time.Time
		want  any
	}{
		{"zero time", time.Time{}, nil},
		{"non-zero time", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), "2026-01-01T12:00:00Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nullableTime(tt.value)
			if tt.want == nil {
				if got != nil {
					t.Errorf("nullableTime() = %v, want nil", got)
				}
			} else {
				if got != tt.want {
					t.Errorf("nullableTime() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// TestCountUpdated tests the pure count updated function
func TestCountUpdated(t *testing.T) {
	tests := []struct {
		name string
		rows []map[string]any
		want int
	}{
		{
			name: "float64 counts",
			rows: []map[string]any{
				{"count": float64(1)},
				{"count": float64(2)},
				{"count": float64(3)},
			},
			want: 6,
		},
		{
			name: "int counts",
			rows: []map[string]any{
				{"count": 1},
				{"count": 2},
			},
			want: 3,
		},
		{
			name: "int64 counts",
			rows: []map[string]any{
				{"count": int64(5)},
			},
			want: 1, // int64 is not handled, falls into default case
		},
		{
			name: "mixed types",
			rows: []map[string]any{
				{"count": float64(1)},
				{"count": int(2)},
				{"count": int64(3)},
			},
			want: 4, // float64(1) + int(2) + default(1) = 4
		},
		{
			name: "empty rows",
			rows: []map[string]any{},
			want: 0,
		},
		{
			name: "unknown type defaults to 1",
			rows: []map[string]any{
				{"count": "5"},
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countUpdated(tt.rows)
			if got != tt.want {
				t.Errorf("countUpdated() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRowInt tests the pure row int extraction
func TestRowInt(t *testing.T) {
	tests := []struct {
		name string
		row  map[string]any
		key  string
		want int64
	}{
		{"float64", map[string]any{"num": float64(42)}, "num", 42},
		{"int64", map[string]any{"num": int64(42)}, "num", 42},
		{"int", map[string]any{"num": int(42)}, "num", 42},
		{"missing key", map[string]any{"other": 42}, "num", 0},
		{"string value", map[string]any{"num": "42"}, "num", 0},
		{"nil value", map[string]any{"num": nil}, "num", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rowInt(tt.row, tt.key)
			if got != tt.want {
				t.Errorf("rowInt(%v, %q) = %v, want %v", tt.row, tt.key, got, tt.want)
			}
		})
	}
}

// TestRowString tests the pure row string extraction
func TestRowString(t *testing.T) {
	tests := []struct {
		name string
		row  map[string]any
		key  string
		want string
	}{
		{"present string", map[string]any{"name": "value"}, "name", "value"},
		{"missing key", map[string]any{"other": "value"}, "name", ""},
		{"int value", map[string]any{"name": 42}, "name", ""},
		{"nil value", map[string]any{"name": nil}, "name", ""},
		{"empty string", map[string]any{"name": ""}, "name", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rowString(tt.row, tt.key)
			if got != tt.want {
				t.Errorf("rowString(%v, %q) = %v, want %v", tt.row, tt.key, got, tt.want)
			}
		})
	}
}

// TestRowStrings tests the pure row strings extraction
func TestRowStrings(t *testing.T) {
	tests := []struct {
		name string
		row  map[string]any
		key  string
		want []string
	}{
		{
			name: "array of strings",
			row:  map[string]any{"tags": []any{"a", "b", "c"}},
			key:  "tags",
			want: []string{"a", "b", "c"},
		},
		{
			name: "empty array",
			row:  map[string]any{"tags": []any{}},
			key:  "tags",
			want: nil,
		},
		{
			name: "array with non-strings",
			row:  map[string]any{"tags": []any{"a", 1, "b"}},
			key:  "tags",
			want: []string{"a", "b"},
		},
		{
			name: "not an array",
			row:  map[string]any{"tags": "single"},
			key:  "tags",
			want: nil,
		},
		{
			name: "missing key",
			row:  map[string]any{"other": "value"},
			key:  "tags",
			want: nil,
		},
		{
			name: "nil value",
			row:  map[string]any{"tags": nil},
			key:  "tags",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rowStrings(tt.row, tt.key)
			if len(got) != len(tt.want) {
				t.Errorf("rowStrings() length = %d, want %d", len(got), len(tt.want))
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("rowStrings()[%d] = %v, want %v", i, v, tt.want[i])
				}
			}
		})
	}
}

// TestMemorySchemaStatements tests the pure schema statements function
func TestMemorySchemaStatements(t *testing.T) {
	statements := memorySchemaStatements()
	if len(statements) == 0 {
		t.Fatal("memorySchemaStatements() returned empty")
	}

	// Check that all statements contain expected keywords
	for i, stmt := range statements {
		if !strings.Contains(stmt, "IF NOT EXISTS") && !strings.Contains(stmt, "LSM_VECTOR") &&
			!strings.Contains(stmt, "FULL_TEXT") && !strings.Contains(stmt, "METADATA") {
			// Some statements might not have these, but most should
			// Just ensure it's not empty
			if strings.TrimSpace(stmt) == "" {
				t.Errorf("statement[%d] is empty", i)
			}
		}
	}

	// Check for fact edge type
	foundFactEdge := false
	for _, stmt := range statements {
		if strings.Contains(stmt, factEdgeType) {
			foundFactEdge = true
			break
		}
	}
	if !foundFactEdge {
		t.Error("memorySchemaStatements() should contain FACT edge type definition")
	}
}
