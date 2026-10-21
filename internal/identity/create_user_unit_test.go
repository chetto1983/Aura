package identity

import (
	"context"
	"errors"
	"testing"
)

// The capability check runs before the transaction is touched, so a nil tx proves nothing
// was written.
func TestCreateUserTx_RejectsCapabilityBeforeWriting(t *testing.T) {
	cases := map[string]error{Wildcard: ErrWildcardManaged, "Not A Capability": ErrInvalidCapability}
	for capability, want := range cases {
		if _, err := CreateUserTx(context.Background(), nil, "x", []string{CapAgentRun, capability}); !errors.Is(err, want) {
			t.Errorf("CreateUserTx(%q) err = %v, want %v", capability, err, want)
		}
	}
}
