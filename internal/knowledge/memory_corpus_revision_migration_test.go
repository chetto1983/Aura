package knowledge

import (
	"strings"
	"testing"
)

func TestMemoryCorpusRevisionMigrationCreatesSingleton(t *testing.T) {
	body, err := cypherFS.ReadFile("migrations/0005_memory_corpus_revision.cypher")
	if err != nil {
		t.Fatalf("read memory corpus revision migration: %v", err)
	}
	text := string(body)
	for _, fragment := range []string{
		"CREATE CONSTRAINT memory_corpus_revision_singleton IF NOT EXISTS",
		"FOR (r:MemoryCorpusRevision) REQUIRE r.singleton IS UNIQUE",
		"MERGE (r:MemoryCorpusRevision {singleton: 'corpus'})",
		"r.epoch = 1",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("memory corpus revision migration missing %q:\n%s", fragment, text)
		}
	}
}
