package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/testutil"
)

// ensureActorExists inserts a principal + actor row if they don't exist yet.
func ensureActorExists(t *testing.T, reader *SQLiteAuthzReader, actorID string) {
	t.Helper()
	principalID := "principal:" + actorID
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := reader.db.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO principals (id, kind, created_at) VALUES (?, 'human', ?)`,
		principalID, now,
	)
	if err != nil {
		t.Fatalf("ensureActorExists principal: %v", err)
	}
	_, err = reader.db.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO actors (id, principal_id, actor_type, created_at) VALUES (?, ?, 'session', ?)`,
		actorID, principalID, now,
	)
	if err != nil {
		t.Fatalf("ensureActorExists actor: %v", err)
	}
}

// seedAuthzDecision inserts one row into authz_decisions. Caller must ensure
// the actor row exists first (call ensureActorExists).
func seedAuthzDecision(t *testing.T, reader *SQLiteAuthzReader, id, actorID, capability, decision, reason string, createdAt time.Time) {
	t.Helper()
	_, err := reader.db.ExecContext(context.Background(),
		`INSERT INTO authz_decisions (id, actor_id, capability, decision, reason, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, actorID, capability, decision, reason, createdAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("seedAuthzDecision: %v", err)
	}
}

func newAuthzTestReader(t *testing.T) *SQLiteAuthzReader {
	t.Helper()
	db := testutil.OpenTestDB(t, migrations.Run)
	return NewSQLiteAuthzReader(db)
}

func TestHandleMaintenanceAuthz_ShapeAndCounts(t *testing.T) {
	reader := newAuthzTestReader(t)
	now := time.Now().UTC()

	ensureActorExists(t, reader, "actor-alice")

	// Seed 10 rows: 6 denied, 4 allowed, within last 24h.
	for i := 0; i < 6; i++ {
		seedAuthzDecision(t, reader,
			fmt.Sprintf("denied-%d", i),
			"actor-alice",
			"skills.install",
			"denied",
			"missing grant",
			now.Add(-time.Duration(i)*time.Hour),
		)
	}
	for i := 0; i < 4; i++ {
		seedAuthzDecision(t, reader,
			fmt.Sprintf("allowed-%d", i),
			"actor-alice",
			"api.chat",
			"allowed",
			"",
			now.Add(-time.Duration(i)*time.Hour),
		)
	}

	router := NewRouter(Deps{AuthzDecisions: reader})
	req := httptest.NewRequest("GET", "/maintenance/authz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var body AuthzResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// denial_rate_24h = 6/10 = 0.6
	const wantRate = 0.6
	if got := body.DenialRate24h; got < wantRate-0.001 || got > wantRate+0.001 {
		t.Errorf("denial_rate_24h = %f, want ~%f", got, wantRate)
	}

	// top_denied_capabilities: only "skills.install" with count 6.
	if len(body.TopDeniedCapabilities) != 1 {
		t.Fatalf("want 1 capability, got %d: %+v", len(body.TopDeniedCapabilities), body.TopDeniedCapabilities)
	}
	if body.TopDeniedCapabilities[0].Capability != "skills.install" {
		t.Errorf("capability = %q, want %q", body.TopDeniedCapabilities[0].Capability, "skills.install")
	}
	if body.TopDeniedCapabilities[0].Count != 6 {
		t.Errorf("count = %d, want 6", body.TopDeniedCapabilities[0].Count)
	}

	// recent_denials: 6 rows.
	if len(body.RecentDenials) != 6 {
		t.Errorf("want 6 recent_denials, got %d", len(body.RecentDenials))
	}

	// Each denial has all 4 required keys non-empty.
	for i, d := range body.RecentDenials {
		if d.ActorID == "" {
			t.Errorf("recent_denials[%d].actor_id is empty", i)
		}
		if d.Capability == "" {
			t.Errorf("recent_denials[%d].capability is empty", i)
		}
		if d.CreatedAt == "" {
			t.Errorf("recent_denials[%d].created_at is empty", i)
		}
	}
}

func TestHandleMaintenanceAuthz_NilReader(t *testing.T) {
	router := NewRouter(Deps{AuthzDecisions: nil})
	req := httptest.NewRequest("GET", "/maintenance/authz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 with nil reader, got %d", w.Code)
	}

	var body AuthzResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.DenialRate24h != 0 {
		t.Errorf("want zero rate, got %f", body.DenialRate24h)
	}
	if len(body.TopDeniedCapabilities) != 0 {
		t.Errorf("want empty caps, got %d", len(body.TopDeniedCapabilities))
	}
	if len(body.RecentDenials) != 0 {
		t.Errorf("want empty denials, got %d", len(body.RecentDenials))
	}
}

func TestHandleMaintenanceAuthz_OldRowsExcludedFromRate(t *testing.T) {
	reader := newAuthzTestReader(t)
	now := time.Now().UTC()

	ensureActorExists(t, reader, "actor-bob")

	// 1 denial inside the 24h window.
	seedAuthzDecision(t, reader, "new-denied", "actor-bob", "settings.write", "denied", "no grant", now.Add(-1*time.Hour))
	// 5 old denials outside the 24h window (should not affect the rate).
	for i := 0; i < 5; i++ {
		seedAuthzDecision(t, reader,
			fmt.Sprintf("old-denied-%d", i),
			"actor-bob",
			"settings.write",
			"denied",
			"no grant",
			now.Add(-48*time.Hour),
		)
	}

	router := NewRouter(Deps{AuthzDecisions: reader})
	req := httptest.NewRequest("GET", "/maintenance/authz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}

	var body AuthzResponse
	json.NewDecoder(w.Body).Decode(&body)

	// Only 1 row in the window, it's denied => rate = 1.0
	if got := body.DenialRate24h; got < 0.99 {
		t.Errorf("denial_rate_24h = %f, want 1.0 (only recent row is denied)", got)
	}

	// recent_denials counts ALL denied rows, not just within the 24h window.
	if len(body.RecentDenials) != 6 {
		t.Errorf("want 6 recent_denials (all denied), got %d", len(body.RecentDenials))
	}
}
