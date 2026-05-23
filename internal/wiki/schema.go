package wiki

import (
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const CurrentSchemaVersion = 3

var promptVersionRe = regexp.MustCompile(`^(v[0-9]+|ingest_v[0-9]+|proposal_v[0-9]+|summarizer_v[0-9]+)$`)

// wikiLinkRe matches [[slug]] and [[slug|type]] — captures only the slug.
var wikiLinkRe = regexp.MustCompile(`\[\[([a-z0-9-]+)(?:\|[a-z][a-z_]*)?\]\]`)

// wikiLinkTypedRe captures both slug (group 1) and optional edge type (group 2).
var wikiLinkTypedRe = regexp.MustCompile(`\[\[([a-z0-9-]+)(?:\|([a-z][a-z_]*))?\]\]`)

// validEdgeTypes is the closed set of typed edge labels for [[slug|type]] syntax.
var validEdgeTypes = map[string]bool{
	"mentions":               true,
	"cites":                  true,
	"applies":                true,
	"implements":             true,
	"references":             true,
	"extends":                true,
	"contradicts":            true,
	"depends_on":             true,
	"derived_from":           true,
	"semantically_similar_to": true,
}

// WikiLinkEdge is a typed outbound edge from [[slug]] or [[slug|type]] syntax.
type WikiLinkEdge struct {
	Slug string
	Type string // one of validEdgeTypes; defaults to "mentions"
}

// ValidEdgeType reports whether t is a recognised edge type.
func ValidEdgeType(t string) bool { return validEdgeTypes[t] }

// validConfidences is the set of accepted Confidence values for RelatedRef.
var validConfidences = map[string]bool{
	"EXTRACTED": true,
	"INFERRED":  true,
	"AMBIGUOUS": true,
	"":          true, // empty defaults to EXTRACTED on read
}

// RelatedRef is a typed entry in a page's `related:` frontmatter list.
// It supports both the legacy bare-string form and the new dict form:
//
//	related: [slug-a, slug-b]             → Confidence defaults to "EXTRACTED"
//	related: [{slug: foo, confidence: INFERRED}]
type RelatedRef struct {
	Slug       string `yaml:"slug"`
	Confidence string `yaml:"confidence,omitempty"`
}

// UnmarshalYAML accepts either a scalar string (bare slug) or a mapping
// ({slug: ..., confidence: ...}) so legacy pages parse without rewriting.
func (r *RelatedRef) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		r.Slug = node.Value
		r.Confidence = "EXTRACTED"
	case yaml.MappingNode:
		// Use an alias to avoid infinite recursion with the custom unmarshaler.
		type relatedRefAlias RelatedRef
		var alias relatedRefAlias
		if err := node.Decode(&alias); err != nil {
			return err
		}
		*r = RelatedRef(alias)
		if r.Confidence == "" {
			r.Confidence = "EXTRACTED"
		}
	default:
		return fmt.Errorf("wiki: RelatedRef must be a string or mapping, got %v", node.Tag)
	}
	return nil
}

// Page represents a wiki page with YAML frontmatter and markdown body.
type Page struct {
	Title         string       `yaml:"title"`
	Aliases       []string     `yaml:"aliases,omitempty"`
	Tags          []string     `yaml:"tags,omitempty"`
	Category      string       `yaml:"category,omitempty"`
	Related       []RelatedRef `yaml:"related,omitempty"`
	Sources       []string     `yaml:"sources,omitempty"`
	SchemaVersion int          `yaml:"schema_version"`
	PromptVersion string       `yaml:"prompt_version"`
	CreatedAt     string       `yaml:"created_at"`
	UpdatedAt     string       `yaml:"updated_at"`

	// Unversioned indicates the most recent on-disk write did not produce a
	// git commit (commit failed or git is unavailable). The page IS saved on
	// disk — only the audit history is degraded. Auto-clears on the next
	// successful commit for the same slug. Phase 2 GIT-01.
	Unversioned bool `yaml:"unversioned,omitempty" json:"unversioned,omitempty"`

	// Body holds the markdown content below the frontmatter.
	// Not serialized in YAML — written after the --- delimiter.
	Body string `yaml:"-"`
}

// RelatedSlugs returns the slug strings from a slice of RelatedRef.
func RelatedSlugs(refs []RelatedRef) []string {
	if len(refs) == 0 {
		return nil
	}
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.Slug
	}
	return out
}

// RelatedFromSlugs converts bare slug strings into EXTRACTED RelatedRefs.
func RelatedFromSlugs(slugs []string) []RelatedRef {
	if len(slugs) == 0 {
		return nil
	}
	out := make([]RelatedRef, len(slugs))
	for i, s := range slugs {
		out[i] = RelatedRef{Slug: s, Confidence: "EXTRACTED"}
	}
	return out
}

// RelatedContainsSlug reports whether any RelatedRef in refs has the given slug.
func RelatedContainsSlug(refs []RelatedRef, slug string) bool {
	for _, r := range refs {
		if r.Slug == slug {
			return true
		}
	}
	return false
}

// RelatedAppendSlug appends a new EXTRACTED RelatedRef for slug if not already
// present, then sorts the slice by slug for deterministic diffs. Returns the
// (possibly new) slice.
func RelatedAppendSlug(refs []RelatedRef, slug string) []RelatedRef {
	if RelatedContainsSlug(refs, slug) {
		return refs
	}
	refs = append(refs, RelatedRef{Slug: slug, Confidence: "EXTRACTED"})
	RelatedSortBySlugs(refs)
	return refs
}

// RelatedSortBySlugs sorts a RelatedRef slice in-place by Slug ascending.
func RelatedSortBySlugs(refs []RelatedRef) {
	sort.Slice(refs, func(i, j int) bool { return refs[i].Slug < refs[j].Slug })
}

// ValidationError contains all validation failures for a wiki page.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("wiki validation failed: %s", strings.Join(e.Errors, "; "))
}

// ParseYAML parses raw YAML bytes into a Page.
// Compatibility path for reading old .yaml files.
func ParseYAML(data []byte) (*Page, error) {
	var page Page
	if err := yaml.Unmarshal(data, &page); err != nil {
		return nil, fmt.Errorf("YAML parse error: %w", err)
	}
	// Older .yaml files had a "content" field. Try to extract it as Body.
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err == nil {
		if content, ok := raw["content"].(string); ok && page.Body == "" {
			page.Body = content
		}
	}
	return &page, nil
}

// Validate checks that a Page conforms to the wiki schema.
func Validate(page *Page) error {
	var errs []string

	if page.Title == "" {
		errs = append(errs, "title is required")
	} else if len(page.Title) > 200 {
		errs = append(errs, "title exceeds 200 characters")
	}

	if page.Body == "" {
		errs = append(errs, "body is required")
	}

	if len(page.Tags) > 10 {
		errs = append(errs, "too many tags (max 10)")
	}
	for i, tag := range page.Tags {
		if len(tag) > 50 {
			errs = append(errs, fmt.Sprintf("tag %d exceeds 50 characters", i))
		}
	}

	if len(page.Category) > 50 {
		errs = append(errs, "category exceeds 50 characters")
	}

	for i, ref := range page.Related {
		if len(ref.Slug) > 100 {
			errs = append(errs, fmt.Sprintf("related[%d] slug exceeds 100 characters", i))
		}
		if !validConfidences[ref.Confidence] {
			errs = append(errs, fmt.Sprintf("related[%d] confidence %q must be EXTRACTED, INFERRED, AMBIGUOUS, or empty", i, ref.Confidence))
		}
	}

	if len(page.Sources) > 10 {
		errs = append(errs, "too many sources (max 10)")
	}
	for i, src := range page.Sources {
		if len(src) > 200 {
			errs = append(errs, fmt.Sprintf("source[%d] exceeds 200 characters", i))
		}
	}

	if page.SchemaVersion < 1 || page.SchemaVersion > CurrentSchemaVersion {
		errs = append(errs, fmt.Sprintf("schema_version must be between 1 and %d", CurrentSchemaVersion))
	}

	if page.PromptVersion == "" {
		errs = append(errs, "prompt_version is required")
	} else if !promptVersionRe.MatchString(page.PromptVersion) {
		errs = append(errs, "prompt_version must match v{n}, ingest_v{n}, proposal_v{n}, or summarizer_v{n}")
	}

	if page.CreatedAt == "" {
		errs = append(errs, "created_at is required")
	} else if _, err := time.Parse(time.RFC3339, page.CreatedAt); err != nil {
		errs = append(errs, "created_at must be ISO 8601")
	}

	if page.UpdatedAt == "" {
		errs = append(errs, "updated_at is required")
	} else if _, err := time.Parse(time.RFC3339, page.UpdatedAt); err != nil {
		errs = append(errs, "updated_at must be ISO 8601")
	}

	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// ExtractWikiLinks returns unique slug strings from [[slug]] and [[slug|type]] body syntax.
func ExtractWikiLinks(body string) []string {
	matches := wikiLinkRe.FindAllStringSubmatch(body, -1)
	seen := make(map[string]bool)
	var links []string
	for _, m := range matches {
		slug := m[1]
		if !seen[slug] {
			seen[slug] = true
			links = append(links, slug)
		}
	}
	return links
}

// ExtractWikiLinksTyped returns unique typed edges from [[slug]] and [[slug|type]] syntax.
// Plain [[slug]] defaults to "mentions". Unknown types fall back to "mentions" with a warning.
// Dedup is by slug — first occurrence wins when the same slug appears multiple times.
func ExtractWikiLinksTyped(body string) []WikiLinkEdge {
	matches := wikiLinkTypedRe.FindAllStringSubmatch(body, -1)
	seen := make(map[string]bool)
	var links []WikiLinkEdge
	for _, m := range matches {
		slug := m[1]
		if seen[slug] {
			continue
		}
		seen[slug] = true
		edgeType := "mentions"
		if m[2] != "" {
			if validEdgeTypes[m[2]] {
				edgeType = m[2]
			} else {
				slog.Warn("wiki: unknown edge type in [[slug|type]], falling back to mentions", "slug", slug, "type", m[2])
			}
		}
		links = append(links, WikiLinkEdge{Slug: slug, Type: edgeType})
	}
	return links
}

// Slug generates a filesystem-safe slug from a title.
func Slug(title string) string {
	s := strings.ToLower(title)
	s = strings.ReplaceAll(s, " ", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else if r >= 0x00C0 && r <= 0x024F {
			b.WriteRune('-')
		} else if r > 127 {
			b.WriteRune('-')
		} else if r == '_' || r == '/' || r == '.' {
			b.WriteRune('-')
		}
	}
	result := b.String()
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	result = strings.Trim(result, "-")
	if result == "" {
		result = "untitled"
	}
	return result
}

// ParseAndValidate combines YAML parsing and validation.
func ParseAndValidate(data []byte) (*Page, error) {
	page, err := ParseYAML(data)
	if err != nil {
		return nil, err
	}
	if err := Validate(page); err != nil {
		return nil, err
	}
	return page, nil
}
