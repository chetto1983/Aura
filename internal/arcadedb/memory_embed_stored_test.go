package arcadedb

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/embeddings"
)

// refusingEmbedder refuses, with status, every request carrying one of refuse; the control
// input is refused too when refuseControl is set.
type refusingEmbedder struct {
	refuse        []string
	refuseControl bool
	status        int
	space         string
	calls         [][]string
}

func (e *refusingEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	e.calls = append(e.calls, append([]string(nil), texts...))
	for _, text := range texts {
		if strings.HasSuffix(text, controlInput) {
			if e.refuseControl {
				return nil, &embeddings.StatusError{Code: e.status}
			}
			continue
		}
		for _, refused := range e.refuse {
			if strings.HasSuffix(text, refused) {
				return nil, &embeddings.StatusError{Code: e.status}
			}
		}
	}
	vectors := make([][]float64, len(texts))
	for i := range texts {
		vectors[i] = vectorOf(float64(i + 1))
	}
	return vectors, nil
}

func (e *refusingEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: e.space}, nil
}

// swappingEmbedder names one space until it has embedded, and another after: a model
// swapped while a request is in flight.
type swappingEmbedder struct{ embedded bool }

func (e *swappingEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	e.embedded = true
	vectors := make([][]float64, len(texts))
	for i := range texts {
		vectors[i] = vectorOf(1)
	}
	return vectors, nil
}

func (e *swappingEmbedder) Space(context.Context) (embeddings.Space, error) {
	if e.embedded {
		return embeddings.Space{ID: "es1-new"}, nil
	}
	return embeddings.Space{ID: "es1-old"}, nil
}

func storedClient(t *testing.T, embedder DenseEmbedder) *Client {
	t.Helper()
	client, _ := recordingClient(t, `{"result":[]}`)
	return client.WithEmbedder(embedder)
}

func TestEmbedStoredStampsEveryVectorWithItsSpace(t *testing.T) {
	vectors, err := storedClient(t, &refusingEmbedder{space: "es1-a"}).
		embedStored(context.Background(), []string{"one", "two"})
	if err != nil {
		t.Fatalf("embedStored: %v", err)
	}
	for i, vector := range vectors {
		if vector.vector == nil || vector.space != "es1-a" {
			t.Fatalf("text %d = %+v, want a vector stamped es1-a", i, vector)
		}
	}
}

func TestEmbedStoredIsolatesTheOneRefusedText(t *testing.T) {
	embedder := &refusingEmbedder{refuse: []string{"poison"}, status: http.StatusBadRequest, space: "es1-a"}
	texts := []string{"alpha", "beta", "poison", "gamma", "delta"}
	vectors, err := storedClient(t, embedder).embedStored(context.Background(), texts)
	if err != nil {
		t.Fatalf("embedStored: %v", err)
	}
	for i, text := range texts {
		got := vectors[i]
		if text == "poison" {
			if got.vector != nil || got.space != "es1-a" {
				t.Fatalf("refused text = %+v, want the refusing space and no vector", got)
			}
			continue
		}
		if got.vector == nil || got.space != "es1-a" {
			t.Fatalf("%q = %+v, want embedded despite a refused neighbour", text, got)
		}
	}
	controls := 0
	for _, call := range embedder.calls {
		if len(call) == 1 && strings.HasSuffix(call[0], controlInput) {
			controls++
		}
	}
	if controls != 1 {
		t.Fatalf("control input sent %d times, want once per call", controls)
	}
}

// An unknown model id answers 400 to every input. Marking the texts would stamp a whole
// memory refused over one typo in a route.
func TestEmbedStoredBlamesTheRouteWhenTheControlIsRefusedToo(t *testing.T) {
	embedder := &refusingEmbedder{
		refuse: []string{"alpha", "beta"}, refuseControl: true, status: http.StatusBadRequest, space: "es1-a",
	}
	if _, err := storedClient(t, embedder).embedStored(context.Background(), []string{"alpha", "beta"}); err == nil {
		t.Fatal("a route refusing the control input too was read as two bad texts")
	}
}

func TestEmbedStoredNeverSplitsARouteFailure(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500, 503} {
		embedder := &refusingEmbedder{refuse: []string{"alpha"}, status: status, space: "es1-a"}
		if _, err := storedClient(t, embedder).embedStored(context.Background(), []string{"alpha", "beta"}); err == nil {
			t.Fatalf("HTTP %d was absorbed", status)
		}
		if len(embedder.calls) != 1 {
			t.Fatalf("HTTP %d was split into %d requests; only an input refusal is split", status, len(embedder.calls))
		}
	}
}

// Review Focus 2: read after the request, a vector from the old model could claim the new
// space and open the gate over it.
func TestEmbedStoredReadsTheSpaceBeforeTheRequest(t *testing.T) {
	vectors, err := storedClient(t, &swappingEmbedder{}).embedStored(context.Background(), []string{"alpha"})
	if err != nil {
		t.Fatalf("embedStored: %v", err)
	}
	if vectors[0].space != "es1-old" {
		t.Fatalf("stamp = %q, want the space read before the request", vectors[0].space)
	}
}

func TestEmbedStoredRefusesAnUnnamedSpace(t *testing.T) {
	if _, err := storedClient(t, &refusingEmbedder{}).embedStored(context.Background(), []string{"alpha"}); err == nil {
		t.Fatal("vectors were produced in a space with no name")
	}
}

func TestEmbedOneIsFailSoft(t *testing.T) {
	// Compared by field: storedVector holds a slice, and == on one would panic, not fail.
	var none *Client
	if got := none.embedOne(context.Background(), "fact"); got.vector != nil || got.space != "" {
		t.Fatalf("nil client = %+v", got)
	}
	down := storedClient(t, &refusingEmbedder{refuse: []string{"fact"}, status: http.StatusServiceUnavailable, space: "es1-a"})
	if got := down.embedOne(context.Background(), "fact"); got.vector != nil || got.space != "" {
		t.Fatalf("a route failure = %+v, want nothing stored", got)
	}
}

func TestUpsertFactStampsTheSpaceThatRefusedIt(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	client.WithEmbedder(&refusingEmbedder{
		refuse: []string{validFact().Statement}, status: http.StatusBadRequest, space: "es1-a",
	})
	if _, err := client.UpsertFact(context.Background(), validFact(), now); err != nil {
		t.Fatalf("UpsertFact: %v", err)
	}
	edge, params := rec.statements[len(rec.statements)-1], rec.params[len(rec.params)-1]
	if strings.Contains(edge, "embedding = :embedding") || !strings.Contains(edge, "embed_space = :embed_space") {
		t.Fatalf("a refused fact must carry the refusing space and no vector:\n%s", edge)
	}
	if params["embed_space"] != "es1-a" {
		t.Fatalf("embed_space = %v, want es1-a", params["embed_space"])
	}
}

func TestFactSchemaStampsTheSpaceWithIndexedNulls(t *testing.T) {
	joined := strings.Join(vectorSchemaStatements(), "\n")
	for _, want := range []string{
		"CREATE PROPERTY FACT.embed_space IF NOT EXISTS STRING",
		"CREATE INDEX IF NOT EXISTS ON FACT (embed_space) NOTUNIQUE NULL_STRATEGY INDEX",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("FACT schema lacks %q", want)
		}
	}
}
