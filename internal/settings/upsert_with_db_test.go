//go:build db_integration

package settings

import (
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/remotetunnel"
)

func TestUpsertWithCommitsSecretAndDesiredStateAtomically(t *testing.T) {
	pool := migratedPool(t)
	secrets, err := NewStore(pool, testAuthulaSecret)
	if err != nil {
		t.Fatal(err)
	}
	state := remotetunnel.NewStore(pool)
	initial, err := state.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	initial.Resources = remotetunnel.Resources{AccountID: initial.Resources.AccountID}
	if _, err = state.Advance(t.Context(), initial); err != nil {
		t.Fatal(err)
	}
	cleanupKeys(t, secrets, "CLOUDFLARE_API_TOKEN")
	if _, err = secrets.Upsert(t.Context(), "CLOUDFLARE_API_TOKEN", "previous", ""); err != nil {
		t.Fatal(err)
	}
	desired := remotetunnel.Desired{Enabled: true, AccountID: "account", ZoneName: "example.com", PublicLabel: "aura", WARPLabel: "aura-warp"}
	for _, mode := range []string{"callback-error", "panic", "stale", "success", "locked"} {
		t.Run(mode, func(t *testing.T) {
			before, e := state.Load(t.Context())
			if e != nil {
				t.Fatal(e)
			}
			previous, e := secrets.Secret(t.Context(), "CLOUDFLARE_API_TOKEN")
			if e != nil {
				t.Fatal(e)
			}
			candidate := desired
			generation := before.Generation
			if mode == "stale" {
				generation--
			}
			if mode == "locked" {
				before.Resources.TunnelID = "owned"
				if _, e = state.Advance(t.Context(), before); e != nil {
					t.Fatal(e)
				}
				candidate.AccountID = "other"
			}
			callback := func(tx sqlc.DBTX) error {
				if _, e := remotetunnel.NewStore(tx).SaveDesired(t.Context(), generation, candidate, "admin"); e != nil {
					return e
				}
				if mode == "callback-error" {
					return errors.New("companion failed")
				}
				if mode == "panic" {
					panic("companion panic")
				}
				return nil
			}
			var writeErr error
			panicked := false
			func() {
				defer func() { panicked = recover() != nil }()
				_, writeErr = secrets.UpsertWith(t.Context(), "CLOUDFLARE_API_TOKEN", "candidate", "admin", callback)
			}()
			after, e := state.Load(t.Context())
			if e != nil {
				t.Fatal(e)
			}
			token, e := secrets.Secret(t.Context(), "CLOUDFLARE_API_TOKEN")
			if e != nil {
				t.Fatal(e)
			}
			if mode == "success" {
				if writeErr != nil || panicked || after.Generation != before.Generation+1 || token != "candidate" {
					t.Fatalf("atomic commit failed: %v", writeErr)
				}
			} else {
				if writeErr == nil && !panicked {
					t.Fatal("failure accepted")
				}
				if after.Generation != before.Generation || token != previous {
					t.Fatal("partial commit")
				}
			}
			var raw string
			if e = pool.QueryRow(t.Context(), "SELECT value FROM aura.settings WHERE key='CLOUDFLARE_API_TOKEN'").Scan(&raw); e != nil {
				t.Fatal(e)
			}
			if !strings.HasPrefix(raw, "enc:v1:") || strings.Contains(raw, token) {
				t.Fatal("plaintext credential persisted")
			}
		})
	}
}
