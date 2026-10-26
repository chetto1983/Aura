package skills

import (
	"fmt"
	"sort"
	"strings"
)

// defaultManifestCapBytes is the manifest byte budget when RenderManifest is
// called with 0 (D-34 default). Past it the render stops and appends the overflow
// tail so a 100-skill set never bloats the model-visible tool Description (D-09).
const defaultManifestCapBytes = 8192

// RenderManifest builds the alphabetically-sorted name+description block that
// becomes the `skill` tool's Description (D-06). The output is BYTE-STABLE for a
// fixed skill set — the sort makes it order-independent of the loader's map
// iteration, and stability is load-bearing: any reshuffle busts the OpenRouter
// implicit prefix cache (T-11-02-I1). Once the running byte count would exceed
// capBytes the render stops and appends "N more — search with skill action=list
// {query}" (D-09 overflow). capBytes <= 0 falls back to the default.
func RenderManifest(skills []Skill, capBytes int) string {
	if capBytes <= 0 {
		capBytes = defaultManifestCapBytes
	}
	sorted := make([]Skill, len(skills))
	copy(sorted, skills)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var b strings.Builder
	rendered := 0
	for _, s := range sorted {
		line := fmt.Sprintf("- %s: %s\n", s.Name, s.Description)
		if rendered > 0 && b.Len()+len(line) > capBytes {
			break
		}
		b.WriteString(line)
		rendered++
	}
	if rendered < len(sorted) {
		fmt.Fprintf(&b, "%d more — search with skill action=list {query}\n", len(sorted)-rendered)
	}
	if b.Len() == 0 {
		return "No skills available.\n"
	}
	return b.String()
}
