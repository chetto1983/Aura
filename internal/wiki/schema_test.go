package wiki

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSchema_UnversionedRoundTrip(t *testing.T) {
	cases := []struct {
		name        string
		unversioned bool
		wantInYAML  bool // omitempty: absent when false, present when true
	}{
		{"false_omitted", false, false},
		{"true_present", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page := &Page{
				Title:         "Test",
				Body:          "x",
				SchemaVersion: CurrentSchemaVersion,
				PromptVersion: "v1",
				CreatedAt:     "2026-05-10T00:00:00Z",
				UpdatedAt:     "2026-05-10T00:00:00Z",
				Unversioned:   tc.unversioned,
			}
			data, err := MarshalMD(page)
			if err != nil {
				t.Fatal(err)
			}
			has := strings.Contains(string(data), "unversioned:")
			if has != tc.wantInYAML {
				t.Fatalf("unversioned in YAML = %v, want %v\nYAML: %s", has, tc.wantInYAML, string(data))
			}
			// Round-trip — ParseMD returns (*Page, error), not (*Page, _, error).
			back, err := ParseMD(data)
			if err != nil {
				t.Fatal(err)
			}
			if back.Unversioned != tc.unversioned {
				t.Fatalf("round-trip Unversioned = %v, want %v", back.Unversioned, tc.unversioned)
			}
		})
	}
}

func TestParseYAML(t *testing.T) {
	input := `
title: "Test Page"
content: "Some content here"
tags:
  - test
schema_version: 2
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
	if page.SchemaVersion != 2 {
		t.Errorf("schema_version = %d, want 2", page.SchemaVersion)
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
		SchemaVersion: CurrentSchemaVersion,
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
schema_version: 2
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

// validPageBase returns a Page that satisfies Validate for schema v3.
func validPageBase() *Page {
	return &Page{
		Title:         "Test",
		Body:          "Content",
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     "2026-01-01T00:00:00Z",
		UpdatedAt:     "2026-01-01T00:00:00Z",
	}
}

func TestRelatedRef_BareStringParsesAsExtracted(t *testing.T) {
	input := `
related:
  - slug-a
  - slug-b
`
	var result struct {
		Related []RelatedRef `yaml:"related"`
	}
	if err := yaml.Unmarshal([]byte(input), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Related) != 2 {
		t.Fatalf("len = %d, want 2", len(result.Related))
	}
	for i, ref := range result.Related {
		if ref.Confidence != "EXTRACTED" {
			t.Errorf("[%d] confidence = %q, want EXTRACTED", i, ref.Confidence)
		}
	}
	if result.Related[0].Slug != "slug-a" || result.Related[1].Slug != "slug-b" {
		t.Errorf("slugs = %v/%v, want slug-a/slug-b", result.Related[0].Slug, result.Related[1].Slug)
	}
}

func TestRelatedRef_ExplicitConfidence(t *testing.T) {
	input := `
related:
  - slug: foo
    confidence: INFERRED
  - slug: bar
    confidence: AMBIGUOUS
  - slug: baz
`
	var result struct {
		Related []RelatedRef `yaml:"related"`
	}
	if err := yaml.Unmarshal([]byte(input), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Related) != 3 {
		t.Fatalf("len = %d, want 3", len(result.Related))
	}
	if result.Related[0].Confidence != "INFERRED" {
		t.Errorf("foo confidence = %q, want INFERRED", result.Related[0].Confidence)
	}
	if result.Related[1].Confidence != "AMBIGUOUS" {
		t.Errorf("bar confidence = %q, want AMBIGUOUS", result.Related[1].Confidence)
	}
	if result.Related[2].Confidence != "EXTRACTED" {
		t.Errorf("baz confidence = %q, want EXTRACTED (default)", result.Related[2].Confidence)
	}
}

func TestRelatedRef_InvalidConfidenceFails(t *testing.T) {
	p := validPageBase()
	p.Related = []RelatedRef{{Slug: "foo", Confidence: "BOGUS"}}
	if err := Validate(p); err == nil {
		t.Fatal("expected validation error for unknown confidence value")
	}
}

func TestPage_SchemaV2_LegacyRelatedStillParses(t *testing.T) {
	input := "---\ntitle: Legacy\nbody: body\nrelated:\n  - alpha\n  - beta\nschema_version: 2\nprompt_version: v1\ncreated_at: \"2026-01-01T00:00:00Z\"\nupdated_at: \"2026-01-01T00:00:00Z\"\n---\nbody\n"
	page, err := ParseMD([]byte(input))
	if err != nil {
		t.Fatalf("ParseMD: %v", err)
	}
	if len(page.Related) != 2 {
		t.Fatalf("len(Related) = %d, want 2", len(page.Related))
	}
	for i, ref := range page.Related {
		if ref.Confidence != "EXTRACTED" {
			t.Errorf("[%d] confidence = %q, want EXTRACTED", i, ref.Confidence)
		}
	}
	// schema_version=2 must still pass Validate (backward compat).
	if err := Validate(page); err != nil {
		t.Fatalf("Validate schema v2: %v", err)
	}
}

func TestExtractWikiLinksTyped_PlainSlugDefaultsMentions(t *testing.T) {
	links := ExtractWikiLinksTyped("see [[alpha]] and [[beta]]")
	if len(links) != 2 {
		t.Fatalf("len = %d, want 2", len(links))
	}
	for _, l := range links {
		if l.Type != "mentions" {
			t.Errorf("slug %q type = %q, want mentions", l.Slug, l.Type)
		}
	}
}

func TestExtractWikiLinksTyped_KnownTypePreserved(t *testing.T) {
	links := ExtractWikiLinksTyped("[[siemens|cites]] and [[acme|depends_on]]")
	if len(links) != 2 {
		t.Fatalf("len = %d, want 2", len(links))
	}
	if links[0].Slug != "siemens" || links[0].Type != "cites" {
		t.Errorf("got %+v, want {siemens cites}", links[0])
	}
	if links[1].Slug != "acme" || links[1].Type != "depends_on" {
		t.Errorf("got %+v, want {acme depends_on}", links[1])
	}
}

func TestExtractWikiLinksTyped_UnknownTypeFallsBackToMentions(t *testing.T) {
	links := ExtractWikiLinksTyped("[[foo|bogustype]]")
	if len(links) != 1 {
		t.Fatalf("len = %d, want 1", len(links))
	}
	if links[0].Type != "mentions" {
		t.Errorf("type = %q, want mentions for unknown edge type", links[0].Type)
	}
}

func TestExtractWikiLinksTyped_DedupBySlugs(t *testing.T) {
	links := ExtractWikiLinksTyped("[[alpha]] then [[alpha|cites]]")
	if len(links) != 1 {
		t.Fatalf("expected 1 unique slug, got %d: %+v", len(links), links)
	}
	if links[0].Slug != "alpha" || links[0].Type != "mentions" {
		t.Errorf("got %+v, want {alpha mentions} (first occurrence wins)", links[0])
	}
}

func TestExtractWikiLinks_HandlesTypeSuffix(t *testing.T) {
	// ExtractWikiLinks must return slugs from both [[slug]] and [[slug|type]] syntax.
	slugs := ExtractWikiLinks("[[alpha|cites]] and [[beta]]")
	if len(slugs) != 2 {
		t.Fatalf("len = %d, want 2: %v", len(slugs), slugs)
	}
	found := map[string]bool{}
	for _, s := range slugs {
		found[s] = true
	}
	if !found["alpha"] || !found["beta"] {
		t.Errorf("slugs = %v, want [alpha beta]", slugs)
	}
}

func TestPage_SchemaV3_RoundTrip(t *testing.T) {
	page := validPageBase()
	page.Related = []RelatedRef{
		{Slug: "a", Confidence: "EXTRACTED"},
		{Slug: "b", Confidence: "INFERRED"},
	}
	data, err := MarshalMD(page)
	if err != nil {
		t.Fatalf("MarshalMD: %v", err)
	}
	back, err := ParseMD(data)
	if err != nil {
		t.Fatalf("ParseMD round-trip: %v", err)
	}
	if len(back.Related) != 2 {
		t.Fatalf("round-trip len(Related) = %d, want 2", len(back.Related))
	}
	// MarshalMD writes bare slugs → re-parsed as EXTRACTED.
	if back.Related[0].Slug != "a" || back.Related[1].Slug != "b" {
		t.Errorf("slugs = %v/%v", back.Related[0].Slug, back.Related[1].Slug)
	}
}
