package wiki

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const CurrentSchemaVersion = 1

var promptVersionRe = regexp.MustCompile(`^(v[0-9]+|ingest_v[0-9]+)$`)

// Page represents a validated wiki page.
type Page struct {
	Title         string   `yaml:"title"`
	Content       string   `yaml:"content"`
	Tags          []string `yaml:"tags,omitempty"`
	SchemaVersion int      `yaml:"schema_version"`
	PromptVersion string   `yaml:"prompt_version"`
	CreatedAt     string   `yaml:"created_at"`
	UpdatedAt     string   `yaml:"updated_at"`
}

// ValidationError contains all validation failures for a wiki page.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("wiki validation failed: %s", strings.Join(e.Errors, "; "))
}

// ParseYAML parses raw YAML bytes into a Page.
func ParseYAML(data []byte) (*Page, error) {
	var page Page
	if err := yaml.Unmarshal(data, &page); err != nil {
		return nil, fmt.Errorf("YAML parse error: %w", err)
	}
	return &page, nil
}

// Validate checks that a Page conforms to SCHEMA.md rules.
func Validate(page *Page) error {
	var errs []string

	if page.Title == "" {
		errs = append(errs, "title is required")
	} else if len(page.Title) > 200 {
		errs = append(errs, "title exceeds 200 characters")
	}

	if page.Content == "" {
		errs = append(errs, "content is required")
	}

	if len(page.Tags) > 10 {
		errs = append(errs, "too many tags (max 10)")
	}
	for i, tag := range page.Tags {
		if len(tag) > 50 {
			errs = append(errs, fmt.Sprintf("tag %d exceeds 50 characters", i))
		}
	}

	if page.SchemaVersion != CurrentSchemaVersion {
		errs = append(errs, fmt.Sprintf("schema_version must be %d", CurrentSchemaVersion))
	}

	if page.PromptVersion == "" {
		errs = append(errs, "prompt_version is required")
	} else if !promptVersionRe.MatchString(page.PromptVersion) {
		errs = append(errs, "prompt_version must match v{n} or ingest_v{n}")
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

// Slug generates a filesystem-safe slug from a title.
func Slug(title string) string {
	s := strings.ToLower(title)
	s = strings.ReplaceAll(s, " ", "-")
	// Remove non-alphanumeric/hyphen chars
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	result := b.String()
	// Collapse consecutive hyphens
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
