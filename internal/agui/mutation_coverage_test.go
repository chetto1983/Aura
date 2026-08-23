package agui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/idempotency"
)

func TestHTTPMutationCoverageHasCompleteIdempotencyMetadata(t *testing.T) {
	t.Parallel()

	if err := validateHTTPMutationInventory(); err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{
		"POST /agent/run",
		// The run-scoped explicit Stop (fix-plan 1.3 Tier B). Its sibling
		// GET /agent/runs/{runID}/events is read-only by construction and thus
		// deliberately absent — the sweep below is method-based, so GETs never
		// require inventory entries (the graph-query/TTS/STT rationale).
		"POST /agent/runs/{runID}/cancel",
		"POST /api/approvals/{token}/resolve",
		"POST /api/conversations",
		"DELETE /api/conversations/{id}",
		"POST /api/assets/{id}/finalize",
		"POST /api/governance/mcp",
		"POST /api/governance/scheduler/{id}/run",
	} {
		meta, ok := httpMutationRoutes[route]
		if !ok {
			t.Errorf("mutating route %q is absent from the idempotency inventory", route)
			continue
		}
		if meta.Scope != idempotency.ScopeHTTPMutation || meta.Normalize == "" || meta.KeyPolicy != keyPolicyRequiredHeader {
			t.Errorf("mutating route %q has incomplete metadata: %+v", route, meta)
		}
	}
}

func TestEveryRegisteredUnsafeHTTPRouteIsClassified(t *testing.T) {
	t.Parallel()

	// POSTs that mutate NOTHING. They are POSTs only because they carry a body too large
	// or too structured for a query string — a replay changes no state, so an idempotency
	// key would be ceremony over a no-op. Anything added here must be provably write-free:
	// the skills validate route, for instance, dry-runs the write boundary and is asserted
	// to leave no skill and no audit row behind
	// (TestGovernanceWriteSkillsValidateDryRun).
	readOnlyPOST := map[string]bool{
		"POST /api/graph/query":                true,
		"POST /api/settings/telegram/check":    true,
		"POST /api/stt":                        true,
		"POST /api/tts":                        true,
		"POST /api/governance/skills/validate": true,
	}
	matcher := regexp.MustCompile(`mux\.Handle(?:Func)?\("((?:POST|PUT|PATCH|DELETE) [^"]+)"`)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		source, err := os.ReadFile(filepath.Clean(entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range matcher.FindAllSubmatch(source, -1) {
			route := string(match[1])
			if readOnlyPOST[route] {
				continue
			}
			if _, ok := httpMutationRoutes[route]; !ok {
				t.Errorf("unsafe registered route %q lacks idempotency metadata", route)
			}
		}
	}
}

func validateHTTPMutationInventory() error {
	for route, meta := range httpMutationRoutes {
		if meta.Scope != idempotency.ScopeHTTPMutation || meta.Normalize == "" || meta.KeyPolicy != keyPolicyRequiredHeader {
			return fmt.Errorf("mutation route %q has incomplete idempotency metadata", route)
		}
	}
	return nil
}
