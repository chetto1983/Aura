package arcadedb

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// exactFactRows holds a fact that names the font and one that does not.
const exactFactRows = `{"result":[` +
	`{"@rid":"#1:5","rid":"#1:5","statement":"The calibre demo embeds the Ubuntu Mono font","subject":"calibre_demo.docx","object":"Ubuntu Mono"},` +
	`{"@rid":"#1:1","rid":"#1:1","statement":"The slides compare monospaced fonts","subject":"SampleShow.pptx","object":"fonts"}]}`

const emptyRows = `{"result":[]}`

// exactMemoryRoutes answers a memory read: fused is the reranked ranking (the floor applied),
// laneFacts and laneTurns what the quoted phrases find, unless their status refuses them,
// hydration facts (the two above when empty) and the blue notebook's turn, lexical the fallback's
// own search. The exact lane is told apart by its phrase.
type exactMemoryRoutes struct {
	fused, laneFacts, laneTurns, lexical, facts string
	laneFactsStatus, laneTurnsStatus            int
}

func exactMemoryClient(t *testing.T, routes exactMemoryRoutes) (*Client, *[]recordedRequest) {
	t.Helper()
	if routes.facts == "" {
		routes.facts = exactFactRows
	}
	return routedClient(t, withOpenGate(func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		params, _ := request.Payload["params"].(map[string]any)
		phrase, _ := params["query"].(string)
		lane := strings.HasPrefix(phrase, `"`)
		facts, turns := strings.Contains(statement, "FROM "+factEdgeType), strings.Contains(statement, "FROM "+conversationTurnType)
		switch {
		case lane && facts && routes.laneFactsStatus != 0:
			return testResponse{Status: routes.laneFactsStatus, Body: `{"error":"phrase refused"}`}
		case lane && facts:
			return testResponse{Body: routes.laneFacts}
		case lane && turns && routes.laneTurnsStatus != 0:
			return testResponse{Status: routes.laneTurnsStatus, Body: `{"error":"phrase refused"}`}
		case lane && turns:
			return testResponse{Body: routes.laneTurns}
		case strings.Contains(statement, "vector.fuse"):
			return testResponse{Body: routes.fused}
		case facts && strings.Contains(statement, "@rid IN"):
			return testResponse{Body: routes.facts}
		case strings.Contains(statement, "FROM "+conversationTurnType) && strings.Contains(statement, "@rid IN"):
			return testResponse{Body: recallAnchorRow}
		case strings.Contains(statement, "turn_seq BETWEEN"):
			return testResponse{Body: recallWindowRows}
		case strings.Contains(statement, "SEARCH_INDEX"):
			return testResponse{Body: routes.lexical}
		default:
			return testResponse{Status: http.StatusBadRequest, Body: `{"detail":"unexpected query"}`}
		}
	}))
}

func exactRecall(t *testing.T, routes exactMemoryRoutes, query string) (RecallResult, *[]recordedRequest) {
	t.Helper()
	client, requests := exactMemoryClient(t, routes)
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})
	result, err := client.RecallMemory(context.Background(), RecallRequest{
		IdentityID: "identity-a", Mode: RecallModeSemantic, Query: query, Limit: 5,
	})
	if err != nil {
		t.Fatalf("RecallMemory: %v", err)
	}
	return result, requests
}

func recalledFacts(result RecallResult) []string {
	var rids []string
	for _, evidence := range result.Evidence {
		if evidence.Fact != nil {
			rids = append(rids, evidence.Fact.RID)
		}
	}
	return rids
}

// The floor dropped the fact that names the font and its file; the exact lane finds it with no
// floor, inside the identity, and it is admitted with no score of its own (spec
// 2026-10-25-document-exactness-and-judge, decision 1, "Memory"). Every term goes in one query per
// record type, over the memory's whole candidate cap.
func TestRecallAdmitsAnExactCarrierTheFloorDropped(t *testing.T) {
	result, requests := exactRecall(t, exactMemoryRoutes{
		fused: emptyRows, laneFacts: `{"result":[{"rid":"#1:5","score":7.2}]}`, laneTurns: emptyRows,
	}, `quale font mostra "Ubuntu Mono" in calibre_demo.docx?`)

	if result.Abstained || !slices.Equal(recalledFacts(result), []string{"#1:5"}) || result.Evidence[0].Score != 0 {
		t.Fatalf("result = %+v, want the carrier alone, with no score", result)
	}
	lane := 0
	for _, request := range *requests {
		params, _ := request.Payload["params"].(map[string]any)
		if phrase, _ := params["query"].(string); !strings.HasPrefix(phrase, `"`) {
			continue
		}
		lane++
		statement, _ := request.Payload["command"].(string)
		if params["query"] != `"Ubuntu Mono" OR "calibre_demo.docx" OR "calibre_demo"` || params["min_lexical_score"] != float64(0) ||
			params["candidates"] != float64(defaultMemoryLimits.HybridCandidates) || strings.Contains(statement, "vector.") {
			t.Fatalf("lane read = %s %v, want every phrase OR-ed, no floor, the whole cap and no vector", statement, params)
		}
		if strings.Contains(statement, conversationTurnType) && params["identity_id"] != "identity-a" {
			t.Fatalf("turn lane params = %v, want the identity", params)
		}
	}
	if lane != 2 {
		t.Fatalf("lane reads = %d, want one per record type whatever the number of terms", lane)
	}
}

// A turn carries a quoted phrase as a fact does: the conversation that named it is admitted.
func TestRecallAdmitsAConversationCarryingTheQuotedPhrase(t *testing.T) {
	result, _ := exactRecall(t, exactMemoryRoutes{
		fused: emptyRows, laneFacts: emptyRows, laneTurns: `{"result":[{"rid":"#20:1","score":3.1}]}`,
	}, `dove abbiamo parlato del "blue notebook"?`)

	if result.Abstained || len(result.Evidence) != 1 || result.Evidence[0].Kind != RecallEvidenceConversation ||
		result.Evidence[0].Conversation.AnchorSeq != 7 {
		t.Fatalf("result = %+v, want the conversation that named it", result)
	}
}

// A carrier the fused ranking already held moves ahead of what outscored it.
func TestRecallRanksTheExactCarrierFirst(t *testing.T) {
	result, _ := exactRecall(t, exactMemoryRoutes{
		fused:     `{"result":[{"rid":"#1:1","score":0.6},{"rid":"#1:5","score":0.4}]}`,
		laneFacts: `{"result":[{"rid":"#1:5","score":7.2}]}`, laneTurns: emptyRows,
	}, `quale font mostra "Ubuntu Mono"?`)

	if got := recalledFacts(result); !slices.Equal(got, []string{"#1:5", "#1:1"}) || result.Evidence[0].Score != 0.4 {
		t.Fatalf("facts = %v (score %v), want the carrier first at its own score", got, result.Evidence[0].Score)
	}
}

// Carrying one exact term of two is no bypass: a record only the lane found, and that misses a
// term, passed no floor. Two phrases, because a missing identifier would be dropped by the
// identifier filter anyway.
func TestRecallDropsALaneRecordMissingATerm(t *testing.T) {
	result, _ := exactRecall(t, exactMemoryRoutes{
		fused: emptyRows, laneFacts: `{"result":[{"rid":"#1:5","score":7.2}]}`, laneTurns: emptyRows,
	}, `"Ubuntu Mono" a "Torino"`)

	if !result.Abstained || len(result.Evidence) != 0 || result.Reason != reasonNoQualifiedCandidates {
		t.Fatalf("result = %+v, want an abstention", result)
	}
}

// Review M4: a fact the lane found with the phrase's words in another order carries nothing, so it
// hides no turn the legs admitted, though it says the same thing.
func TestALaneFactCarryingNothingHidesNoTurn(t *testing.T) {
	result, _ := exactRecall(t, exactMemoryRoutes{
		fused: `{"result":[{"rid":"#20:1","score":0.5}]}`, laneFacts: `{"result":[{"rid":"#1:9","score":4.0}]}`, laneTurns: emptyRows,
		facts: `{"result":[{"@rid":"#1:9","rid":"#1:9","statement":"We discussed the blue notebook in Turin.","subject":"notebook","object":"Turin"}]}`,
	}, `dove abbiamo parlato del "notebook blue"?`)

	if result.Abstained || len(result.Evidence) != 1 || result.Evidence[0].Kind != RecallEvidenceConversation {
		t.Fatalf("result = %+v, want the conversation the legs admitted", result)
	}
}

// A quoted phrase never filters: the fact about Turin still answers a question that quotes
// "Torino", which is what memory's dense leg exists for.
func TestAQuotedPhraseNeverFiltersRecall(t *testing.T) {
	result, _ := exactRecall(t, exactMemoryRoutes{
		fused: `{"result":[{"rid":"#1:1","score":0.6}]}`, laneFacts: emptyRows, laneTurns: emptyRows,
	}, `quali font usa la demo di "Torino"?`)

	if got := recalledFacts(result); !slices.Equal(got, []string{"#1:1"}) {
		t.Fatalf("facts = %v, want the fused answer kept", got)
	}
}

// A lane that fails costs the bypass, not the answer, and the reason says so.
func TestRecallNamesAFailedExactLane(t *testing.T) {
	result, _ := exactRecall(t, exactMemoryRoutes{
		fused:           `{"result":[{"rid":"#1:1","score":0.6}]}`,
		laneFactsStatus: http.StatusInternalServerError, laneTurnsStatus: http.StatusInternalServerError,
	}, `quale font mostra "Ubuntu Mono"?`)

	if got := recalledFacts(result); !slices.Equal(got, []string{"#1:1"}) || result.Reason != reasonExactTermsFailed {
		t.Fatalf("facts = %v reason %q, want the fused answer and %q", got, result.Reason, reasonExactTermsFailed)
	}
}

// Review M5: an abstention keeps the lane's failure beside its own reason.
func TestRecallNamesAFailedLaneAndTheAbstention(t *testing.T) {
	result, _ := exactRecall(t, exactMemoryRoutes{
		fused:           emptyRows,
		laneFactsStatus: http.StatusInternalServerError, laneTurnsStatus: http.StatusInternalServerError,
	}, `quale font mostra "Ubuntu Mono"?`)

	if want := reasonExactTermsFailed + "+" + reasonNoQualifiedCandidates; !result.Abstained || result.Reason != want {
		t.Fatalf("result = %+v, want an abstention reasoned %q", result, want)
	}
}

// Review M3: a fact lane that fails costs the fact carriers only; the turn lane still answers.
func TestRecallKeepsTheTurnLaneWhenTheFactLaneFails(t *testing.T) {
	result, _ := exactRecall(t, exactMemoryRoutes{
		fused: emptyRows, laneFactsStatus: http.StatusInternalServerError, laneTurns: `{"result":[{"rid":"#20:1","score":3.1}]}`,
	}, `dove abbiamo parlato del "blue notebook"?`)

	if result.Abstained || len(result.Evidence) != 1 || result.Evidence[0].Kind != RecallEvidenceConversation ||
		result.Reason != reasonExactTermsFailed {
		t.Fatalf("result = %+v, want the conversation and the failed fact lane named", result)
	}
}

func factRIDs(facts []FactHit) []string {
	rids := make([]string, 0, len(facts))
	for _, fact := range facts {
		rids = append(rids, fact.RID)
	}
	return rids
}

// The fact search runs the same lane: the floor dropped the carrier, the lane admits it.
func TestFactSearchAdmitsAnExactCarrierTheFloorDropped(t *testing.T) {
	client, _ := exactMemoryClient(t, exactMemoryRoutes{fused: emptyRows, laneFacts: `{"result":[{"rid":"#1:5","score":7.2}]}`})
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})

	result, err := client.SearchFactsHybrid(t.Context(), `quale font mostra "Ubuntu Mono"?`, 5, time.Time{})

	if err != nil || result.Abstained || result.Reason != "" || !slices.Equal(factRIDs(result.Facts), []string{"#1:5"}) {
		t.Fatalf("result = %+v err %v, want the carrier", result, err)
	}
}

// Review M5: an empty hybrid fact search whose lane failed names both the failure and the abstention.
func TestFactSearchNamesAFailedLaneAndTheAbstention(t *testing.T) {
	client, _ := exactMemoryClient(t, exactMemoryRoutes{fused: emptyRows, laneFactsStatus: http.StatusInternalServerError})
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})

	result, err := client.SearchFactsHybrid(t.Context(), `quale font mostra "Ubuntu Mono"?`, 5, time.Time{})

	if want := reasonExactTermsFailed + "+" + reasonNoQualifiedCandidates; err != nil || !result.Abstained || result.Reason != want {
		t.Fatalf("result = %+v err %v, want an abstention reasoned %q", result, err, want)
	}
}

// On the lexical fallback the carrier it already holds moves first, and nothing is read twice.
func TestFactSearchRanksTheExactCarrierFirstOnTheLexicalFallback(t *testing.T) {
	// The lexical search ranks the slides first.
	client, requests := exactMemoryClient(t, exactMemoryRoutes{
		lexical: `{"result":[` +
			`{"rid":"#1:1","statement":"The slides compare monospaced fonts","subject":"SampleShow.pptx"},` +
			`{"rid":"#1:5","statement":"The calibre demo embeds the Ubuntu Mono font","subject":"calibre_demo.docx"}]}`,
		laneFacts: `{"result":[{"rid":"#1:5","score":7.2}]}`,
	})

	result, err := client.SearchFactsHybrid(t.Context(), `font "Ubuntu Mono"`, 5, time.Time{})

	if err != nil || result.RetrievalPath != retrievalPathLexical || !slices.Equal(factRIDs(result.Facts), []string{"#1:5", "#1:1"}) {
		t.Fatalf("result = %+v err %v, want the carrier first on the lexical path", result, err)
	}
	for _, request := range *requests {
		if statement, _ := request.Payload["command"].(string); strings.Contains(statement, "@rid IN") {
			t.Fatalf("hydrated %s for a carrier the search already held", statement)
		}
	}
}
