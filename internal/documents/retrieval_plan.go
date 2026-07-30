package documents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Supported retrieval plan IDs form the frozen action catalog.
const (
	RetrievalPlanSparse             = "sparse"
	RetrievalPlanStatic             = "static"
	RetrievalPlanVector             = "vector"
	RetrievalPlanVectorRerank       = "vector_rerank"
	RetrievalPlanVectorRerankExpand = "vector_rerank_expand"
)

type retrievalSeed string

const (
	retrievalSeedSparse retrievalSeed = "sparse"
	retrievalSeedVector retrievalSeed = "vector"
)

// RetrievalRevisions freezes every implementation and data dependency that can
// change chunk membership or order without retaining query or content bytes.
type RetrievalRevisions struct {
	CorpusEpoch uint64 `json:"corpus_epoch"`
	Parser      string `json:"parser"`
	Chunker     string `json:"chunker"`
	Embedding   string `json:"embedding"`
	Index       string `json:"index"`
	Reranker    string `json:"reranker"`
	Retriever   string `json:"retriever"`
}

// RetrievalDependencyHealth is a pre-I/O composition snapshot.
type RetrievalDependencyHealth struct {
	Sparse   bool
	Vector   bool
	Reranker bool
	Expand   bool
}

// RetrievalScope is copied into every candidate before assignment.
type RetrievalScope struct {
	OwnerID      string `json:"owner_id"`
	TenantID     string `json:"tenant_id"`
	DocumentID   string `json:"document_id"`
	MaxResults   int    `json:"max_results"`
	ACLIsolation bool   `json:"acl_isolation"`
}

// RetrievalPlan is one supported, versioned execution strategy.
type RetrievalPlan struct {
	ID        string             `json:"id"`
	Revision  string             `json:"revision"`
	Scope     RetrievalScope     `json:"scope"`
	Revisions RetrievalRevisions `json:"revisions"`
	Seed      string             `json:"seed"`
	Rerank    bool               `json:"rerank"`
	Expand    bool               `json:"expand"`
	FailSoft  bool               `json:"fail_soft"`
}

// FrozenRetrievalPlans is the immutable candidate catalog seen by assignment
// and execution. Query bytes deliberately are not a field.
type FrozenRetrievalPlans struct {
	plans           []RetrievalPlan
	catalogRevision string
}

// FreezeRetrievalPlans snapshots the supported plans, scope, and revisions
// without performing embedding, graph, or reranker I/O.
func (s *Service) FreezeRetrievalPlans(req SearchRequest) (FrozenRetrievalPlans, error) {
	if s == nil {
		return FrozenRetrievalPlans{}, errors.New("documents: retrieval service is unavailable")
	}
	health := s.retrievalDependencyHealth()
	if !health.Sparse {
		return FrozenRetrievalPlans{}, errors.New("documents: sparse retrieval is unavailable")
	}
	if err := validateRetrievalRevisions(s.RetrievalRevisions); err != nil {
		return FrozenRetrievalPlans{}, err
	}
	scope := RetrievalScope{
		OwnerID: req.IdentityID, TenantID: req.IdentityID,
		DocumentID: req.DocumentID, MaxResults: effectiveLimit(req.Limit),
		ACLIsolation: s.MUSRIsolation,
	}
	staticSeed := retrievalSeedSparse
	if health.Vector && health.Reranker {
		staticSeed = retrievalSeedVector
	}
	static := RetrievalPlan{
		ID: RetrievalPlanStatic, Revision: "static-v1",
		Scope: scope, Revisions: s.RetrievalRevisions,
		Seed: string(staticSeed), FailSoft: true,
		Rerank: health.Reranker, Expand: health.Reranker && health.Expand,
	}
	plans := []RetrievalPlan{{
		ID: RetrievalPlanSparse, Revision: "sparse-v1",
		Scope: scope, Revisions: s.RetrievalRevisions,
		Seed: string(retrievalSeedSparse),
	}, static}
	if health.Vector {
		plans = append(plans, RetrievalPlan{
			ID: RetrievalPlanVector, Revision: "vector-v1",
			Scope: scope, Revisions: s.RetrievalRevisions,
			Seed: string(retrievalSeedVector),
		})
	}
	if health.Vector && health.Reranker {
		plans = append(plans, RetrievalPlan{
			ID: RetrievalPlanVectorRerank, Revision: "vector-rerank-v1",
			Scope: scope, Revisions: s.RetrievalRevisions,
			Seed: string(retrievalSeedVector), Rerank: true,
		})
		if health.Expand {
			plans = append(plans, RetrievalPlan{
				ID:       RetrievalPlanVectorRerankExpand,
				Revision: "vector-rerank-expand-v1",
				Scope:    scope, Revisions: s.RetrievalRevisions,
				Seed: string(retrievalSeedVector), Rerank: true,
				Expand: true,
			})
		}
	}
	slices.SortFunc(plans, func(left, right RetrievalPlan) int {
		switch {
		case left.ID < right.ID:
			return -1
		case left.ID > right.ID:
			return 1
		default:
			return 0
		}
	})
	revision, err := retrievalCatalogRevision(plans)
	if err != nil {
		return FrozenRetrievalPlans{}, err
	}
	return FrozenRetrievalPlans{plans: plans, catalogRevision: revision}, nil
}

func (s *Service) retrievalDependencyHealth() RetrievalDependencyHealth {
	if s.RetrievalHealth != nil {
		return *s.RetrievalHealth
	}
	return RetrievalDependencyHealth{
		Sparse:   s.Searcher != nil,
		Vector:   s.Knowledge != nil && s.QueryEmbedder != nil,
		Reranker: s.Knowledge != nil && s.QueryEmbedder != nil && s.Reranker != nil,
		Expand:   s.Knowledge != nil && s.QueryEmbedder != nil && s.Reranker != nil,
	}
}

func (s *Service) staticRetrievalPlan(req SearchRequest) RetrievalPlan {
	seed := retrievalSeedSparse
	if s.Reranker != nil {
		seed = retrievalSeedVector
	}
	return RetrievalPlan{
		ID: RetrievalPlanStatic, Revision: "static-v1",
		Scope: RetrievalScope{
			OwnerID: req.IdentityID, TenantID: req.IdentityID,
			DocumentID: req.DocumentID, MaxResults: effectiveLimit(req.Limit),
			ACLIsolation: s.MUSRIsolation,
		},
		Revisions: s.RetrievalRevisions,
		Seed:      string(seed), Rerank: s.Reranker != nil,
		Expand:   s.Reranker != nil && s.Knowledge != nil,
		FailSoft: true,
	}
}

// Plans returns a copy of the frozen ordered catalog.
func (f FrozenRetrievalPlans) Plans() []RetrievalPlan {
	return slices.Clone(f.plans)
}

// PlanIDs returns the frozen plan identifiers in canonical order.
func (f FrozenRetrievalPlans) PlanIDs() []string {
	ids := make([]string, len(f.plans))
	for index := range f.plans {
		ids[index] = f.plans[index].ID
	}
	return ids
}

// CatalogRevision returns the digest of the frozen ordered catalog.
func (f FrozenRetrievalPlans) CatalogRevision() string {
	return f.catalogRevision
}

// CorpusEpoch returns the service-owned corpus revision shared by every
// candidate, or zero for an invalid/empty catalog.
func (f FrozenRetrievalPlans) CorpusEpoch() uint64 {
	if len(f.plans) == 0 {
		return 0
	}
	return f.plans[0].Revisions.CorpusEpoch
}

// Plan finds a plan by its frozen identifier.
func (f FrozenRetrievalPlans) Plan(id string) (RetrievalPlan, bool) {
	for _, plan := range f.plans {
		if plan.ID == id {
			return plan, true
		}
	}
	return RetrievalPlan{}, false
}

// ValidateRequest rejects scope or catalog drift after assignment.
func (f FrozenRetrievalPlans) ValidateRequest(req SearchRequest) error {
	revision, err := retrievalCatalogRevision(f.plans)
	if err != nil || revision != f.catalogRevision {
		return errors.New("documents: frozen retrieval catalog is invalid")
	}
	if len(f.plans) == 0 {
		return errors.New("documents: frozen retrieval catalog is empty")
	}
	want := RetrievalScope{
		OwnerID: req.IdentityID, TenantID: req.IdentityID,
		DocumentID: req.DocumentID, MaxResults: effectiveLimit(req.Limit),
		ACLIsolation: f.plans[0].Scope.ACLIsolation,
	}
	for _, plan := range f.plans {
		if plan.Scope != want || plan.Revisions != f.plans[0].Revisions {
			return errors.New("documents: retrieval scope or revisions changed after assignment")
		}
	}
	return nil
}

// ValidateResult is the final pre-exposure guard. It accepts only stable,
// unique chunk IDs produced under the frozen scope and hard caps.
func (f FrozenRetrievalPlans) ValidateResult(
	result RetrievalResult,
	req SearchRequest,
) error {
	if err := f.ValidateRequest(req); err != nil {
		return err
	}
	plan, ok := f.Plan(result.PlanID)
	if !ok {
		return errors.New("documents: result names an unfrozen retrieval plan")
	}
	if result.EffectiveLimit != plan.Scope.MaxResults ||
		len(result.Hits) > plan.Scope.MaxResults {
		return errors.New("documents: retrieval result exceeds its frozen limit")
	}
	if !plan.Expand && len(result.Context) != 0 {
		return errors.New("documents: non-expanding plan returned context")
	}
	if len(result.Context) > len(result.Hits)*neighborsPerWinner {
		return errors.New("documents: retrieval context exceeds its frozen expansion cap")
	}
	seen := make(map[string]struct{}, len(result.Hits)+len(result.Context))
	for _, hit := range result.Flatten() {
		if !validRegisteredChunkID(hit.ChunkID) {
			return errors.New("documents: retrieval result has an invalid chunk ID")
		}
		if req.DocumentID != "" && hit.DocumentID != req.DocumentID {
			return errors.New("documents: retrieval result escaped document scope")
		}
		if _, duplicate := seen[hit.ChunkID]; duplicate {
			return errors.New("documents: retrieval result repeats a chunk ID")
		}
		seen[hit.ChunkID] = struct{}{}
	}
	return nil
}

func validateRetrievalRevisions(revisions RetrievalRevisions) error {
	if revisions.CorpusEpoch == 0 {
		return errors.New("documents: corpus revision is unavailable")
	}
	for name, value := range map[string]string{
		"parser": revisions.Parser, "chunker": revisions.Chunker,
		"embedding": revisions.Embedding, "index": revisions.Index,
		"reranker": revisions.Reranker, "retriever": revisions.Retriever,
	} {
		if !validRetrievalRevision(value) {
			return fmt.Errorf("documents: %s revision %q is invalid", name, value)
		}
	}
	return nil
}

func validRetrievalRevision(value string) bool {
	if len(value) < 3 || len(value) > 96 {
		return false
	}
	hasVersion := false
	for index := range len(value) {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			(index > 0 && (character == '-' || character == '_' || character == '.')) {
			if index > 0 && value[index-1] == 'v' &&
				character >= '0' && character <= '9' {
				hasVersion = true
			}
			continue
		}
		return false
	}
	return hasVersion
}

func validRegisteredChunkID(value string) bool {
	if len(value) < 3 || len(value) > 256 {
		return false
	}
	for index := range len(value) {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			(index > 0 && (character == '-' || character == '_' || character == '.')) {
			continue
		}
		return false
	}
	return true
}

func retrievalCatalogRevision(plans []RetrievalPlan) (string, error) {
	payload, err := json.Marshal(plans)
	if err != nil {
		return "", fmt.Errorf("documents: marshal retrieval catalog: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "retrieval-catalog-v1-" + hex.EncodeToString(sum[:6]), nil
}
