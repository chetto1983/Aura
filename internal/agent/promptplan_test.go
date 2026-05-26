package agent

import (
	"strings"
	"testing"
	"time"
)

func TestComposeAgentPromptPlacesPinnedOperationalBeforeTools(t *testing.T) {
	plan := ComposeAgentPrompt(nil, nil, "## Overlay\noperator", "## Pinned Operational Lessons\n- [critical] pin", "## Skills\nskill", "## Tool Catalog\ntool", "", time.Now())
	overlayIdx := strings.Index(plan.Content, "## Overlay")
	pinnedIdx := strings.Index(plan.Content, "## Pinned Operational Lessons")
	toolsIdx := strings.Index(plan.Content, "## Tool Surface")
	if !(overlayIdx >= 0 && pinnedIdx > overlayIdx && toolsIdx > pinnedIdx) {
		t.Fatalf("unexpected prompt order: overlay=%d pinned=%d tools=%d\n%s", overlayIdx, pinnedIdx, toolsIdx, plan.Content)
	}
	if !containsString(plan.Modules, "pinned-operational") {
		t.Fatalf("modules = %v, want pinned-operational", plan.Modules)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
