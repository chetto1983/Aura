package retention

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// Action identifies a finite adapter-owned cleanup effect.
type Action string

// Supported cleanup actions.
const (
	ActionDeleteArtifact Action = "delete_artifact"
	ActionExpireReplay   Action = "expire_replay"
	ActionDeleteMetadata Action = "delete_metadata"
)

// ErrTokenMismatch rejects blank, changed, or stale apply requests.
var ErrTokenMismatch = errors.New("retention plan token mismatch")

// Candidate contains only trusted identifiers. Adapters reconstruct paths/keys.
type Candidate struct {
	IdentityID     string `json:"identity_id"`
	ConversationID string `json:"conversation_id,omitempty"`
	ArtifactID     string `json:"artifact_id,omitempty"`
	Version        int64  `json:"version"`
	Action         Action `json:"action"`
	Class          Class  `json:"class"`
	Bytes          int64  `json:"bytes"`
}

// Plan is a deterministic immutable dry-run snapshot.
type Plan struct {
	PolicyVersion string      `json:"policy_version"`
	Candidates    []Candidate `json:"candidates"`
	Token         string      `json:"token"`
	TotalBytes    int64       `json:"total_bytes"`
}

type tokenCandidate struct {
	IdentityID     string `json:"identity_id"`
	ConversationID string `json:"conversation_id,omitempty"`
	ArtifactID     string `json:"artifact_id,omitempty"`
	Version        int64  `json:"version"`
	Action         Action `json:"action"`
}

// BuildPlan validates, sorts, caps, and hashes one candidate snapshot.
func BuildPlan(policyVersion string, candidates []Candidate, cap int) (Plan, error) {
	if strings.TrimSpace(policyVersion) == "" {
		return Plan{}, fmt.Errorf("policy version is required")
	}
	if cap <= 0 {
		return Plan{}, fmt.Errorf("candidate cap must be positive")
	}
	sorted := slices.Clone(candidates)
	for i := range sorted {
		if err := sorted[i].Validate(); err != nil {
			return Plan{}, fmt.Errorf("candidate %d: %w", i, err)
		}
	}
	slices.SortFunc(sorted, compareCandidate)
	tokenShape := struct {
		PolicyVersion string           `json:"policy_version"`
		Candidates    []tokenCandidate `json:"candidates"`
	}{PolicyVersion: policyVersion, Candidates: make([]tokenCandidate, 0, len(sorted))}
	for _, candidate := range sorted {
		tokenShape.Candidates = append(tokenShape.Candidates, tokenCandidate{
			IdentityID: candidate.IdentityID, ConversationID: candidate.ConversationID,
			ArtifactID: candidate.ArtifactID, Version: candidate.Version, Action: candidate.Action,
		})
	}
	body, err := json.Marshal(tokenShape)
	if err != nil {
		return Plan{}, fmt.Errorf("marshal retention plan: %w", err)
	}
	planned := sorted
	if len(planned) > cap {
		planned = planned[:cap]
	}
	var total int64
	for _, candidate := range planned {
		total += candidate.Bytes
	}
	sum := sha256.Sum256(body)
	return Plan{PolicyVersion: policyVersion, Candidates: planned, Token: hex.EncodeToString(sum[:]), TotalBytes: total}, nil
}

// RequireToken authorizes apply only for this exact snapshot.
func (p Plan) RequireToken(token string) error {
	if token == "" || token != p.Token {
		return ErrTokenMismatch
	}
	return nil
}

// Validate rejects path-like or incomplete candidate identifiers.
func (c Candidate) Validate() error {
	if err := validateTrustedID("identity", c.IdentityID); err != nil {
		return err
	}
	if c.ConversationID == "" && c.ArtifactID == "" {
		return fmt.Errorf("conversation or artifact id is required")
	}
	for name, value := range map[string]string{"conversation": c.ConversationID, "artifact": c.ArtifactID} {
		if value != "" {
			if err := validateTrustedID(name, value); err != nil {
				return err
			}
		}
	}
	if c.Version < 0 || c.Bytes < 0 {
		return fmt.Errorf("version and bytes cannot be negative")
	}
	if c.Action == "" {
		return fmt.Errorf("action is required")
	}
	return nil
}

// ItemKey returns a deterministic content-free database ordering key.
func (c Candidate) ItemKey() string {
	body, _ := json.Marshal(tokenCandidate{
		IdentityID: c.IdentityID, ConversationID: c.ConversationID,
		ArtifactID: c.ArtifactID, Version: c.Version, Action: c.Action,
	})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func compareCandidate(a, b Candidate) int {
	return strings.Compare(a.ItemKey(), b.ItemKey())
}

func validateTrustedID(name, value string) error {
	if value == "" || len(value) > 200 || value == "." || value == ".." || strings.ContainsAny(value, `/\\`) {
		return fmt.Errorf("invalid %s id", name)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("invalid %s id", name)
		}
	}
	return nil
}
