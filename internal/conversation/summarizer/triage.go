package summarizer

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

// TargetLayer indicates where a Candidate's fact should be stored.
type TargetLayer string

const (
	TargetWiki       TargetLayer = "wiki"
	TargetUserMemory TargetLayer = "user_memory"
)

var internalSpace = regexp.MustCompile(`\s+`)

// TriageCandidate routes a Candidate to the correct storage layer.
// person/preference/fact/todo -> TargetUserMemory; project/empty -> TargetWiki.
func TriageCandidate(c Candidate) TargetLayer {
	switch c.Category {
	case "person", "preference", "fact", "todo":
		return TargetUserMemory
	default:
		return TargetWiki
	}
}

// UserFactHandle returns a stable idempotency handle for a user-memory candidate.
// Format: user_memory:<sha256(category+normalizedFact)[:16]>
// normalizedFact = ToLower(TrimSpace(Fact)) with internal whitespace collapsed to single space.
func UserFactHandle(c Candidate) string {
	normalized := internalSpace.ReplaceAllString(strings.TrimSpace(c.Fact), " ")
	normalized = strings.ToLower(normalized)
	sum := sha256.Sum256([]byte(c.Category + normalized))
	return fmt.Sprintf("user_memory:%x", sum[:8])
}
