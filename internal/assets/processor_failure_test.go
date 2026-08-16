package assets

import (
	"errors"
	"fmt"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
)

// The sentinel reaches here through two wraps -- documents.Service adds "catalog document:"
// and the processor returns it on -- so the classification is asserted through that same
// chain rather than against a bare sentinel that no caller ever produces.
func TestProcessorFailureCodeSurvivesTheRealWrappingChain(t *testing.T) {
	wrapped := fmt.Errorf(
		"catalog document: %w", documents.ErrDocumentDeleteInFlight)
	returned := fmt.Errorf("process asset: %w", wrapped)

	if got := processorFailureCode(returned); got != DeleteInFlightCode {
		t.Fatalf("code = %q, want %q through the wraps the real path applies", got, DeleteInFlightCode)
	}
}

// Everything else stays what it was: a file that is not going to become searchable.
func TestProcessorFailureCodeLeavesOtherFailuresAlone(t *testing.T) {
	for name, err := range map[string]error{
		"unsupported type": errors.New(`unsupported document type ".xyz"`),
		"too large":        fmt.Errorf("%w: 90 bytes exceeds 10", documents.ErrFileTooLarge),
		"transport":        errors.New("object store unreachable"),
	} {
		t.Run(name, func(t *testing.T) {
			if got := processorFailureCode(err); got != ProcessorFailedCode {
				t.Fatalf("code = %q, want %q", got, ProcessorFailedCode)
			}
		})
	}
}

// A nil error has no code to give, and asking for one would mean a caller reached this on
// the success path.
func TestProcessorFailureCodeOnNilIsTheGenericCode(t *testing.T) {
	if got := processorFailureCode(nil); got != ProcessorFailedCode {
		t.Fatalf("code = %q, want %q", got, ProcessorFailedCode)
	}
}
