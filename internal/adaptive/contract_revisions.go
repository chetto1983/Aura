package adaptive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

type RevisionKind string

const (
	RevisionCorpus    RevisionKind = "corpus"
	RevisionEmbedding RevisionKind = "embedding"
	RevisionIndex     RevisionKind = "index"
	RevisionReranker  RevisionKind = "reranker"
	RevisionRetriever RevisionKind = "retriever"
	RevisionSchema    RevisionKind = "schema"
)

type Revision struct {
	kind      RevisionKind
	value     string
	validated bool
}

// RevisionCatalog is a sealed capability minted only after verified snapshot/config loading.
type RevisionCatalog struct {
	registered map[RevisionKind]map[string]struct{}
}

type RevisionSet struct {
	values map[RevisionKind]Revision
}

func newRevisionCatalog(entries map[RevisionKind][]string) (RevisionCatalog, error) {
	registered := make(map[RevisionKind]map[string]struct{}, len(entries))
	for kind, values := range entries {
		if !kind.fixed() {
			return RevisionCatalog{}, fmt.Errorf("adaptive revision catalog kind %q is invalid", kind)
		}
		kindEntries := make(map[string]struct{}, len(values))
		for _, value := range values {
			if !validImmutableRevisionID(value) {
				return RevisionCatalog{}, fmt.Errorf("adaptive revision %q is invalid", value)
			}
			if _, exists := kindEntries[value]; exists {
				return RevisionCatalog{}, fmt.Errorf("adaptive revision %q is duplicated", value)
			}
			kindEntries[value] = struct{}{}
		}
		registered[kind] = kindEntries
	}
	return RevisionCatalog{registered: registered}, nil
}

func NewRegisteredRevision(
	kind RevisionKind,
	value string,
	catalog RevisionCatalog,
) (Revision, error) {
	if !kind.fixed() || !validImmutableRevisionID(value) {
		return Revision{}, fmt.Errorf("adaptive %s revision %q is invalid", kind, value)
	}
	if _, exists := catalog.registered[kind][value]; !exists {
		return Revision{}, fmt.Errorf("adaptive %s revision %q is not registered", kind, value)
	}
	return Revision{kind: kind, value: value, validated: true}, nil
}

func NewCorpusRevision(epoch uint64) (Revision, error) {
	if epoch == 0 {
		return Revision{}, errors.New("adaptive corpus revision epoch is required")
	}
	return Revision{
		kind:      RevisionCorpus,
		value:     strconv.FormatUint(epoch, 10),
		validated: true,
	}, nil
}

func NewRevisionSet(revisions ...Revision) (RevisionSet, error) {
	values := make(map[RevisionKind]Revision, len(revisions))
	for _, revision := range revisions {
		if !revision.validated || !revision.kind.valid() || revision.value == "" {
			return RevisionSet{}, errors.New("adaptive revision was not constructed by a validated API")
		}
		if _, exists := values[revision.kind]; exists {
			return RevisionSet{}, fmt.Errorf("adaptive revision kind %q is duplicated", revision.kind)
		}
		values[revision.kind] = revision
	}
	return RevisionSet{values: values}, nil
}

func (revisions RevisionSet) MarshalJSON() ([]byte, error) {
	if err := revisions.validate(); err != nil {
		return nil, err
	}
	encoded := make(map[string]string, len(revisions.values))
	for kind, revision := range revisions.values {
		encoded[string(kind)] = revision.value
	}
	return json.Marshal(encoded)
}

func (revisions *RevisionSet) UnmarshalJSON(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	var encoded map[string]string
	if err := decoder.Decode(&encoded); err != nil {
		return fmt.Errorf("decode adaptive revisions: %w", err)
	}
	if len(encoded) != 0 {
		return errors.New("adaptive revisions require a frozen catalog to decode")
	}
	revisions.values = make(map[RevisionKind]Revision)
	return nil
}

func (revisions RevisionSet) clone() RevisionSet {
	values := make(map[RevisionKind]Revision, len(revisions.values))
	for kind, revision := range revisions.values {
		values[kind] = revision
	}
	return RevisionSet{values: values}
}

func (revisions RevisionSet) validate() error {
	for kind, revision := range revisions.values {
		if kind != revision.kind || !revision.validated || !kind.valid() || revision.value == "" {
			return fmt.Errorf("adaptive delivery revision %q is invalid", kind)
		}
	}
	return nil
}

func (kind RevisionKind) fixed() bool {
	switch kind {
	case RevisionEmbedding, RevisionIndex, RevisionReranker, RevisionRetriever, RevisionSchema:
		return true
	default:
		return false
	}
}

func (kind RevisionKind) valid() bool {
	return kind == RevisionCorpus || kind.fixed()
}
