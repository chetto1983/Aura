package pimprovider

import (
	"strings"
	"testing"
)

const testSecretHex = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func TestNewStoreRequiresAPool(t *testing.T) {
	if _, err := NewStore(nil, testSecretHex); err == nil || !strings.Contains(err.Error(), "pool") {
		t.Fatalf("NewStore(nil pool) err = %v, want a pool error", err)
	}
}
