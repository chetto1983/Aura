package arcadedb

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

const recallFactRow = `{"result":[{"@rid":"#10:1","statement":"Davide keeps the blue notebook.","predicate":"keeps","subject":"Davide","object":"blue notebook","valid_from":"2026-01-01T00:00:00Z","fact_key":"fact-blue","sources":[{"run_id":"run-1","writer_role":"parent"}]}]}`

const recallAnchorRow = `{"result":[{"@rid":"#20:1","identity_id":"identity-a","conversation_id":"conversation-1","turn_seq":7,"role":"user","content":"We discussed the blue notebook in Turin.","content_hash":"hash-7","occurred_at":"2026-08-31T12:00:00Z","source_ref":"postgres://conversation/conversation-1/turn/7"}]}`

const recallWindowRows = `{"result":[{"identity_id":"identity-a","conversation_id":"conversation-1","turn_seq":6,"role":"assistant","content":"I found the earlier itinerary.","content_hash":"hash-6","occurred_at":"2026-08-31T11:59:00Z","source_ref":"postgres://conversation/conversation-1/turn/6"},{"identity_id":"identity-a","conversation_id":"conversation-1","turn_seq":7,"role":"user","content":"We discussed the blue notebook in Turin.","content_hash":"hash-7","occurred_at":"2026-08-31T12:00:00Z","source_ref":"postgres://conversation/conversation-1/turn/7"}]}`

func TestMemoryRecallVectorFuse(t *testing.T) {
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		switch {
		case strings.Contains(statement, "vector.fuse"):
			return testResponse{Body: `{"result":[{"rid":"#20:1","score":0.04},{"rid":"#10:1","score":0.03}]}`}
		case strings.Contains(statement, "FROM FACT") && strings.Contains(statement, "@rid IN"):
			return testResponse{Body: recallFactRow}
		case strings.Contains(statement, "FROM ConversationTurn") && strings.Contains(statement, "@rid IN"):
			return testResponse{Body: recallAnchorRow}
		case strings.Contains(statement, "turn_seq"):
			return testResponse{Body: recallWindowRows}
		default:
			return testResponse{Status: http.StatusBadRequest, Body: `{"detail":"unexpected query"}`}
		}
	})
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})

	result, err := client.RecallMemory(context.Background(), RecallRequest{
		IdentityID: "identity-a", Mode: RecallModeSemantic,
		Query: "where was the blue notebook discussed", Limit: 5,
	})
	if err != nil {
		t.Fatalf("RecallMemory: %v", err)
	}
	if len(result.Evidence) != 2 || result.Evidence[0].Kind != RecallEvidenceConversation ||
		result.Evidence[1].Kind != RecallEvidenceFact {
		t.Fatalf("fused evidence = %+v", result.Evidence)
	}
	if result.Retrieval.EffectivePath != "mixed" || result.Retrieval.Path != retrievalPathHybrid {
		t.Fatalf("retrieval = %+v", result.Retrieval)
	}
	// Two rankings, one per record type, and NEITHER may name the other's type. The
	// nested triple fusion this used to pin was the defect: it fused facts with turns
	// and reranked the result by one cosine, which cost half the fact recall on four
	// separate embedding models (see memory_recall_quota.go). A statement that mentions
	// both types is that architecture returning.
	var fusions []string
	for _, request := range *requests {
		statement, _ := request.Payload["command"].(string)
		if strings.Contains(statement, "vector.fuse") {
			fusions = append(fusions, statement)
		}
	}
	if len(fusions) != 2 {
		t.Fatalf("recall ran %d fused rankings, want one per record type", len(fusions))
	}
	for _, fusion := range fusions {
		facts := strings.Contains(fusion, factEdgeType+"[")
		turns := strings.Contains(fusion, conversationTurnType+"[")
		if facts == turns {
			t.Fatalf("a ranking mixes record types (or names neither): %s", fusion)
		}
		if strings.Count(fusion, "`vector.fuse`") != 1 || !strings.Contains(fusion, "fusion: 'RRF'") {
			t.Fatalf("ranking is not one native rank-only fusion of its dense and lexical legs: %s", fusion)
		}
		if !strings.Contains(fusion, "ORDER BY score DESC, rid ASC") {
			t.Fatalf("fusion ties are not deterministic: %s", fusion)
		}
	}
}

func TestMemoryRecallBackendPath(t *testing.T) {
	tests := []struct {
		name     string
		request  RecallRequest
		embedder Embedder
		wantPath string
	}{
		{name: "query", request: RecallRequest{IdentityID: "identity-a", Query: "blue notebook"}, embedder: &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}, wantPath: retrievalPathHybrid},
		{name: "entity", request: RecallRequest{IdentityID: "identity-a", Entity: "Davide"}, wantPath: "graph"},
		{name: "embedding unavailable", request: RecallRequest{IdentityID: "identity-a", Query: "blue notebook"}, embedder: &stubEmbedder{err: context.DeadlineExceeded}, wantPath: retrievalPathLexical},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := routedClient(t, func(request recordedRequest) testResponse {
				statement, _ := request.Payload["command"].(string)
				switch {
				case strings.Contains(statement, "vector.fuse"):
					return testResponse{Body: `{"result":[{"rid":"#10:1","score":0.03}]}`}
				case strings.Contains(statement, "FROM FACT") && strings.Contains(statement, "@rid IN"):
					return testResponse{Body: recallFactRow}
				case strings.Contains(statement, "outV().name"):
					return testResponse{Body: recallFactRow}
				default:
					return testResponse{Body: `{"result":[]}`}
				}
			})
			client.WithEmbedder(tt.embedder)
			result, err := client.RecallMemory(context.Background(), tt.request)
			if err != nil {
				t.Fatalf("RecallMemory: %v", err)
			}
			if result.Retrieval.Path != tt.wantPath {
				t.Fatalf("path = %q, want %q", result.Retrieval.Path, tt.wantPath)
			}
		})
	}
}

func TestMemoryRecallAbstains(t *testing.T) {
	client, requests := routedClient(t, func(recordedRequest) testResponse {
		return testResponse{Body: `{"result":[]}`}
	})
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})
	result, err := client.RecallMemory(context.Background(), RecallRequest{
		IdentityID: "identity-a", Query: "unrelated", Limit: 5,
	})
	if err != nil {
		t.Fatalf("RecallMemory: %v", err)
	}
	if !result.Abstained || result.Reason != reasonNoQualifiedCandidates || len(result.Evidence) != 0 {
		t.Fatalf("result = %+v", result)
	}
	// Two, not one: facts and conversations are ranked separately now, and an
	// abstention must still cost exactly the two rankings and nothing after them --
	// no hydration, no conversation window for a candidate set that is empty.
	if len(*requests) != 2 {
		t.Fatalf("abstention made %d queries, want one ranked retrieval per record type", len(*requests))
	}
	if result.Retrieval.BackendLatency < 0 || result.Retrieval.BackendLatency > time.Minute {
		t.Fatalf("backend latency = %s", result.Retrieval.BackendLatency)
	}
}

func TestMemoryRecallModeContract(t *testing.T) {
	t.Run("omitted mode remains semantic", func(t *testing.T) {
		call := func(mode RecallMode) RecallResult {
			client, _ := routedClient(t, func(request recordedRequest) testResponse {
				statement, _ := request.Payload["command"].(string)
				switch {
				case strings.Contains(statement, "vector.fuse"):
					return testResponse{Body: `{"result":[{"rid":"#10:1","score":0.03}]}`}
				case strings.Contains(statement, "FROM FACT") && strings.Contains(statement, "@rid IN"):
					return testResponse{Body: recallFactRow}
				default:
					return testResponse{Body: `{"result":[]}`}
				}
			})
			result, err := client.RecallMemory(context.Background(), RecallRequest{
				IdentityID: "identity-a", Mode: mode, Query: "blue notebook",
			})
			if err != nil {
				t.Fatalf("RecallMemory(%q): %v", mode, err)
			}
			return result
		}
		omitted, explicit := call(""), call(RecallModeSemantic)
		if omitted.Retrieval.Path != explicit.Retrieval.Path ||
			omitted.Retrieval.EffectivePath != explicit.Retrieval.EffectivePath ||
			len(omitted.Evidence) != len(explicit.Evidence) {
			t.Fatalf("omitted=%+v explicit=%+v", omitted, explicit)
		}
	})

	t.Run("reasoning is reserved", func(t *testing.T) {
		client, rec := recordingClient(t, `{"result":[]}`)
		result, err := client.RecallMemory(context.Background(), RecallRequest{
			IdentityID: "identity-a", Mode: RecallModeReasoning,
		})
		if err != nil {
			t.Fatalf("reserved reasoning mode: %v", err)
		}
		if !result.Abstained || result.Reason != "reasoning_not_available" {
			t.Fatalf("result = %+v", result)
		}
		if len(rec.statements) != 0 {
			t.Fatalf("reserved mode queried storage: %v", rec.statements)
		}
	})

	t.Run("unknown mode fails before access", func(t *testing.T) {
		client, rec := recordingClient(t, `{"result":[]}`)
		_, err := client.RecallMemory(context.Background(), RecallRequest{
			IdentityID: "identity-a", Mode: "invented",
		})
		if err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Fatalf("err = %v", err)
		}
		if len(rec.statements) != 0 {
			t.Fatalf("unknown mode queried storage: %v", rec.statements)
		}
	})
}

func TestMemoryRecallWindow(t *testing.T) {
	t.Run("recent is capped", func(t *testing.T) {
		responseIndex := 0
		client, requests := routedClient(t, func(recordedRequest) testResponse {
			responses := []string{recallAnchorRow, recallWindowRows}
			response := responses[min(responseIndex, len(responses)-1)]
			responseIndex++
			return testResponse{Body: response}
		})
		result, err := client.RecallMemory(context.Background(), RecallRequest{
			IdentityID: "identity-a", Mode: RecallModeRecent, Limit: 1000,
		})
		if err != nil {
			t.Fatalf("recent: %v", err)
		}
		if len(result.Evidence) != 1 || result.Evidence[0].Conversation == nil {
			t.Fatalf("evidence = %+v", result.Evidence)
		}
		params := (*requests)[0].Payload["params"].(map[string]any)
		if got := params["recent_limit"]; got != float64(20) {
			t.Fatalf("recent limit = %v, want server cap 20", got)
		}
	})

	t.Run("open returns a bounded cursor", func(t *testing.T) {
		// A FULL page, because a short one now ends the paging and emits no cursor --
		// which is the point of the two sub-tests below. The cursor's shape still has
		// to be checked, and only a full page produces one.
		client, rec := recordingClient(t, recallFullPageRows(20))
		result, err := client.RecallMemory(context.Background(), RecallRequest{
			IdentityID: "identity-a", Mode: RecallModeOpen,
			ConversationID: "conversation-1", AnchorSeq: 7,
			Direction: RecallDirectionAfter, Limit: 1000,
		})
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		if len(result.Evidence) != 1 || result.NextCursor == "" {
			t.Fatalf("result = %+v", result)
		}
		decoded := decodeTestRecallCursor(t, result.NextCursor)
		if decoded.Version != 1 || decoded.IdentityID != "identity-a" ||
			decoded.ConversationID != "conversation-1" || decoded.PageSize != 20 {
			t.Fatalf("cursor = %+v", decoded)
		}
		raw, _ := base64.RawURLEncoding.DecodeString(result.NextCursor)
		if strings.Contains(string(raw), "#") {
			t.Fatalf("cursor leaked a RID: %s", raw)
		}
		if got := rec.params[0]["page_size"]; got != float64(20) {
			t.Fatalf("page size = %v", got)
		}
	})
}

func TestRecallCursorFailsClosedBeforeQuery(t *testing.T) {
	valid := RecallCursor{
		Version: 1, IdentityID: "identity-a", ConversationID: "conversation-1",
		AnchorSeq: 7, Direction: RecallDirectionAfter, PageSize: 3,
	}
	unknownField := base64.RawURLEncoding.EncodeToString([]byte(
		`{"version":1,"identity_id":"identity-a","conversation_id":"conversation-1","anchor_seq":7,"direction":"after","page_size":3,"rid":"#3:1"}`,
	))
	tests := []struct {
		name   string
		cursor string
		mutate func(*RecallRequest)
		want   string
	}{
		{name: "malformed", cursor: "%%%", want: "cursor"},
		{name: "oversized", cursor: strings.Repeat("a", 2049), want: "cursor"},
		{name: "wrong version", cursor: encodeTestRecallCursor(t, withRecallCursor(valid, func(cursor *RecallCursor) { cursor.Version = 2 })), want: "version"},
		{name: "foreign identity", cursor: encodeTestRecallCursor(t, withRecallCursor(valid, func(cursor *RecallCursor) { cursor.IdentityID = "identity-b" })), want: "identity"},
		{name: "wrong conversation", cursor: encodeTestRecallCursor(t, withRecallCursor(valid, func(cursor *RecallCursor) { cursor.ConversationID = "conversation-2" })), want: "conversation"},
		{name: "direction mismatch", cursor: encodeTestRecallCursor(t, valid), mutate: func(request *RecallRequest) { request.Direction = RecallDirectionBefore }, want: "direction"},
		{name: "anchor mismatch", cursor: encodeTestRecallCursor(t, valid), mutate: func(request *RecallRequest) { request.AnchorSeq = 8 }, want: "anchor"},
		{name: "page cap", cursor: encodeTestRecallCursor(t, withRecallCursor(valid, func(cursor *RecallCursor) { cursor.PageSize = 999 })), want: "page"},
		{name: "RID field", cursor: unknownField, want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, rec := recordingClient(t, `{"result":[]}`)
			request := RecallRequest{
				IdentityID: "identity-a", Mode: RecallModeScroll,
				ConversationID: "conversation-1", AnchorSeq: 7,
				Direction: RecallDirectionAfter, Cursor: tt.cursor,
			}
			if tt.mutate != nil {
				tt.mutate(&request)
			}
			_, err := client.RecallMemory(context.Background(), request)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.want)) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if len(rec.statements) != 0 {
				t.Fatalf("invalid cursor reached query: %v", rec.statements)
			}
		})
	}
}

func encodeTestRecallCursor(t *testing.T, cursor RecallCursor) string {
	t.Helper()
	raw, err := json.Marshal(cursor)
	if err != nil {
		t.Fatalf("marshal cursor: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// recallFullPageRows builds a page of exactly count turns, numbered from 7 upward, so a
// test can produce the "there may be more" case the cursor rule now depends on.
func recallFullPageRows(count int) string {
	rows := make([]string, 0, count)
	for index := range count {
		seq := 7 + index
		rows = append(rows, fmt.Sprintf(
			`{"identity_id":"identity-a","conversation_id":"conversation-1","turn_seq":%d,`+
				`"role":"user","content":"turn %d","content_hash":"hash-%d",`+
				`"occurred_at":"2026-08-31T12:00:00Z","source_ref":"postgres://c/1/turn/%d"}`,
			seq, seq, seq, seq))
	}
	return `{"result":[` + strings.Join(rows, ",") + `]}`
}

// A short page means the conversation ran out, and a cursor pointing past the end is how
// the paging failed to terminate before: measured live, `scroll` after the last turn of a
// real conversation returned that turn again with a byte-identical next_cursor.
func TestMemoryRecallPagingTerminatesAtTheEndOfAConversation(t *testing.T) {
	client, _ := recordingClient(t, recallWindowRows)
	result, err := client.RecallMemory(context.Background(), RecallRequest{
		IdentityID: "identity-a", Mode: RecallModeOpen,
		ConversationID: "conversation-1", AnchorSeq: 6,
		Direction: RecallDirectionAfter, Limit: 20,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if result.NextCursor != "" {
		t.Fatalf("a short page still offered another page: %q", result.NextCursor)
	}
}

// The next page must start PAST what this one returned, or every page repeats the
// previous page's boundary turn.
func TestMemoryRecallNextCursorStartsAfterTheLastReturnedTurn(t *testing.T) {
	client, _ := recordingClient(t, recallFullPageRows(20))
	result, err := client.RecallMemory(context.Background(), RecallRequest{
		IdentityID: "identity-a", Mode: RecallModeOpen,
		ConversationID: "conversation-1", AnchorSeq: 7,
		Direction: RecallDirectionAfter, Limit: 20,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if result.NextCursor == "" {
		t.Fatal("a full page offered no next page")
	}
	// The fixture's last turn is 7+20-1 = 26, so the next page must begin at 27.
	if got := decodeTestRecallCursor(t, result.NextCursor).AnchorSeq; got != 27 {
		t.Fatalf("next anchor = %d, want 27 (past the last returned turn)", got)
	}
}

// The cursor is documented as opaque, so handing it straight back must work. It used to
// fail with "conversation mismatch" unless the caller also repeated the conversation, the
// anchor and the direction the cursor already carried.
func TestMemoryRecallScrollAcceptsTheCursorAlone(t *testing.T) {
	client, _ := recordingClient(t, recallFullPageRows(20))
	opened, err := client.RecallMemory(context.Background(), RecallRequest{
		IdentityID: "identity-a", Mode: RecallModeOpen,
		ConversationID: "conversation-1", AnchorSeq: 7,
		Direction: RecallDirectionAfter, Limit: 20,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err = client.RecallMemory(context.Background(), RecallRequest{
		IdentityID: "identity-a", Mode: RecallModeScroll, Cursor: opened.NextCursor,
	}); err != nil {
		t.Fatalf("scroll with the cursor alone: %v", err)
	}
}

// Contradicting the cursor is still refused: that is confusion about what is being paged,
// not the omission the rule above forgives.
func TestMemoryRecallScrollRefusesACursorTheRequestContradicts(t *testing.T) {
	client, _ := recordingClient(t, recallFullPageRows(20))
	opened, err := client.RecallMemory(context.Background(), RecallRequest{
		IdentityID: "identity-a", Mode: RecallModeOpen,
		ConversationID: "conversation-1", AnchorSeq: 7,
		Direction: RecallDirectionAfter, Limit: 20,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for name, request := range map[string]RecallRequest{
		"another conversation": {ConversationID: "conversation-2"},
		"another anchor":       {AnchorSeq: 3},
		"another direction":    {Direction: RecallDirectionBefore},
	} {
		request.IdentityID, request.Mode, request.Cursor = "identity-a", RecallModeScroll, opened.NextCursor
		if _, err := client.RecallMemory(context.Background(), request); err == nil {
			t.Errorf("%s was accepted against the cursor", name)
		}
	}
}

func decodeTestRecallCursor(t *testing.T, encoded string) RecallCursor {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	var cursor RecallCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		t.Fatalf("unmarshal cursor: %v", err)
	}
	return cursor
}

func withRecallCursor(cursor RecallCursor, mutate func(*RecallCursor)) RecallCursor {
	mutate(&cursor)
	return cursor
}

// The two empty pages are not the same answer. Opening at an anchor that holds no turn is
// a bad request; following a cursor off the end is the conversation running out, and
// telling the caller its anchor was not found sends it hunting a bug that is not there.
func TestMemoryRecallNamesAnExhaustedConversationApartFromABadAnchor(t *testing.T) {
	const empty = `{"result":[]}`
	opened, err := recallClientResult(t, empty, RecallRequest{
		IdentityID: "identity-a", Mode: RecallModeOpen,
		ConversationID: "conversation-1", AnchorSeq: 999, Direction: RecallDirectionAfter,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if opened.Reason != "conversation_anchor_not_found" {
		t.Errorf("open reason = %q, want conversation_anchor_not_found", opened.Reason)
	}

	client, _ := recordingClient(t, empty)
	cursor, err := encodeRecallCursor(RecallCursor{
		Version: recallCursorVersion, IdentityID: "identity-a",
		ConversationID: "conversation-1", AnchorSeq: 999,
		Direction: RecallDirectionAfter, PageSize: 3,
	})
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	scrolled, err := client.RecallMemory(context.Background(), RecallRequest{
		IdentityID: "identity-a", Mode: RecallModeScroll, Cursor: cursor,
	})
	if err != nil {
		t.Fatalf("scroll: %v", err)
	}
	if scrolled.Reason != "conversation_exhausted" {
		t.Errorf("scroll reason = %q, want conversation_exhausted", scrolled.Reason)
	}
	if scrolled.NextCursor != "" {
		t.Errorf("an exhausted conversation still offered another page: %q", scrolled.NextCursor)
	}
}

func recallClientResult(t *testing.T, body string, request RecallRequest) (RecallResult, error) {
	t.Helper()
	client, _ := recordingClient(t, body)
	return client.RecallMemory(context.Background(), request)
}
