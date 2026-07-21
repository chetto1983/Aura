package idempotency

import (
	"errors"
	"strings"
	"testing"
)

const rawSecretSentinel = "AURA-RAW-SECRET-39-01"

type fingerprintFixture struct {
	Name   string         `json:"name"`
	Labels map[string]int `json:"labels"`
}

type leakingMarshaler struct{}

func (leakingMarshaler) MarshalJSON() ([]byte, error) {
	return nil, errors.New(rawSecretSentinel)
}

func TestFingerprintDeterministicForTypedPayload(t *testing.T) {
	t.Parallel()

	left := fingerprintFixture{Name: "operation", Labels: map[string]int{"alpha": 1, "beta": 2}}
	right := fingerprintFixture{Name: "operation", Labels: map[string]int{"beta": 2, "alpha": 1}}

	leftHash, err := FingerprintTyped(left)
	if err != nil {
		t.Fatalf("FingerprintTyped(left): %v", err)
	}
	rightHash, err := FingerprintTyped(right)
	if err != nil {
		t.Fatalf("FingerprintTyped(right): %v", err)
	}
	if leftHash != rightHash {
		t.Fatalf("equal typed payloads produced different hashes: %x != %x", leftHash, rightHash)
	}
	if got := FingerprintHex(leftHash); len(got) != 64 || got != strings.ToLower(got) {
		t.Fatalf("FingerprintHex() = %q, want 64 lowercase hex characters", got)
	}
}

func TestFingerprintChangesWithMaterialPayload(t *testing.T) {
	t.Parallel()

	first, err := FingerprintTyped(fingerprintFixture{Name: "operation", Labels: map[string]int{"alpha": 1}})
	if err != nil {
		t.Fatalf("FingerprintTyped(first): %v", err)
	}
	second, err := FingerprintTyped(fingerprintFixture{Name: "operation", Labels: map[string]int{"alpha": 2}})
	if err != nil {
		t.Fatalf("FingerprintTyped(second): %v", err)
	}
	if first == second {
		t.Fatalf("materially different payloads produced the same hash: %x", first)
	}
}

func TestFingerprintRejectsNilPayloads(t *testing.T) {
	t.Parallel()

	var ptr *fingerprintFixture
	var values map[string]int
	var bytes []byte
	for _, payload := range []any{nil, ptr, values, bytes} {
		if _, err := FingerprintTyped(payload); !errors.Is(err, ErrFingerprintPayload) {
			t.Errorf("FingerprintTyped(%T) error = %v, want ErrFingerprintPayload", payload, err)
		}
	}
}

func TestFingerprintErrorsDoNotExposeRawPayload(t *testing.T) {
	t.Parallel()

	_, err := FingerprintTyped(leakingMarshaler{})
	if !errors.Is(err, ErrFingerprintPayload) {
		t.Fatalf("error = %v, want ErrFingerprintPayload", err)
	}
	if strings.Contains(err.Error(), rawSecretSentinel) {
		t.Fatalf("fingerprint error exposed raw payload sentinel: %v", err)
	}
}
