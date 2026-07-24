package adaptive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

type ResultID struct {
	kind ResultKind
	id   string
}

type ResultCatalog struct {
	registered map[ResultKind]map[string]struct{}
}

func NewResultCatalog(entries map[ResultKind][]string) (ResultCatalog, error) {
	registered := make(map[ResultKind]map[string]struct{}, len(entries))
	for kind, ids := range entries {
		if kind != ResultTool && kind != ResultSkill {
			return ResultCatalog{}, fmt.Errorf("adaptive result catalog kind %q is invalid", kind)
		}
		kindEntries := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			if !validSafeSlug(id, maxResultIDLength) {
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

func NewArtifactResultID(id string) (ResultID, error) {
	return newUUIDResultID(ResultArtifact, id)
}

func NewNodeResultID(id string) (ResultID, error) {
	return newUUIDResultID(ResultNode, id)
}

func NewMemoryEntityResultID(id string) (ResultID, error) {
	return newUUIDResultID(ResultMemoryEntity, id)
}

func NewMemoryPreferenceResultID(id string) (ResultID, error) {
	return newUUIDResultID(ResultMemoryPreference, id)
}

func NewMemoryMessageResultID(id string) (ResultID, error) {
	return newUUIDResultID(ResultMemoryMessage, id)
}

func NewMemoryReasoningTraceResultID(id string) (ResultID, error) {
	return newUUIDResultID(ResultMemoryReasoningTrace, id)
}

func NewToolResultID(id string, catalog ResultCatalog) (ResultID, error) {
	return newCatalogResultID(ResultTool, id, catalog)
}

func NewSkillResultID(id string, catalog ResultCatalog) (ResultID, error) {
	return newCatalogResultID(ResultSkill, id, catalog)
}

func (result ResultID) Kind() ResultKind {
	return result.kind
}

func (result ResultID) ID() string {
	return result.id
}

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
	case ResultTool, ResultSkill:
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
	case ResultTool, ResultSkill:
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
