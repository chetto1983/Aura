package summarizer_test

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/wiki"
)

func TestAutoApplierActionNewWritesPage(t *testing.T) {
	ws := &fakeWikiStore{}
	a := summarizer.NewAutoApplier(ws)

	err := a.Apply(context.Background(), summarizer.Decision{
		Candidate: summarizer.Candidate{
			Fact:          "Marco likes concise agents",
			Category:      "preference",
			RelatedSlugs:  []string{"aura-agent"},
			SourceTurnIDs: []int64{1, 2},
		},
		Action: summarizer.ActionNew,
	})
	if err != nil {
		t.Fatalf("Apply(new): %v", err)
	}
	if len(ws.writes) != 1 {
		t.Fatalf("writes = %d, want 1", len(ws.writes))
	}
	page := ws.writes[0]
	if page.SchemaVersion != wiki.CurrentSchemaVersion || page.PromptVersion != "proposal_v1" {
		t.Fatalf("page versions = schema %d prompt %q", page.SchemaVersion, page.PromptVersion)
	}
	if page.Body == "" || page.Category != "preference" {
		t.Fatalf("page = %+v", page)
	}
}

func TestAutoApplierActionNewTagsLowScoreRelatedAsAmbiguous(t *testing.T) {
	ws := &fakeWikiStore{}
	a := summarizer.NewAutoApplier(ws)

	err := a.Apply(context.Background(), summarizer.Decision{
		Candidate: summarizer.Candidate{
			Fact:         "Marco likes concise agents",
			Score:        0.42,
			Category:     "preference",
			RelatedSlugs: []string{"aura-agent"},
		},
		Action: summarizer.ActionNew,
	})
	if err != nil {
		t.Fatalf("Apply(new): %v", err)
	}
	page := ws.writes[0]
	if len(page.Related) != 1 {
		t.Fatalf("related = %#v, want one ref", page.Related)
	}
	if page.Related[0].Confidence != wiki.ConfidenceAmbiguous {
		t.Fatalf("related confidence = %q, want %q", page.Related[0].Confidence, wiki.ConfidenceAmbiguous)
	}
}

func TestAutoApplierActionPatchAppendsToBody(t *testing.T) {
	ws := &fakeWikiStore{
		pages: map[string]*wiki.Page{
			"marco-info": {Title: "Marco Info", Body: "Original", Sources: []string{"turn:1"}},
		},
	}
	a := summarizer.NewAutoApplier(ws)

	err := a.Apply(context.Background(), summarizer.Decision{
		Candidate: summarizer.Candidate{
			Fact:          "Marco prefers fast loops",
			RelatedSlugs:  []string{"runtime-diet"},
			SourceTurnIDs: []int64{1, 3},
		},
		Action:     summarizer.ActionPatch,
		TargetSlug: "marco-info",
	})
	if err != nil {
		t.Fatalf("Apply(patch): %v", err)
	}
	page := ws.pages["marco-info"]
	if page.Body == "Original" {
		t.Fatalf("body was not patched")
	}
	if len(page.Sources) != 2 || page.Sources[1] != "turn:3" {
		t.Fatalf("sources = %#v, want turn:3 appended once", page.Sources)
	}
}

func TestAutoApplierActionPatchTagsMidScoreRelatedAsInferred(t *testing.T) {
	ws := &fakeWikiStore{
		pages: map[string]*wiki.Page{
			"marco-info": {Title: "Marco Info", Body: "Original"},
		},
	}
	a := summarizer.NewAutoApplier(ws)

	err := a.Apply(context.Background(), summarizer.Decision{
		Candidate: summarizer.Candidate{
			Fact:         "Marco prefers fast loops",
			Score:        0.60,
			RelatedSlugs: []string{"runtime-diet"},
		},
		Action:     summarizer.ActionPatch,
		TargetSlug: "marco-info",
	})
	if err != nil {
		t.Fatalf("Apply(patch): %v", err)
	}
	page := ws.pages["marco-info"]
	if len(page.Related) != 1 {
		t.Fatalf("related = %#v, want one ref", page.Related)
	}
	if page.Related[0].Confidence != wiki.ConfidenceInferred {
		t.Fatalf("related confidence = %q, want %q", page.Related[0].Confidence, wiki.ConfidenceInferred)
	}
}

func TestAutoApplierActionSkipWritesLogOnly(t *testing.T) {
	ws := &fakeWikiStore{}
	a := summarizer.NewAutoApplier(ws)

	if err := a.Apply(context.Background(), summarizer.Decision{Action: summarizer.ActionSkip, TargetSlug: "existing-page"}); err != nil {
		t.Fatalf("Apply(skip): %v", err)
	}
	if len(ws.writes) != 0 {
		t.Fatalf("writes = %d, want none", len(ws.writes))
	}
	if got := ws.logs[0]; got.action != "proposal skip" || got.slug != "existing-page" {
		t.Fatalf("log = %+v", got)
	}
}

type fakeWikiStore struct {
	pages  map[string]*wiki.Page
	writes []*wiki.Page
	logs   []struct{ action, slug string }
}

func (f *fakeWikiStore) WritePage(_ context.Context, page *wiki.Page, _ ...string) error {
	if f.pages == nil {
		f.pages = map[string]*wiki.Page{}
	}
	f.writes = append(f.writes, page)
	f.pages[wiki.Slug(page.Title)] = page
	return nil
}

func (f *fakeWikiStore) ReadPage(slug string) (*wiki.Page, error) {
	return f.pages[slug], nil
}

func (f *fakeWikiStore) AppendLog(_ context.Context, action, slug string) {
	f.logs = append(f.logs, struct{ action, slug string }{action: action, slug: slug})
}
