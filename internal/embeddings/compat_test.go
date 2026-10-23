package embeddings

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// compatServer answers /v1/embeddings per model as the OpenRouter models measured on
// 2026-10-20 did (spikes/embedding-cloud-models): "ok" with the width it is asked, "refuses"
// with 422 when `dimensions` is sent, "wide" with 1024 values whatever it is asked, "flaky"
// with 429 on its first request and the asked width after.
type compatServer struct {
	*httptest.Server
	mu     sync.Mutex
	asked  map[string]int
	bodies []map[string]any
	bearer string
}

func newCompatServer(t *testing.T) *compatServer {
	t.Helper()
	srv := &compatServer{asked: map[string]int{}}
	srv.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		model, _ := body["model"].(string)
		asked, hasWidth := body["dimensions"].(float64)
		srv.mu.Lock()
		srv.asked[model]++
		calls := srv.asked[model]
		srv.bodies = append(srv.bodies, body)
		srv.bearer = r.Header.Get("Authorization")
		srv.mu.Unlock()
		width := int(asked)
		switch {
		case model == "refuses" && hasWidth:
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		case model == "flaky" && calls == 1:
			w.WriteHeader(http.StatusTooManyRequests)
			return
		case model == "wide" || !hasWidth:
			width = 1024
		}
		vector := make([]float64, width)
		vector[0] = 1
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"index": 0, "embedding": vector}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (s *compatServer) calls(model string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asked[model]
}

func TestAnswersAtWidthSendsWhatAHostedRouteSends(t *testing.T) {
	srv := newCompatServer(t)
	client := &Client{BaseURL: srv.URL, Model: "ok", APIKey: "key", Client: srv.Client()}

	if err := client.AnswersAtWidth(t.Context(), 768); err != nil {
		t.Fatalf("AnswersAtWidth: %v", err)
	}
	body := srv.bodies[0]
	inputs, _ := body["input"].([]any)
	text, _ := inputs[0].(string)
	if body["dimensions"] != float64(768) || !strings.HasPrefix(text, UntitledDocumentPrefix) || srv.bearer != "Bearer key" {
		t.Fatalf("request = %v, bearer %q: want dimensions 768, the document prefix and the key", body, srv.bearer)
	}
}

func TestAnswersAtWidthRefusesAModelThatRejectsDimensionsOrAnswersAnotherWidth(t *testing.T) {
	srv := newCompatServer(t)
	refuses := &Client{BaseURL: srv.URL, Model: "refuses", APIKey: "key", Client: srv.Client()}
	if err := refuses.AnswersAtWidth(t.Context(), 768); !RejectsInput(err) {
		t.Fatalf("err = %v, want the 422 a model gives when it refuses dimensions", err)
	}
	wide := &Client{BaseURL: srv.URL, Model: "wide", APIKey: "key", Client: srv.Client()}
	if err := wide.AnswersAtWidth(t.Context(), 768); err == nil || !strings.Contains(err.Error(), "1024") {
		t.Fatalf("err = %v, want a refusal naming the 1024 values it answered", err)
	}
}

// A verdict about the model is kept; a failure that says nothing about it (a 429 here) is not,
// and Refresh asks every model again.
func TestCompatibilityRemembersVerdictsAndRetriesWhatFailedForAMoment(t *testing.T) {
	srv := newCompatServer(t)
	compat := &Compatibility{Client: srv.Client(), CloudBaseURL: srv.URL, Width: 768, Key: staticCredential("key")}
	models := []string{"ok", "refuses", "wide", "flaky"}

	first, err := compat.Check(t.Context(), models, false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !first["ok"] || first["refuses"] || first["wide"] || first["flaky"] {
		t.Fatalf("first check = %v, want only ok", first)
	}

	second, err := compat.Check(t.Context(), models, false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !second["ok"] || !second["flaky"] || second["refuses"] || second["wide"] {
		t.Fatalf("second check = %v, want ok and the retried flaky", second)
	}
	for model, want := range map[string]int{"ok": 1, "refuses": 1, "wide": 1, "flaky": 2} {
		if got := srv.calls(model); got != want {
			t.Fatalf("%s asked %d times, want %d", model, got, want)
		}
	}

	if _, err := compat.Check(t.Context(), models, true); err != nil {
		t.Fatalf("Check(refresh): %v", err)
	}
	if got := srv.calls("ok"); got != 2 {
		t.Fatalf("refresh asked ok %d times in all, want 2", got)
	}
}

func TestCompatibilityWithoutAKeySendsNothing(t *testing.T) {
	srv := newCompatServer(t)
	compat := &Compatibility{Client: srv.Client(), CloudBaseURL: srv.URL, Width: 768, Key: staticCredential(" ")}
	if _, err := compat.Check(t.Context(), []string{"ok"}, false); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("err = %v, want ErrNoCredential", err)
	}
	if got := srv.calls("ok"); got != 0 {
		t.Fatalf("requests = %d, want none", got)
	}
}
