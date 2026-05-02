package wiki

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const CurrentSchemaVersion = 2

var promptVersionRe = regexp.MustCompile(`^(v[0-9]+|ingest_v[0-9]+|summarizer_v[0-9]+)$`)
var wikiLinkRe = regexp.MustCompile(`\[\[([a-z0-9-]+)\]\]`)

// Page represents a wiki page with YAML frontmatter and markdown body.
type Page struct {
	Title         string   `yaml:"title"`
	Tags          []string `yaml:"tags,omitempty"`
	Category      string   `yaml:"category,omitempty"`
	Related       []string `yaml:"related,omitempty"`
	Sources       []string `yaml:"sources,omitempty"`
	SchemaVersion int      `yaml:"schema_version"`
	PromptVersion string   `yaml:"prompt_version"`
	CreatedAt     string   `yaml:"created_at"`
	UpdatedAt     string   `yaml:"updated_at"`

	// Body holds the markdown content below the frontmatter.
	// Not serialized in YAML — written after the --- delimiter.
	Body string `yaml:"-"`
}

// ValidationError contains all validation failures for a wiki page.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("wiki validation failed: %s", strings.Join(e.Errors, "; "))
}

// ParseYAML parses raw YAML bytes into a Page.
// Legacy path — used for backward-compatible reading of old .yaml files.
func ParseYAML(data []byte) (*Page, error) {
	var page Page
	if err := yaml.Unmarshal(data, &page); err != nil {
		return nil, fmt.Errorf("YAML parse error: %w", err)
	}
	// Legacy .yaml files had a "content" field. Try to extract it as Body.
	var raw map[string]interface{}
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

	for i, rel := range page.Related {
		if len(rel) > 100 {
			errs = append(errs, fmt.Sprintf("related[%d] exceeds 100 characters", i))
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

	if page.SchemaVersion != CurrentSchemaVersion {
		errs = append(errs, fmt.Sprintf("schema_version must be %d", CurrentSchemaVersion))
	}

	if page.PromptVersion == "" {
		errs = append(errs, "prompt_version is required")
	} else if !promptVersionRe.MatchString(page.PromptVersion) {
		errs = append(errs, "prompt_version must match v{n}, ingest_v{n}, or summarizer_v{n}")
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

// ExtractWikiLinks returns unique [[slug]] references from the markdown body.
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