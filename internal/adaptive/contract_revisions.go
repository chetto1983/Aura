package adaptive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// RevisionKind names one moving part of the retrieval stack whose version changes results.
type RevisionKind string

// The revision kinds. Corpus is an epoch counter minted by NewCorpusRevision; the rest are
// fixed identifiers that must be registered in a RevisionCatalog.
const (
	RevisionCorpus    RevisionKind = "corpus"
	RevisionParser    RevisionKind = "parser"
	RevisionChunker   RevisionKind = "chunker"
	RevisionEmbedding RevisionKind = "embedding"
	RevisionIndex     RevisionKind = "index"
	RevisionReranker  RevisionKind = "reranker"
	RevisionRetriever RevisionKind = "retriever"
	RevisionSchema    RevisionKind = "schema"
)

// Revision is one component's version, carrying the proof it was built by a validating
// constructor. The validated flag is unexported and checked by NewRevisionSet, so a
// zero-value Revision cannot be smuggled into a set by struct literal.
type Revision struct {
	kind      RevisionKind
	value     string
	validated bool
}

// RevisionCatalog is a sealed capability minted only after verified snapshot/config loading.
type RevisionCatalog struct {
	registered map[RevisionKind]map[string]struct{}
}

// RevisionSet is the set of component versions a delivery ran under — the record of which
// corpus, index and reranker produced a result, without which a later comparison of two
// deliveries cannot tell a policy change from a stack change.
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

// NewRegisteredRevision builds a revision for a fixed-kind component, requiring the value to
// be registered in catalog — an unregistered version is a typo or a drifted deployment, and
// either way it must not be recorded as fact.
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

// NewCorpusRevision builds the corpus revision from its epoch counter. Corpus is the one
// kind with no catalog: its values are minted by ingestion, not registered in advance.
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

// NewRevisionSet collects revisions, rejecting any that did not come from a validating
// constructor and any duplicate kind.
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

// MarshalJSON encodes the set as kind/value pairs, re-validating first.
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

// UnmarshalJSON accepts only an EMPTY set: rebuilding a revision means re-checking it
// against the catalog, which decoding does not have. Callers that need the values back must
// decode through the catalog-aware path rather than trusting the payload.
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
	case RevisionParser, RevisionChunker, RevisionEmbedding, RevisionIndex,
		RevisionReranker, RevisionRetriever, RevisionSchema:
		return true
	default:
		return false
	}
}

func (kind RevisionKind) valid() bool {
	return kind == RevisionCorpus || kind.fixed()
}
