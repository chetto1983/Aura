package adaptive

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/google/uuid"
)

// RandomizedArm is the frozen independent-Bernoulli arm identity.
type RandomizedArm string

const (
	RandomizedArmBaseline   RandomizedArm = "baseline"
	RandomizedArmChallenger RandomizedArm = "challenger"
)

// RandomizedArmSelection records the one-byte mechanism output.
type RandomizedArmSelection struct {
	Arm       RandomizedArm
	DrawByte  string
	MappedBit uint8
}

// MapRandomizationByte maps bit zero to the frozen arm order.
func MapRandomizationByte(draw byte) RandomizedArm {
	if draw&1 == 0 {
		return RandomizedArmBaseline
	}
	return RandomizedArmChallenger
}

// DrawRandomizedArm reads one byte from the production OS CSPRNG boundary.
func DrawRandomizedArm() (RandomizedArmSelection, error) {
	return drawRandomizedArm(rand.Reader)
}

func drawRandomizedArm(source io.Reader) (RandomizedArmSelection, error) {
	if source == nil {
		return RandomizedArmSelection{}, errors.New("randomization entropy source is nil")
	}
	var draw [1]byte
	if _, err := io.ReadFull(source, draw[:]); err != nil {
		return RandomizedArmSelection{}, fmt.Errorf("read randomization byte: %w", err)
	}
	bit := draw[0] & 1
	return RandomizedArmSelection{
		Arm:       MapRandomizationByte(draw[0]),
		DrawByte:  hex.EncodeToString(draw[:]),
		MappedBit: bit,
	}, nil
}

// ExactActionProbability is one ordered selected-intention behavior probability.
type ExactActionProbability struct {
	ActionID    string        `json:"action_id"`
	Probability ExactRational `json:"probability"`
}

// MarginalActionProbabilities derives mu(a|x) from the two deterministic arms.
func MarginalActionProbabilities(
	supportedActions []string,
	championActionID string,
	challengerActionID string,
) ([]ExactActionProbability, error) {
	if len(supportedActions) == 0 {
		return nil, errors.New("supported action catalog is empty")
	}
	if championActionID != "static" {
		return nil, errors.New("champion action must be static")
	}
	if !slices.Contains(supportedActions, championActionID) ||
		!slices.Contains(supportedActions, challengerActionID) {
		return nil, errors.New("arm action is outside the supported catalog")
	}
	seen := make(map[string]struct{}, len(supportedActions))
	probabilities := make([]ExactActionProbability, len(supportedActions))
	for index, actionID := range supportedActions {
		if actionID == "" {
			return nil, errors.New("supported action id is empty")
		}
		if _, duplicate := seen[actionID]; duplicate {
			return nil, fmt.Errorf("duplicate supported action %q", actionID)
		}
		seen[actionID] = struct{}{}
		numerator := int64(0)
		if actionID == championActionID {
			numerator++
		}
		if actionID == challengerActionID {
			numerator++
		}
		probabilities[index] = ExactActionProbability{
			ActionID:    actionID,
			Probability: MustExactRational(numerator, 2),
		}
	}
	return probabilities, nil
}

const (
	analysisStratumSchemaID  = "aura.adaptive.analysis-stratum/owner-claim-time-block/v1"
	interferenceSchemaID     = "aura.adaptive.interference-cluster/conversation/v1"
	randomizationMechanismID = "aura/go-crypto-rand-bit"
	randomizationEntropyID   = "go-crypto-rand/os-csprng"
)

// AnalysisStratum is the frozen owner plus trusted claim-time block.
type AnalysisStratum struct {
	SchemaSHA256 string
	ID           string
	TimeBlockID  int64
}

// NewAnalysisStratum derives both descriptor and member hashes.
func NewAnalysisStratum(ownerID string, claimedAt time.Time, blockWidthSeconds int64) (AnalysisStratum, error) {
	if err := validateCanonicalUUID(ownerID); err != nil {
		return AnalysisStratum{}, fmt.Errorf("owner id: %w", err)
	}
	if blockWidthSeconds <= 0 {
		return AnalysisStratum{}, errors.New("block width must be positive")
	}
	claimedAt = claimedAt.UTC().Truncate(time.Microsecond)
	if claimedAt.Before(time.Unix(0, 0).UTC()) {
		return AnalysisStratum{}, errors.New("claim time precedes block origin")
	}
	timeBlockID := claimedAt.UnixMicro() / (blockWidthSeconds * 1_000_000)
	descriptor := fmt.Sprintf(
		`{"schema_id":"%s","revision":1,"field_order":["schema_id","revision","owner_id","time_block_id"],"block_origin":"1970-01-01T00:00:00Z","block_width_seconds":%d}`,
		analysisStratumSchemaID,
		blockWidthSeconds,
	)
	member := fmt.Sprintf(
		`{"schema_id":"%s","revision":1,"owner_id":"%s","time_block_id":%d}`,
		analysisStratumSchemaID,
		ownerID,
		timeBlockID,
	)
	return AnalysisStratum{
		SchemaSHA256: sha256Hex([]byte(descriptor)),
		ID:           sha256Hex([]byte(member)),
		TimeBlockID:  timeBlockID,
	}, nil
}

// InterferenceCluster is the frozen owner-conversation resampling unit.
type InterferenceCluster struct {
	ID string
}

// NewInterferenceCluster derives the canonical conversation cluster ID.
func NewInterferenceCluster(ownerID, evaluationConversationID string) (InterferenceCluster, error) {
	if err := validateCanonicalUUID(ownerID); err != nil {
		return InterferenceCluster{}, fmt.Errorf("owner id: %w", err)
	}
	if err := validateCanonicalUUID(evaluationConversationID); err != nil {
		return InterferenceCluster{}, fmt.Errorf("evaluation conversation id: %w", err)
	}
	preimage := fmt.Sprintf(
		`{"schema_id":"%s","revision":1,"owner_id":"%s","evaluation_conversation_id":"%s"}`,
		interferenceSchemaID,
		ownerID,
		evaluationConversationID,
	)
	return InterferenceCluster{ID: sha256Hex([]byte(preimage))}, nil
}

func validateCanonicalUUID(value string) error {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value {
		return errors.New("must be a lowercase hyphenated UUID")
	}
	return nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
