package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
)

// fakeWarnings is a test double for attempts.WarningsReader.
type fakeWarnings struct {
	rows []attempts.WarningRow
	err  error
}

func (f *fakeWarnings) Warnings(_ context.Context, _ int) ([]attempts.WarningRow, error) {
	return f.rows, f.err
}

func toolWarningsRouter(t *testing.T, admin bool, tw attempts.WarningsReader) http.Handler {
	t.Helper()
	return NewRouter(Deps{
		SkillsAdmin:  admin,
		ToolWarnings: tw,
	})
}

// TestToolWarnings_AdminGate_Forbidden verifies that non-admin requests get 403.
func TestToolWarnings_AdminGate_Forbidden(t *testing.T) {
	router := toolWarningsRouter(t, false, &fakeWarnings{})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/tool-warnings", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", rr.Code, rr.Body)
	}
}

// TestToolWarnings_NilRepo_EmptyArray verifies that a nil ToolWarnings returns
// an empty warnings array, not 503 or 404.
func TestToolWarnings_NilRepo_EmptyArray(t *testing.T) {
	router := NewRouter(Deps{SkillsAdmin: true, ToolWarnings: nil})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/tool-warnings", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body)
	}
	var resp ToolWarningsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Warnings == nil {
		t.Error("want non-nil empty array, got nil (would JSON-encode as null)")
	}
	if len(resp.Warnings) != 0 {
		t.Errorf("want 0 warnings, got %d", len(resp.Warnings))
	}
}

// TestToolWarnings_EmptyRepo_EmptyArray verifies that a repo with no rows
// returns an empty warnings array.
func TestToolWarnings_EmptyRepo_EmptyArray(t *testing.T) {
	router := toolWarningsRouter(t, true, &fakeWarnings{rows: nil})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/tool-warnings", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body)
	}
	var resp ToolWarningsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Warnings) != 0 {
		t.Errorf("want empty warnings, got %+v", resp.Warnings)
	}
}

// TestToolWarnings_MixedNativeAndMCP verifies both tool kinds appear in the
// response and that the kind field is populated correctly.
func TestToolWarnings_MixedNativeAndMCP(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	rows := []attempts.WarningRow{
		{ToolName: "web_fetch", ToolKind: "native", Class: "io", N: 5, LastSeen: now},
		{ToolName: "mcp_github_search", ToolKind: "mcp", Class: "rate_limited", N: 3, LastSeen: now.Add(-10 * time.Minute)},
	}
	router := toolWarningsRouter(t, true, &fakeWarnings{rows: rows})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/tool-warnings", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body)
	}
	var resp ToolWarningsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Warnings) != 2 {
		t.Fatalf("want 2 warnings, got %d: %+v", len(resp.Warnings), resp.Warnings)
	}
	// First row: web_fetch (native)
	if resp.Warnings[0].Kind != "native" {
		t.Errorf("warnings[0].kind = %q, want native", resp.Warnings[0].Kind)
	}
	if resp.Warnings[0].Tool != "web_fetch" {
		t.Errorf("warnings[0].tool = %q, want web_fetch", resp.Warnings[0].Tool)
	}
	if resp.Warnings[0].N != 5 {
		t.Errorf("warnings[0].n = %d, want 5", resp.Warnings[0].N)
	}
	// Second row: mcp_github_search (mcp)
	if resp.Warnings[1].Kind != "mcp" {
		t.Errorf("warnings[1].kind = %q, want mcp", resp.Warnings[1].Kind)
	}
	if resp.Warnings[1].Tool != "mcp_github_search" {
		t.Errorf("warnings[1].tool = %q, want mcp_github_search", resp.Warnings[1].Tool)
	}
}

// TestToolWarnings_SortedByNDesc verifies that the response preserves the
// order returned by the repo (n DESC — the SQL enforces this; here we verify
// the handler doesn't re-sort or truncate).
func TestToolWarnings_SortedByNDesc(t *testing.T) {
	now := time.Now().UTC()
	rows := []attempts.WarningRow{
		{ToolName: "tool_a", ToolKind: "native", Class: "io", N: 10, LastSeen: now},
		{ToolName: "tool_b", ToolKind: "native", Class: "io", N: 7, LastSeen: now},
		{ToolName: "tool_c", ToolKind: "native", Class: "io", N: 3, LastSeen: now},
	}
	router := toolWarningsRouter(t, true, &fakeWarnings{rows: rows})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/tool-warnings", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	var resp ToolWarningsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Warnings) != 3 {
		t.Fatalf("want 3, got %d", len(resp.Warnings))
	}
	if resp.Warnings[0].N < resp.Warnings[1].N || resp.Warnings[1].N < resp.Warnings[2].N {
		t.Errorf("order not n DESC: %v %v %v",
			resp.Warnings[0].N, resp.Warnings[1].N, resp.Warnings[2].N)
	}
}

// TestToolWarnings_LastSeenIsRFC3339 verifies the last_seen field is RFC3339.
func TestToolWarnings_LastSeenIsRFC3339(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	rows := []attempts.WarningRow{
		{ToolName: "web_fetch", ToolKind: "native", Class: "io", N: 1, LastSeen: now},
	}
	router := toolWarningsRouter(t, true, &fakeWarnings{rows: rows})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/tool-warnings", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	var resp ToolWarningsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Warnings) != 1 {
		t.Fatalf("want 1 warning, got %d", len(resp.Warnings))
	}
	parsed, err := time.Parse(time.RFC3339, resp.Warnings[0].LastSeen)
	if err != nil {
		t.Fatalf("last_seen %q is not RFC3339: %v", resp.Warnings[0].LastSeen, err)
	}
	if !parsed.Equal(now) {
		t.Errorf("last_seen %v != original %v", parsed, now)
	}
}
