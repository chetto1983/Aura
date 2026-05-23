package wiki

import (
	"strings"
	"testing"
)

func TestInjectEntityLinks_WrapsUnlinkedMentions(t *testing.T) {
	body := `# Source: foo

The document references Siemens and Sacchi.
Siemens has a TIA Portal product.
PSA di Arese Aldo SAS is the customer.`
	candidates := []EntityCandidate{
		{Title: "Siemens", Slug: "siemens"},
		{Title: "Sacchi", Slug: "sacchi"},
		{Title: "TIA Portal", Slug: "tia-portal"},
		{Title: "PSA di Arese Aldo SAS", Slug: "psa"},
	}

	out, injected := InjectEntityLinks(body, candidates, "source-foo")
	if len(injected) != 4 {
		t.Fatalf("injected = %v, want 4 slugs", injected)
	}
	for _, slug := range []string{"siemens", "sacchi", "tia-portal", "psa"} {
		if !strings.Contains(out, "[["+slug+"]]") {
			t.Errorf("output missing [[%s]]:\n%s", slug, out)
		}
	}
	// Both occurrences of Siemens wrapped (whole-word, case-insensitive).
	if strings.Count(out, "[[siemens]]") != 2 {
		t.Errorf("expected 2 [[siemens]] occurrences, got %d:\n%s",
			strings.Count(out, "[[siemens]]"), out)
	}
}

func TestInjectEntityLinks_PreservesExistingLinks(t *testing.T) {
	body := `Siemens is linked already: [[siemens]]. Also Siemens unlinked here.`
	candidates := []EntityCandidate{
		{Title: "Siemens", Slug: "siemens"},
	}
	out, injected := InjectEntityLinks(body, candidates, "")
	if len(injected) != 1 {
		t.Fatalf("injected = %v", injected)
	}
	// Existing [[siemens]] preserved as-is; bare "Siemens" wrapped.
	if !strings.Contains(out, "[[siemens]]. Also [[siemens]] unlinked") {
		t.Errorf("unexpected output:\n%s", out)
	}
	// No double-wrap like [[[[siemens]]]].
	if strings.Contains(out, "[[[[") {
		t.Fatalf("double-wrap detected:\n%s", out)
	}
}

func TestInjectEntityLinks_SkipsCodeBlocks(t *testing.T) {
	body := "Plain Siemens mention.\n\n```\nSiemens inside code block\n```\n\nMore Siemens after."
	candidates := []EntityCandidate{
		{Title: "Siemens", Slug: "siemens"},
	}
	out, _ := InjectEntityLinks(body, candidates, "")
	// The code block content must be untouched.
	if !strings.Contains(out, "```\nSiemens inside code block\n```") {
		t.Errorf("code block content was rewritten:\n%s", out)
	}
	// Plain mentions outside the fence are wrapped.
	if !strings.Contains(out, "Plain [[siemens]] mention") {
		t.Errorf("plain mention not wrapped:\n%s", out)
	}
	if !strings.Contains(out, "More [[siemens]] after") {
		t.Errorf("post-fence mention not wrapped:\n%s", out)
	}
}

func TestInjectEntityLinks_SkipsInlineCode(t *testing.T) {
	body := "Use `Siemens` as a literal but Siemens elsewhere is wrapped."
	candidates := []EntityCandidate{{Title: "Siemens", Slug: "siemens"}}
	out, _ := InjectEntityLinks(body, candidates, "")
	if !strings.Contains(out, "Use `Siemens` as a literal") {
		t.Errorf("inline code rewritten:\n%s", out)
	}
	if !strings.Contains(out, "but [[siemens]] elsewhere") {
		t.Errorf("plain mention not wrapped:\n%s", out)
	}
}

func TestInjectEntityLinks_WordBoundaries(t *testing.T) {
	body := "Siemens, Siemenslike, semens, but not pseudoSiemens or Siemensian."
	candidates := []EntityCandidate{{Title: "Siemens", Slug: "siemens"}}
	out, _ := InjectEntityLinks(body, candidates, "")
	// Only the bare "Siemens," word match should be wrapped.
	if !strings.Contains(out, "[[siemens]],") {
		t.Errorf("standalone Siemens not wrapped:\n%s", out)
	}
	if strings.Contains(out, "[[siemens]]like") || strings.Contains(out, "[[siemens]]ian") {
		t.Errorf("suffix matches incorrectly wrapped:\n%s", out)
	}
}

func TestInjectEntityLinks_LongestTitleWins(t *testing.T) {
	body := "STEP 7 Professional V16 is broader than STEP 7 alone."
	candidates := []EntityCandidate{
		{Title: "STEP 7", Slug: "step-7"},
		{Title: "STEP 7 Professional V16", Slug: "step-7-prof-v16"},
	}
	out, injected := InjectEntityLinks(body, candidates, "")
	// Both slugs may inject — the longer wins on its phrase, the shorter
	// catches the second standalone "STEP 7" mention.
	if !strings.Contains(out, "[[step-7-prof-v16]] is broader") {
		t.Errorf("longer title did not win:\n%s", out)
	}
	if !strings.Contains(out, "than [[step-7]] alone") {
		t.Errorf("shorter title missed the standalone mention:\n%s", out)
	}
	if len(injected) != 2 {
		t.Errorf("expected both slugs injected, got %v", injected)
	}
}

func TestInjectEntityLinks_NoSelfLink(t *testing.T) {
	body := "This page is about Siemens specifically."
	candidates := []EntityCandidate{{Title: "Siemens", Slug: "siemens"}}
	out, injected := InjectEntityLinks(body, candidates, "siemens")
	if len(injected) != 0 {
		t.Errorf("self-link injected: %v", injected)
	}
	if strings.Contains(out, "[[siemens]]") {
		t.Errorf("self-link wrapped:\n%s", out)
	}
}

func TestInjectEntityLinks_EmptyInputsReturnNoChange(t *testing.T) {
	if out, inj := InjectEntityLinks("", []EntityCandidate{{Title: "X", Slug: "x"}}, ""); out != "" || inj != nil {
		t.Errorf("empty body: got out=%q inj=%v", out, inj)
	}
	if out, inj := InjectEntityLinks("body", nil, ""); out != "body" || inj != nil {
		t.Errorf("empty candidates: got out=%q inj=%v", out, inj)
	}
}

func TestIsLinkableCategory(t *testing.T) {
	for _, cat := range []string{"entity", "concept"} {
		if !IsLinkableCategory(cat) {
			t.Errorf("%q should be linkable", cat)
		}
	}
	for _, cat := range []string{"sources", "operational", "hub", "", "unknown"} {
		if IsLinkableCategory(cat) {
			t.Errorf("%q should NOT be linkable", cat)
		}
	}
}
