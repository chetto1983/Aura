//go:build db_integration

package runner

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db"
	"github.com/jackc/pgx/v5"
)

// The same scenario through the real conversations.Store: AppendTurn and LoadManagedHistory's
// ladder sit between the turn that read the page and the next one. The large page outgrows
// the store's 64 KiB turn cap, so its framed turn round-trips through the content sidecar.
func TestReloadedToolResultKeepsWhatTheModelSaw_Integration(t *testing.T) {
	pool := migratedRunnerPool(t)
	for name, tc := range map[string]struct {
		page        string
		wantSpilled int
	}{
		"inline":  {page: injectedPage},
		"sidecar": {page: injectedPage + strings.Repeat(" more page text", 70<<10/15), wantSpilled: 1},
	} {
		t.Run(name, func(t *testing.T) {
			client := reloadScenarioClient()
			r, convStore, _ := newIntegrationRunner(t, pool, client)
			convID := newIntegrationConversation(t, pool, convStore)
			assertReloadKeepsWhatTheModelSaw(ownerCtx(), t, r, client, convID, tc.page)

			var spilled int
			if err := db.WithIdentityTxRaw(ownerCtx(), pool, localIdentityID, func(tx pgx.Tx) error {
				return tx.QueryRow(ownerCtx(), `SELECT count(*) FROM aura.conversation_turns
					WHERE conversation_id = $1 AND role = 'tool' AND content_sidecar_path IS NOT NULL`, convID).Scan(&spilled)
			}); err != nil {
				t.Fatalf("count spilled tool turns: %v", err)
			}
			if spilled != tc.wantSpilled {
				t.Fatalf("spilled tool turns = %d, want %d", spilled, tc.wantSpilled)
			}
		})
	}
}
