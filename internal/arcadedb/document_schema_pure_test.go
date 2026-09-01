package arcadedb

import (
	"testing"
)

// TestValidateIdentifier tests the pure identifier validation
func TestValidateIdentifier(t *testing.T) {
	tests := []struct {
		name    string
		nname   string
		value   string
		wantErr bool
	}{
		{"valid name", "field", "my-name", false},
		{"empty value", "field", "", true},
		{"oversized value", "field", "x" + string(make([]byte, maxDocumentIdentifierRunes)), true},
		{"exactly at limit", "field", string(make([]byte, maxDocumentIdentifierRunes)), false},
		{"one under limit", "field", string(make([]byte, maxDocumentIdentifierRunes-1)), false},
		{"unicode at limit", "field", string(make([]rune, maxDocumentIdentifierRunes)), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIdentifier(tt.nname, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateIdentifier(%q, %q) error = %v, wantErr %v",
					tt.nname, tt.value, err, tt.wantErr)
			}
		})
	}
}

// TestValidSHA256 tests the pure SHA256 validation
func TestValidSHA256(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"valid lowercase hex 64 chars", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2", true},
		{"invalid length", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4", false},
		{"uppercase", "A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6", false},
		{"mixed case", "A1b2C3d4E5f6A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6A1B2C3D4E5F6", false},
		{"invalid hex", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6gxx", false},
		{"empty", "", false},
		{"too short", "abc", false},
		{"valid with all hex digits", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validSHA256(tt.value)
			if got != tt.want {
				t.Errorf("validSHA256(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// TestRequiredString tests the pure required string extraction
func TestRequiredString(t *testing.T) {
	tests := []struct {
		name    string
		row     map[string]any
		key     string
		want    string
		wantErr bool
	}{
		{"present", map[string]any{"name": "value"}, "name", "value", false},
		{"missing", map[string]any{"other": "value"}, "name", "", true},
		{"empty string", map[string]any{"name": ""}, "name", "", true},
		{"whitespace only", map[string]any{"name": "   "}, "name", "", true},
		{"not a string", map[string]any{"name": 123}, "name", "", true},
		{"nil value", map[string]any{"name": nil}, "name", "", true},
		{"tab whitespace", map[string]any{"name": "\t"}, "name", "", true},
		{"valid with spaces", map[string]any{"name": "my name"}, "name", "my name", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := requiredString(tt.row, tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("requiredString(%v, %q) error = %v, wantErr %v", tt.row, tt.key, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("requiredString(%v, %q) = %v, want %v", tt.row, tt.key, got, tt.want)
			}
		})
	}
}
