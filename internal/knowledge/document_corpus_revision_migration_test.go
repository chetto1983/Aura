package knowledge

import (
	"strings"
	"testing"
)

func TestDocumentCorpusRevisionMigrationCreatesSingleton(t *testing.T) {
	body, err := cypherFS.ReadFile("migrations/0008_document_corpus_revision.cypher")
	if err != nil {
		t.Fatalf("read document corpus revision migration: %v", err)
	}
	text := string(body)
	for _, fragment := range []string{
		"CREATE CONSTRAINT document_corpus_revision_singleton IF NOT EXISTS",
		"FOR (r:DocumentCorpusRevision) REQUIRE r.singleton IS UNIQUE",
		"MERGE (r:DocumentCorpusRevision {singleton: 'corpus'})",
		"r.epoch = 1",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("document corpus revision migration missing %q:\n%s", fragment, text)
		}
	}
}
