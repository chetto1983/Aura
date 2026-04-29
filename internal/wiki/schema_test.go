package wiki

import (
	"testing"
)

func TestParseYAML(t *testing.T) {
	input := `
title: "Test Page"
content: "Some content here"
tags:
  - test
schema_version: 1
prompt_version: ingest_v1
created_at: "2026-04-28T10:00:00Z"
updated_at: "2026-04-28T10:00:00Z"
`
	page, err := ParseYAML([]byte(input))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}
	if page.Title != "Test Page" {
		t.Errorf("title = %q, want %q", page.Title, "Test Page")
	}
	if page.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", page.SchemaVersion)
	}
	if len(page.Tags) != 1 || page.Tags[0] != "test" {
		t.Errorf("tags = %v, want [test]", page.Tags)
	}
}

func TestParseYAMLInvalid(t *testing.T) {
	_, err := ParseYAML([]byte("not: valid: yaml: ["))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestValidate(t *testing.T) {
	validPage := &Page{
		Title:         "Test",
		Body:          "Content",
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "ingest_v1",
		CreatedAt:     "2026-04-28T10:00:00Z",
		UpdatedAt:     "2026-04-28T10:00:00Z",
	}
	if err := Validate(validPage); err != nil {
		t.Fatalf("valid page should pass: %v", err)
	}
}

func TestValidateMissingTitle(t *testing.T) {
	page := &Page{
		Body:          "Content",
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     "2026-04-28T10:00:00Z",
		UpdatedAt:     "2026-04-28T10:00:00Z",
	}
	err := Validate(page)
	if err == nil {
		t.Fatal("expected validation error for missing title")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if len(ve.Errors) == 0 {
		t.Fatal("expected at least one error")
	}
}

func TestValidateMissingBody(t *testing.T) {
	page := &Page{
		Title:         "Test",
		SchemaVersion: 1,
		PromptVersion: "v1",
		CreatedAt:     "2026-04-28T10:00:00Z",
		UpdatedAt:     "2026-04-28T10:00:00Z",
	}
	if err := Validate(page); err == nil {
		t.Fatal("expected validation error for missing body")
	}
}

func TestValidateWrongSchemaVersion(t *testing.T) {
	page := &Page{
		Title:         "Test",
		Body:          "Content",
		SchemaVersion: 99,
		PromptVersion: "v1",
		CreatedAt:     "2026-04-28T10:00:00Z",
		UpdatedAt:     "2026-04-28T10:00:00Z",
	}
	err := Validate(page)
	if err == nil {
		t.Fatal("expected validation error for wrong schema version")
	}
}

func TestValidateBadPromptVersion(t *testing.T) {
	page := &Page{
		Title:         "Test",
		Body:          "Content",
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "bad_format",
		CreatedAt:     "2026-04-28T10:00:00Z",
		UpdatedAt:     "2026-04-28T10:00:00Z",
	}
	err := Validate(page)
	if err == nil {
		t.Fatal("expected validation error for bad prompt version")
	}
}

func TestValidateBadTimestamp(t *testing.T) {
	page := &Page{
		Title:         "Test",
		Body:          "Content",
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     "not-a-date",
		UpdatedAt:     "2026-04-28T10:00:00Z",
	}
	err := Validate(page)
	if err == nil {
		t.Fatal("expected validation error for bad created_at")
	}
}

func TestValidateTooManyTags(t *testing.T) {
	tags := make([]string, 12)
	for i := range tags {
		tags[i] = "tag"
	}
	page := &Page{
		Title:         "Test",
		Body:          "Content",
		Tags:          tags,
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     "2026-04-28T10:00:00Z",
		UpdatedAt:     "2026-04-28T10:00:00Z",
	}
	err := Validate(page)
	if err == nil {
		t.Fatal("expected validation error for too many tags")
	}
}

func TestParseAndValidate(t *testing.T) {
	input := `
title: "Test Page"
content: "Some content"
schema_version: 1
prompt_version: v1
created_at: "2026-04-28T10:00:00Z"
updated_at: "2026-04-28T10:00:00Z"
`
	page, err := ParseAndValidate([]byte(input))
	if err != nil {
		t.Fatalf("ParseAndValidate failed: %v", err)
	}
	if page.Title != "Test Page" {
		t.Errorf("title = %q, want %q", page.Title, "Test Page")
	}
}

func TestParseAndValidateInvalid(t *testing.T) {
	input := `
title: ""
content: ""
schema_version: 0
prompt_version: bad
created_at: "not-a-date"
updated_at: "not-a-date"
`
	_, err := ParseAndValidate([]byte(input))
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestSlug(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Machine Learning Basics", "machine-learning-basics"},
		{"Hello World!", "hello-world"},
		{"Go / Rust", "go-rust"},
		{"  spaces  ", "spaces"},
		{"A & B", "a-b"},
		{"", "untitled"},
		{"---", "untitled"},
	}
	for _, tt := range tests {
		got := Slug(tt.input)
		if got != tt.want {
			t.Errorf("Slug(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
