package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/memory"
)

var errInvalidCompactionMemoryRequest = errors.New("invalid compaction memory request")

// CompactionMemoryStore is the operator control-plane seam for the IC-10 durable-memory
// lifecycle. The concrete memory.Store is wired only at daemon boot; no model tool uses it.
type CompactionMemoryStore interface {
	CreateCandidate(context.Context, memory.CandidateInput) (memory.Candidate, error)
	Promote(context.Context, memory.Policy, memory.Principal, string) (memory.Memory, error)
	Retrieve(context.Context, memory.Policy, memory.Principal, int) ([]memory.Memory, error)
	WithdrawConsent(context.Context, string, string, string) error
	DeleteSource(context.Context, string, string) error
	ForgetMe(context.Context, string, string) error
	Expire(context.Context, time.Time) error
	Supersede(context.Context, string, string) error
}

type compactionMemoryCandidateRequest struct {
	TenantID        string             `json:"tenant_id"`
	OwnerID         string             `json:"owner_id"`
	Class           string             `json:"class"`
	Purpose         string             `json:"purpose"`
	ConsentBasis    string             `json:"consent_basis"`
	SourceManifest  []memory.SourceRef `json:"source_manifest"`
	Evidence        string             `json:"evidence"`
	Confidence      float64            `json:"confidence"`
	Authority       string             `json:"authority"`
	Sensitivity     string             `json:"sensitivity"`
	Region          string             `json:"region"`
	EncryptionClass string             `json:"encryption_class"`
	RetentionClass  string             `json:"retention_class"`
	ExpiresAt       time.Time          `json:"expires_at"`
}

func (r compactionMemoryCandidateRequest) input() memory.CandidateInput {
	return memory.CandidateInput{
		TenantID: r.TenantID, OwnerID: r.OwnerID, Class: r.Class, Purpose: r.Purpose,
		ConsentBasis: r.ConsentBasis, SourceManifest: r.SourceManifest, Evidence: r.Evidence,
		Confidence: r.Confidence, Authority: r.Authority, Sensitivity: r.Sensitivity,
		Region: r.Region, EncryptionClass: r.EncryptionClass, RetentionClass: r.RetentionClass,
		ExpiresAt: r.ExpiresAt,
	}
}

type compactionMemoryClassPolicyRequest struct {
	AllowPromotion bool `json:"allow_promotion"`
	AllowRetrieval bool `json:"allow_retrieval"`
}

type compactionMemoryPolicyRequest struct {
	ReviewApproved bool                                          `json:"review_approved"`
	Classes        map[string]compactionMemoryClassPolicyRequest `json:"classes"`
}

func (r compactionMemoryPolicyRequest) policy() memory.Policy {
	classes := make(map[string]memory.ClassPolicy, len(r.Classes))
	for name, class := range r.Classes {
		classes[name] = memory.ClassPolicy{AllowPromotion: class.AllowPromotion, AllowRetrieval: class.AllowRetrieval}
	}
	return memory.Policy{ReviewApproved: r.ReviewApproved, Classes: classes}
}

type compactionMemoryPrincipalRequest struct {
	TenantID       string   `json:"tenant_id"`
	OwnerID        string   `json:"owner_id"`
	Purpose        string   `json:"purpose"`
	Region         string   `json:"region"`
	Capabilities   []string `json:"capabilities"`
	MaxSensitivity string   `json:"max_sensitivity"`
}

func (r compactionMemoryPrincipalRequest) principal() memory.Principal {
	return memory.Principal{TenantID: r.TenantID, OwnerID: r.OwnerID, Purpose: r.Purpose, Region: r.Region, Capabilities: r.Capabilities, MaxSensitivity: r.MaxSensitivity}
}

type compactionMemoryRequest struct {
	Candidate    *compactionMemoryCandidateRequest `json:"candidate"`
	Policy       compactionMemoryPolicyRequest     `json:"policy"`
	Principal    compactionMemoryPrincipalRequest  `json:"principal"`
	CandidateID  string                            `json:"candidate_id"`
	Tenant       string                            `json:"tenant"`
	Owner        string                            `json:"owner"`
	Purpose      string                            `json:"purpose"`
	SourceKind   string                            `json:"source_kind"`
	SourceID     string                            `json:"source_id"`
	OldCandidate string                            `json:"old_candidate"`
	NewCandidate string                            `json:"new_candidate"`
	Limit        int                               `json:"limit"`
	Now          time.Time                         `json:"now"`
}

func (s *Server) registerCompactionMemoryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/admin/compaction-memory/{action}", s.handleCompactionMemoryAction)
}

func (s *Server) handleCompactionMemoryAction(w http.ResponseWriter, r *http.Request) {
	if s.compactionMemory == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "compaction memory not configured"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	var req compactionMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	result, err := s.runCompactionMemoryAction(r.Context(), r.PathValue("action"), req)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, errInvalidCompactionMemoryRequest) || errors.Is(err, memory.ErrPromotionDisabled) || errors.Is(err, memory.ErrPolicyDenied) || errors.Is(err, memory.ErrSecretEvidence) || errors.Is(err, memory.ErrEvidenceOvercollection) {
			status = http.StatusBadRequest
		}
		writeJSONStatus(w, status, map[string]string{"error": "compaction memory operation failed"})
		return
	}
	writeJSON(w, result)
}

func (s *Server) runCompactionMemoryAction(ctx context.Context, action string, req compactionMemoryRequest) (any, error) {
	switch action {
	case "create":
		if req.Candidate == nil {
			return nil, errInvalidCompactionMemoryRequest
		}
		return s.compactionMemory.CreateCandidate(ctx, req.Candidate.input())
	case "promote":
		if req.CandidateID == "" {
			return nil, errInvalidCompactionMemoryRequest
		}
		return s.compactionMemory.Promote(ctx, req.Policy.policy(), req.Principal.principal(), req.CandidateID)
	case "retrieve":
		if req.Limit <= 0 {
			return nil, errInvalidCompactionMemoryRequest
		}
		return s.compactionMemory.Retrieve(ctx, req.Policy.policy(), req.Principal.principal(), req.Limit)
	case "withdraw_consent":
		if req.Tenant == "" || req.Owner == "" || req.Purpose == "" {
			return nil, errInvalidCompactionMemoryRequest
		}
		return compactionMemoryOK(s.compactionMemory.WithdrawConsent(ctx, req.Tenant, req.Owner, req.Purpose))
	case "delete_source":
		if req.SourceKind == "" || req.SourceID == "" {
			return nil, errInvalidCompactionMemoryRequest
		}
		return compactionMemoryOK(s.compactionMemory.DeleteSource(ctx, req.SourceKind, req.SourceID))
	case "forget_me":
		if req.Tenant == "" || req.Owner == "" {
			return nil, errInvalidCompactionMemoryRequest
		}
		return compactionMemoryOK(s.compactionMemory.ForgetMe(ctx, req.Tenant, req.Owner))
	case "expire":
		if req.Now.IsZero() {
			return nil, errInvalidCompactionMemoryRequest
		}
		return compactionMemoryOK(s.compactionMemory.Expire(ctx, req.Now))
	case "supersede":
		if req.OldCandidate == "" || req.NewCandidate == "" {
			return nil, errInvalidCompactionMemoryRequest
		}
		return compactionMemoryOK(s.compactionMemory.Supersede(ctx, req.OldCandidate, req.NewCandidate))
	default:
		return nil, errInvalidCompactionMemoryRequest
	}
}

func compactionMemoryOK(err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return map[string]string{"status": "ok"}, nil
}
