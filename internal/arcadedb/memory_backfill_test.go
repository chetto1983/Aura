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

	"github.com/chetto1983/aura/internal/embeddings"
)

// batchEmbedder answers every batch with one correctly-sized vector per input, so a
// test can drive as many rounds as it likes without queueing fixtures.
type batchEmbedder struct{}

func (b *batchEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i := range texts {
		out[i] = make([]float64, vectorDimensions)
	}
	return out, nil
}

func (b *batchEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: "es1-batch"}, nil
}

// tenantServer is a multi-tenant fake ArcadeDB. It routes by the database in the path
// and by the basic-auth user, which is what makes it able to answer the two questions
// the sweep asks: "is this tenant provisioned" (a bind against /api/v1/ready) and
// "which of its facts are outside the space" (a query against its own database).
type tenantServer struct {
	// provisioned holds the databases that exist. A credential for anything else is
	// refused, exactly as the real server refuses a user it never created.
	provisioned map[string]bool
	// pending is how many facts outside the space each database still holds.
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
	if !strings.Contains(payload.Command, " FROM "+factEdgeType+" ") {
		_, _ = io.WriteString(w, `{"result":[]}`) // this fake holds no turns and no traces
		return
	}
	s.selects[database]++
	take := min(s.pending[database], backfillBatch)
	s.pending[database] -= take
	rows := make([]string, 0, take)
	for i := range take {
		rows = append(rows, fmt.Sprintf(`{"rid":"#3:%d","text":"fact %d"}`, i, i))
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

func testBackfill(t *testing.T, s *tenantServer, roster MemoryIdentities, embedder DenseEmbedder) *TenantBackfill {
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

// The selection is "not in the daemon's space", paged by RID: it selects the facts a route
// change left behind as well as the ones never embedded, and it moves past a row it could
// not fix instead of selecting it again.
func TestTenantBackfillSelectsRowsOutsideTheSpace(t *testing.T) {
	server := newTenantServer(t, map[string]bool{databaseA: true}, map[string]int{databaseA: 1})
	backfill := testBackfill(t, server, staticRoster{ids: []string{tenantA}}, &batchEmbedder{})

	if _, err := backfill.EmbedMissing(context.Background(), time.Time{}); err != nil {
		t.Fatalf("EmbedMissing: %v", err)
	}
	selected := ""
	for _, statement := range server.statements {
		if strings.Contains(statement, "SELECT @rid AS rid") && strings.Contains(statement, " FROM "+factEdgeType+" ") {
			selected = statement
		}
	}
	for _, want := range []string{otherSpace, "@rid > :cursor", "ORDER BY @rid"} {
		if !strings.Contains(selected, want) {
			t.Fatalf("selection = %q, want %q", selected, want)
		}
	}
	if strings.Contains(selected, "embedding IS NULL") {
		t.Fatalf("selection = %q still keys on a missing vector", selected)
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

// A tenant is drained within the run's budget, not cut at a round count: a route change
// leaves the whole memory behind, and a cap of 20 rounds would take a large tenant many
// runs while its reads stay lexical.
func TestTenantBackfillDrainsATenantUntilNothingIsLeft(t *testing.T) {
	const backlog = (backfillRoundsPerTenant + 5) * backfillBatch
	server := newTenantServer(t, map[string]bool{databaseA: true}, map[string]int{databaseA: backlog})
	backfill := testBackfill(t, server, staticRoster{ids: []string{tenantA}}, &batchEmbedder{})

	embedded, err := backfill.EmbedMissing(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("EmbedMissing: %v", err)
	}
	if embedded != backlog {
		t.Fatalf("embedded = %d, want the whole backlog of %d", embedded, backlog)
	}
}

// Review Focus 4: the tenant a run starts from moves on, so a backlog that outlasts one
// run's budget cannot starve the tenants behind it.
func TestTenantBackfillRotatesTheTenantItStartsFrom(t *testing.T) {
	if got := rotated([]string{"a", "b", "c"}, 4); strings.Join(got, "") != "bca" {
		t.Fatalf("rotated = %v, want b c a", got)
	}
	if got := rotated(nil, 3); len(got) != 0 {
		t.Fatalf("rotated(nil) = %v", got)
	}
}

// Review Focus 4: the run budget ending is not a failure; the next run resumes.
func TestTenantBackfillStopsAtTheBudgetWithoutFailing(t *testing.T) {
	server := newTenantServer(t, map[string]bool{databaseA: true, databaseB: true},
		map[string]int{databaseA: 10, databaseB: 10})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := testBackfill(t, server, staticRoster{ids: []string{tenantA, tenantB}}, &batchEmbedder{}).
		EmbedMissing(ctx, time.Time{}); err != nil {
		t.Fatalf("a spent budget was reported as a failure: %v", err)
	}
}

// No space, no pass: a hosted route without its key, or a sidecar that cannot name its
// model, would fail every tenant the same way.
func TestTenantBackfillDoesNotRunWithoutASpace(t *testing.T) {
	server := newTenantServer(t, map[string]bool{databaseA: true}, map[string]int{databaseA: 3})
	embedder := &stubEmbedder{spaceErr: embeddings.ErrNoCredential}
	_, err := testBackfill(t, server, staticRoster{ids: []string{tenantA}}, embedder).EmbedMissing(context.Background(), time.Time{})
	if !errors.Is(err, embeddings.ErrNoCredential) {
		t.Fatalf("err = %v, want ErrNoCredential", err)
	}
	if server.sawDatabase(databaseA) {
		t.Fatal("the pass visited a tenant with no space to embed in")
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
