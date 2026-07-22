package agui

import (
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
		"POST /api/approvals/{token}/resolve",
		"POST /api/conversations",
		"DELETE /api/conversations/{id}",
		"POST /api/documents",
		"PATCH /api/documents/{id}",
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

	readOnlyPOST := map[string]bool{
		"POST /api/graph/query":             true,
		"POST /api/settings/telegram/check": true,
		"POST /api/stt":                     true,
		"POST /api/tts":                     true,
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
