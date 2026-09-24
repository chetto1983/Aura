package arcadedb

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
)

// The lists are Lucene's own, byte for byte, from the jar ArcadeDB 26.9.1 runs: a list edited
// here would silently disagree with the one the measurement in spec §8 used.
func TestStopwordListsAreLucenesOwn(t *testing.T) {
	for _, list := range []struct {
		name, text, sha string
		words           int
	}{
		{"italian", italianStopwords, "23e3d7c9d977756e5dd8ae9a120b51a468f904e79ea12bd084dcccfbba70ac5f", 279},
		{"english", englishStopwords, "c8d811c265112dc3e6c2f12bbc59af49f34c874be16436ca9ac7c56dd62a24f8", 174},
	} {
		sum := sha256.Sum256([]byte(list.text))
		if hex.EncodeToString(sum[:]) != list.sha {
			t.Fatalf("%s list is not Lucene's: sha256 %x", list.name, sum)
		}
		if n := len(stopwordSet(list.text)); n != list.words {
			t.Fatalf("%s list reads %d words, want %d", list.name, n, list.words)
		}
	}
	if len(documentStopwords) != 449 {
		t.Fatalf("union = %d words, want 449", len(documentStopwords))
	}
}

func TestLexicalQueryDropsStopwordsAndEarnsItsFloor(t *testing.T) {
	for _, test := range []struct {
		query, terms string
		floor        float64
	}{
		{"orari dei traghetti per la Sardegna", "orari traghetti Sardegna", 2},
		{"chi ha vinto il mondiale 1982", "vinto mondiale 1982", 2},
		{"approvals and durable grants", "approvals durable grants", 2},
		{"PID_Temp (multi-zone)?", "PID_Temp (multi-zone)?", 2},
		{"videoplayback", "videoplayback", 0},
		{"the PidTemp?", "PidTemp?", 0},
		{"chi è il?", "", 0},
		{"what is it", "", 0},
		{"?", "", 0},
	} {
		terms, floor := lexicalQuery(test.query)
		if terms != test.terms || floor != test.floor {
			t.Fatalf("lexicalQuery(%q) = %q, %v; want %q, %v", test.query, terms, floor, test.terms, test.floor)
		}
	}
}

// Review Focus 1: nothing left to search for is an empty answer, never a statement ArcadeDB
// would read as "match everything" or refuse to parse.
func TestLexicalReadsSendNothingForAStopwordOnlyQuery(t *testing.T) {
	index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
		return testResponse{Status: 500, Body: `{"detail":"no statement was expected"}`}
	})
	filter := CandidateFilter{IdentityID: documentTestIdentity, Limit: 3}
	passages, err := index.LexicalCandidates(t.Context(), filter, "chi è il?")
	if err != nil || passages != nil {
		t.Fatalf("passages = %v err = %v", passages, err)
	}
	cards, err := index.LexicalDocumentCards(t.Context(), filter, "what is it")
	if err != nil || cards != nil {
		t.Fatalf("cards = %v err = %v", cards, err)
	}
	if len(*requests) != 0 {
		t.Fatalf("requests = %d, want none", len(*requests))
	}
}

func TestLexicalCandidatesRankByTheFullTextScoreAlone(t *testing.T) {
	var index *DocumentIndex
	index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
		other := candidateFixture(index, "doc_b:3", "doc_b", 3, "lexical_score", 2.2)
		other["raw_sha256"] = strings.Repeat("c", 64) // another file, not a copy of doc_a
		return testResponse{Body: resultBody([]any{
			candidateFixture(index, "doc_a:0", "doc_a", 0, "lexical_score", 7.5), other,
		})}
	})
	passages, err := index.LexicalCandidates(t.Context(), CandidateFilter{
		IdentityID: documentTestIdentity, Limit: 3, DocumentIDs: []string{"doc_a", "doc_b"},
	}, "orari dei traghetti per la Sardegna")
	if err != nil {
		t.Fatal(err)
	}
	if len(passages) != 2 || passages[0].Leg != RetrievalLegLexical || passages[0].FusedScore != nil ||
		passages[0].LexicalScore == nil || *passages[0].Score() != 7.5 {
		t.Fatalf("passages = %+v", passages)
	}
	statement, _ := (*requests)[0].Payload["command"].(string)
	params, _ := (*requests)[0].Payload["params"].(map[string]any)
	for _, want := range []string{
		"SEARCH_INDEX('Passage[text]', :query) = true", "search_document_id IN :document_ids",
		"$score >= :min_lexical_score", "ORDER BY lexical_score DESC",
	} {
		if !strings.Contains(statement, want) {
			t.Fatalf("statement lacks %q:\n%s", want, statement)
		}
	}
	if strings.Contains(statement, "vector.") || params["query"] != "orari traghetti Sardegna" ||
		params["min_lexical_score"] != float64(2) {
		t.Fatalf("statement or params wrong:\n%s\n%v", statement, params)
	}
}

// One OR query's $score depends on predicate order (audit F8), so each index is asked apart
// and a document keeps the higher of its two scores.
func TestLexicalDocumentCardsMergeTheTwoIndexesOnTheHigherScore(t *testing.T) {
	index, requests := testDocumentIndex(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		card := func(id string, score float64) map[string]any {
			row := documentCardFixture(id, id+".pdf", "card of "+id)
			row["card_score"] = score
			return row
		}
		if strings.Contains(statement, "IndexedDocument[card]") {
			return testResponse{Body: resultBody([]any{card("doc_a", 3.1), card("doc_b", 2.5)})}
		}
		return testResponse{Body: resultBody([]any{card("doc_b", 4.0), card("doc_c", 2.2)})}
	})
	cards, err := index.LexicalDocumentCards(t.Context(),
		CandidateFilter{IdentityID: documentTestIdentity, Limit: 3}, "fatturato clienti torino")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, card := range cards {
		got = append(got, card.SearchDocumentID)
	}
	if strings.Join(got, ",") != "doc_b,doc_a,doc_c" || cards[0].Score != 4.0 {
		t.Fatalf("cards = %v (first score %v), want doc_b 4.0 first, then doc_a, doc_c", got, cards[0].Score)
	}
	if len(*requests) != 2 {
		t.Fatalf("requests = %d, want one per index", len(*requests))
	}
	for _, request := range *requests {
		statement, _ := request.Payload["command"].(string)
		if strings.Count(statement, "SEARCH_INDEX(") != 1 || strings.Contains(statement, "vector.") {
			t.Fatalf("a card statement asks more than one index:\n%s", statement)
		}
	}
}

func TestLexicalReadsTreatAMissingTypeAsAnEmptyLibrary(t *testing.T) {
	filter := CandidateFilter{IdentityID: documentTestIdentity, Limit: 3}
	passages, err := func() ([]PassageCandidate, error) {
		index, _ := testDocumentIndex(t, func(recordedRequest) testResponse {
			return testResponse{Status: 500, Body: missingPassageBody}
		})
		return index.LexicalCandidates(t.Context(), filter, "fatturato clienti")
	}()
	if err != nil || len(passages) != 0 {
		t.Fatalf("passages = %v err = %v", passages, err)
	}
	cards, err := missingTypeIndex(t).LexicalDocumentCards(t.Context(), filter, "fatturato clienti")
	if err != nil || len(cards) != 0 {
		t.Fatalf("cards = %v err = %v", cards, err)
	}
}

// Spec §8 groups by raw_sha256, as the fused statement's groupSize 1 does inside the engine:
// rankDocuments reads one passage per file, and a passage two files share verbatim would
// otherwise merge them under one title. The leg over-fetches so grouping does not starve the
// pool (final review #1, #6).
func TestLexicalCandidatesKeepEachFilesBestPassage(t *testing.T) {
	var index *DocumentIndex
	index, requests := testDocumentIndex(t, func(recordedRequest) testResponse {
		row := func(passage, doc string, raw string, score float64) map[string]any {
			fixture := candidateFixture(index, passage, doc, 0, "lexical_score", score)
			fixture["raw_sha256"] = strings.Repeat(raw, 64)
			return fixture
		}
		return testResponse{Body: resultBody([]any{
			row("a:0", "a", "a", 7.5), row("a:3", "a", "a", 5.1), row("b:1", "b", "b", 4.2),
		})}
	})
	passages, err := index.LexicalCandidates(t.Context(),
		CandidateFilter{IdentityID: documentTestIdentity, Limit: 2}, "safety warning")
	if err != nil {
		t.Fatal(err)
	}
	if len(passages) != 2 || passages[0].PassageID != "a:0" || passages[1].PassageID != "b:1" {
		t.Fatalf("passages = %+v, want each file's best passage once", passages)
	}
	// The fused leg's over-fetch, min(max(limit*4, 20), cap), with this index's cap.
	fetch := "LIMIT " + strconv.Itoa(min(20, index.config.MaxRetrievalCandidates))
	if statement, _ := (*requests)[0].Payload["command"].(string); !strings.HasSuffix(statement, fetch) {
		t.Fatalf("the lexical leg does not over-fetch before grouping:\n%s", statement)
	}
}
