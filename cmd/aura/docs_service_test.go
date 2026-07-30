package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/knowledge"
)

// unsetDocumentServiceFields names every EXPORTED documents.Service field that no
// composition sets, with the reason it stays zero. The map is a claim the test enforces
// in both directions, so it cannot be used to silence a real gap: an entry here must be
// zero in ALL THREE compositions (a stale entry fails), and any exported field NOT here
// must be non-zero in the CLI composition (a new collaborator fails until `aura docs`
// wires it too).
var unsetDocumentServiceFields = map[string]string{
	"Clock":           "test-only wall-clock seam; production stamps times through Service.now → time.Now",
	"RerankThreshold": "non-monotonic rerank guard, unset in the runtime compositions too — wiring it here would change live rerank behaviour",
	"RerankBlend":     "same guard as RerankThreshold, same reason",
	"RetrievalHealth": "pre-I/O snapshot consumed only by FreezeRetrievalPlans; Retrieve derives health from the collaborators that are actually wired",
}

// TestDocumentServiceCompositionsDoNotDrift is a SHAPE assertion, not a behavioural one.
// The defect it locks out — the CLI building a documents.Service with five of fifteen
// fields — produced no error and no warning: IngestPath skips a nil Embedder silently and
// Retrieve degrades to the sparse seed when Reranker is nil. Every behavioural test
// passed while `docs bench` measured a pipeline that did not exist.
func TestDocumentServiceCompositionsDoNotDrift(t *testing.T) {
	deps := driftTestDeps()
	cli := reflect.ValueOf(*newDocumentCLIService(deps))
	runtimes := map[string]reflect.Value{
		"ingest":    reflect.ValueOf(*newDocumentIngestService(deps)),
		"retrieval": reflect.ValueOf(*newDocumentRetrievalService(deps)),
	}

	serviceType := cli.Type()
	for index := range serviceType.NumField() {
		field := serviceType.Field(index)
		if !field.IsExported() {
			// Unexported fields cannot be set from package main by ANY composition, so
			// they cannot drift between them.
			continue
		}
		reason, unset := unsetDocumentServiceFields[field.Name]
		if unset {
			assertUnsetEverywhere(t, field.Name, reason, cli, runtimes, index)
			continue
		}
		if cli.Field(index).IsZero() {
			t.Errorf("documents.Service.%s is zero in the `aura docs` composition: "+
				"wire it in newDocumentCLIService, or add it to unsetDocumentServiceFields with a reason",
				field.Name)
		}
		for name, runtime := range runtimes {
			if !runtime.Field(index).IsZero() && cli.Field(index).IsZero() {
				t.Errorf("documents.Service.%s is set by the %s composition but not by `aura docs`",
					field.Name, name)
			}
		}
	}
}

func assertUnsetEverywhere(
	t *testing.T,
	name, reason string,
	cli reflect.Value,
	runtimes map[string]reflect.Value,
	index int,
) {
	t.Helper()
	if !cli.Field(index).IsZero() {
		t.Errorf("documents.Service.%s is allowlisted as unset (%s) but the `aura docs` "+
			"composition sets it: drop the allowlist entry", name, reason)
	}
	for runtimeName, runtime := range runtimes {
		if !runtime.Field(index).IsZero() {
			t.Errorf("documents.Service.%s is allowlisted as unset (%s) but the %s "+
				"composition sets it: the allowlist is hiding a real gap", name, reason, runtimeName)
		}
	}
}

// driftTestDeps turns EVERY switch on. A zero-value config would make the assertion
// vacuous for the flags: MUSRIsolation and MaxBytes are zero in a default config, so a
// composition that forgot them would look identical to one that set them.
func driftTestDeps() documentServiceDeps {
	cfg := &config.Config{
		Neo4j: knowledge.Config{
			BoltURL:         "bolt://neo4j.test:7687",
			EmbedURL:        "http://embed.test",
			EmbedDimensions: 1024,
		},
		DocumentsBaseURL:      "http://markitdown.test",
		RerankBaseURL:         "http://rerank.test",
		RerankRelevanceFloor:  0.1,
		AssetMaxDocumentBytes: 104857600,
		MultimodalTimeoutSec:  120,
		MUSRIsolation:         true,
	}
	return documentServiceDeps{
		cfg:       cfg,
		graph:     stubKnowledgeClient{},
		maxBytes:  documentMaxBytes(cfg),
		revisions: defaultRetrievalRevisions(),
	}
}

type stubKnowledgeClient struct{}

func (stubKnowledgeClient) Read(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (stubKnowledgeClient) Write(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}
