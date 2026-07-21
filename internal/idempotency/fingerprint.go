package idempotency

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
)

// FingerprintTyped hashes the deterministic encoding/json representation of a
// typed, caller-normalized boundary value. Marshal failures are intentionally
// collapsed so a hostile Marshaler cannot leak payload bytes through errors.
func FingerprintTyped(payload any) ([32]byte, error) {
	if isNilPayload(payload) {
		return [32]byte{}, fmt.Errorf("%w: %T", ErrFingerprintPayload, payload)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return [32]byte{}, fmt.Errorf("%w: %T", ErrFingerprintPayload, payload)
	}
	return sha256.Sum256(encoded), nil
}

// FingerprintHex renders a fingerprint for wire and diagnostic metadata.
func FingerprintHex(fingerprint [32]byte) string {
	return hex.EncodeToString(fingerprint[:])
}

func isNilPayload(payload any) bool {
	if payload == nil {
		return true
	}
	value := reflect.ValueOf(payload)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
