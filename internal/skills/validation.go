package skills

import (
	"regexp"
	"strings"
)

var (
	catalogSourceRE  = regexp.MustCompile(`^[A-Za-z0-9@:._/\-]{1,200}$`)
	catalogSkillIDRE = regexp.MustCompile(`^[A-Za-z0-9._\-]{1,64}$`)
)

// ValidSkillName applies the dashboard/tool-facing skill name policy.
// Loader parsing accepts the same character set; callers that mutate or
// expose a single named skill also cap length to keep paths and URLs tidy.
func ValidSkillName(name string) bool {
	name = strings.TrimSpace(name)
	return len(name) >= 1 && len(name) <= 64 && skillNameRE.MatchString(name)
}

// ValidCatalogSource matches the safe source forms accepted by skills.sh:
// GitHub shorthands, GitHub URLs, and npm-style package specs. Calls never
// invoke a shell, but traversal-looking input is rejected before reaching npx.
func ValidCatalogSource(source string) bool {
	source = strings.TrimSpace(source)
	return catalogSourceRE.MatchString(source) && !strings.Contains(source, "..")
}

// ValidCatalogSkillID matches the skills.sh skillId format.
func ValidCatalogSkillID(skillID string) bool {
	skillID = strings.TrimSpace(skillID)
	return skillID == "" || catalogSkillIDRE.MatchString(skillID)
}
