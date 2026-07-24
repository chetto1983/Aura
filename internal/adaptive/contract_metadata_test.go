package adaptive

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRevisionValuesRequireTypedConstructorsAndFrozenCatalog(t *testing.T) {
	t.Parallel()
	revisionType := reflect.TypeOf(Revision{})
	for i := range revisionType.NumField() {
		if revisionType.Field(i).IsExported() {
			t.Fatalf("Revision field %q is exported", revisionType.Field(i).Name)
		}
	}
	entries := validRevisionCatalogEntries()
	catalog, err := NewRevisionCatalog(entries)
	if err != nil {
		t.Fatalf("NewRevisionCatalog: %v", err)
	}
	entries[RevisionRetriever][0] = "api-key"

	retriever, err := NewRegisteredRevision(RevisionRetriever, "retriever-v2", catalog)
	if err != nil {
		t.Fatalf("NewRegisteredRevision: %v", err)
	}
	corpus, err := NewCorpusRevision(42)
	if err != nil {
		t.Fatalf("NewCorpusRevision: %v", err)
	}
	revisions, err := NewRevisionSet(retriever, corpus)
	if err != nil {
		t.Fatalf("NewRevisionSet: %v", err)
	}
	assignment := validAssignment()
	delivery := assignmentBoundDelivery(assignment)
	delivery.Revisions = revisions
	event, err := NewDeliveryEvent(assignment, delivery)
	if err != nil {
		t.Fatalf("NewDeliveryEvent: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("unmarshal delivery payload: %v", err)
	}
	got := payload["revisions"].(map[string]any)
	if got["corpus"] != "42" || got["retriever"] != "retriever-v2" {
		t.Fatalf("revision JSON = %#v, want approved map shape", got)
	}
}

func TestRevisionRegistryRejectsSecretsAndProse(t *testing.T) {
	t.Parallel()
	catalog, err := NewRevisionCatalog(validRevisionCatalogEntries())
	if err != nil {
		t.Fatalf("NewRevisionCatalog: %v", err)
	}
	for _, value := range []string{
		"api-key",
		"operator-notes",
		"private_summary",
		"revision prose",
		strings.Repeat("r", 129),
	} {
		if _, err := NewRegisteredRevision(RevisionRetriever, value, catalog); err == nil {
			t.Fatalf("NewRegisteredRevision accepted %q", value)
		}
	}
	if _, err := NewCorpusRevision(0); err == nil {
		t.Fatal("NewCorpusRevision accepted zero epoch")
	}
}

func TestFeatureSchemaDistinguishesContinuousAndIntegerValues(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	assignment.Features = map[FeatureKey]float64{
		FeatureCandidateCount: 2,
		FeatureKNNSimilarity:  0.375,
	}
	if _, err := NewAssignmentEvent(assignment); err != nil {
		t.Fatalf("NewAssignmentEvent continuous feature: %v", err)
	}
	tests := []struct {
		name  string
		key   FeatureKey
		value float64
	}{
		{name: "fractional count", key: FeatureCandidateCount, value: 1.5},
		{name: "negative count", key: FeatureCandidateCount, value: -1},
		{name: "continuous above maximum", key: FeatureKNNSimilarity, value: 1.01},
		{name: "continuous below minimum", key: FeatureKNNSimilarity, value: -0.01},
		{name: "unknown key", key: FeatureKey("private_score"), value: 0.5},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			invalid := validAssignment()
			invalid.Features = map[FeatureKey]float64{tt.key: tt.value}
			if _, err := NewAssignmentEvent(invalid); err == nil {
				t.Fatal("NewAssignmentEvent error = nil, want feature schema rejection")
			}
		})
	}
}

func validRevisionCatalogEntries() map[RevisionKind][]string {
	return map[RevisionKind][]string{
		RevisionEmbedding: {"embedding-v2"},
		RevisionIndex:     {"index-v3"},
		RevisionReranker:  {"reranker-v4"},
		RevisionRetriever: {"retriever-v2", "retriever-v5"},
		RevisionSchema:    {"schema-v6"},
	}
}

func mustRevisionSet(revisions ...Revision) RevisionSet {
	set, err := NewRevisionSet(revisions...)
	if err != nil {
		panic(err)
	}
	return set
}

func mustCorpusRevision(epoch uint64) Revision {
	revision, err := NewCorpusRevision(epoch)
	if err != nil {
		panic(err)
	}
	return revision
}

func mustRegisteredRevision(
	kind RevisionKind,
	value string,
	catalog RevisionCatalog,
) Revision {
	revision, err := NewRegisteredRevision(kind, value, catalog)
	if err != nil {
		panic(err)
	}
	return revision
}
