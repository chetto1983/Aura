package wiki

import (
	"testing"
	"time"
)

// Snapshot tests verify that wiki validation produces identical results
// for fixed inputs, ensuring deterministic behavior.

func TestSnapshotValidPage(t *testing.T) {
	page := &Page{
		Title:         "Go Concurrency Patterns",
		Content:       "Goroutines and channels are the building blocks of Go concurrency.",
		Tags:          []string{"go", "concurrency", "patterns"},
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "ingest_v1",
		CreatedAt:     "2026-04-28T12:00:00Z",
		UpdatedAt:     "2026-04-28T12:00:00Z",
	}

	err := Validate(page)
	if err != nil {
		t.Fatalf("valid page should pass validation: %v", err)
	}

	// Run multiple times to verify determinism
	for i := 0; i < 5; i++ {
		err := Validate(page)
		if err != nil {
			t.Errorf("validation should be deterministic: pass %d got error: %v", i+1, err)
		}
	}
}

func TestSnapshotInvalidPage(t *testing.T) {
	page := &Page{
		Title:         "",
		Content:       "",
		SchemaVersion: 0,
		PromptVersion: "",
		CreatedAt:     "invalid-date",
		UpdatedAt:     "invalid-date",
		Tags:          make([]string, 15), // exceeds max
	}

	err := Validate(page)
	if err == nil {
		t.Fatal("invalid page should fail validation")
	}

	validationErr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}

	// Expected error set for this input
	expectedErrors := map[string]bool{
		"title is required":           false,
		"content is required":         false,
		"too many tags (max 10)":      false,
		"schema_version must be 1":    false,
		"prompt_version is required":  false,
		"created_at must be ISO 8601": false,
		"updated_at must be ISO 8601": false,
	}

	for _, e := range validationErr.Errors {
		if _, expected := expectedErrors[e]; !expected {
			t.Errorf("unexpected validation error: %q", e)
		}
		expectedErrors[e] = true
	}

	for e, found := range expectedErrors {
		if !found {
			t.Errorf("expected validation error not found: %q", e)
		}
	}

	// Verify determinism: run again and get identical errors
	err2 := Validate(page)
	validationErr2, _ := err2.(*ValidationError)
	if len(validationErr.Errors) != len(validationErr2.Errors) {
		t.Errorf("validation should be deterministic: got %d errors first, %d second",
			len(validationErr.Errors), len(validationErr2.Errors))
	}
	for i, e := range validationErr.Errors {
		if validationErr2.Errors[i] != e {
			t.Errorf("error order should be deterministic: first[%d]=%q, second[%d]=%q",
				i, e, i, validationErr2.Errors[i])
		}
	}
}

func TestSnapshotSchemaVersion(t *testing.T) {
	tests := []struct {
		name          string
		schemaVersion int
		wantValid     bool
	}{
		{"current version", CurrentSchemaVersion, true},
		{"wrong version", 2, false},
		{"zero version", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := &Page{
				Title:         "Test Page",
				Content:       "Test content",
				SchemaVersion: tt.schemaVersion,
				PromptVersion: "ingest_v1",
				CreatedAt:     time.Now().UTC().Format(time.RFC3339),
				UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
			}

			err := Validate(page)
			if tt.wantValid && err != nil {
				t.Errorf("schema_version=%d should be valid: %v", tt.schemaVersion, err)
			}
			if !tt.wantValid && err == nil {
				t.Errorf("schema_version=%d should be invalid", tt.schemaVersion)
			}
		})
	}
}

func TestSnapshotPromptVersion(t *testing.T) {
	tests := []struct {
		name          string
		promptVersion string
		wantValid     bool
	}{
		{"ingest format", "ingest_v1", true},
		{"v format", "v1", true},
		{"v2", "v2", true},
		{"ingest v2", "ingest_v2", true},
		{"empty", "", false},
		{"random string", "random", false},
		{"no underscore", "ingest1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := &Page{
				Title:         "Test Page",
				Content:       "Test content",
				SchemaVersion: CurrentSchemaVersion,
				PromptVersion: tt.promptVersion,
				CreatedAt:     time.Now().UTC().Format(time.RFC3339),
				UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
			}

			err := Validate(page)
			if tt.wantValid && err != nil {
				t.Errorf("prompt_version=%q should be valid: %v", tt.promptVersion, err)
			}
			if !tt.wantValid && err == nil {
				t.Errorf("prompt_version=%q should be invalid", tt.promptVersion)
			}
		})
	}
}

func TestSnapshotSlugDeterminism(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Go Concurrency Patterns", "go-concurrency-patterns"},
		{"Go / Rust", "go-rust"},
		{"A & B", "a-b"},
		{"Test---Multiple---Hyphens", "test-multiple-hyphens"},
		{"  Leading Trailing  ", "leading-trailing"},
		{"UPPERCASE TITLE", "uppercase-title"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// Run multiple times to verify determinism
			for i := 0; i < 5; i++ {
				got := Slug(tt.input)
				if got != tt.want {
					t.Errorf("Slug(%q) = %q, want %q (pass %d)", tt.input, got, tt.want, i+1)
				}
			}
		})
	}
}

func TestSnapshotParseYAML(t *testing.T) {
	input := `title: Test Page
content: This is test content.
schema_version: 1
prompt_version: ingest_v1
created_at: "2026-04-28T12:00:00Z"
updated_at: "2026-04-28T12:00:00Z"
tags:
  - test
  - snapshot
`

	// Run multiple times to verify deterministic parsing
	for i := 0; i < 5; i++ {
		page, err := ParseYAML([]byte(input))
		if err != nil {
			t.Fatalf("ParseYAML failed on pass %d: %v", i+1, err)
		}
		if page.Title != "Test Page" {
			t.Errorf("Title = %q, want %q (pass %d)", page.Title, "Test Page", i+1)
		}
		if page.SchemaVersion != 1 {
			t.Errorf("SchemaVersion = %d, want 1 (pass %d)", page.SchemaVersion, i+1)
		}
		if page.PromptVersion != "ingest_v1" {
			t.Errorf("PromptVersion = %q, want %q (pass %d)", page.PromptVersion, "ingest_v1", i+1)
		}
		if len(page.Tags) != 2 {
			t.Errorf("Tags length = %d, want 2 (pass %d)", len(page.Tags), i+1)
		}
	}
}
