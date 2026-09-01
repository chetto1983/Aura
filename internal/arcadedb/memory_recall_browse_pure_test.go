package arcadedb

import (
	"strings"
	"testing"
)

// TestLooksLikeArcadeRID tests the pure ArcadeDB RID detection
func TestLooksLikeArcadeRID(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		// Valid RIDs: #cluster:position
		{"valid RID", "#123:456", true},
		{"valid RID with large numbers", "#999999999:999999999", true},
		{"valid RID zero position", "#123:0", true},
		{"valid RID zero cluster", "#0:456", true},

		// Invalid RIDs
		{"missing hash", "123:456", false},
		{"missing colon", "#123456", false},
		{"missing position", "#123:", false},
		{"missing cluster", "#:456", false},
		{"empty", "", false},
		{"only hash", "#", false},
		{"hash with colon only", "#:", false},

		// Non-numeric parts
		{"non-numeric cluster", "#abc:456", false},
		{"non-numeric position", "#123:abc", false},
		{"negative cluster", "#-123:456", false},
		{"negative position", "#123:-456", false},
		{"float cluster", "#123.5:456", false},
		{"float position", "#123:456.5", false},

		// Edge cases
		{"spaces around", " #123:456 ", false}, // spaces make it invalid
		{"hash in middle", "123#456:789", false},
		{"multiple colons", "#123:456:789", false},
		{"multiple hashes", "##123:456", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := looksLikeArcadeRID(tt.value)
			if got != tt.want {
				t.Errorf("looksLikeArcadeRID(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// TestValidRecallCursorID tests the pure cursor ID validation
func TestValidRecallCursorID(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		// Valid IDs
		{"valid UUID", "b130c94d-a213-463a-a797-ec124104363a", true},
		{"valid short name", "my-conversation", true},
		{"empty after trim becomes valid RID", "#123:456", false}, // looks like RID
		{"with spaces", " my-id ", true},                          // spaces are trimmed

		// Invalid IDs
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"with null byte", "my\x00id", false},
		{"too long", "x" + strings.Repeat("x", recallBrowseIDMaxRunes+1), false},
		{"exactly at limit", strings.Repeat("x", recallBrowseIDMaxRunes), true},
		{"one under limit", strings.Repeat("x", recallBrowseIDMaxRunes-1), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validRecallCursorID(tt.value)
			if got != tt.want {
				t.Errorf("validRecallCursorID(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// TestNormalizeRecallDirection tests the pure direction normalization
func TestNormalizeRecallDirection(t *testing.T) {
	tests := []struct {
		name      string
		direction RecallDirection
		want      RecallDirection
		wantErr   bool
	}{
		{"empty defaults to after", "", RecallDirectionAfter, false},
		{"before unchanged", RecallDirectionBefore, RecallDirectionBefore, false},
		{"after unchanged", RecallDirectionAfter, RecallDirectionAfter, false},
		{"invalid direction", "invalid", "", true},
		{"uppercase before", "BEFORE", "", true},
		{"uppercase after", "AFTER", "", true},
		{"mixed case", "Before", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeRecallDirection(tt.direction)
			if (err != nil) != tt.wantErr {
				t.Errorf("normalizeRecallDirection(%q) error = %v, wantErr %v",
					tt.direction, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("normalizeRecallDirection(%q) = %v, want %v",
					tt.direction, got, tt.want)
			}
		})
	}
}
