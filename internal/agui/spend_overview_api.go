package agui

// spend_overview_api.go implements GET /api/admin/spend/overview (RBAC-11/CRED-06, plan
// 02-09) — the account-wide reconciliation surface the approved UI-SPEC's §Admin spend
// dashboard specifies: five KPI tiles with sparklines and prior-period deltas, a
// Top-Identities-by-spend ranked list, and the over-allocation advisory. Everything the
// Overview renders comes from ONE call, on credit_api.go's own conventions: ports declared
// consumer-side, a Set*-after-construct wiring method, 503 until wired.
//
// The management credential NEVER reaches this file's response. It crosses exactly one
// boundary — the daemon's own calls to GET /keys, GET /credits and POST /analytics/query,
// made by the spendReconciliation adapter this file only calls through an interface — and
// this file never touches a provider key, a hash, or an Authorization header (T-02-11).
// TestSpendOverviewCarriesNoCredential asserts this on the marshalled response.
//
// This is reconciliation tier (D-08): the KPI figures and the ranked list's "Lifetime
// spend" come from OpenRouter's own analytics/roster, refreshed periodically, and are
// NEVER the number CRED-05's pre-flight refusal or CRED-06's Credit panel read — those stay
// on Aura's in-band ledger. The two will legitimately disagree during the provider's
// measured 30-40s lag (M-07) and across a reset boundary; that is by design, not a bug.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/openrouterprovision"
)

// kpiWindowDays is the number of whole UTC days each of the Overview's five KPI tiles
// covers, and the most points its sparkline can have (02-UI-SPEC.md §KPI row: "a 12-point
// inline SVG polyline"): a day with no OpenRouter traffic has no bucket, so no point. The
// prior window is the SAME number of days, immediately preceding it, so the two calls
// KPIWindows makes are of equal length per 02-UI-SPEC.md's own data-source ledger.
const kpiWindowDays = 12

// spendReconciliation is the three OpenRouter reconciliation calls (COVERAGE.md INTEGRATE,
// plan 02-09) this endpoint needs — GET /keys, GET /credits, POST /analytics/query —
// bundled behind one port so a fake proves this file's join/rank/over-allocation logic
// with no live provider and no management credential in the test binary at all.
type spendReconciliation interface {
	ListKeys(ctx context.Context) ([]openrouterprovision.KeyRecord, error)
	GetCredits(ctx context.Context) (openrouterprovision.Credits, error)
	KPIWindows(ctx context.Context, current, prior openrouterprovision.TimeRange) ([]openrouterprovision.KPITile, error)
}

// spendCapReader matches identitykey.Store's own Load signature exactly (same shape
// credit_api.go's creditKeyStore already declares for the identical method) — the
// over-allocation sum needs each identity's OWN stored cap, read scoped to that identity
// via identityctx.WithIdentityID, never the provider's own recorded limit (which the RLS
// floor on aura.identity_llm_key would not let a caller enumerate across identities
// without exactly this per-identity scoping anyway).
type spendCapReader interface {
	Load(ctx context.Context) (identitykey.Record, error)
}

// This file needs one more read — the identity roster, to turn a key row's external_user
// (an Aura identity id, set at mint by plan 02-06) into a display name — but declares no
// second identity seam for it: Server.idAdmin (audit_api.go's identityAdmin port) already
// exposes ListIdentities, and handleSpendOverview calls it directly at the call site.

// spendOverviewPorts bundles this file's own dependencies (Server gains one field, not
// three). now is the injectable clock the KPI window boundaries are computed from —
// tests override it for a deterministic window; production leaves it nil and gets
// time.Now.
type spendOverviewPorts struct {
	reconciliation spendReconciliation
	caps           spendCapReader
	now            func() time.Time
}

func (p *spendOverviewPorts) clock() time.Time {
	if p == nil || p.now == nil {
		return time.Now()
	}
	return p.now()
}

// SetSpendOverview wires the Overview's reconciliation ports. Until called, the route
// answers 503 — the SetCreditAPI/SetAuditStore precedent this whole package follows.
func (s *Server) SetSpendOverview(reconciliation spendReconciliation, caps spendCapReader, now func() time.Time) {
	s.spendOverview = &spendOverviewPorts{reconciliation: reconciliation, caps: caps, now: now}
}

func (s *Server) registerSpendOverviewRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/spend/overview", s.handleSpendOverview)
}

// spendOverviewResponse is GET /api/admin/spend/overview's whole body — everything the
// Overview renders in ONE round trip (02-UI-SPEC.md's own reasoning: a second call per
// panel is a request storm on a page that also loads the roster and a credit panel per
// row). No field here is key-shaped; see the file header for the T-02-11 boundary.
type spendOverviewResponse struct {
	KPIs           []spendKPIDTO          `json:"kpis"`
	TopIdentities  []spendIdentityDTO     `json:"top_identities"`
	OverAllocation spendOverAllocationDTO `json:"over_allocation"`
}

type spendKPIDTO struct {
	Metric string    `json:"metric"`
	Value  float64   `json:"value"`
	Delta  *float64  `json:"delta_percent"`
	Series []float64 `json:"series"`
}

// spendIdentityDTO is one Top-Identities-by-spend row. MaskedLabel is OpenRouter's own
// masked form (e.g. "sk-or-v1-caa...61c") — safe to display, and the only key-adjacent
// field this DTO carries.
type spendIdentityDTO struct {
	IdentityID    string  `json:"identity_id"`
	Name          string  `json:"name"`
	MaskedLabel   string  `json:"masked_label"`
	LifetimeSpend float64 `json:"lifetime_spend"`
}

// spendOverAllocationDTO is the M-12 advisory. Triggered is decided by STRICT inequality
// (backstop decision, see buildOverAllocation's own comment) — the boundary case where
// Σ(cap) exactly equals the available pool is NOT an over-allocation, because every cap
// can still be honored in full at that exact point.
type spendOverAllocationDTO struct {
	Triggered bool    `json:"triggered"`
	SumCaps   float64 `json:"sum_caps"`
	Available float64 `json:"available"`
}

func (s *Server) handleSpendOverview(w http.ResponseWriter, r *http.Request) {
	if s.spendOverview == nil || s.spendOverview.reconciliation == nil || s.spendOverview.caps == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "spend overview not configured"})
		return
	}
	if s.idAdmin == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "identity admin not configured"})
		return
	}
	ctx := r.Context()

	identities, err := s.idAdmin.ListIdentities(ctx)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "identity store unavailable"})
		return
	}

	current, prior := kpiWindows(s.spendOverview.clock())
	tiles, err := s.spendOverview.reconciliation.KPIWindows(ctx, current, prior)
	if err != nil {
		// Isolated by construction: this is the ONLY call this handler makes for the KPI
		// row, and its failure never touches the credit or roster endpoints — those are
		// separate handlers reading a different source (D-08).
		failSpendReconciliation(w, err)
		return
	}
	keys, err := s.spendOverview.reconciliation.ListKeys(ctx)
	if err != nil {
		failSpendReconciliation(w, err)
		return
	}
	credits, err := s.spendOverview.reconciliation.GetCredits(ctx)
	if err != nil {
		failSpendReconciliation(w, err)
		return
	}

	sumCaps, err := s.sumIdentityCaps(ctx, identities)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "credit store unavailable"})
		return
	}

	writeJSON(w, spendOverviewResponse{
		KPIs:           kpiDTOsFrom(tiles),
		TopIdentities:  topIdentitiesFrom(identities, keys),
		OverAllocation: buildOverAllocation(sumCaps, credits),
	})
}

// sumIdentityCaps sums EVERY roster identity's own stored OpenRouter cap. Each read is
// scoped to that ONE identity (identityctx.WithIdentityID) — aura.identity_llm_key's RLS
// floor (migration 0122) admits only the identity named by app.current_identity, so this
// is N scoped reads, not one unscoped enumeration. An identity with no key contributes
// zero rather than failing the whole sum (identitykey.ErrNoKey is a normal, expected
// answer per that package's own doc — a deployment with some identities still on a local
// backend, or provisioned before credit minting existed, is not an error).
func (s *Server) sumIdentityCaps(ctx context.Context, identities []identity.Identity) (float64, error) {
	var sum float64
	for _, idn := range identities {
		scoped := identityctx.WithIdentityID(ctx, idn.ID)
		rec, err := s.spendOverview.caps.Load(scoped)
		if err != nil {
			if errors.Is(err, identitykey.ErrNoKey) {
				continue
			}
			return 0, err
		}
		sum += rec.LimitUSD
	}
	return sum, nil
}

// failSpendReconciliation answers a provider-side failure with the generic 502 and keeps the
// cause in the log only: the body must not carry provider internals, and without the log a
// decode error behind this 502 was invisible (measured 2026-09-10).
func failSpendReconciliation(w http.ResponseWriter, err error) {
	slog.Error("aura admin: spend overview reconciliation failed", "err", err)
	writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "couldn't load the spend overview"})
}

// kpiWindows derives the current and prior windows as kpiWindowDays whole UTC days each,
// back to back (02-UI-SPEC.md: "a second analytics/query call over the prior window of equal
// length"); the current one runs to now, so its last day is today so far. Whole days because
// the provider widens a time_range to every UTC day it touches: measured 2026-09-10,
// 12:00-13:00Z on 2026-08-29 returned that day's full 677 requests, while an end at exactly
// midnight leaves the next day out. A boundary at the current time of day therefore landed
// the same day, in full, in both windows.
func kpiWindows(now time.Time) (current, prior openrouterprovision.TimeRange) {
	end := now.UTC()
	today := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	start := today.AddDate(0, 0, -(kpiWindowDays - 1))
	priorStart := start.AddDate(0, 0, -kpiWindowDays)
	return openrouterprovision.NewTimeRange(start, end), openrouterprovision.NewTimeRange(priorStart, start)
}

// kpiDTOsFrom is a pure projection: it never drops or substitutes a tile. The partial-
// metrics backstop (UI-SPEC "Overview KPI row · overflow" — "when the analytics query
// returns some metrics but not others... a tile renders a placeholder or the whole row
// degrades") is decided ONE LAYER DOWN, in openrouterprovision.KPIWindows: EACH TILE
// DEGRADES INDEPENDENTLY, never the whole row. AnalyticsRow is a map[string]float64 keyed
// by metric name, so a metric the provider's response omits for a given day-bucket reads
// as that metric's zero value for that bucket ONLY (Go's normal map-lookup zero value) —
// every OTHER tile for that same bucket, and every other bucket for THIS metric, is
// unaffected. The alternative (dropping the whole row on any partial response) would turn
// a single missing field into a blank dashboard; per-tile, per-bucket degradation confines
// the damage to the one number the provider actually omitted.
func kpiDTOsFrom(tiles []openrouterprovision.KPITile) []spendKPIDTO {
	out := make([]spendKPIDTO, 0, len(tiles))
	for _, tile := range tiles {
		out = append(out, spendKPIDTO{Metric: tile.Metric, Value: tile.Value, Delta: tile.Delta, Series: tile.Series})
	}
	return out
}

// topIdentitiesFrom joins the roster to the provider's key roster by external_user (set
// to the identity id at mint, plan 02-06) and ranks by lifetime spend descending, capped
// at 5. Iterating the ROSTER (not the provider's key list) as the base is what makes an
// identity with no usage yet (or no key at all) still appear at zero rather than being
// omitted (Top Identities by spend · partial) — and what makes a provider key whose
// external_user matches NO known identity contribute to nobody's row rather than being
// silently folded into one (the join only ever looks an identity's OWN id up in the key
// map; an orphaned key is simply never looked up by anybody).
//
// Tiebreak (backstop decision): identity id — the one key every identity always has,
// stable across refreshes. Two identities tied on lifetime spend therefore order the
// same way every time, so a reader can tell a genuine re-rank from a refresh that changed
// nothing.
func topIdentitiesFrom(identities []identity.Identity, keys []openrouterprovision.KeyRecord) []spendIdentityDTO {
	byExternalUser := make(map[string]openrouterprovision.KeyRecord, len(keys))
	for _, k := range keys {
		if k.ExternalUser != "" {
			byExternalUser[k.ExternalUser] = k
		}
	}

	rows := make([]spendIdentityDTO, 0, len(identities))
	for _, idn := range identities {
		row := spendIdentityDTO{IdentityID: idn.ID, Name: SanitizeString(idn.Name)}
		if key, ok := byExternalUser[idn.ID]; ok {
			row.MaskedLabel = key.Label
			row.LifetimeSpend = key.Usage
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].LifetimeSpend != rows[j].LifetimeSpend {
			return rows[i].LifetimeSpend > rows[j].LifetimeSpend
		}
		return rows[i].IdentityID < rows[j].IdentityID
	})
	const topN = 5
	if len(rows) > topN {
		rows = rows[:topN]
	}
	return rows
}

// buildOverAllocation decides the M-12 trigger by STRICT inequality (backstop decision,
// UI-SPEC "Over-allocation advisory banner · zero-one-many"): Σ(cap) EXCEEDING the
// available pool, not merely reaching it. At exact equality every assigned cap can still
// be honored in full if every identity spends up to its own cap simultaneously — nothing
// is starved YET, so `>` (not `>=`) is the correct boundary. `>=` would warn about a
// perfectly sustainable allocation the day it happens to sum to exactly the pool.
func buildOverAllocation(sumCaps float64, credits openrouterprovision.Credits) spendOverAllocationDTO {
	available := credits.TotalCredits - credits.TotalUsage
	return spendOverAllocationDTO{
		Triggered: sumCaps > available,
		SumCaps:   sumCaps,
		Available: available,
	}
}
