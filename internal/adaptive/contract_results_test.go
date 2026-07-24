package adaptive

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const (
	artifactResultUUID = "b1457f37-9afb-4c69-943b-cd5e9add1f4f"
	nodeResultUUID     = "e7f50ce8-a9e5-49e3-ae05-10a2f4b602e7"
	entityResultUUID   = "f7407026-2a1f-4bdb-bff8-0358464a8a07"
	preferenceUUID     = "287ed97a-6ba3-4b63-a456-d37290381890"
	messageResultUUID  = "f17e32d0-dd25-47dc-815a-e122f0335761"
	reasoningTraceUUID = "ec36f025-24d8-4901-a374-2b5df2ff6536"
)

func TestResultIDHasNoExternallyConstructibleFields(t *testing.T) {
	t.Parallel()
	resultType := reflect.TypeOf(ResultID{})
	for i := range resultType.NumField() {
		if resultType.Field(i).IsExported() {
			t.Fatalf("ResultID field %q is exported", resultType.Field(i).Name)
		}
	}
}

func TestUUIDResultConstructorsRequireCanonicalUUIDs(t *testing.T) {
	t.Parallel()
	constructors := []struct {
		name string
		kind ResultKind
		id   string
		new  func(string) (ResultID, error)
	}{
		{name: "artifact", kind: ResultArtifact, id: artifactResultUUID, new: NewArtifactResultID},
		{name: "node", kind: ResultNode, id: nodeResultUUID, new: NewNodeResultID},
		{name: "memory entity", kind: ResultMemoryEntity, id: entityResultUUID, new: NewMemoryEntityResultID},
		{name: "memory preference", kind: ResultMemoryPreference, id: preferenceUUID, new: NewMemoryPreferenceResultID},
		{name: "memory message", kind: ResultMemoryMessage, id: messageResultUUID, new: NewMemoryMessageResultID},
		{
			name: "memory reasoning trace",
			kind: ResultMemoryReasoningTrace,
			id:   reasoningTraceUUID,
			new:  NewMemoryReasoningTraceResultID,
		},
	}
	for _, tt := range constructors {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := tt.new(tt.id)
			if err != nil {
				t.Fatalf("construct canonical UUID result: %v", err)
			}
			if result.Kind() != tt.kind || result.ID() != tt.id {
				t.Fatalf("result = (%q, %q), want (%q, %q)",
					result.Kind(), result.ID(), tt.kind, tt.id)
			}
			for _, invalid := range []string{
				"",
				"00000000-0000-0000-0000-000000000000",
				strings.ToUpper(tt.id),
				"artifact-secret",
			} {
				if _, err := tt.new(invalid); err == nil {
					t.Fatalf("constructor accepted noncanonical UUID %q", invalid)
				}
			}
		})
	}
}

func TestToolAndSkillResultsRequireFrozenCatalogMembership(t *testing.T) {
	t.Parallel()
	entries := map[ResultKind][]string{
		ResultTool:  {"memory_get_context"},
		ResultSkill: {"document-search"},
	}
	catalog, err := NewResultCatalog(entries)
	if err != nil {
		t.Fatalf("NewResultCatalog: %v", err)
	}
	entries[ResultTool][0] = "api-key"

	tool, err := NewToolResultID("memory_get_context", catalog)
	if err != nil {
		t.Fatalf("NewToolResultID: %v", err)
	}
	skill, err := NewSkillResultID("document-search", catalog)
	if err != nil {
		t.Fatalf("NewSkillResultID: %v", err)
	}
	if tool.Kind() != ResultTool || skill.Kind() != ResultSkill {
		t.Fatalf("catalog results have wrong kinds: %q, %q", tool.Kind(), skill.Kind())
	}
	for _, secretOrProse := range []string{"api-key", "operator-notes", "private_summary"} {
		if _, err := NewToolResultID(secretOrProse, catalog); err == nil {
			t.Fatalf("NewToolResultID accepted unregistered safe slug %q", secretOrProse)
		}
	}
	if _, err := NewSkillResultID("document search", catalog); err == nil {
		t.Fatal("NewSkillResultID accepted unsafe slug")
	}
}

func TestResultIDMarshalsApprovedShape(t *testing.T) {
	t.Parallel()
	result, err := NewArtifactResultID(artifactResultUUID)
	if err != nil {
		t.Fatalf("NewArtifactResultID: %v", err)
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `{"kind":"artifact","id":"` + artifactResultUUID + `"}`
	if string(payload) != want {
		t.Fatalf("result JSON = %s, want %s", payload, want)
	}
}

func mustResultID(result ResultID, err error) ResultID {
	if err != nil {
		panic(err)
	}
	return result
}
