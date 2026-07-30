package documents

import (
	"errors"
	"strings"
	"testing"
)

func TestReadDocumentCorpusEpoch(t *testing.T) {
	client := &fakeKnowledgeClient{
		readRows: []map[string]any{{"epoch": int64(17)}},
	}
	epoch, err := ReadDocumentCorpusEpoch(t.Context(), client)
	if err != nil {
		t.Fatal(err)
	}
	if epoch != 17 {
		t.Fatalf("epoch = %d, want 17", epoch)
	}
	if len(client.readCalls) != 1 ||
		!strings.Contains(client.readCalls[0].query, "DocumentCorpusRevision") {
		t.Fatalf("corpus epoch read = %#v", client.readCalls)
	}
}

func TestReadDocumentCorpusEpochFailsClosed(t *testing.T) {
	for name, client := range map[string]*fakeKnowledgeClient{
		"missing":    {},
		"zero":       {readRows: []map[string]any{{"epoch": int64(0)}}},
		"fractional": {readRows: []map[string]any{{"epoch": 1.5}}},
		"read error": {failRead: errors.New("neo4j unavailable")},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadDocumentCorpusEpoch(t.Context(), client); err == nil {
				t.Fatal("invalid corpus epoch accepted")
			}
		})
	}
}
