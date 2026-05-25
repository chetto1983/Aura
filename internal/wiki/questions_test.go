package wiki

import (
	"reflect"
	"strings"
	"testing"
)

func TestSuggestQuestions_AmbiguousEdges(t *testing.T) {
	store := newSurpriseTestStore(t, map[string]*Page{
		"alpha": {
			Title:    "Alpha",
			Category: "project",
			Body:     "Alpha.",
			Related:  []RelatedRef{{Slug: "beta", Confidence: ConfidenceAmbiguous}},
		},
		"beta": {
			Title:    "Beta",
			Category: "person",
			Body:     "Beta.",
		},
	})

	q := findQuestion(t, store.SuggestQuestions(7), "ambiguous_edge")
	if q.Text != "What is the exact relationship between [[alpha]] and [[beta]]?" {
		t.Fatalf("question = %q", q.Text)
	}
	if !reflect.DeepEqual(q.Related, []string{"alpha", "beta"}) {
		t.Fatalf("related = %#v, want alpha/beta", q.Related)
	}
}

func TestSuggestQuestions_BridgeNodes(t *testing.T) {
	store := newSurpriseTestStore(t, map[string]*Page{
		"bridge": {Title: "Bridge", Category: "system", Body: "[[entity-a]] [[concept-a]]"},
		"entity-a": {
			Title:    "Entity A",
			Category: "entity",
			Body:     "Entity.",
		},
		"concept-a": {
			Title:    "Concept A",
			Category: "concept",
			Body:     "Concept.",
		},
	})

	q := findQuestion(t, store.SuggestQuestions(7), "bridge_node")
	if !strings.Contains(q.Text, "[[bridge]]") || !strings.Contains(q.Text, "concept") || !strings.Contains(q.Text, "entity") {
		t.Fatalf("bridge question missing slug/categories: %+v", q)
	}
	if !strings.Contains(q.Why, "outbound spans 2 distinct categories") {
		t.Fatalf("why = %q, want category span reason", q.Why)
	}
}

func TestSuggestQuestions_VerifyInferred(t *testing.T) {
	store := newSurpriseTestStore(t, map[string]*Page{
		"hub": {
			Title:    "Hub",
			Category: "system",
			Body:     "[[spoke-a]] [[spoke-b]] [[spoke-c]]",
			Related: []RelatedRef{
				{Slug: "spoke-a", Confidence: ConfidenceInferred},
				{Slug: "spoke-b", Confidence: ConfidenceInferred},
				{Slug: "spoke-c", Confidence: ConfidenceInferred},
			},
		},
		"spoke-a": {Title: "Spoke A", Category: "entity", Body: "A."},
		"spoke-b": {Title: "Spoke B", Category: "entity", Body: "B."},
		"spoke-c": {Title: "Spoke C", Category: "entity", Body: "C."},
	})

	q := findQuestion(t, store.SuggestQuestions(7), "verify_inferred")
	if !strings.Contains(q.Text, "3 inferred relationships involving [[hub]]") {
		t.Fatalf("question = %q, want inferred count and hub slug", q.Text)
	}
	if !strings.Contains(q.Text, "[[spoke-a]]") || !strings.Contains(q.Text, "[[spoke-b]]") {
		t.Fatalf("question = %q, want first two inferred targets", q.Text)
	}
}

func TestSuggestQuestions_IsolatedNodes(t *testing.T) {
	store := newSurpriseTestStore(t, map[string]*Page{
		"hub":      {Title: "Hub", Category: "system", Body: "[[linked]]"},
		"linked":   {Title: "Linked", Category: "system", Body: "[[hub]]"},
		"island-a": {Title: "Island A", Category: "note", Body: "No links."},
		"island-b": {Title: "Island B", Category: "note", Body: "No links."},
	})

	q := findQuestion(t, store.SuggestQuestions(7), "isolated_nodes")
	if !strings.Contains(q.Text, "[[island-a]]") || !strings.Contains(q.Text, "[[island-b]]") {
		t.Fatalf("question = %q, want both isolated slugs", q.Text)
	}
	if got, want := q.Related, []string{"island-a", "island-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("related = %#v, want %#v", got, want)
	}
}

func TestSuggestQuestions_StableOrdering(t *testing.T) {
	store := newSuggestQuestionsRichStore(t)
	want := store.SuggestQuestions(7)
	for i := 0; i < 10; i++ {
		if got := store.SuggestQuestions(7); !reflect.DeepEqual(got, want) {
			t.Fatalf("run %d questions differ:\ngot  %#v\nwant %#v", i, got, want)
		}
	}
}

func TestSuggestQuestions_RespectsCap(t *testing.T) {
	store := newSuggestQuestionsRichStore(t)
	if got := store.SuggestQuestions(2); len(got) != 2 {
		t.Fatalf("len=%d, want 2: %#v", len(got), got)
	}
	if got := store.SuggestQuestions(50); len(got) > suggestQuestionsMaxTopK {
		t.Fatalf("len=%d, want <= %d", len(got), suggestQuestionsMaxTopK)
	}
}

func TestSuggestQuestions_EmptyIndex(t *testing.T) {
	var nilStore *Store
	if got := nilStore.SuggestQuestions(7); got != nil {
		t.Fatalf("nil store got %#v, want nil", got)
	}
	store := newSurpriseTestStore(t, nil)
	if got := store.SuggestQuestions(7); got != nil {
		t.Fatalf("empty graph got %#v, want nil", got)
	}
}

func TestSuggestQuestions_SkipsAuxiliaryNodes(t *testing.T) {
	store := newSurpriseTestStore(t, map[string]*Page{
		"index": {
			Title:    "Index",
			Category: "index",
			Body:     "Operational hub.",
			Related:  []RelatedRef{{Slug: "real-page", Confidence: ConfidenceAmbiguous}},
		},
		"real-page": {Title: "Real Page", Category: "entity", Body: "Useful page."},
	})
	for _, q := range store.SuggestQuestions(7) {
		if strings.Contains(q.Text, "[[index]]") {
			t.Fatalf("auxiliary index emitted in question: %+v", q)
		}
	}
}

func TestSuggestQuestions_TemplateHygiene(t *testing.T) {
	store := newSuggestQuestionsRichStore(t)
	questions := store.SuggestQuestions(7)
	if len(questions) < 4 {
		t.Fatalf("len=%d, want at least 4 bucket examples: %#v", len(questions), questions)
	}
	banned := []string{"how can i help", "posso aiutarti", "what would you like"}
	for _, q := range questions {
		if !strings.HasSuffix(q.Text, "?") {
			t.Fatalf("%s question does not end with ?: %q", q.Type, q.Text)
		}
		if !strings.Contains(q.Text, "[[") || !strings.Contains(q.Text, "]]") {
			t.Fatalf("%s question has no wiki citation: %q", q.Type, q.Text)
		}
		lower := strings.ToLower(q.Text)
		for _, phrase := range banned {
			if strings.Contains(lower, phrase) {
				t.Fatalf("%s question contains banned phrase %q: %q", q.Type, phrase, q.Text)
			}
		}
	}
}

func newSuggestQuestionsRichStore(t *testing.T) *Store {
	t.Helper()
	return newSurpriseTestStore(t, map[string]*Page{
		"ambiguous-a": {
			Title:    "Ambiguous A",
			Category: "project",
			Body:     "Ambiguous A.",
			Related:  []RelatedRef{{Slug: "ambiguous-b", Confidence: ConfidenceAmbiguous}},
		},
		"ambiguous-b": {Title: "Ambiguous B", Category: "person", Body: "Ambiguous B."},
		"bridge":      {Title: "Bridge", Category: "system", Body: "[[entity-a]] [[concept-a]]"},
		"entity-a":    {Title: "Entity A", Category: "entity", Body: "Entity."},
		"concept-a":   {Title: "Concept A", Category: "concept", Body: "Concept."},
		"hub": {
			Title:    "Hub",
			Category: "system",
			Body:     "[[spoke-a]] [[spoke-b]] [[spoke-c]]",
			Related: []RelatedRef{
				{Slug: "spoke-a", Confidence: ConfidenceInferred},
				{Slug: "spoke-b", Confidence: ConfidenceInferred},
				{Slug: "spoke-c", Confidence: ConfidenceInferred},
			},
		},
		"spoke-a": {Title: "Spoke A", Category: "entity", Body: "A."},
		"spoke-b": {Title: "Spoke B", Category: "entity", Body: "B."},
		"spoke-c": {Title: "Spoke C", Category: "entity", Body: "C."},
		"island":  {Title: "Island", Category: "note", Body: "No links."},
	})
}

func findQuestion(t *testing.T, questions []Question, typ string) Question {
	t.Helper()
	for _, q := range questions {
		if q.Type == typ {
			return q
		}
	}
	t.Fatalf("missing question type %q in %#v", typ, questions)
	return Question{}
}
