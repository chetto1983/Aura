//go:build db_integration

package agui

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/board"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
)

func TestBoardProgressHTTPWithRealPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrateURL, appURL := os.Getenv("AURA_DB_MIGRATE_URL"), os.Getenv("AURA_DB_URL")
	if migrateURL == "" || appURL == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("board progress integration needs composed database URLs under CI")
		}
		t.Skip("board progress integration needs a disposable Postgres")
	}
	if _, err := db.Migrate(ctx, dbtest.MigrateURL(t, migrateURL)); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	owner := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO aura.identities (id,name,kind) VALUES ($1,$2,'user')`, owner, "progress-http-"+owner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, owner) })
	s := boardServer(board.New(pool))
	created := policyRequest(t, s, http.MethodPost, "/api/board/cards", `{"label":"renew the contract","progress":0.55}`, owner)
	if created.Code != http.StatusOK {
		t.Fatalf("add: %d %s", created.Code, created.Body.String())
	}
	card := decodeBoard[boardCardJSON](t, created)
	if card.Progress != 0.55 {
		t.Fatalf("add progress: %+v", card)
	}
	path := "/api/board/cards/" + card.ID
	for _, body := range []string{`{"label":"confirm renewal"}`, `{"progress":null}`} {
		updated := policyRequest(t, s, http.MethodPut, path, body, owner)
		if updated.Code != http.StatusOK || decodeBoard[boardCardJSON](t, updated).Progress != 0.55 {
			t.Fatalf("omitted/null progress: %s", updated.Body.String())
		}
	}
	for _, body := range []string{`{"progress":-0.01}`, `{"progress":1.01}`, `{"progress":"55%"}`} {
		refused := policyRequest(t, s, http.MethodPut, path, body, owner)
		if refused.Code != http.StatusBadRequest {
			t.Fatalf("invalid %s: %d %s", body, refused.Code, refused.Body.String())
		}
	}
	reset := policyRequest(t, s, http.MethodPut, path, `{"progress":0}`, owner)
	if reset.Code != http.StatusOK || decodeBoard[boardCardJSON](t, reset).Progress != 0 {
		t.Fatalf("reset: %s", reset.Body.String())
	}
	listed := policyRequest(t, s, http.MethodGet, "/api/board/cards", "", owner)
	cards := decodeBoard[[]boardCardJSON](t, listed)
	if listed.Code != http.StatusOK || len(cards) != 1 || cards[0].Progress != 0 || cards[0].Label != "confirm renewal" {
		t.Fatalf("persisted reset: %s", listed.Body.String())
	}
}
