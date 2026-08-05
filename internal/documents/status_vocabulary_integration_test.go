//go:build db_integration

// The Go status vocabularies and the database CHECK constraints that close them.
//
// This test exists because the two drifted apart and nothing noticed. The production
// recorder wrote "processing" for a document version, which aura.document_versions_status_check
// has never admitted since migration 0025 — so RecordAssetVersion could not insert a row
// at all, and the live catalog reported three ready documents behind a single version.
// A constant that no longer matches its constraint is a 23514 in production and a green
// test suite everywhere else; this is the assertion that makes that divergence a red build.

package documents

import (
	"context"
	"maps"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// checkLiteral matches the quoted values PostgreSQL renders back inside a CHECK
// definition: status = ANY (ARRAY['ready'::text, 'failed'::text, ...]).
var checkLiteral = regexp.MustCompile(`'([a-z_]+)'::text`)

func TestDocumentVocabulariesMatchTheDatabase(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	documentValues := make([]string, 0, len(AllDocumentStatuses))
	for _, status := range AllDocumentStatuses {
		documentValues = append(documentValues, string(status))
	}
	versionValues := make([]string, 0, len(AllDocumentVersionStatuses))
	for _, status := range AllDocumentVersionStatuses {
		versionValues = append(versionValues, string(status))
	}

	for _, tc := range []struct {
		table      string
		constraint string
		declared   []string
	}{
		{"aura.documents", "documents_status_check", documentValues},
		{"aura.document_versions", "document_versions_status_check", versionValues},
	} {
		admitted := constraintValues(t, ctx, pool, tc.table, tc.constraint)
		declared := slices.Sorted(slices.Values(tc.declared))
		if !slices.Equal(declared, admitted) {
			t.Errorf("%s admits %v; Go declares %v", tc.constraint, admitted, declared)
		}
	}
}

// constraintValues returns the sorted literals one CHECK constraint admits, read from
// the migrated database rather than from the .sql files: the files are a history of
// ALTERs and only the server knows which one won.
func constraintValues(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	table, constraint string,
) []string {
	t.Helper()
	var def string
	if err := pool.QueryRow(ctx,
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint
		 WHERE conrelid = $1::regclass AND conname = $2`,
		table, constraint).Scan(&def); err != nil {
		t.Fatalf("read %s on %s: %v", constraint, table, err)
	}
	seen := make(map[string]struct{})
	for _, match := range checkLiteral.FindAllStringSubmatch(def, -1) {
		seen[match[1]] = struct{}{}
	}
	if len(seen) == 0 {
		t.Fatalf("%s parsed to no values from %q", constraint, def)
	}
	return slices.Sorted(maps.Keys(seen))
}
