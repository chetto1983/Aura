package conversations

import (
	"encoding/base64"
	"errors"
	"fmt"
)

// AuthorityLevel records the original instruction authority without permitting promotion.
type AuthorityLevel string

const (
	// AuthorityUser preserves user-level authority without promotion.
	AuthorityUser AuthorityLevel = "user"
	// AuthorityTool records quoted tool data, never executable tool authority.
	AuthorityTool AuthorityLevel = "tool"
	// AuthoritySystem is parseable only so validation can explicitly reject summary promotion.
	AuthoritySystem AuthorityLevel = "system"
)

// InstructionState records whether an instruction remains unresolved or was revoked.
type InstructionState string

const (
	// InstructionActive marks an unresolved instruction still in force at its source authority.
	InstructionActive InstructionState = "active"
	// InstructionRevoked records an explicit revocation of an earlier source event.
	InstructionRevoked InstructionState = "revoked"
)

// AuthorityLedgerEvent is a typed, source-addressed unresolved-instruction event.
type AuthorityLedgerEvent struct {
	SourceSeq        int              `json:"source_seq"`
	Authority        AuthorityLevel   `json:"authority"`
	State            InstructionState `json:"state"`
	RevokesSourceSeq int              `json:"revokes_source_seq,omitempty"`
	QuotedData       string           `json:"quoted_data_b64"`
}

// ValidateAuthorityLedger rejects promotions, raw quotations, and invalid revocations.
func ValidateAuthorityLedger(events []AuthorityLedgerEvent) error {
	seen := make(map[int]struct{}, len(events))
	for _, event := range events {
		if event.SourceSeq <= 0 || (event.Authority != AuthorityUser && event.Authority != AuthorityTool) {
			return errors.New("invalid authority ledger source or authority")
		}
		if _, exists := seen[event.SourceSeq]; exists {
			return errors.New("duplicate authority source")
		}
		if _, err := base64.StdEncoding.DecodeString(event.QuotedData); err != nil {
			return fmt.Errorf("quotation must be base64 data: %w", err)
		}
		if event.State == InstructionRevoked {
			if event.RevokesSourceSeq <= 0 {
				return errors.New("revocation missing source")
			}
			if _, ok := seen[event.RevokesSourceSeq]; !ok {
				return errors.New("revocation references unknown source")
			}
		} else if event.State != InstructionActive {
			return errors.New("invalid instruction state")
		}
		seen[event.SourceSeq] = struct{}{}
	}
	return nil
}
