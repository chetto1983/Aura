package arcadedb

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The readiness probe reads through FactsAbout because this read reaches no embedding route,
// so a hosted model that stops answering cannot make a working memory look down (lab VM,
// 2026-11-01). The hybrid search on the same client is the control: it does ask the route.
func TestFactsAboutReachesNoEmbeddingRoute(t *testing.T) {
	client, _ := recordingClient(t, `{"result":[]}`)
	asked := 0
	client.WithEmbedder(&hookEmbedder{
		inner: &failingEmbedder{err: errors.New("the route is silent"), space: "space-a"},
		hook:  func([]string) { asked++ },
	})

	hits, err := client.FactsAbout(context.Background(), "Aura readiness", "", 1, time.Time{}, FactsAboutDirect)
	if err != nil {
		t.Fatalf("FactsAbout: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("hits = %v, want none", hits)
	}
	if asked != 0 {
		t.Fatalf("FactsAbout asked the embedding route %d times", asked)
	}

	if _, err := client.SearchFactsHybrid(context.Background(), "Aura readiness", 1, time.Time{}); err != nil {
		t.Fatalf("SearchFactsHybrid: %v", err)
	}
	if asked == 0 {
		t.Fatal("the hybrid search never asked the route, so this test proves nothing")
	}
}
