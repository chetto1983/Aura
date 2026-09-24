package arcadedb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/embeddings"
)

type passRow struct {
	position int
	text     string
	vector   bool
	space    string
	deleted  bool
}

// passServer holds rows per type and answers exactly the statements the pass sends.
type passServer struct {
	mu      sync.Mutex
	rows    map[string][]*passRow // by type
	selects int
}

var passUpdate = regexp.MustCompile(`UPDATE (\w+) SET embedding = :(v\d+), embed_space = :(s\d+) WHERE @rid = :(r\d+)`)

func (s *passServer) rid(typeName string, row *passRow) string {
	return fmt.Sprintf("#%d:%d", map[string]int{factEdgeType: 10, conversationTurnType: 20, reasoningTraceType: 30}[typeName], row.position)
}

func (s *passServer) serve(w http.ResponseWriter, r *http.Request) {
	if handleTransactionEndpoints(w, r) {
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var payload struct {
		Command string         `json:"command"`
		Params  map[string]any `json:"params"`
	}
	_ = json.Unmarshal(raw, &payload)
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if strings.HasPrefix(payload.Command, "SELECT @rid AS rid, ") {
		s.selects++
		_ = json.NewEncoder(w).Encode(map[string]any{"result": s.selectRows(payload.Command, payload.Params)})
		return
	}
	for _, match := range passUpdate.FindAllStringSubmatch(payload.Command, -1) {
		typeName, rid := match[1], payload.Params[match[4]]
		for _, row := range s.rows[typeName] {
			if s.rid(typeName, row) == rid {
				row.vector = payload.Params[match[2]] != nil
				row.space, _ = payload.Params[match[3]].(string)
			}
		}
	}
	_, _ = io.WriteString(w, `{"result":[{"count":1}]}`)
}

func (s *passServer) selectRows(statement string, params map[string]any) []map[string]any {
	typeName := strings.Fields(statement[strings.Index(statement, " FROM ")+6:])[0]
	space, _ := params["space"].(string)
	cursor, _ := params["cursor"].(string)
	batch := int(params["page"].(float64))
	rows := append([]*passRow(nil), s.rows[typeName]...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].position < rows[j].position })
	out := []map[string]any{}
	for _, row := range rows {
		if len(out) == batch {
			break
		}
		if ridAfter(s.rid(typeName, row), cursor) && row.space != space &&
			(!strings.Contains(statement, "deleted_at IS NULL") || !row.deleted) &&
			(!strings.Contains(statement, "(embedding IS NOT NULL OR embed_space IS NOT NULL)") || row.vector || row.space != "") {
			out = append(out, map[string]any{"rid": s.rid(typeName, row), "text": row.text})
		}
	}
	return out
}

func ridAfter(rid, cursor string) bool {
	if cursor == "#-1:-1" {
		return true
	}
	position := func(value string) int { n, _ := strconv.Atoi(value[strings.Index(value, ":")+1:]); return n }
	return position(rid) > position(cursor)
}

func newPassClient(t *testing.T, rows map[string][]*passRow, embedder DenseEmbedder) (*Client, *passServer) {
	t.Helper()
	server := &passServer{rows: rows}
	srv := httptest.NewServer(http.HandlerFunc(server.serve))
	t.Cleanup(srv.Close)
	return mustClient(t, srv.URL).WithEmbedder(embedder), server
}

func stampedRows(n int, space string, vector bool) []*passRow {
	rows := make([]*passRow, n)
	for i := range rows {
		rows[i] = &passRow{position: i, text: fmt.Sprintf("text %d", i), vector: vector, space: space}
	}
	return rows
}

func TestPassMovesEveryTypeToTheNewSpace(t *testing.T) {
	rows := map[string][]*passRow{
		factEdgeType:         append(stampedRows(40, "es1-a", true), &passRow{position: 40, text: "never embedded"}),
		conversationTurnType: stampedRows(3, "", true),
		reasoningTraceType:   stampedRows(2, "es1-a", true),
	}
	client, _ := newPassClient(t, rows, &refusingEmbedder{space: "es1-b"})
	tally, err := client.reembedMemory(context.Background())
	if err != nil {
		t.Fatalf("reembedMemory: %v", err)
	}
	if tally.embedded != 46 || tally.refused != 0 {
		t.Fatalf("tally = %+v, want 46 embedded", tally)
	}
	for typeName, typed := range rows {
		for _, row := range typed {
			if !row.vector || row.space != "es1-b" {
				t.Fatalf("%s %+v was left outside es1-b", typeName, *row)
			}
		}
	}
}

// A turn with neither vector nor stamp is the reconciler's; a turn refused in another
// space, and a soft-deleted turn, are not the same case (Review Focus 5).
func TestPassLeavesTheReconcilersTurnsAlone(t *testing.T) {
	rows := map[string][]*passRow{
		conversationTurnType: {
			{position: 0, text: "never answered"},
			{position: 1, text: "refused elsewhere", space: "es1-a"},
			{position: 2, text: "deleted", vector: true, space: "es1-a", deleted: true},
		},
	}
	client, _ := newPassClient(t, rows, &refusingEmbedder{space: "es1-b"})
	if _, err := client.reembedType(context.Background(), turnSpace, backfillBatch, 0); err != nil {
		t.Fatalf("reembedType: %v", err)
	}
	turns := rows[conversationTurnType]
	if turns[0].space != "" || turns[1].space != "es1-b" || turns[2].space != "es1-a" {
		t.Fatalf("turns = %+v %+v %+v", *turns[0], *turns[1], *turns[2])
	}
}

// A cursor, not a re-selection: a row whose vector comes back unusable must not be selected
// again on every round.
func TestPassCursorPassesARowItCouldNotFix(t *testing.T) {
	rows := map[string][]*passRow{factEdgeType: stampedRows(70, "es1-a", true)}
	embedder := &widthEmbedder{short: "text 5", space: "es1-b"}
	client, server := newPassClient(t, rows, embedder)
	if _, err := client.reembedType(context.Background(), factSpace, backfillBatch, 0); err != nil {
		t.Fatalf("reembedType: %v", err)
	}
	if server.selects != 3 {
		t.Fatalf("selections = %d, want 3 for 70 rows in rounds of %d", server.selects, backfillBatch)
	}
	if rows[factEdgeType][5].space != "es1-a" {
		t.Fatal("a row with an unusable vector was stamped")
	}
}

func TestPassQuarantinesOnlyTheRefusedRecord(t *testing.T) {
	rows := map[string][]*passRow{factEdgeType: stampedRows(5, "es1-a", true)}
	embedder := &refusingEmbedder{refuse: []string{"text 3"}, status: http.StatusBadRequest, space: "es1-b"}
	client, _ := newPassClient(t, rows, embedder)
	tally, err := client.reembedType(context.Background(), factSpace, backfillBatch, 0)
	if err != nil {
		t.Fatalf("reembedType: %v", err)
	}
	if tally.embedded != 4 || tally.refused != 1 {
		t.Fatalf("tally = %+v, want 4 embedded, 1 refused", tally)
	}
	refused := rows[factEdgeType][3]
	if refused.vector || refused.space != "es1-b" {
		t.Fatalf("refused record = %+v, want no vector and the refusing space", *refused)
	}
}

// Review Focus 3: a route failure ends the run and moves nothing.
func TestPassEndsOnARouteFailureWithoutMarkingAnything(t *testing.T) {
	for _, failure := range []error{
		&embeddings.StatusError{Code: 401}, &embeddings.StatusError{Code: 429},
		&embeddings.StatusError{Code: 503}, errors.New("request: connection refused"),
	} {
		rows := map[string][]*passRow{factEdgeType: stampedRows(3, "es1-a", true)}
		client, _ := newPassClient(t, rows, &failingEmbedder{err: failure, space: "es1-b"})
		if _, err := client.reembedType(context.Background(), factSpace, backfillBatch, 0); err == nil {
			t.Fatalf("%v was absorbed", failure)
		}
		for _, row := range rows[factEdgeType] {
			if row.space != "es1-a" || !row.vector {
				t.Fatalf("%v moved %+v", failure, *row)
			}
		}
	}
}

// A second route change in mid-pass needs no protocol: B-stamped rows differ from C.
func TestPassFollowsASecondRouteChange(t *testing.T) {
	rows := map[string][]*passRow{factEdgeType: append(stampedRows(2, "es1-a", true), stampedRows(1, "es1-b", true)...)}
	rows[factEdgeType][2].position = 2
	client, _ := newPassClient(t, rows, &refusingEmbedder{space: "es1-c"})
	if _, err := client.reembedMemory(context.Background()); err != nil {
		t.Fatalf("reembedMemory: %v", err)
	}
	for _, row := range rows[factEdgeType] {
		if row.space != "es1-c" {
			t.Fatalf("row %+v left behind by A→B→C", *row)
		}
	}
}

func TestEmbedMissingFactsTakesOneBoundedRound(t *testing.T) {
	rows := map[string][]*passRow{factEdgeType: stampedRows(150, "", false)}
	client, server := newPassClient(t, rows, &refusingEmbedder{space: "es1-b"})
	embedded, err := client.EmbedMissingFacts(context.Background(), 1000)
	if err != nil {
		t.Fatalf("EmbedMissingFacts: %v", err)
	}
	if embedded != defaultMemoryLimits.MaintenanceBatch || server.selects != 1 {
		t.Fatalf("embedded %d in %d selections, want one round capped at %d",
			embedded, server.selects, defaultMemoryLimits.MaintenanceBatch)
	}
}

type widthEmbedder struct {
	short string
	space string
}

func (e *widthEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	vectors := make([][]float64, len(texts))
	for i, text := range texts {
		vectors[i] = vectorOf(1)
		if strings.HasSuffix(text, e.short) {
			vectors[i] = []float64{1}
		}
	}
	return vectors, nil
}

func (e *widthEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: e.space}, nil
}

type failingEmbedder struct {
	err   error
	space string
}

func (e *failingEmbedder) Embed(context.Context, []string) ([][]float64, error) { return nil, e.err }

func (e *failingEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: e.space}, nil
}
