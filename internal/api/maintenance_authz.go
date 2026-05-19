package api

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

// AuthzDecisionsReader aggregates authz_decisions for the operator dashboard.
type AuthzDecisionsReader interface {
	AuthzStats(ctx context.Context, window time.Duration) (AuthzStats, error)
}

// AuthzStats is the aggregated view returned by GET /maintenance/authz.
type AuthzStats struct {
	DenialRate24h         float64
	TopDeniedCapabilities []CapabilityCount
	RecentDenials         []RecentDenial
}

// SQLiteAuthzReader reads authz_decisions from a *sql.DB.
type SQLiteAuthzReader struct {
	db *sql.DB
}

// NewSQLiteAuthzReader wraps an existing connection pool.
func NewSQLiteAuthzReader(db *sql.DB) *SQLiteAuthzReader {
	return &SQLiteAuthzReader{db: db}
}

func (r *SQLiteAuthzReader) AuthzStats(ctx context.Context, window time.Duration) (AuthzStats, error) {
	cutoff := time.Now().UTC().Add(-window).Format(time.RFC3339)

	// denial_rate_24h — ratio of denials over all decisions in the window.
	var total, denied int
	err := r.db.QueryRowContext(ctx, `
SELECT
  COUNT(*) AS total,
  SUM(CASE WHEN decision = 'denied' THEN 1 ELSE 0 END) AS denied
FROM authz_decisions
WHERE created_at >= ?`, cutoff).Scan(&total, &denied)
	if err != nil {
		return AuthzStats{}, err
	}

	var rate float64
	if total > 0 {
		rate = float64(denied) / float64(total)
	}

	// top_denied_capabilities — sorted descending by count.
	capRows, err := r.db.QueryContext(ctx, `
SELECT capability, COUNT(*) AS n
FROM authz_decisions
WHERE decision = 'denied' AND created_at >= ?
GROUP BY capability
ORDER BY n DESC
LIMIT 20`, cutoff)
	if err != nil {
		return AuthzStats{}, err
	}
	defer capRows.Close()

	var caps []CapabilityCount
	for capRows.Next() {
		var row CapabilityCount
		if err := capRows.Scan(&row.Capability, &row.Count); err != nil {
			return AuthzStats{}, err
		}
		caps = append(caps, row)
	}
	if err := capRows.Err(); err != nil {
		return AuthzStats{}, err
	}

	// recent_denials — last 50 denied decisions.
	denialRows, err := r.db.QueryContext(ctx, `
SELECT actor_id, capability, reason, created_at
FROM authz_decisions
WHERE decision = 'denied'
ORDER BY created_at DESC
LIMIT 50`)
	if err != nil {
		return AuthzStats{}, err
	}
	defer denialRows.Close()

	var denials []RecentDenial
	for denialRows.Next() {
		var row RecentDenial
		if err := denialRows.Scan(&row.ActorID, &row.Capability, &row.Reason, &row.CreatedAt); err != nil {
			return AuthzStats{}, err
		}
		denials = append(denials, row)
	}
	if err := denialRows.Err(); err != nil {
		return AuthzStats{}, err
	}

	return AuthzStats{
		DenialRate24h:         rate,
		TopDeniedCapabilities: caps,
		RecentDenials:         denials,
	}, nil
}

func handleMaintenanceAuthz(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.AuthzDecisions == nil {
			writeJSON(w, deps.Logger, http.StatusOK, AuthzResponse{
				DenialRate24h:         0,
				TopDeniedCapabilities: []CapabilityCount{},
				RecentDenials:         []RecentDenial{},
			})
			return
		}

		stats, err := deps.AuthzDecisions.AuthzStats(r.Context(), 24*time.Hour)
		if err != nil {
			deps.Logger.Warn("api: authz stats query", "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to query authz decisions")
			return
		}

		writeJSON(w, deps.Logger, http.StatusOK, AuthzResponse(stats))
	}
}
