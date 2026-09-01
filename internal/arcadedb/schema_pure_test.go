package arcadedb

import (
	"testing"
)

// TestStringList tests the pure string list extraction
func TestStringList(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  []string
	}{
		{
			name:  "array of strings",
			value: []any{"a", "c", "b"},
			want:  []string{"a", "b", "c"}, // sorted
		},
		{
			name:  "array with maps",
			value: []any{"a", map[string]any{"name": "b"}, "c"},
			want:  []string{"a", "b", "c"}, // sorted, with name extracted from map
		},
		{
			name:  "array with maps without name",
			value: []any{"a", map[string]any{"other": "b"}, "c"},
			want:  []string{"a", "c"}, // sorted, map without name is skipped
		},
		{
			name:  "empty array",
			value: []any{},
			want:  nil,
		},
		{
			name:  "not an array",
			value: "string",
			want:  nil,
		},
		{
			name:  "nil",
			value: nil,
			want:  nil,
		},
		{
			name:  "array with all maps",
			value: []any{map[string]any{"name": "b"}, map[string]any{"name": "a"}},
			want:  []string{"a", "b"}, // sorted, names extracted
		},
		{
			name:  "empty strings in array",
			value: []any{"a", "", "b"},
			want:  []string{"", "a", "b"}, // sorted, empty strings are included
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringList(tt.value)
			if tt.want == nil && got != nil {
				t.Errorf("stringList() = %v, want nil", got)
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("stringList() length = %d, want %d", len(got), len(tt.want))
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("stringList()[%d] = %v, want %v", i, v, tt.want[i])
				}
			}
		})
	}
}

// TestStringField tests the pure string field extraction
func TestStringField(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"string", "hello", "hello"},
		{"int", 42, ""},
		{"nil", nil, ""},
		{"bool", true, ""},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringField(tt.value)
			if got != tt.want {
				t.Errorf("stringField(%v) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// TestIntField tests the pure int field extraction
func TestIntField(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  int64
	}{
		{"float64", float64(42), 42},
		{"int64", int64(42), 42},
		{"int", int(42), 42},
		{"string", "42", 0},
		{"nil", nil, 0},
		{"bool", true, 0},
		{"negative float64", float64(-42), -42},
		{"zero", float64(0), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intField(tt.value)
			if got != tt.want {
				t.Errorf("intField(%v) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
