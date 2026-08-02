//go:build reasoning_live

// Live validation of the SHIPPED reasoning classifier against the real granite
// embedding sidecar (aura-llama-embed :8081), using Aura's production embedding
// client (documents.EmbeddingClient) — not a spike harness. Gated behind the
// reasoning_live tag and skipped when the sidecar is unreachable.
//
//	go test -tags reasoning_live -run TestReasoningClassifierLive ./internal/agent/prompt/
package prompt_test

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
)

func graniteURL() string {
	if v := os.Getenv("AURA_EMBED_BASE_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:8081"
}

func requireGranite(t *testing.T) {
	t.Helper()
	resp, err := http.Get(graniteURL() + "/health")
	if err != nil {
		t.Skipf("granite sidecar unreachable at %s: %v", graniteURL(), err)
	}
	_ = resp.Body.Close()
}

var liveCorpus = []struct{ prompt, gold string }{
	{"buongiorno!", "none"}, {"grazie mille per l'aiuto", "none"}, {"come ti chiami?", "none"},
	{"qual e la capitale dell'Italia?", "none"}, {"quanto fa 9 per 6?", "none"},
	{"traduci 'cane' in francese", "none"}, {"a presto, buona giornata", "none"}, {"ok perfetto grazie", "none"},
	{"che tempo fa domani a Cuneo?", "low"}, {"cerca le ultime notizie di oggi", "low"},
	{"prezzo del bitcoin adesso", "low"}, {"a che ora apre la posta?", "low"},
	{"trova un ristorante aperto stasera", "low"}, {"quando parte il prossimo treno per Torino?", "low"},
	{"risultati delle partite di ieri", "low"}, {"che traffico c'e sulla A6 ora?", "low"},
	{"scrivi una funzione Go che inverte una stringa", "high"}, {"debugga questo nil pointer dereference", "high"},
	{"progetta lo schema di un database per prenotazioni", "high"}, {"dimostra che radice di 2 e irrazionale", "high"},
	{"ottimizza questo algoritmo O(n^2)", "high"}, {"scrivi uno scraper con rate limiting", "high"},
	{"analizza questo stack trace e trova la causa", "high"}, {"crea una pipeline CI con build e test", "high"},
}

func TestReasoningClassifierLive(t *testing.T) {
	requireGranite(t)
	embedder := &documents.EmbeddingClient{
		BaseURL:    graniteURL(),
		Client:     &http.Client{Timeout: 30 * time.Second},
		Dimensions: config.DefaultEmbedDimensions,
	}
	defer embedder.Client.CloseIdleConnections() // goleak: drain keep-alive conns
	c := prompt.NewReasoningClassifier(embedder)
	if c == nil {
		t.Fatal("classifier nil with a real embedder")
	}

	correct, noneVsRest, usable := 0, 0, 0
	for _, tc := range liveCorpus {
		tier, ok := c.Classify(context.Background(), tc.prompt)
		if !ok {
			t.Errorf("Classify(%q) returned not-usable against live granite", tc.prompt)
			continue
		}
		usable++
		if string(tier) == tc.gold {
			correct++
		} else {
			t.Logf("miss: %q gold=%s pred=%s", tc.prompt, tc.gold, tier)
		}
		if (string(tier) == "none") == (tc.gold == "none") {
			noneVsRest++
		}
	}
	n := len(liveCorpus)
	if usable != n {
		t.Fatalf("only %d/%d classifications usable (granite must answer every turn)", usable, n)
	}
	accPct := 100 * correct / n
	nvrPct := 100 * noneVsRest / n
	t.Logf("LIVE production classifier: accuracy %d%% (%d/%d) | none-vs-rest %d%% (%d/%d)", accPct, correct, n, nvrPct, noneVsRest, n)
	// Thresholds are the spike-052 validated floor (90% accuracy / 92%
	// none-vs-rest), RAISED here from the shipped 80/88 per the 2026-06-12
	// operator amendment (D-04) as a deliberate accuracy improvement, sequenced
	// AFTER the Plan-02 behavior-equivalence proof. A regression below these is a
	// real signal — the seed/anchor set is tuned to meet this floor on the live
	// Italian corpus.
	if accPct < 90 {
		t.Errorf("live accuracy %d%% < 90%% floor", accPct)
	}
	if nvrPct < 92 {
		t.Errorf("live none-vs-rest %d%% < 92%% floor", nvrPct)
	}
}
