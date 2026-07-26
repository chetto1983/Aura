package adaptive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ResultID is a validated reference to something a delivery produced. Both fields are
// unexported and every constructor validates, so a ResultID cannot be assembled by struct
// literal — a delivery can only ever cite results that passed the checks for their kind.
// UUID-shaped kinds validate structurally; chunk, tool, and skill IDs must additionally
// appear in a frozen ResultCatalog.
type ResultID struct {
	kind ResultKind
	id   string
}

// ResultCatalog is a sealed capability minted only after verified snapshot/config loading.
type ResultCatalog struct {
	registered map[ResultKind]map[string]struct{}
}

func newResultCatalog(entries map[ResultKind][]string) (ResultCatalog, error) {
	registered := make(map[ResultKind]map[string]struct{}, len(entries))
	for kind, ids := range entries {
		if kind != ResultTool && kind != ResultSkill && kind != ResultChunk {
			return ResultCatalog{}, fmt.Errorf("adaptive result catalog kind %q is invalid", kind)
		}
		kindEntries := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			if !validSafeSlug(id, maxResultIDLength) || sensitiveCatalogID(id) {
				return ResultCatalog{}, fmt.Errorf("adaptive result catalog ID %q is invalid", id)
			}
			if _, exists := kindEntries[id]; exists {
				return ResultCatalog{}, fmt.Errorf("adaptive result catalog ID %q is duplicated", id)
			}
			kindEntries[id] = struct{}{}
		}
		registered[kind] = kindEntries
	}
	return ResultCatalog{registered: registered}, nil
}

// NewArtifactResultID references a produced artifact by UUID.
func NewArtifactResultID(id string) (ResultID, error) {
	return newUUIDResultID(ResultArtifact, id)
}

// NewNodeResultID references a knowledge-graph node by UUID.
func NewNodeResultID(id string) (ResultID, error) {
	return newUUIDResultID(ResultNode, id)
}

// NewChunkResultID references a stable indexed chunk from a frozen catalog.
func NewChunkResultID(id string, catalog ResultCatalog) (ResultID, error) {
	return newCatalogResultID(ResultChunk, id, catalog)
}

// NewMemoryEntityResultID references a memory entity by UUID.
func NewMemoryEntityResultID(id string) (ResultID, error) {
	return newUUIDResultID(ResultMemoryEntity, id)
}

// NewMemoryPreferenceResultID references a memory preference by UUID.
func NewMemoryPreferenceResultID(id string) (ResultID, error) {
	return newUUIDResultID(ResultMemoryPreference, id)
}

// NewMemoryMessageResultID references a stored message by UUID.
func NewMemoryMessageResultID(id string) (ResultID, error) {
	return newUUIDResultID(ResultMemoryMessage, id)
}

// NewMemoryReasoningTraceResultID references a reasoning trace by UUID.
func NewMemoryReasoningTraceResultID(id string) (ResultID, error) {
	return newUUIDResultID(ResultMemoryReasoningTrace, id)
}

// NewToolResultID references a tool, which must be registered in catalog. Tool IDs are
// free-form names rather than UUIDs, so the catalog is the only thing that can tell a real
// tool from an invented one.
func NewToolResultID(id string, catalog ResultCatalog) (ResultID, error) {
	return newCatalogResultID(ResultTool, id, catalog)
}

// NewSkillResultID references a skill, which must be registered in catalog (see
// NewToolResultID for why a catalog is required here and not for the UUID kinds).
func NewSkillResultID(id string, catalog ResultCatalog) (ResultID, error) {
	return newCatalogResultID(ResultSkill, id, catalog)
}

// Kind returns what sort of thing this result refers to.
func (result ResultID) Kind() ResultKind {
	return result.kind
}

// ID returns the referenced identifier.
func (result ResultID) ID() string {
	return result.id
}

// MarshalJSON re-validates before encoding, so a zero-value ResultID that reached a payload
// by some route other than a constructor fails here instead of being persisted.
func (result ResultID) MarshalJSON() ([]byte, error) {
	if err := validateResultID(result); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Kind ResultKind `json:"kind"`
		ID   string     `json:"id"`
	}{
		Kind: result.kind,
		ID:   result.id,
	})
}

// UnmarshalJSON decodes the UUID-shaped kinds and REFUSES catalog IDs: verifying
// those needs the frozen catalog, which decoding does not have, and accepting them
// unverified would let a payload name any tool it liked.
func (result *ResultID) UnmarshalJSON(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var encoded struct {
		Kind ResultKind `json:"kind"`
		ID   string     `json:"id"`
	}
	if err := decoder.Decode(&encoded); err != nil {
		return fmt.Errorf("decode adaptive result ID: %w", err)
	}
	switch encoded.Kind {
	case ResultArtifact, ResultNode, ResultMemoryEntity, ResultMemoryPreference,
		ResultMemoryMessage, ResultMemoryReasoningTrace:
		decoded, err := newUUIDResultID(encoded.Kind, encoded.ID)
		if err != nil {
			return err
		}
		*result = decoded
		return nil
	case ResultTool, ResultSkill, ResultChunk:
		return errors.New("adaptive catalog result IDs require a frozen catalog")
	default:
		return fmt.Errorf("adaptive result kind %q is invalid", encoded.Kind)
	}
}

func newUUIDResultID(kind ResultKind, id string) (ResultID, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil || parsed.String() != id {
		return ResultID{}, fmt.Errorf("adaptive %s result ID must be a canonical UUID", kind)
	}
	return ResultID{kind: kind, id: id}, nil
}

func newCatalogResultID(kind ResultKind, id string, catalog ResultCatalog) (ResultID, error) {
	if !validSafeSlug(id, maxResultIDLength) {
		return ResultID{}, fmt.Errorf("adaptive %s result ID %q is invalid", kind, id)
	}
	if _, exists := catalog.registered[kind][id]; !exists {
		return ResultID{}, fmt.Errorf("adaptive %s result ID %q is not registered", kind, id)
	}
	return ResultID{kind: kind, id: id}, nil
}

func validateResultID(result ResultID) error {
	switch result.kind {
	case ResultArtifact, ResultNode, ResultMemoryEntity, ResultMemoryPreference,
		ResultMemoryMessage, ResultMemoryReasoningTrace:
		_, err := newUUIDResultID(result.kind, result.id)
		return err
	case ResultTool, ResultSkill, ResultChunk:
		if !validSafeSlug(result.id, maxResultIDLength) {
			return fmt.Errorf("adaptive %s result ID %q is invalid", result.kind, result.id)
		}
		return nil
	default:
		return fmt.Errorf("adaptive result kind %q is invalid", result.kind)
	}
}

func validSafeSlug(value string, maximumLength int) bool {
	if value == "" || len(value) > maximumLength {
		return false
	}
	for i := range len(value) {
		character := value[i]
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			(i > 0 && (character == '-' || character == '_' || character == '.')) {
			continue
		}
		return false
	}
	return true
}
