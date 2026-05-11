package wiki

import (
	"fmt"
)

// ConflictError is returned by WritePage when the on-disk updated_at does
// not match the caller-supplied expectedUpdatedAt, OR when expectedUpdatedAt
// is "" (create-only sentinel) but a page with that slug already exists.
// The write_wiki_page tool turns this into a structured JSON tool RESULT
// (D-03) so the LLM can re-read and retry deterministically.
type ConflictError struct {
	Slug     string
	Expected string // "" means create-only sentinel (D-02)
	Actual   string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("page %s was modified since last read (expected %s, got %s)",
		e.Slug, e.Expected, e.Actual)
}
