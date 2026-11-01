package agui

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/chetto1983/aura/internal/board"
)

func TestBoardProgressReturnedByTheHTTPAPI(t *testing.T) {
	var card board.Card
	if err := json.Unmarshal([]byte(`{"ID":"c-1","Label":"renew the contract","Progress":0.55}`), &card); err != nil {
		t.Fatal(err)
	}
	s := boardServer(&fakeBoardStore{cards: []board.Card{card}})
	response := boardRequest(t, s, http.MethodGet, "/api/board/cards", "")
	if response.Code != http.StatusOK {
		t.Fatalf("list: %d %s", response.Code, response.Body.String())
	}
	cards := decodeBoard[[]map[string]any](t, response)
	if len(cards) != 1 || cards[0]["progress"] != 0.55 {
		t.Fatalf("progress did not survive the store-to-HTTP boundary: %s", response.Body.String())
	}
}

func TestBoardProgressHTTPInputsPreserveExplicitZero(t *testing.T) {
	store := &fakeBoardStore{}
	s := boardServer(store)
	for _, input := range []struct {
		encoded string
		want    float64
	}{{"0.55", 0.55}, {"0", 0}, {"1", 1}} {
		progress, want := input.encoded, input.want
		response := boardRequest(t, s, http.MethodPost, "/api/board/cards", `{"label":"renew contract","progress":`+progress+`}`)
		if response.Code != http.StatusOK {
			t.Fatalf("add %s: %s", progress, response.Body.String())
		}
		created := decodeBoard[map[string]any](t, response)
		if store.added.Progress != want || created["progress"] != want {
			t.Fatalf("progress %s lost at add boundary: %+v %s", progress, store.added, response.Body.String())
		}
		updated := boardRequest(t, s, http.MethodPut, "/api/board/cards/c-1", `{"progress":`+progress+`}`)
		if updated.Code != http.StatusOK || store.patch.Progress == nil || *store.patch.Progress != want {
			t.Fatalf("progress %s lost at update boundary: %+v", progress, store.patch)
		}
	}
	for _, body := range []string{`{"label":"rename"}`, `{"progress":null}`} {
		response := boardRequest(t, s, http.MethodPut, "/api/board/cards/c-1", body)
		if response.Code != http.StatusOK || store.patch.Progress != nil {
			t.Fatalf("omitted/null progress must preserve the stored value: %+v", store.patch)
		}
	}
	for _, body := range []string{`{"progress":"55%"}`, `{"progress":true}`, `{"progress":1e309}`} {
		response := boardRequest(t, s, http.MethodPut, "/api/board/cards/c-1", body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("nonnumeric progress %s: %d", body, response.Code)
		}
	}
}
