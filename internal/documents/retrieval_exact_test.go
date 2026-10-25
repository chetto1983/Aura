package documents

import (
	"bytes"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/documents/filecard"
)

// fontQuery is the lab VM's s04 with the name quoted (spec 2026-10-25-document-exactness-and-judge,
// acceptance 1).
const fontQuery = `quale font mostra "Ubuntu Mono"?`

func exactPassage(documentID string, ordinal int64, score float64, text string) arcadedb.PassageCandidate {
	passage := passageWithText(documentID, 0, text)
	passage.PassageID = documentID + ":" + strconv.FormatInt(ordinal, 10)
	passage.Ordinal, passage.Leg = ordinal, arcadedb.RetrievalLegExact
	passage.FusedScore, passage.LexicalScore = nil, new(score)
	return passage
}

func exactCard(documentID, text string, score float64) arcadedb.DocumentCard {
	return arcadedb.DocumentCard{
		SearchDocumentID: documentID, SourceKind: "s3", SourceKey: documentID, FileName: documentID,
		RawSHA256: documentID + "-raw", Card: renderedCard(documentID, text), Score: score, PassageCount: 2,
	}
}

func renderedCard(fileName string, text ...string) string {
	return filecard.Card{FileName: fileName, Kind: filecard.KindDocument, Text: text}.Render()
}

func exactLaneFor(query string, hits ...exactHits) exactLane {
	return exactLane{terms: arcadedb.QueryExactTerms(query), hits: hits}
}

// The lab VM's case (spec 2026-10-25-document-exactness-and-judge, "The problem, measured"): the
// font is named once in a 5,037-character passage the reranked cosine scores 0.199, under the 0.39
// floor. The exact lane found it, so it ranks first, ahead of the slides that only resemble the
// question, and keeps the score its legs gave it: none.
func TestAnExactCarrierTheFloorDroppedRanksFirst(t *testing.T) {
	fused := []arcadedb.PassageCandidate{
		passageWithText("SampleShow.pptx", 0.62, "Which font family does the document embed?"),
		passageWithText("note.md", 0.45, "monospaced fonts in general"),
	}
	lane := exactLaneFor(fontQuery, exactHits{passages: []arcadedb.PassageCandidate{
		exactPassage("calibre_demo.docx", 7, 6.1, "here is some text in the Ubuntu Mono typeface"),
	}})

	documents := rankDocuments(nil, fused, lane, nil, 8, 3)

	if got, want := rankedIDs(documents), []string{"calibre_demo.docx", "SampleShow.pptx", "note.md"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	carrier := documents[0]
	if carrier.Score != 0 || carrier.RequiresOpen || len(carrier.Evidence) != 1 ||
		carrier.Evidence[0].Leg != string(arcadedb.RetrievalLegExact) || *carrier.Evidence[0].Score != 6.1 {
		t.Fatalf("carrier = %+v, want exact evidence at its BM25 and no score from a leg that never scored it", carrier)
	}
}

// Carrying some of the question's exact terms is no bypass: only a document that carries every one
// ranks first, and the others keep the score order.
func TestOnlyACarrierOfEveryExactTermRanksFirst(t *testing.T) {
	fused := []arcadedb.PassageCandidate{
		passageWithText("a.md", 0.6, "turni di notte"),
		passageWithText("b.md", 0.5, "il font Ubuntu Mono"),
		passageWithText("c.md", 0.4, "RC-2026-040 stampato in Ubuntu Mono"),
	}

	got := rankedIDs(rankDocuments(nil, fused, exactLaneFor(`il reclamo RC-2026-040 cita "Ubuntu Mono"?`), nil, 8, 3))

	if want := []string{"c.md", "a.md", "b.md"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// The trailer filecard writes into every card names Aura, so it carries nothing: measured on the
// lab VM, "Aura" matched all 8 documents through it.
func TestTheCardTrailerCarriesNoExactTerm(t *testing.T) {
	cards := []RetrievalCard{{
		DocumentID: "x.pdf", Title: "x.pdf", Card: renderedCard("x.pdf", "Quarterly figures."),
		OriginalSHA256: "x-raw", Rank: 0.5, PassageCount: 3,
	}}
	fused := []arcadedb.PassageCandidate{passageWithText("esempio.md", 0.3, "Questo è un file di esempio creato da Aura.")}

	got := rankedIDs(rankDocuments(cards, fused, exactLaneFor(`"Aura"`), nil, 8, 3))

	if want := []string{"esempio.md", "x.pdf"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// A document whose exact terms sit in more passages than the response carries keeps the passages
// that show them, so it stays a carrier: the cut keeps exact evidence first.
func TestThePassageCutKeepsTheExactEvidence(t *testing.T) {
	fused := []arcadedb.PassageCandidate{
		passageWithText("other.md", 0.7, "sensori in generale"),
		passageWithText("manual.pdf", 0.5, "introduzione al manuale"),
	}
	lane := exactLaneFor("dove compaiono PT-100, PT-200 e PT-300?",
		exactHits{passages: []arcadedb.PassageCandidate{exactPassage("manual.pdf", 4, 3, "la sonda PT-100")}},
		exactHits{passages: []arcadedb.PassageCandidate{exactPassage("manual.pdf", 6, 3, "la sonda PT-200")}},
		exactHits{passages: []arcadedb.PassageCandidate{exactPassage("manual.pdf", 9, 3, "la sonda PT-300")}},
	)

	documents := rankDocuments(nil, fused, lane, nil, 8, 3)

	if got, want := rankedIDs(documents), []string{"manual.pdf", "other.md"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	ordinals := make([]int64, 0, 3)
	for _, passage := range documents[0].Passages {
		ordinals = append(ordinals, passage.Ordinal)
	}
	if want := []int64{4, 6, 9}; !slices.Equal(ordinals, want) {
		t.Fatalf("passages = %v, want the three that carry the terms", ordinals)
	}
}

// The place reserved for a file with no passage sits behind the carriers, never between them.
func TestTheReservedPlaceSitsBehindTheCarriers(t *testing.T) {
	cards := []RetrievalCard{{DocumentID: "sheet.xlsx", Title: "sheet.xlsx", OriginalSHA256: "sheet-raw", Rank: 0.2, PassageCount: 0}}
	fused := []arcadedb.PassageCandidate{
		passageWithText("a.md", 0.6, "turni"), passageWithText("b.md", 0.5, "ferie"),
		passageWithText("x.md", 0.3, "il reclamo RC-2026-040"), passageWithText("y.md", 0.25, "RC-2026-040 di nuovo"),
	}

	got := rankedIDs(rankDocuments(cards, fused, exactLaneFor("reclamo RC-2026-040"), nil, 8, 3))

	if want := []string{"x.md", "y.md", "a.md", "sheet.xlsx", "b.md"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func fontIndex() *fakePassageIndex {
	return &fakePassageIndex{
		fused:   []arcadedb.PassageCandidate{passageWithText("SampleShow.pptx", 0.62, "Which font family?")},
		lexical: []arcadedb.PassageCandidate{lexicalCandidate("SampleShow.pptx", "SampleShow.pptx-raw", 4.2)},
		exact: map[string]arcadedb.ExactTermHits{"ubuntu mono": {Passages: []arcadedb.PassageCandidate{
			exactPassage("calibre_demo.docx", 7, 6.1, "here is some text in the Ubuntu Mono typeface"),
		}}},
	}
}

// The lane runs beside the fused leg and beside the lexical one, inside their scope, and its
// carrier reaches the agent first in both (spec 2026-10-25-document-exactness-and-judge, decision 1).
func TestRetrieveRanksTheExactCarrierFirstInBothModes(t *testing.T) {
	for _, test := range []struct {
		name     string
		embedder QueryEmbedder
		status   RetrievalStatus
	}{
		{"dense", &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}}, RetrievalComplete},
		{"lexical", nil, RetrievalLexicalOnly},
	} {
		t.Run(test.name, func(t *testing.T) {
			index, control := fontIndex(), &fakeRetrievalControl{scope: []string{"calibre_demo.docx", "SampleShow.pptx"}}
			response, err := (&HostRetriever{ControlPlane: control, PassageIndex: index, Embedder: test.embedder}).
				Retrieve(t.Context(), RetrievalRequest{
					IdentityID: retrievalIdentity, Query: fontQuery, DocumentIDs: []string{"calibre_demo.docx", "SampleShow.pptx"},
				})
			if err != nil {
				t.Fatal(err)
			}
			if got, want := rankedIDs(response.Documents), []string{"calibre_demo.docx", "SampleShow.pptx"}; !slices.Equal(got, want) ||
				response.Status != test.status || response.Abstained {
				t.Fatalf("response = %v %s abstained %v, want %v", got, response.Status, response.Abstained, want)
			}
			if leg := response.Documents[0].Passages[0].Evidence[0].Leg; leg != string(arcadedb.RetrievalLegExact) {
				t.Fatalf("carrier's passage evidence = %s, want exact", leg)
			}
			if len(index.exactTerms) != 1 || index.exactTerms[0].Raw != "Ubuntu Mono" ||
				!reflect.DeepEqual(index.exactFilter.DocumentIDs, []string{"calibre_demo.docx", "SampleShow.pptx"}) {
				t.Fatalf("lane asked %+v in %+v, want the quoted name inside the resolved scope", index.exactTerms, index.exactFilter)
			}
			if !slices.Contains(control.namesRequest, "calibre_demo.docx") {
				t.Fatalf("names asked for %v, want the carrier no card ranked", control.namesRequest)
			}
		})
	}
}

// No leg admitted anything, the exact lane did: the corpus answered, so the response does not
// abstain.
func TestRetrieveAnswersWithAnExactCarrierAlone(t *testing.T) {
	index := fontIndex()
	index.fused = nil
	response, err := (&HostRetriever{
		ControlPlane: &fakeRetrievalControl{}, PassageIndex: index,
		Embedder: &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}},
	}).Retrieve(t.Context(), RetrievalRequest{IdentityID: retrievalIdentity, Query: fontQuery})
	if err != nil {
		t.Fatal(err)
	}
	if response.Abstained || len(response.Documents) != 1 || response.Documents[0].DocumentID != "calibre_demo.docx" {
		t.Fatalf("response = %+v, want the carrier and no abstention", response)
	}
}

// A document the lane found for one exact term but not for another is not admitted: no leg passed
// it, and carrying some terms is no bypass.
func TestRetrieveAdmitsOnlyTheDocumentsCarryingEveryExactTerm(t *testing.T) {
	index := fontIndex()
	index.exact = map[string]arcadedb.ExactTermHits{
		"ubuntu mono": {Passages: []arcadedb.PassageCandidate{
			exactPassage("calibre_demo.docx", 7, 6.1, "the Ubuntu Mono typeface"),
			exactPassage("other.docx", 1, 5.0, "Ubuntu Mono again"),
		}},
		"calibre_demo docx": {Cards: []arcadedb.DocumentCard{exactCard("calibre_demo.docx", "calibre demo", 3.3)}},
		"calibre_demo":      {Cards: []arcadedb.DocumentCard{exactCard("calibre_demo.docx", "calibre demo", 2.9)}},
	}
	response, err := (&HostRetriever{
		ControlPlane: &fakeRetrievalControl{}, PassageIndex: index,
		Embedder: &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}},
	}).Retrieve(t.Context(), RetrievalRequest{IdentityID: retrievalIdentity, Query: `"Ubuntu Mono" in calibre_demo.docx`})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rankedIDs(response.Documents), []string{"calibre_demo.docx", "SampleShow.pptx"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v: other.docx carries one term of three", got, want)
	}
	if len(index.exactTerms) != 3 {
		t.Fatalf("terms asked = %+v, want the phrase, the file name and its identifier", index.exactTerms)
	}
}

// A lane that fails costs the bypass, not the answer: the legs' ranking comes back as it would
// without exact terms, and the cause is logged.
func TestAnExactLaneFailureKeepsTheAnswer(t *testing.T) {
	var buf bytes.Buffer
	restore := swapDefaultLogger(t, &buf)
	defer restore()
	index := fontIndex()
	index.exactErr = errors.New("phrase query refused")
	response, err := (&HostRetriever{
		ControlPlane: &fakeRetrievalControl{}, PassageIndex: index,
		Embedder: &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}},
	}).Retrieve(t.Context(), RetrievalRequest{IdentityID: retrievalIdentity, Query: fontQuery})
	if err != nil {
		t.Fatal(err)
	}
	if got := rankedIDs(response.Documents); !slices.Equal(got, []string{"SampleShow.pptx"}) ||
		response.Status != RetrievalComplete || response.DegradationReason != "" {
		t.Fatalf("response = %v %s %q, want the fused answer, complete", got, response.Status, response.DegradationReason)
	}
	if !strings.Contains(buf.String(), exactTermsUnavailable) || !strings.Contains(buf.String(), "phrase query refused") {
		t.Fatalf("log = %q, want the lane's failure and its cause", buf.String())
	}
}

// A question with no exact term costs no exact query.
func TestAQuestionWithoutExactTermsAsksNoExactLane(t *testing.T) {
	index := fontIndex()
	if _, err := (&HostRetriever{
		ControlPlane: &fakeRetrievalControl{}, PassageIndex: index,
		Embedder: &fakeRetrievalEmbedder{vector: []float64{0.1, 0.2}},
	}).Retrieve(t.Context(), RetrievalRequest{IdentityID: retrievalIdentity, Query: "quale font mostra Ubuntu Mono?"}); err != nil {
		t.Fatal(err)
	}
	if len(index.exactTerms) != 0 {
		t.Fatalf("lane asked %+v for a question with no exact term", index.exactTerms)
	}
}
