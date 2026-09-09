package openrouterprovision

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestClassify403LimitExceeded(t *testing.T) {
	err := classify(http.StatusForbidden, []byte(`{"error":{"message":"Key limit exceeded (monthly limit)"}}`))
	if !errors.Is(err, ErrKeyLimitExceeded) {
		t.Fatalf("err = %v, want ErrKeyLimitExceeded", err)
	}
}

func TestClassify403NotLimitExceeded(t *testing.T) {
	err := classify(http.StatusForbidden, []byte("forbidden"))
	if errors.Is(err, ErrKeyLimitExceeded) {
		t.Fatalf("err = %v, must NOT be ErrKeyLimitExceeded — the status alone is not enough, an auth failure is also 403-shaped", err)
	}
}

func TestClassify401IsRevoked(t *testing.T) {
	err := classify(http.StatusUnauthorized, []byte(`{"error":{"message":"invalid api key"}}`))
	if !errors.Is(err, ErrKeyRevoked) {
		t.Fatalf("err = %v, want ErrKeyRevoked", err)
	}
	if errors.Is(err, ErrKeyLimitExceeded) {
		t.Error("a 401 must not also satisfy ErrKeyLimitExceeded")
	}
}

func TestClassify404IsNotFound(t *testing.T) {
	err := classify(http.StatusNotFound, []byte(`{"error":{"message":"key not found"}}`))
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("err = %v, want ErrKeyNotFound", err)
	}
}

func TestClassifyPreservesStatusInMessage(t *testing.T) {
	const fakeKey = "sk-or-v1-should-never-appear"
	err := classify(http.StatusForbidden, []byte(`{"error":{"message":"Key limit exceeded (monthly limit)"}}`))
	if err == nil {
		t.Fatal("classify returned nil for a 403")
	}
	msg := err.Error()
	if !strings.Contains(msg, "403") {
		t.Errorf("message = %q, want it to carry the status 403", msg)
	}
	if strings.Contains(msg, fakeKey) {
		t.Errorf("message = %q, must never carry a credential", msg)
	}
}

func TestErrSpendNotApplicableIsReused(t *testing.T) {
	if !errors.Is(ErrKeyNotApplicable, llm.ErrSpendNotApplicable) {
		t.Fatal("ErrKeyNotApplicable must be internal/llm's ErrSpendNotApplicable, not a second sentinel")
	}
}
