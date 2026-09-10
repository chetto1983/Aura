package agui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/openrouterprovision"
)

// fakeSpendReconciliation answers the three reconciliation calls from in-memory fixtures —
// never a live provider, never a management credential.
type fakeSpendReconciliation struct {
	tiles      []openrouterprovision.KPITile
	tilesErr   error
	keys       []openrouterprovision.KeyRecord
	keysErr    error
	credits    openrouterprovision.Credits
	creditsErr error
	kpiCalls   int
}

func (f *fakeSpendReconciliation) KPIWindows(context.Context, openrouterprovision.TimeRange, openrouterprovision.TimeRange) ([]openrouterprovision.KPITile, error) {
	f.kpiCalls++
	if f.tilesErr != nil {
		return nil, f.tilesErr
	}
	return f.tiles, nil
}

func (f *fakeSpendReconciliation) ListKeys(context.Context) ([]openrouterprovision.KeyRecord, error) {
	if f.keysErr != nil {
		return nil, f.keysErr
	}
	return f.keys, nil
}

func (f *fakeSpendReconciliation) GetCredits(context.Context) (openrouterprovision.Credits, error) {
	if f.creditsErr != nil {
		return openrouterprovision.Credits{}, f.creditsErr
	}
	return f.credits, nil
}

// fakeSpendCapReader answers Load from an in-memory map keyed on whatever identity the
// caller scoped ctx to (identityctx.WithIdentityID) — mirrors identitykey.Store's own
// ctx-scoping contract, same fixture shape credit_api_test.go's fakeCreditKeyStore uses.
type fakeSpendCapReader struct {
	caps     map[string]float64 // identity id -> cap; a missing id -> ErrNoKey
	uncapped map[string]bool    // identity id -> has a key with no limit
}

func (f *fakeSpendCapReader) Load(ctx context.Context) (identitykey.Record, error) {
	id := identityctx.IdentityID(ctx)
	if f.uncapped[id] {
		return identitykey.Record{}, nil
	}
	cap, ok := f.caps[id]
	if !ok {
		return identitykey.Record{}, identitykey.ErrNoKey
	}
	return identitykey.Record{LimitUSD: &cap}, nil
}

func spendFiveTiles() []openrouterprovision.KPITile {
	series := make([]float64, 12)
	for i := range series {
		series[i] = float64(i)
	}
	delta := 12.5
	tiles := make([]openrouterprovision.KPITile, 0, len(openrouterprovision.KPIMetrics))
	for _, metric := range openrouterprovision.KPIMetrics {
		tiles = append(tiles, openrouterprovision.KPITile{
			Metric: metric, Value: 42, Delta: &delta, Series: append([]float64(nil), series...),
		})
	}
	return tiles
}

func newTestSpendServer(recon *fakeSpendReconciliation, caps *fakeSpendCapReader, ids []identity.Identity) *Server {
	s := &Server{idAdmin: &fakeIdentityAdmin{identities: ids}}
	var r spendReconciliation
	if recon != nil {
		r = recon
	}
	var c spendCapReader
	if caps != nil {
		c = caps
	}
	s.SetSpendOverview(r, c, func() time.Time { return time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC) })
	return s
}

func spendRequest() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/api/admin/spend/overview", nil)
}

// TestSpendOverviewReturnsFiveKPIs proves the response carries five named metrics, each
// with a value, a delta and a 12-point sparkline series.
func TestSpendOverviewReturnsFiveKPIs(t *testing.T) {
	recon := &fakeSpendReconciliation{tiles: spendFiveTiles(), credits: openrouterprovision.Credits{TotalCredits: 100, TotalUsage: 10}}
	s := newTestSpendServer(recon, &fakeSpendCapReader{caps: map[string]float64{}}, []identity.Identity{{ID: testLocalID, Name: "local"}})

	rec := httptest.NewRecorder()
	s.handleSpendOverview(rec, spendRequest())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out spendOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.KPIs) != 5 {
		t.Fatalf("len(KPIs) = %d, want 5", len(out.KPIs))
	}
	for _, kpi := range out.KPIs {
		if kpi.Delta == nil {
			t.Errorf("kpi %s: Delta = nil, want a value", kpi.Metric)
		}
		if len(kpi.Series) != 12 {
			t.Errorf("kpi %s: len(Series) = %d, want 12", kpi.Metric, len(kpi.Series))
		}
	}
	if recon.kpiCalls != 1 {
		t.Errorf("KPIWindows calls = %d, want 1 (one call composing both windows)", recon.kpiCalls)
	}
}

// TestSpendOverviewJoinsIdentitiesByExternalUser proves a provider key row whose
// external_user matches an Aura identity is attributed to that identity's name, and one
// that matches NOTHING is not silently attributed to anybody (the join iterates the
// roster, not the provider's key list — an orphaned key is simply never looked up).
func TestSpendOverviewJoinsIdentitiesByExternalUser(t *testing.T) {
	ids := []identity.Identity{{ID: "id-alice", Name: "alice@example.com"}}
	keys := []openrouterprovision.KeyRecord{
		{ExternalUser: "id-alice", Label: "sk-or-v1-aaa...111", Usage: 12.5},
		{ExternalUser: "id-orphan-no-such-identity", Label: "sk-or-v1-zzz...999", Usage: 999},
	}
	recon := &fakeSpendReconciliation{tiles: spendFiveTiles(), keys: keys, credits: openrouterprovision.Credits{TotalCredits: 100, TotalUsage: 10}}
	s := newTestSpendServer(recon, &fakeSpendCapReader{caps: map[string]float64{}}, ids)

	rec := httptest.NewRecorder()
	s.handleSpendOverview(rec, spendRequest())
	var out spendOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.TopIdentities) != 1 {
		t.Fatalf("len(TopIdentities) = %d, want 1 (the orphaned key attaches to nobody)", len(out.TopIdentities))
	}
	row := out.TopIdentities[0]
	if row.Name != "alice@example.com" || row.MaskedLabel != "sk-or-v1-aaa...111" || row.LifetimeSpend != 12.5 {
		t.Errorf("row = %+v, want alice attributed her own key", row)
	}
	if strings.Contains(rec.Body.String(), "999") {
		t.Errorf("orphaned key's spend (999) leaked into the response: %s", rec.Body.String())
	}
}

// TestSpendOverviewRanksTopFive proves six identities yield five rows ordered by
// lifetime spend descending; the sixth is absent.
func TestSpendOverviewRanksTopFive(t *testing.T) {
	var ids []identity.Identity
	var keys []openrouterprovision.KeyRecord
	for i := range 6 {
		id := "id-" + string(rune('a'+i))
		ids = append(ids, identity.Identity{ID: id, Name: id})
		keys = append(keys, openrouterprovision.KeyRecord{ExternalUser: id, Usage: float64(i)})
	}
	recon := &fakeSpendReconciliation{tiles: spendFiveTiles(), keys: keys, credits: openrouterprovision.Credits{TotalCredits: 100, TotalUsage: 10}}
	s := newTestSpendServer(recon, &fakeSpendCapReader{caps: map[string]float64{}}, ids)

	rec := httptest.NewRecorder()
	s.handleSpendOverview(rec, spendRequest())
	var out spendOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.TopIdentities) != 5 {
		t.Fatalf("len(TopIdentities) = %d, want 5", len(out.TopIdentities))
	}
	// Descending by usage: id-e(5), id-d(4), id-c(3), id-b(2), id-a(1); id-f(0) excluded
	// only because there were 6 candidates and it happens to be lowest here — the
	// assertion is on ORDER, not on which literal id is cut.
	wantOrder := []string{"id-f", "id-e", "id-d", "id-c", "id-b"}
	for i, row := range out.TopIdentities {
		if row.IdentityID != wantOrder[i] {
			t.Errorf("row %d = %s, want %s (descending by spend)", i, row.IdentityID, wantOrder[i])
		}
	}
}

// TestSpendOverviewTiebreakIsStable proves two identities with equal lifetime spend order
// identically across repeated calls — the decided tiebreak (identity id), asserted.
func TestSpendOverviewTiebreakIsStable(t *testing.T) {
	ids := []identity.Identity{{ID: "id-zzz", Name: "z"}, {ID: "id-aaa", Name: "a"}}
	keys := []openrouterprovision.KeyRecord{
		{ExternalUser: "id-zzz", Usage: 5},
		{ExternalUser: "id-aaa", Usage: 5},
	}
	recon := &fakeSpendReconciliation{tiles: spendFiveTiles(), keys: keys, credits: openrouterprovision.Credits{TotalCredits: 100, TotalUsage: 10}}
	s := newTestSpendServer(recon, &fakeSpendCapReader{caps: map[string]float64{}}, ids)

	for i := range 3 {
		rec := httptest.NewRecorder()
		s.handleSpendOverview(rec, spendRequest())
		var out spendOverviewResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(out.TopIdentities) != 2 || out.TopIdentities[0].IdentityID != "id-aaa" || out.TopIdentities[1].IdentityID != "id-zzz" {
			t.Fatalf("call %d: order = %+v, want [id-aaa, id-zzz] (lexicographic identity-id tiebreak)", i, out.TopIdentities)
		}
	}
}

// TestSpendOverviewIncludesZeroSpendIdentities proves an identity with a key but no usage
// yet appears with a zero, rather than being omitted.
func TestSpendOverviewIncludesZeroSpendIdentities(t *testing.T) {
	ids := []identity.Identity{{ID: "id-fresh", Name: "fresh@example.com"}}
	keys := []openrouterprovision.KeyRecord{{ExternalUser: "id-fresh", Label: "sk-or-v1-new...000", Usage: 0}}
	recon := &fakeSpendReconciliation{tiles: spendFiveTiles(), keys: keys, credits: openrouterprovision.Credits{TotalCredits: 100, TotalUsage: 10}}
	s := newTestSpendServer(recon, &fakeSpendCapReader{caps: map[string]float64{}}, ids)

	rec := httptest.NewRecorder()
	s.handleSpendOverview(rec, spendRequest())
	var out spendOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.TopIdentities) != 1 {
		t.Fatalf("len(TopIdentities) = %d, want 1 (zero spend is still a row, not an omission)", len(out.TopIdentities))
	}
	if out.TopIdentities[0].LifetimeSpend != 0 {
		t.Errorf("LifetimeSpend = %v, want 0", out.TopIdentities[0].LifetimeSpend)
	}
}

// TestSpendOverviewOverAllocationBoundary proves caps summing to EXACTLY total_credits -
// total_usage do NOT trigger the banner; one cent either side does the decided thing.
func TestSpendOverviewOverAllocationBoundary(t *testing.T) {
	ids := []identity.Identity{{ID: "id-a"}, {ID: "id-b"}}
	credits := openrouterprovision.Credits{TotalCredits: 100, TotalUsage: 20} // available = 80

	cases := []struct {
		name      string
		capA      float64
		capB      float64
		triggered bool
	}{
		{"exactly equal to available: not triggered", 40, 40, false},
		{"one cent under available: not triggered", 39.99, 40, false},
		{"one cent over available: triggered", 40.01, 40, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recon := &fakeSpendReconciliation{tiles: spendFiveTiles(), credits: credits}
			caps := &fakeSpendCapReader{caps: map[string]float64{"id-a": tc.capA, "id-b": tc.capB}}
			s := newTestSpendServer(recon, caps, ids)

			rec := httptest.NewRecorder()
			s.handleSpendOverview(rec, spendRequest())
			var out spendOverviewResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if out.OverAllocation.Triggered != tc.triggered {
				t.Errorf("Triggered = %v, want %v (sum=%v, available=80)", out.OverAllocation.Triggered, tc.triggered, tc.capA+tc.capB)
			}
		})
	}
}

// TestSpendOverviewCarriesNoCredential proves the marshalled response contains no key, no
// hash and no management credential; only the masked label (T-02-11).
func TestSpendOverviewCarriesNoCredential(t *testing.T) {
	ids := []identity.Identity{{ID: "id-a", Name: "a@example.com"}}
	keys := []openrouterprovision.KeyRecord{{ExternalUser: "id-a", Label: "sk-or-v1-aaa...111", Usage: 1, Hash: "hash-should-never-appear"}}
	recon := &fakeSpendReconciliation{tiles: spendFiveTiles(), keys: keys, credits: openrouterprovision.Credits{TotalCredits: 100, TotalUsage: 10}}
	s := newTestSpendServer(recon, &fakeSpendCapReader{caps: map[string]float64{}}, ids)

	rec := httptest.NewRecorder()
	s.handleSpendOverview(rec, spendRequest())
	body := rec.Body.String()
	for _, forbidden := range []string{"hash-should-never-appear", `"hash"`, `"key"`, "Bearer", "ciphertext"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("response leaks %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, "sk-or-v1-aaa...111") {
		t.Errorf("response missing the masked label: %s", body)
	}
}

// TestSpendOverviewRequiresAdminCapability is the server-side fail-closed proof, mirroring
// TestAdminRoutesRefuseNonAdmin: refused for a caller lacking the mount's capability,
// served once the capability is granted. serve_webui_musr.go mounts this route the same
// way.
func TestSpendOverviewRequiresAdminCapability(t *testing.T) {
	ids := []identity.Identity{{ID: testLocalID, Name: "local"}}
	recon := &fakeSpendReconciliation{tiles: spendFiveTiles(), credits: openrouterprovision.Credits{TotalCredits: 100, TotalUsage: 10}}
	s := newTestSpendServer(recon, &fakeSpendCapReader{caps: map[string]float64{}}, ids)
	const govWrite = "governance.write"

	nonAdmin := testDeps("operator-secret")
	gated := RequireCapability(http.HandlerFunc(s.handleSpendOverview), nonAdmin, govWrite)
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, withPrincipal(spendRequest(), testLocalID))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, want 403", rec.Code)
	}

	adminDeps := testDeps("operator-secret")
	adminDeps.Identities = &fakeIdentities{
		known:        map[string]Identity{testLocalID: {ID: testLocalID, Name: "local", Kind: "user"}},
		capabilities: map[string]bool{testLocalID + "|" + govWrite: true},
	}
	gatedAdmin := RequireCapability(http.HandlerFunc(s.handleSpendOverview), adminDeps, govWrite)
	rec2 := httptest.NewRecorder()
	gatedAdmin.ServeHTTP(rec2, withPrincipal(spendRequest(), testLocalID))
	if rec2.Code != http.StatusOK {
		t.Fatalf("admin status = %d, want 200: %s", rec2.Code, rec2.Body.String())
	}
}

// TestSpendOverviewProviderFailureIsIsolated proves a provider error yields a 502-shaped
// response for THIS endpoint only, and does not affect the credit endpoint — a separate
// call reading a different source.
func TestSpendOverviewProviderFailureIsIsolated(t *testing.T) {
	ids := []identity.Identity{{ID: testLocalID, Name: "local"}}
	recon := &fakeSpendReconciliation{tilesErr: errors.New("provider unreachable")}
	s := newTestSpendServer(recon, &fakeSpendCapReader{caps: map[string]float64{}}, ids)
	// Also wire the credit API on the SAME server with working fakes, proving its own
	// route is unaffected by the Overview's failure.
	keys := &fakeCreditKeyStore{hasKey: true, rec: identitykey.Record{Hash: "h", LimitUSD: capUSD(5), LimitReset: "monthly"}}
	s.SetCreditAPI(&fakeCreditSpendReader{spend: map[string]float64{testLocalID: 1}}, keys, &fakeCreditProvider{}, &fakeCreditInvalidator{}, true)

	overviewRec := httptest.NewRecorder()
	s.handleSpendOverview(overviewRec, spendRequest())
	if overviewRec.Code != http.StatusBadGateway {
		t.Fatalf("overview status = %d, want 502: %s", overviewRec.Code, overviewRec.Body.String())
	}

	creditRec := httptest.NewRecorder()
	s.handleGetCredit(creditRec, creditRequest(http.MethodGet, "/api/admin/identities/"+testLocalID+"/credit", ""))
	if creditRec.Code != http.StatusOK {
		t.Fatalf("credit status = %d, want 200 (must be unaffected by the overview's own failure): %s", creditRec.Code, creditRec.Body.String())
	}
}

// A reconciliation failure still answers the generic 502, and now leaves its cause in the
// log. Measured 2026-09-10: a decode error behind this 502 stayed invisible because the
// handler answered without logging anything.
func TestSpendOverviewLogsTheReconciliationFailure(t *testing.T) {
	cause := errors.New("openrouterprovision: analytics query: decode response: metric request_count: not a number")
	for name, recon := range map[string]*fakeSpendReconciliation{
		"kpi windows": {tilesErr: cause},
		"list keys":   {tiles: spendFiveTiles(), keysErr: cause},
		"get credits": {tiles: spendFiveTiles(), creditsErr: cause},
	} {
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(prev) })
			s := newTestSpendServer(recon, &fakeSpendCapReader{caps: map[string]float64{}}, []identity.Identity{{ID: testLocalID, Name: "local"}})

			rec := httptest.NewRecorder()
			s.handleSpendOverview(rec, spendRequest())

			if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "couldn't load the spend overview") {
				t.Fatalf("status = %d body = %s, want the generic 502", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "request_count") {
				t.Fatalf("body leaks the internal cause: %s", rec.Body.String())
			}
			if !strings.Contains(logs.String(), "metric request_count: not a number") {
				t.Fatalf("log = %q, want the reconciliation cause", logs.String())
			}
		})
	}
}

// TestKPIWindowsAreWholeUTCDaysThatShareNone pins the windows to UTC midnight. The provider
// widens a time_range to every UTC day it touches, so windows cut at the current time of day
// both counted the boundary day in full (measured 2026-09-10: 2026-08-29's $2.51 appeared in
// the current AND the prior window). Expected bounds are hand-computed.
func TestKPIWindowsAreWholeUTCDaysThatShareNone(t *testing.T) {
	cest := time.FixedZone("CEST", 2*60*60)
	for name, tc := range map[string]struct {
		now  time.Time
		want [4]string // current start, current end, prior start, prior end
	}{
		"mid-morning":             {time.Date(2026, 9, 10, 11, 42, 7, 0, cest), [4]string{"2026-08-30T00:00:00Z", "2026-09-10T09:42:07Z", "2026-08-18T00:00:00Z", "2026-08-30T00:00:00Z"}},
		"local date ahead of UTC": {time.Date(2026, 9, 10, 1, 0, 0, 0, cest), [4]string{"2026-08-29T00:00:00Z", "2026-09-09T23:00:00Z", "2026-08-17T00:00:00Z", "2026-08-29T00:00:00Z"}},
	} {
		t.Run(name, func(t *testing.T) {
			current, prior := kpiWindows(tc.now)
			if got := [4]string{current.Start, current.End, prior.Start, prior.End}; got != tc.want {
				t.Fatalf("windows = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSpendOverviewRequiresAdminCapability's sibling — unwired ports answer 503.
func TestSpendOverviewUnwiredReturns503(t *testing.T) {
	s := &Server{idAdmin: &fakeIdentityAdmin{identities: []identity.Identity{{ID: testLocalID}}}}
	rec := httptest.NewRecorder()
	s.handleSpendOverview(rec, spendRequest())
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// TestSpendOverviewIsNotIdempotencyRegistered proves the route is absent from
// httpMutationRoutes — a read is not a mutation, and inventorying it would give it a
// replay key it has no use for.
func TestSpendOverviewIsNotIdempotencyRegistered(t *testing.T) {
	if _, ok := httpMutationRoutes["GET /api/admin/spend/overview"]; ok {
		t.Fatal("GET /api/admin/spend/overview must NOT be registered in httpMutationRoutes")
	}
}

func TestSpendOverviewCountsUncappedKeys(t *testing.T) {
	ids := []identity.Identity{{ID: "id-admin", Name: "admin"}, {ID: "id-member", Name: "member"}}
	recon := &fakeSpendReconciliation{tiles: spendFiveTiles(), credits: openrouterprovision.Credits{TotalCredits: 100, TotalUsage: 10}}
	caps := &fakeSpendCapReader{caps: map[string]float64{"id-member": 5}, uncapped: map[string]bool{"id-admin": true}}
	s := newTestSpendServer(recon, caps, ids)

	rec := httptest.NewRecorder()
	s.handleSpendOverview(rec, spendRequest())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out spendOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.OverAllocation.SumCaps != 5 || out.OverAllocation.UncappedKeys != 1 {
		t.Fatalf("over_allocation = %+v, want sum_caps 5 and uncapped_keys 1", out.OverAllocation)
	}
}
