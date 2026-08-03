package filecard

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// minimalPDF is a hand-written one-page PDF whose only content is the text
// passed in. Its cross-reference table is deliberately absent: poppler rebuilds
// one by scanning, which is also what makes it a fair stand-in for the damaged
// files ingest actually receives.
func minimalPDF(text string) string {
	return "%PDF-1.4\n" +
		"1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n" +
		"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
		"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 300 200]" +
		"/Resources<</Font<</F1 4 0 R>>>>/Contents 5 0 R>>endobj\n" +
		"4 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj\n" +
		"5 0 obj<</Length 200>>stream\n" +
		"BT /F1 12 Tf 20 100 Td (" + text + ") Tj ET\n" +
		"endstream\nendobj\ntrailer<</Root 1 0 R/Size 6>>\n%%EOF\n"
}

// requireExtractor keeps a missing extractor from passing for a test whose whole
// subject is the extractor. Locally it skips; in CI it fails, because a job that
// silently exercises nothing is the falsely-green case the test discipline bans
// (the CI workflows install poppler-utils for exactly this).
func requireExtractor(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(pdfTextTool); err == nil {
		return
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		t.Fatalf("%s is not installed and $CI is set: the extraction path would run nowhere", pdfTextTool)
	}
	t.Skipf("%s is not installed", pdfTextTool)
}

// hideExtractor points PATH at an empty directory so exec.LookPath fails. It is
// how the no-extractor host — a developer laptop, a runner without
// poppler-utils — is exercised without uninstalling anything.
func hideExtractor(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

func TestPDFTextLandsInTheCard(t *testing.T) {
	requireExtractor(t)
	path := writeFile(t, "decreto.pdf", minimalPDF("Codice dell amministrazione digitale"))
	card := build(t, path, "decreto.pdf")
	rendered := card.Render()

	if card.Title != "Codice dell amministrazione digitale" {
		t.Errorf("title = %q, want the first line of the page", card.Title)
	}
	if !strings.Contains(rendered, "Opens: Codice dell amministrazione digitale") {
		t.Errorf("the extracted text is not in the card:\n%s", rendered)
	}
	if strings.Contains(rendered, "did not read this PDF's text") {
		t.Errorf("a PDF whose text WAS read still claims it was not:\n%s", rendered)
	}
	if !strings.Contains(rendered, "read at ingest") {
		t.Errorf("the card does not say how much of the file it read:\n%s", rendered)
	}
}

// A PDF that opens but carries no text — a scan — must say which of the two it
// is, because "no text" and "could not open" call for different actions.
func TestPDFWithoutExtractableTextSaysItIsProbablyAScan(t *testing.T) {
	requireExtractor(t)
	path := writeFile(t, "scansione.pdf", minimalPDF(""))
	rendered := build(t, path, "scansione.pdf").Render()
	if !strings.Contains(rendered, "probably a scan") {
		t.Errorf("a text-free PDF does not say so:\n%s", rendered)
	}
}

// Without an extractor the card is exactly what it was before there was one:
// metadata, page count, and the statement that the words were not read.
func TestPDFWithoutAnExtractorKeepsTheHonestCard(t *testing.T) {
	hideExtractor(t)
	path := writeFile(t, "manuale.pdf", "%PDF-1.7\n"+
		"1 0 obj<</Title (SINAMICS G220 converter)/Author (Siemens)>>endobj\n"+
		"2 0 obj<</Type /Page>>endobj\n3 0 obj<</Type /Page>>endobj\n%%EOF\n")
	rendered := build(t, path, "manuale.pdf").Render()
	for _, want := range []string{
		"Titled: SINAMICS G220 converter",
		"Author: Siemens",
		"2 pages",
		"did not read this PDF's text",
		"No PDF text extractor is installed on this host.",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the degraded card is missing %q\n%s", want, rendered)
		}
	}
}

// A PDF that cannot be read must never fail an ingest.
func TestUnreadablePDFStillReturnsACard(t *testing.T) {
	path := writeFile(t, "rotto.pdf", "not a PDF at all")
	card, err := Build(Request{Path: path, FileName: "rotto.pdf"})
	if err != nil {
		t.Fatalf("a broken PDF failed the build: %v", err)
	}
	if !strings.Contains(card.Render(), "did not read this PDF's text") {
		t.Errorf("a broken PDF pretends to be described:\n%s", card.Render())
	}
}

// A producer that writes its source file name into /Title has described nothing.
// The reference corpus' Costituzione.pdf does exactly that, and it displaced the
// title the first page states.
func TestFileNameTitleIsNotATitle(t *testing.T) {
	for _, tc := range []struct {
		title string
		want  bool
	}{
		{"Costituzione VIGENTE DEF.pdf", true},
		{"relazione.docx", true},
		{"SINAMICS G220 converter", false},
		{"art. 3", false},
		{"", false},
	} {
		if got := titleIsFileName(tc.title); got != tc.want {
			t.Errorf("titleIsFileName(%q) = %v, want %v", tc.title, got, tc.want)
		}
	}
}

func TestPDFBlocksGroupsOnBlankLines(t *testing.T) {
	blocks := pdfBlocks("DECRETO 7 marzo 2005\nn. 82\n\n\nCapo I\nPRINCIPI\n\n   \n")
	want := []string{"DECRETO 7 marzo 2005 n. 82", "Capo I PRINCIPI"}
	if len(blocks) != len(want) {
		t.Fatalf("blocks = %q, want %q", blocks, want)
	}
	for i := range want {
		if blocks[i] != want[i] {
			t.Errorf("block %d = %q, want %q", i, blocks[i], want[i])
		}
	}
	if got := pdfBlocks(""); got != nil {
		t.Errorf("empty output produced blocks %q", got)
	}
}

// The 30 MB manual's cover page decodes to "&EJUJPO" — a subset font with no
// usable ToUnicode — and naming a file after that is worse than leaving it
// unnamed.
func TestPDFTitleSkipsOneWordBlocks(t *testing.T) {
	if got := pdfTitle([]string{"&EJUJPO", "1", "Fundamental safety instructions"}); got != "Fundamental safety instructions" {
		t.Errorf("title = %q, want the first block wide enough to be a statement", got)
	}
	if got := pdfTitle([]string{"a", "b"}); got != "" {
		t.Errorf("title = %q, want none", got)
	}
	long := strings.Repeat("parola ", 60)
	if got := pdfTitle([]string{long}); !strings.HasSuffix(got, "…") {
		t.Errorf("a long first block was not truncated: %q", got)
	}
}

// The recurring terms sketch what the opening does not cover. Keeping the two
// lists disjoint is what makes a stop-word list unnecessary: the function words
// are all spent in the opening.
func TestRecurringTermsSketchWhatTheOpeningDoesNotCover(t *testing.T) {
	opening := []string{"il marchio della societa"}
	blocks := []string{
		"il marchio della societa",
		"registrazione registrazione registrazione della domanda",
		"brevetto brevetto brevetto brevetto il il il",
		"solo due due",
	}
	terms := recurringTerms(blocks, opening)
	if got := joinValues(terms); got != "brevetto (4), registrazione (3)" {
		t.Errorf("terms = %q", got)
	}
	for _, term := range terms {
		if term.Text == "marchio" || term.Text == "della" || term.Text == "il" {
			t.Errorf("a term already spent in the opening came back: %q", term.Text)
		}
	}
}

func TestRecurringTermsAreCappedAndOrdered(t *testing.T) {
	var blocks []string
	for i := range pdfTerms + 5 {
		// Later words repeat more often, so a positional selection would keep the
		// wrong ones and the frequency rule keeps the right ones.
		blocks = append(blocks, strings.TrimSpace(strings.Repeat(string(rune('a'+i))+"word ", i+pdfTermFloor)))
	}
	terms := recurringTerms(blocks, nil)
	if len(terms) != pdfTerms {
		t.Fatalf("kept %d terms, want the cap of %d", len(terms), pdfTerms)
	}
	for i := 1; i < len(terms); i++ {
		if terms[i-1].Count < terms[i].Count {
			t.Errorf("terms are not ordered by frequency: %q", joinValues(terms))
		}
	}
}

func TestPDFUnreadReasonNamesTheCause(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"no error at all", nil, "probably a scan"},
		{"binary missing", exec.ErrNotFound, "No PDF text extractor is installed"},
		{"wrapped binary missing", &exec.Error{Name: pdfTextTool, Err: exec.ErrNotFound}, "No PDF text extractor is installed"},
		{"not an exit at all", errors.New("boom"), "could not be run"},
	} {
		if got := pdfUnreadReason(tc.err); !strings.Contains(got, tc.want) {
			t.Errorf("%s: reason = %q, want it to contain %q", tc.name, got, tc.want)
		}
	}
}

// Poppler's exit codes are documented (pdftotext(1) EXIT CODES) and each one
// calls for a different sentence: "not a PDF" and "locked" are different
// problems for whoever uploaded the file.
func TestPDFExitReasonPerDocumentedCode(t *testing.T) {
	for _, tc := range []struct {
		code int
		want string
	}{
		{1, "could not be opened as a PDF"},
		{3, "permissions forbid"},
		{99, "could not read it"},
	} {
		if got := pdfExitReason(tc.code); !strings.Contains(got, tc.want) {
			t.Errorf("exit %d: reason = %q, want it to contain %q", tc.code, got, tc.want)
		}
	}
}

func TestPDFReadCaveatStatesWhatWasLeftOut(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pages int
		exact bool
		want  string
	}{
		{"whole file", 4, true, "Its pages were all read"},
		{"capped with a known count", 830, true, "Only the first 20 of its 830 pages"},
		{"count is only a floor", 40, false, "At most the first 20 pages"},
		{"no count at all", 0, false, "At most the first 20 pages"},
	} {
		if got := pdfReadCaveat(tc.pages, tc.exact); !strings.Contains(got, tc.want) {
			t.Errorf("%s: caveat = %q, want it to contain %q", tc.name, got, tc.want)
		}
	}
}

func TestExtractorFailsCleanlyWithoutABinary(t *testing.T) {
	hideExtractor(t)
	text, capped, err := pdfExtractText(writeFile(t, "x.pdf", "%PDF-1.4\n"))
	if err == nil {
		t.Fatal("a missing extractor reported success")
	}
	if !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("err = %v, want it to wrap exec.ErrNotFound", err)
	}
	if text != "" || capped {
		t.Errorf("text = %q, capped = %v, want neither", text, capped)
	}
}

// The byte cap has to bite before the page cap does on a page dense enough to
// hold a book, and the card has to say that it did.
func TestPDFExtractStopsAtTheByteCap(t *testing.T) {
	requireExtractor(t)
	// One very tall page, one line of text per 8 units. Stacking the lines at
	// distinct coordinates matters: poppler lays text out by position, and lines
	// written on top of each other come back once.
	const lineSpacing, pageHeight = 8, 14000
	var stream strings.Builder
	for line := 0; line*lineSpacing < pageHeight-20; line++ {
		fmt.Fprintf(&stream, "BT /F1 6 Tf 20 %d Td (riga %d lorem ipsum dolor sit amet consectetur "+
			"adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua enim "+
			"ad minim veniam quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea) Tj ET\n",
			pageHeight-10-line*lineSpacing, line)
	}
	body := stream.String()
	path := writeFile(t, "denso.pdf", "%PDF-1.4\n"+
		"1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n"+
		"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n"+
		"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 2000 14000]"+
		"/Resources<</Font<</F1 4 0 R>>>>/Contents 5 0 R>>endobj\n"+
		"4 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj\n"+
		fmt.Sprintf("5 0 obj<</Length %d>>stream\n", len(body))+body+
		"endstream\nendobj\ntrailer<</Root 1 0 R/Size 6>>\n%%EOF\n")

	text, capped, err := pdfExtractText(path)
	if err != nil {
		t.Fatalf("pdfExtractText: %v", err)
	}
	if !capped {
		t.Fatalf("the byte cap did not bite on %d bytes of output", len(text))
	}
	if len(text) != maxTextChars {
		t.Errorf("kept %d bytes, want exactly the cap of %d", len(text), maxTextChars)
	}
	if !strings.Contains(build(t, path, "denso.pdf").Render(), "The extract was cut at 200 KB") {
		t.Error("the card does not say the extract was cut")
	}
}

func TestTopValuesHonoursFloorAndLimit(t *testing.T) {
	counts := map[string]int{"alfa": 5, "beta": 5, "gamma": 3, "delta": 1}
	got := joinValues(topValues(counts, 2, 2))
	if got != "alfa (5), beta (5)" {
		t.Errorf("topValues = %q", got)
	}
	if got := joinValues(topValues(counts, 6, 10)); got != "" {
		t.Errorf("nothing clears the floor, yet %q came back", got)
	}
}
