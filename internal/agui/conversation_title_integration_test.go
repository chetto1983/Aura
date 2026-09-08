//go:build db_integration

package agui

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/google/uuid"
)

func TestConversationTitleResource(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	id := seedConversation(t, store, pool, nil)
	server := newConvAPIServer(t, store)
	path := "/api/conversations/" + id + "/title"
	if status, _ := getJSON(t, server, path); status != http.StatusNotFound {
		t.Fatalf("pending title status=%d", status)
	}
	if err := store.SetTitleIfNull(ownerCtx(), id, "Titolo generato"); err != nil {
		t.Fatal(err)
	}
	if affected, err := store.RenameForIdentity(ownerCtx(), id, localIdentityID, "Titolo manuale"); err != nil || affected != 1 {
		t.Fatalf("rename: %d %v", affected, err)
	}
	if err := store.SetTitleIfNull(ownerCtx(), id, "Late generated title"); err != nil {
		t.Fatal(err)
	}
	status, body := getJSON(t, server, path)
	var data map[string]string
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || data["title"] != "Titolo manuale" {
		t.Fatalf("status=%d body=%s", status, body)
	}
	for _, absent := range []string{"invalid", uuid.NewString()} {
		if status, _ := getJSON(t, server, "/api/conversations/"+absent+"/title"); status != http.StatusNotFound {
			t.Fatalf("absent status=%d", status)
		}
	}
}
