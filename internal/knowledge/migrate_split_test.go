package knowledge

import (
	"strings"
	"testing"
)

// TestSplitCypherStatementsIgnoresSemicolonsInComments pins the bug that broke migration
// 0006 on first apply. The splitter cut the body at a ';' inside a // comment; the tail of
// that comment lost its '//' prefix along with the line it started on, and Neo4j was asked
// to execute a fragment of English prose:
//
//	apply migration 0006: Neo4jError: Invalid input 'this': expected 'FOREACH', 'ALTER', …
//	"this catches the ones"
//
// Prose in a comment is the most natural place for a semicolon to appear, so this is not
// an exotic input — it is what happens the first time someone explains a migration
// properly.
func TestSplitCypherStatementsIgnoresSemicolonsInComments(t *testing.T) {
	body := `// -- migrate:up
// New facts are linked at write time (long_term._link_fact_to_subject_entity); this
// catches the ones already stored.

MATCH (u:User)-[:HAS_FACT]->(f:Fact)
MATCH (u)-[:HAS_ENTITY]->(e:Entity)
WHERE toLower(e.name) = toLower(f.subject)
MERGE (f)-[:ABOUT_SUBJECT]->(e);
`

	statements := splitCypherStatements(body)

	if len(statements) != 1 {
		t.Fatalf("got %d statements, want 1:\n%q", len(statements), statements)
	}
	if !strings.HasPrefix(statements[0], "MATCH (u:User)") {
		t.Fatalf("statement does not start at the Cypher: %q", statements[0])
	}
	if strings.Contains(statements[0], "catches the ones") {
		t.Fatalf("comment prose leaked into the statement: %q", statements[0])
	}
}

func TestSplitCypherStatementsKeepsRealStatementsSeparate(t *testing.T) {
	body := `// two statements

CREATE CONSTRAINT c IF NOT EXISTS FOR (r:R) REQUIRE r.k IS UNIQUE;

MERGE (r:R {k: 'x'})
ON CREATE SET r.created_at = datetime();
`

	statements := splitCypherStatements(body)

	if len(statements) != 2 {
		t.Fatalf("got %d statements, want 2:\n%q", len(statements), statements)
	}
	if !strings.HasPrefix(statements[0], "CREATE CONSTRAINT") ||
		!strings.HasPrefix(statements[1], "MERGE (r:R") {
		t.Fatalf("statements = %q", statements)
	}
}

func TestSplitCypherStatementsDropsACommentOnlyBody(t *testing.T) {
	if got := splitCypherStatements("// nothing to do here;\n// really nothing\n"); len(got) != 0 {
		t.Fatalf("comment-only body produced statements: %q", got)
	}
}
