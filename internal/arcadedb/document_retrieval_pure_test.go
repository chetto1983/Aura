package arcadedb

import (
	"math"
	"testing"
)

// TestEscapeLikePrefix tests the pure LIKE prefix escaping
// Note: The current implementation only escapes \, %, and ?, not _
func TestEscapeLikePrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		want   string
	}{
		{"no special chars", "hello", "hello%"},
		{"with backslash", "path\\to\\file", "path\\\\to\\\\file%"},
		{"with percent", "100%", "100\\%%"},
		{"with underscore", "my_file", "my_file%"}, // _ is not escaped by current implementation
		{"mixed", "path\\100%_file", "path\\\\100\\%_file%"},
		{"empty", "", "%"},
		{"only backslash", "\\", "\\\\%"},
		{"only percent", "%", "\\%%"},
		{"only underscore", "_", "_%"}, // _ is not escaped by current implementation
		{"with question mark", "test?", "test\\?%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeLikePrefix(tt.prefix)
			if got != tt.want {
				t.Errorf("escapeLikePrefix(%q) = %q, want %q", tt.prefix, got, tt.want)
			}
		})
	}
}

// TestValidateDenseVector tests the pure dense vector validation
func TestValidateDenseVector(t *testing.T) {
	tests := []struct {
		name       string
		vector     []float64
		dimensions int
		wantErr    bool
	}{
		{"valid vector", []float64{0.1, 0.2, 0.3}, 3, false},
		{"empty vector", []float64{}, 0, false},
		{"dimension mismatch", []float64{0.1, 0.2}, 3, true},
		{"extra dimension", []float64{0.1, 0.2, 0.3, 0.4}, 3, true},
		{"negative dimension", []float64{0.1}, -1, true},
		{"zero dimension", []float64{0.1}, 0, true},
		{"nil vector", nil, 3, true},
		{"with NaN", []float64{0.1, math.NaN(), 0.3}, 3, true},
		{"with Inf", []float64{0.1, math.Inf(1), 0.3}, 3, true},
		{"with -Inf", []float64{0.1, math.Inf(-1), 0.3}, 3, true},
		{"exact match", []float64{0.1, 0.2, 0.3}, 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDenseVector(tt.vector, tt.dimensions)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateDenseVector(%v, %d) error = %v, wantErr %v",
					tt.vector, tt.dimensions, err, tt.wantErr)
			}
		})
	}
}

// TestNormalizeSourceFilters tests the pure source filter normalization
func TestNormalizeSourceFilters(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		values  []string
		limit   int
		want    []string
		wantErr bool
	}{
		{
			name:   "empty values",
			kind:   "source_ref",
			values: []string{},
			limit:  100,
			want:   []string{},
		},
		{
			name:   "within limit",
			kind:   "source_ref",
			values: []string{"a", "b", "c"},
			limit:  10,
			want:   []string{"a", "b", "c"},
		},
		{
			name:    "exceeds limit",
			kind:    "source_ref",
			values:  []string{"a", "b", "c", "d", "e"},
			limit:   3,
			wantErr: true, // returns error when count exceeds limit
		},
		{
			name:    "negative limit",
			kind:    "source_ref",
			values:  []string{"a"},
			limit:   -1,
			wantErr: true,
		},
		{
			name:    "empty strings filtered with error",
			kind:    "source_ref",
			values:  []string{"a", "", "b", "   "},
			limit:   10,
			wantErr: true, // empty string after trim causes error
		},
		{
			name:    "whitespace only filtered with error",
			kind:    "source_ref",
			values:  []string{"a", "\t", "b"},
			limit:   10,
			wantErr: true, // whitespace only after trim becomes empty
		},
		{
			name:   "duplicates removed",
			kind:   "source_ref",
			values: []string{"a", "b", "a", "c"},
			limit:  10,
			want:   []string{"a", "b", "c"},
		},
		{
			name:   "whitespace trimmed",
			kind:   "source_ref",
			values: []string{" a ", "b", " c "},
			limit:  10,
			want:   []string{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeSourceFilters(tt.kind, tt.values, tt.limit)
			if (err != nil) != tt.wantErr {
				t.Errorf("normalizeSourceFilters(%q, %v, %d) error = %v, wantErr %v",
					tt.kind, tt.values, tt.limit, err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("normalizeSourceFilters() returned %d items, want %d", len(got), len(tt.want))
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("normalizeSourceFilters()[%d] = %v, want %v", i, v, tt.want[i])
				}
			}
		})
	}
}
