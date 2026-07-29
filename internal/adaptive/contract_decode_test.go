package adaptive

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDecodeDeliveryRoundTripsCatalogBackedPayload(t *testing.T) {
	t.Parallel()
	assignment, delivery, results, revisions := catalogBackedDelivery(t)
	event, err := NewDeliveryEvent(assignment, delivery)
	if err != nil {
		t.Fatalf("NewDeliveryEvent: %v", err)
	}
	persisted := append([]byte(nil), event.Payload...)

	decoded, err := DecodeDelivery(persisted, assignment, results, revisions)
	if err != nil {
		t.Fatalf("DecodeDelivery: %v", err)
	}
	roundTrip, err := NewDeliveryEvent(assignment, decoded)
	if err != nil {
		t.Fatalf("NewDeliveryEvent(decoded): %v", err)
	}
	if !bytes.Equal(roundTrip.Payload, persisted) {
		t.Fatalf("roundtrip payload mismatch:\nwant: %s\ngot:  %s", persisted, roundTrip.Payload)
	}
}

func TestDecodeDeliveryRejectsWrongCapabilitiesAndAssignment(t *testing.T) {
	t.Parallel()
	assignment, delivery, results, revisions := catalogBackedDelivery(t)
	event, err := NewDeliveryEvent(assignment, delivery)
	if err != nil {
		t.Fatalf("NewDeliveryEvent: %v", err)
	}
	changedResults := mustResultCatalog(map[ResultKind][]string{
		ResultTool:  {"other-tool"},
		ResultSkill: {"other-skill"},
	})
	changedRevisions := mustRevisionCatalog(map[RevisionKind][]string{
		RevisionEmbedding: {"embedding-v9"},
		RevisionIndex:     {"index-v9"},
		RevisionReranker:  {"reranker-v9"},
		RevisionRetriever: {"retriever-v9"},
		RevisionSchema:    {"schema-v9"},
	})
	changedAssignment := assignment
	changedAssignment.RequestID = uuid.MustParse("0fd07935-94fa-4896-be42-7363f9280368")
	changedAssignment.AssignmentID = mustAssignmentID(
		changedAssignment.OwnerID,
		changedAssignment.RequestID,
		changedAssignment.Point,
		changedAssignment.PointOrdinal,
	)
	tests := []struct {
		name       string
		assignment Assignment
		results    ResultCatalog
		revisions  RevisionCatalog
	}{
		{name: "missing result catalog", assignment: assignment, revisions: revisions},
		{name: "changed result catalog", assignment: assignment, results: changedResults, revisions: revisions},
		{name: "missing revision catalog", assignment: assignment, results: results},
		{name: "changed revision catalog", assignment: assignment, results: results, revisions: changedRevisions},
		{name: "changed assignment", assignment: changedAssignment, results: results, revisions: revisions},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeDelivery(
				event.Payload,
				tt.assignment,
				tt.results,
				tt.revisions,
			); err == nil {
				t.Fatal("DecodeDelivery error = nil, want capability or assignment rejection")
			}
		})
	}
}

func TestDecodeDeliveryRejectsMalformedOrUnregisteredWireValues(t *testing.T) {
	t.Parallel()
	assignment, delivery, results, revisions := catalogBackedDelivery(t)
	event, err := NewDeliveryEvent(assignment, delivery)
	if err != nil {
		t.Fatalf("NewDeliveryEvent: %v", err)
	}
	valid := string(event.Payload)
	tests := []struct {
		name    string
		payload string
	}{
		{name: "unknown outer field", payload: strings.Replace(valid, "{", `{"unknown":true,`, 1)},
		{name: "unknown result field", payload: strings.Replace(
			valid,
			`{"id":"`+artifactResultUUID+`"`,
			`{"unknown":true,"id":"`+artifactResultUUID+`"`,
			1,
		)},
		{name: "unknown result kind", payload: strings.Replace(valid, `"kind":"tool"`, `"kind":"surprise"`, 1)},
		{name: "unknown revision", payload: strings.Replace(
			valid,
			`"schema":"schema-v6"`,
			`"surprise":"schema-v6"`,
			1,
		)},
		{name: "noncanonical UUID", payload: strings.Replace(
			valid,
			artifactResultUUID,
			strings.ToUpper(artifactResultUUID),
			1,
		)},
		{name: "zero corpus epoch", payload: strings.Replace(valid, `"corpus":"42"`, `"corpus":"0"`, 1)},
		{name: "noncanonical corpus epoch", payload: strings.Replace(valid, `"corpus":"42"`, `"corpus":"042"`, 1)},
		{name: "unregistered fixed revision", payload: strings.Replace(
			valid,
			`"retriever":"retriever-v2"`,
			`"retriever":"operator-notes"`,
			1,
		)},
		{name: "trailing JSON", payload: valid + ` {}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeDelivery(
				[]byte(tt.payload),
				assignment,
				results,
				revisions,
			); err == nil {
				t.Fatal("DecodeDelivery error = nil, want strict wire rejection")
			}
		})
	}
}

func TestPrivateCatalogMintRejectsSensitiveAndProseEntries(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"api-key",
		"api_key",
		"secret",
		"password",
		"credential",
		"bearer-token",
		"private_summary",
		"raw-content",
	} {
		if _, err := newResultCatalog(map[ResultKind][]string{
			ResultTool: {value},
		}); err == nil {
			t.Fatalf("newResultCatalog accepted sensitive entry %q", value)
		}
	}
	for _, value := range []string{"api-key-v1", "private_summary-v1", "operator-notes"} {
		if _, err := newRevisionCatalog(map[RevisionKind][]string{
			RevisionRetriever: {value},
		}); err == nil {
			t.Fatalf("newRevisionCatalog accepted sensitive or prose entry %q", value)
		}
	}
}

func catalogBackedDelivery(
	t *testing.T,
) (Assignment, Delivery, ResultCatalog, RevisionCatalog) {
	t.Helper()
	results := mustResultCatalog(map[ResultKind][]string{
		ResultTool:  {"memory_get_context"},
		ResultSkill: {"document-search"},
	})
	revisions := mustRevisionCatalog(validRevisionCatalogEntries())
	assignment := validAssignment()
	delivery := assignmentBoundDelivery(assignment)
	delivery.ResultCount = 3
	delivery.ResultIDs = []ResultID{
		mustResultID(NewArtifactResultID(artifactResultUUID)),
		mustResultID(NewToolResultID("memory_get_context", results)),
		mustResultID(NewSkillResultID("document-search", results)),
	}
	delivery.Revisions = mustRevisionSet(
		mustCorpusRevision(42),
		mustRegisteredRevision(RevisionEmbedding, "embedding-v2", revisions),
		mustRegisteredRevision(RevisionIndex, "index-v3", revisions),
		mustRegisteredRevision(RevisionReranker, "reranker-v4", revisions),
		mustRegisteredRevision(RevisionRetriever, "retriever-v2", revisions),
		mustRegisteredRevision(RevisionSchema, "schema-v6", revisions),
	)
	return assignment, delivery, results, revisions
}

func mustResultCatalog(entries map[ResultKind][]string) ResultCatalog {
	catalog, err := newResultCatalog(entries)
	if err != nil {
		panic(err)
	}
	return catalog
}

func mustRevisionCatalog(entries map[RevisionKind][]string) RevisionCatalog {
	catalog, err := newRevisionCatalog(entries)
	if err != nil {
		panic(err)
	}
	return catalog
}
