//go:build db_integration

package runner

import "testing"

// The same scenario through the real conversations.Store: AppendTurn, the sidecar spill and
// LoadManagedHistory's ladder all sit between the turn that read the page and the next one.
func TestReloadedToolResultKeepsWhatTheModelSaw_Integration(t *testing.T) {
	pool := migratedRunnerPool(t)
	client := reloadScenarioClient()
	r, convStore, _ := newIntegrationRunner(t, pool, client)
	convID := newIntegrationConversation(t, pool, convStore)
	assertReloadKeepsWhatTheModelSaw(ownerCtx(), t, r, client, convID)
}
