package manager

import (
	"strings"

	"github.com/chetto1983/aura/internal/mcp"
)

// first_party.go answers ONE question: is this server a sidecar Aura itself ships,
// provisions and starts — or is it somebody else's?
//
// Only the first kind may be handed a grant Aura mints for itself. For an operator-added
// or remote server the OAuth flow IS the consent, and self-issuing around it would turn
// the whole authorization surface into decoration. So the predicate is deliberately
// narrow and positive: it matches against the shipped catalog rather than excluding a
// list of things that look dangerous.
//
// It keys on the recipe SOURCE and the recipe URL together, and neither alone would do:
//   - the name is not enough — `aura mcp install memory mymem` renames the server, which
//     is exactly the case mcp.SourceRecipeMemory's own comment was written for;
//   - the source is not enough — servers.json is an operator-editable file, so a
//     hand-planted entry could borrow `recipe:calendar` and point at a host of its
//     choosing. Pinning the URL to the catalog's means such an entry is NOT first-party.
//
// `aura mcp add` cannot reach this at all: it hard-codes Source "manual".

// FirstPartyRecipe reports whether server is, byte for byte, one of the MCP sidecars in
// Aura's built-in catalog (calendar, memory, whatsapp) — the same recipe, at the same
// address this deployment resolves for it.
func FirstPartyRecipe(server mcp.ManagedServer) bool {
	source := strings.TrimSpace(server.Source)
	url := strings.TrimSpace(server.URL)
	if source == "" || url == "" {
		return false
	}
	for _, entry := range BuiltInCatalog() {
		if strings.TrimSpace(entry.Server.Source) != source {
			continue
		}
		return strings.TrimSpace(entry.Server.URL) == url
	}
	return false
}
