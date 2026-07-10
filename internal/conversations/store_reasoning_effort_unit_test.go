// Unit tier (no build tag): the pure reasoning-effort read-projection logic — metadata
// jsonb parse + conversationFromRow wiring — that needs no database (Phase 37E / D-06).
// The DB write round-trip (UpdateReasoningEffortForIdentity) is exercised under
// db_integration in store_reasoning_effort_test.go; this file guards the defensive parse
// so a poisoned/absent metadata value can never panic the read path, and gives the
// otherwise DB-gated projection daemon-free coverage (CLAUDE.md owned-surface ≥85% floor).
package conversations

import (
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestReasoningEffortFromMetadata(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string // "" here means a nil []byte (see the nil case below)
		want string
	}{
		{name: "high", raw: `{"reasoning_effort":"high"}`, want: "high"},
		{name: "auto is a stored symbol too", raw: `{"reasoning_effort":"auto"}`, want: "auto"},
		{name: "off", raw: `{"reasoning_effort":"off"}`, want: "off"},
		{name: "max", raw: `{"reasoning_effort":"max"}`, want: "max"},
		{name: "coexists with other metadata keys", raw: `{"foo":1,"reasoning_effort":"mid"}`, want: "mid"},
		{name: "empty-string value stays empty", raw: `{"reasoning_effort":""}`, want: ""},
		{name: "object missing the key", raw: `{"model":"x"}`, want: ""},
		{name: "empty object", raw: `{}`, want: ""},
		{name: "malformed json", raw: `{not json`, want: ""},
		{name: "non-object array", raw: `[1,2,3]`, want: ""},
		{name: "bare string", raw: `"just a string"`, want: ""},
		{name: "bare number", raw: `42`, want: ""},
		{name: "non-string effort value (number) is rejected", raw: `{"reasoning_effort":123}`, want: ""},
		{name: "non-string effort value (bool) is rejected", raw: `{"reasoning_effort":true}`, want: ""},
		{name: "json null literal", raw: `null`, want: ""},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := reasoningEffortFromMetadata([]byte(tc.raw)); got != tc.want {
				t.Errorf("reasoningEffortFromMetadata(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestReasoningEffortFromMetadata_NilAndEmptyBytes(t *testing.T) {
	t.Parallel()
	if got := reasoningEffortFromMetadata(nil); got != "" {
		t.Errorf("nil metadata: got %q, want \"\"", got)
	}
	if got := reasoningEffortFromMetadata([]byte{}); got != "" {
		t.Errorf("empty metadata: got %q, want \"\"", got)
	}
}

// TestConversationFromRow_SurfacesReasoningEffort proves the projection wiring: a row whose
// metadata jsonb carries reasoning_effort surfaces it on Conversation.ReasoningEffort (the
// field the frontend hydrates from), while a metadata-less row projects to "" (auto, D-07).
// Pure — conversationFromRow takes an sqlc row and returns the domain type, no DB.
func TestConversationFromRow_SurfacesReasoningEffort(t *testing.T) {
	t.Parallel()
	id := pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}
	owner := pgtype.UUID{Bytes: uuid.Must(uuid.NewV7()), Valid: true}

	withEffort := conversationFromRow(sqlc.AuraConversations{
		ID:         id,
		IdentityID: owner,
		Status:     StatusActive,
		Model:      "test-model",
		Metadata:   []byte(`{"reasoning_effort":"high"}`),
	})
	if withEffort.ReasoningEffort != "high" {
		t.Errorf("projection dropped the stored effort: got %q, want %q", withEffort.ReasoningEffort, "high")
	}
	if withEffort.ID != uuid.UUID(id.Bytes).String() {
		t.Errorf("projection id wrong: %q", withEffort.ID)
	}

	noMeta := conversationFromRow(sqlc.AuraConversations{
		ID:         id,
		IdentityID: owner,
		Status:     StatusActive,
		Model:      "test-model",
		Metadata:   nil,
	})
	if noMeta.ReasoningEffort != "" {
		t.Errorf("metadata-less row must hydrate as auto (\"\"), got %q", noMeta.ReasoningEffort)
	}
}
