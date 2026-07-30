package main

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/knowledge"
)

type recordingDocumentKnowledgeClient struct {
	closeCalls int
	epochReads []any
	readIndex  int
}

func (c *recordingDocumentKnowledgeClient) Read(
	_ context.Context,
	query string,
	_ map[string]any,
) ([]map[string]any, error) {
	if !strings.Contains(query, "DocumentCorpusRevision") {
		return nil, nil
	}
	if c.readIndex >= len(c.epochReads) {
		return nil, nil
	}
	value := c.epochReads[c.readIndex]
	c.readIndex++
	if err, ok := value.(error); ok {
		return nil, err
	}
	return []map[string]any{{"epoch": value}}, nil
}

func (*recordingDocumentKnowledgeClient) Write(
	context.Context,
	string,
	map[string]any,
) ([]map[string]any, error) {
	return nil, nil
}

func (c *recordingDocumentKnowledgeClient) Close() error {
	c.closeCalls++
	return nil
}

func TestDocsToolSearcherReusesGraphSessionUntilRuntimeClose(t *testing.T) {
	client := &recordingDocumentKnowledgeClient{}
	openCalls := 0
	searcher := &docsToolSearcher{
		cfg: &config.Config{},
		openKnowledge: func(context.Context, *knowledge.Config) (documentKnowledgeClient, error) {
			openCalls++
			return client, nil
		},
	}

	first, err := searcher.graphClient(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := searcher.graphClient(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if first != client || second != client || openCalls != 1 {
		t.Fatalf(
			"graph sessions first=%p second=%p opens=%d, want same client and one open",
			first,
			second,
			openCalls,
		)
	}
	if err := searcher.Close(); err != nil {
		t.Fatal(err)
	}
	if err := searcher.Close(); err != nil {
		t.Fatal(err)
	}
	if client.closeCalls != 1 {
		t.Fatalf("graph session closes = %d, want one idempotent close", client.closeCalls)
	}
}
