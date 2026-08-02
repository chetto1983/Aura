package main

import (
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// TestDocumentIngestServiceIsFullyWired is a SHAPE assertion, not a behavioural one.
// The defect it locks out — a documents.Service built with a subset of its fields —
// produces no error and no warning: IngestPath skips a nil Catalog silently, so a file
// lands in the job ledger, reports "searchable", and is invisible to document_search.
// Every behavioural test passed while that was true.
func TestDocumentIngestServiceIsFullyWired(t *testing.T) {
	svc := reflect.ValueOf(*newDocumentIngestService(driftTestDeps()))
	serviceType := svc.Type()
	for index := range serviceType.NumField() {
		field := serviceType.Field(index)
		if !field.IsExported() {
			continue
		}
		if svc.Field(index).IsZero() {
			t.Errorf("documents.Service.%s is zero in newDocumentIngestService: wire it, "+
				"or delete the field if ingestion no longer needs it", field.Name)
		}
	}
}

// driftTestDeps turns EVERY switch on. A zero-value config would make the assertion
// vacuous for MaxBytes, which is zero in a default config, so a composition that forgot
// it would look identical to one that set it.
func driftTestDeps() documentServiceDeps {
	cfg := &config.Config{AssetMaxDocumentBytes: 104857600}
	return documentServiceDeps{cfg: cfg, maxBytes: documentMaxBytes(cfg)}
}
