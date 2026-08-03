package arcadedb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// batchEmbedder answers every batch with one correctly-sized vector per input, so a
// test can drive as many rounds as it likes without queueing fixtures.
type batchEmbedder struct {
	err    error
	widths int // when non-zero, the width returned instead of vectorDimensions
	calls  int
}

func (b *batchEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	b.calls++
	if b.err != nil {
		return nil, b.err
	}
	width := vectorDimensions
	if b.widths != 0 {
		width = b.widths
	}
	out := make([][]float64, len(texts))
	for i := range texts {
		out[i] = make([]float64, width)
	}
	return out, nil
}

// tenantServer is a multi-tenant fake ArcadeDB. It routes by the database in the path
// and by the basic-auth user, which is what makes it able to answer the two questions
// the sweep asks: "is this tenant provisioned" (a bind against /api/v1/ready) and
// "which of its facts have no vector" (a query against its own database).
type tenantServer struct {
	// provisioned holds the databases that exist. A credential for anything else is
	// refused, exactly as the real server refuses a user it never created.
	provisioned map[string]bool
	// pending is how many vector-less facts each database still holds.
	pending map[string]int

	mu         sync.Mutex
	selects    map[string]int
	statements []string
	url        string
}

func newTenantServer(t *testing.T, provisioned map[string]bool, pending map[string]int) *tenantServer {
	t.Helper()
	s := &tenantServer{
		provisioned: provisioned,
		pending:     pending,
		selects:     map[string]int{},
	}
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(srv.Close)
	s.url = srv.URL
	return s
}

func (s *tenantServer) serve(w http.ResponseWriter, r *http.Request) {
	user, _, _ := r.BasicAuth()
	if strings.HasSuffix(r.URL.Path, "/api/v1/ready") {
		// The credential exists exactly when its database does: the sidecar creates
		// both in one provisioning step. 204 is what ArcadeDB's /api/v1/ready answers
		// an accepted credential — 200 would be a fake that proves the wrong thing.
		if s.provisioned["mem_"+strings.TrimPrefix(user, "u_")] {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	database := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	raw, _ := io.ReadAll(r.Body)
	var payload struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(raw, &payload)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.statements = append(s.statements, database+": "+payload.Command)
	w.Header().Set("Content-Type", "application/json")
	if !strings.HasPrefix(payload.Command, "SELECT @rid AS rid") {
		_, _ = io.WriteString(w, `{"result":[{"count":1}]}`)
		return
	}
	s.selects[database]++
	take := min(s.pending[database], backfillBatch)
	s.pending[database] -= take
	rows := make([]string, 0, take)
	for i := range take {
		rows = append(rows, fmt.Sprintf(`{"rid":"#3:%d","statement":"fact %d"}`, i, i))
	}
	_, _ = io.WriteString(w, `{"result":[`+strings.Join(rows, ",")+`]}`)
}

func (s *tenantServer) sawDatabase(database string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, statement := range s.statements {
		if strings.HasPrefix(statement, database+": ") {
			return true
		}
	}
	return false
}

type staticRoster struct {
	ids []string
	err error
}

func (r staticRoster) IdentityIDs(context.Context) ([]string, error) { return r.ids, r.err }

func testCredentials(t *testing.T) *TenantCredentials {
	t.Helper()
	t.Setenv(tenantSecretEnv, strings.Repeat("s", 32))
	credentials, err := NewTenantCredentials()
	if err != nil {
		t.Fatalf("NewTenantCredentials: %v", err)
	}
	return credentials
}

func testBackfill(t *testing.T, s *tenantServer, roster MemoryIdentities, embedder Embedder) *TenantBackfill {
	t.Helper()
	return NewTenantBackfill(roster, Config{BaseURL: s.url}, testCredentials(t), embedder)
}

const (
	tenantA   = "11111111-1111-1111-1111-111111111111"
	tenantB   = "22222222-2222-2222-2222-222222222222"
	databaseA = "mem_11111111_1111_1111_1111_111111111111"
	databaseB = "mem_22222222_2222_2222_2222_222222222222"
)

// A tenant whose memory has never been provisioned is a SKIP, not a failure: databases
// are created lazily on first use, so a registered identity that never stored a fact
// legitimately has neither database nor credential. The sweep must still embed everyone
// else's facts, and must not touch the absent database at all.
func TestTenantBackfillSkipsTenantWithoutMemory(t *testing.T) {
	server := newTenantServer(t,
		map[string]bool{databaseA: true},
		map[string]int{databaseA: 3})
	backfill := testBackfill(t, server, staticRoster{ids: []string{tenantA, tenantB}}, &batchEmbedder{})

	embedded, err := backfill.EmbedMissing(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("EmbedMissing: %v", err)
	}
	if embedded != 3 {
		t.Fatalf("embedded = %d, want the provisioned tenant's 3 facts", embedded)
	}
	if server.sawDatabase(databaseB) {
		t.Fatal("the sweep queried a database that does not exist")
	}
}

// Every tenant is visited, not just the first: memory is one database per identity, so a
// sweep that stopped at one would leave every other person's recall blind.
func TestTenantBackfillReachesEveryTenant(t *testing.T) {
	server := newTenantServer(t,
		map[string]bool{databaseA: true, databaseB: true},
		map[string]int{databaseA: 2, databaseB: 5})
	backfill := testBackfill(t, server, staticRoster{ids: []string{tenantA, tenantB}}, &batchEmbedder{})

	embedded, err := backfill.EmbedMissing(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("EmbedMissing: %v", err)
	}
	if embedded != 7 {
		t.Fatalf("embedded = %d, want 2+5 across both tenants", embedded)
	}
	for _, database := range []string{databaseA, databaseB} {
		if !server.sawDatabase(database) {
			t.Fatalf("tenant %s was never swept", database)
		}
	}
}

// The selection is the "has no vector" predicate and nothing else — no recency window, no
// writer marker — which is what makes the sweep catch an upsert from any writer.
func TestTenantBackfillSelectsOnlyFactsWithoutAVector(t *testing.T) {
	server := newTenantServer(t, map[string]bool{databaseA: true}, map[string]int{databaseA: 1})
	backfill := testBackfill(t, server, staticRoster{ids: []string{tenantA}}, &batchEmbedder{})

	if _, err := backfill.EmbedMissing(context.Background(), time.Time{}); err != nil {
		t.Fatalf("EmbedMissing: %v", err)
	}
	selected := ""
	for _, statement := range server.statements {
		if strings.Contains(statement, "SELECT @rid AS rid") {
			selected = statement
		}
	}
	if !strings.Contains(selected, "embedding IS NULL") {
		t.Fatalf("selection = %q, want the vector-absence predicate", selected)
	}
}

// A backlog is drained a batch at a time, and one full batch is the signal there may be
// more: 45ms per fact in a batch versus 85-105ms alone is the whole reason to batch.
func TestTenantBackfillBatchesUntilAShortRound(t *testing.T) {
	server := newTenantServer(t,
		map[string]bool{databaseA: true},
		map[string]int{databaseA: 2*backfillBatch + 1})
	backfill := testBackfill(t, server, staticRoster{ids: []string{tenantA}}, &batchEmbedder{})

	embedded, err := backfill.EmbedMissing(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("EmbedMissing: %v", err)
	}
	if embedded != 2*backfillBatch+1 {
		t.Fatalf("embedded = %d, want the whole backlog", embedded)
	}
	// Two full rounds, then the short one that ends it — and NOT a fourth against an
	// empty database.
	if server.selects[databaseA] != 3 {
		t.Fatalf("select rounds = %d, want 3", server.selects[databaseA])
	}
}

// One tenant's share of one sweep is bounded, so a huge backlog cannot starve the tenants
// queued behind it or overrun the handler's budget. The remainder is the next tick's.
func TestTenantBackfillBoundsOneTenantsRounds(t *testing.T) {
	server := newTenantServer(t,
		map[string]bool{databaseA: true},
		map[string]int{databaseA: (backfillRoundsPerTenant + 5) * backfillBatch})
	backfill := testBackfill(t, server, staticRoster{ids: []string{tenantA}}, &batchEmbedder{})

	embedded, err := backfill.EmbedMissing(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("EmbedMissing: %v", err)
	}
	if embedded != backfillRoundsPerTenant*backfillBatch {
		t.Fatalf("embedded = %d, want the per-tenant bound", embedded)
	}
}

// A vector of the wrong width is left unwritten by EmbedMissingFacts, so the same rows are
// selected again forever. The sweep stops on a round that embedded nothing, which turns a
// spin into a bounded no-op.
func TestTenantBackfillStopsOnARoundThatEmbedsNothing(t *testing.T) {
	server := newTenantServer(t,
		map[string]bool{databaseA: true},
		map[string]int{databaseA: 10 * backfillBatch})
	backfill := testBackfill(t, server, staticRoster{ids: []string{tenantA}}, &batchEmbedder{widths: 3})

	embedded, err := backfill.EmbedMissing(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("EmbedMissing: %v", err)
	}
	if embedded != 0 {
		t.Fatalf("embedded = %d, want none written at the wrong width", embedded)
	}
	if server.selects[databaseA] != 1 {
		t.Fatalf("select rounds = %d, want the sweep to stop after the first fruitless round",
			server.selects[databaseA])
	}
}

// A tenant that fails for a real reason must not stop the others: the failure is reported
// through the log and retried next tick, while everyone else still gets their vectors.
func TestTenantBackfillContinuesPastAFailingTenant(t *testing.T) {
	server := newTenantServer(t,
		map[string]bool{databaseA: true, databaseB: true},
		map[string]int{databaseB: 4})
	// tenantA is provisioned but its identity is unparseable, which fails before any I/O.
	backfill := testBackfill(t, server, staticRoster{ids: []string{"not-a-uuid", tenantB}}, &batchEmbedder{})

	embedded, err := backfill.EmbedMissing(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("EmbedMissing: %v", err)
	}
	if embedded != 4 {
		t.Fatalf("embedded = %d, want the healthy tenant's facts", embedded)
	}
}

// When NOTHING could be swept and something failed hard, the sweep FAILS. A wrong tenant
// secret or an unreachable server would otherwise look like a healthy empty sweep, every
// five minutes, forever.
func TestTenantBackfillFailsWhenNoTenantCouldBeSwept(t *testing.T) {
	server := newTenantServer(t, map[string]bool{}, map[string]int{})
	backfill := testBackfill(t, server, staticRoster{ids: []string{"not-a-uuid"}}, &batchEmbedder{})

	if _, err := backfill.EmbedMissing(context.Background(), time.Time{}); err == nil {
		t.Fatal("a sweep that swept nothing and failed must report the failure")
	}
}

func TestTenantBackfillRefusesAnIncompleteWiring(t *testing.T) {
	server := newTenantServer(t, map[string]bool{}, map[string]int{})
	roster := staticRoster{ids: []string{tenantA}}
	for name, backfill := range map[string]*TenantBackfill{
		"nil":            nil,
		"no roster":      NewTenantBackfill(nil, Config{BaseURL: server.url}, testCredentials(t), &batchEmbedder{}),
		"no embedder":    NewTenantBackfill(roster, Config{BaseURL: server.url}, testCredentials(t), nil),
		"no credentials": NewTenantBackfill(roster, Config{BaseURL: server.url}, nil, &batchEmbedder{}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := backfill.EmbedMissing(context.Background(), time.Time{}); err == nil {
				t.Fatal("an unconfigured backfill must not report a clean sweep")
			}
		})
	}
}

func TestTenantBackfillReportsARosterFailure(t *testing.T) {
	server := newTenantServer(t, map[string]bool{}, map[string]int{})
	backfill := testBackfill(t, server, staticRoster{err: errors.New("postgres down")}, &batchEmbedder{})

	_, err := backfill.EmbedMissing(context.Background(), time.Time{})
	if err == nil || !strings.Contains(err.Error(), "postgres down") {
		t.Fatalf("error = %v, want the roster failure", err)
	}
}
