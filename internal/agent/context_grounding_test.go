package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/aura/aura/internal/storage/memoryindex"
)

type fakeGroundingStore struct {
	docs []memoryindex.Document
}

func (f fakeGroundingStore) Search(_ context.Context, query string, filter memoryindex.Filter) ([]memoryindex.Document, error) {
	var out []memoryindex.Document
	for _, doc := range f.docs {
		if len(filter.Kinds) > 0 && !groundingTestContains(filter.Kinds, doc.Kind) {
			continue
		}
		if doc.Kind == memoryindex.KindUserMemory || groundingTestMatches(query, doc.Title+" "+doc.Body) {
			out = append(out, doc)
		}
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func TestBuildTurnGroundingCapsuleIncludesLocationAndLocalSource(t *testing.T) {
	store := fakeGroundingStore{docs: []memoryindex.Document{
		{
			ID:       "user:loc",
			Kind:     memoryindex.KindUserMemory,
			Title:    "Location",
			Body:     "Davide vive a Caraglio (CN).",
			Status:   "active",
			Entities: []string{"Davide Marchetto", "Caraglio"},
		},
		{
			ID:     "source:meteo",
			Kind:   memoryindex.KindSource,
			Title:  "Meteo Caraglio",
			Body:   "METEO_CARAGLIO_GROUND_TRUTH: Caraglio oggi e nuvoloso, 18C, vento NW.",
			Status: "active",
		},
	}}

	got := BuildTurnGroundingCapsule(context.Background(), store, "Che tempo fa oggi da me?")
	for _, want := range []string{"Turn Grounding Capsule", "Davide vive a Caraglio", "18C", "vento NW"} {
		if !strings.Contains(got, want) {
			t.Fatalf("grounding capsule missing %q:\n%s", want, got)
		}
	}
}

func TestBuildTurnGroundingCapsuleSkipsWithoutCue(t *testing.T) {
	store := fakeGroundingStore{docs: []memoryindex.Document{{
		ID:     "user:loc",
		Kind:   memoryindex.KindUserMemory,
		Title:  "Location",
		Body:   "Davide vive a Caraglio.",
		Status: "active",
	}}}
	if got := BuildTurnGroundingCapsule(context.Background(), store, "Ciao"); got != "" {
		t.Fatalf("grounding capsule for no-cue turn = %q, want empty", got)
	}
}

func groundingTestContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func groundingTestMatches(query, text string) bool {
	text = strings.ToLower(text)
	for _, term := range strings.Fields(strings.ToLower(query)) {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}
