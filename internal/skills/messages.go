package skills

import (
	"sort"
	"strings"
)

// alwaysBlockHeader is the ONE English header literal that leads the messages[1]
// always-block (D-07). It is a frozen constant — never templated, never carrying a
// per-turn value — so the rendered block is byte-stable across turns (the same
// discipline as the manifest and messages[0]). The block is the Phase-14 Agent.md
// seam: a future profile renders FIRST, the always-skills after; the ordering hook
// is documented here and at the bootChat assembly.
const alwaysBlockHeader = "Active skill instructions (always-on):\n\n"

// RenderAlwaysBlock concatenates the bodies of the always:true skills into ONE
// byte-stable user-role block destined for messages[1] (D-07). The skills are
// emitted in DETERMINISTIC order (alphabetical by name — the same byte-stable
// discipline as the manifest), each body separated by a blank line, under the frozen
// English header. present=false (and an empty block) when no always:true skill
// exists, so the caller can omit the messages[1] turn entirely. Per-skill bodies are
// already ≤32KiB-capped at load (D-10), so the block has a bounded size.
//
// Two calls over the same skill set produce byte-identical output (no clock, no map
// iteration order): this is the CAP-04 byte-stability the messages[0]+tools prefix
// cache depends on — a skill add/remove changes messages[1] but never messages[0].
func RenderAlwaysBlock(loaded []Skill) (block string, present bool) {
	always := make([]Skill, 0, len(loaded))
	for _, s := range loaded {
		if s.Always {
			always = append(always, s)
		}
	}
	if len(always) == 0 {
		return "", false
	}
	sort.Slice(always, func(i, j int) bool { return always[i].Name < always[j].Name })

	var b strings.Builder
	b.WriteString(alwaysBlockHeader)
	for i, s := range always {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.TrimRight(s.Body, "\n"))
	}
	return b.String(), true
}
