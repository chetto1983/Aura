package conversations

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewCompactionOperationIDReturnsDistinctParsableUUIDs(t *testing.T) {
	a, err := NewCompactionOperationID()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewCompactionOperationID()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("NewCompactionOperationID produced duplicate IDs: %s", a)
	}
	if _, err := uuid.Parse(a); err != nil {
		t.Fatalf("NewCompactionOperationID produced unparsable UUID %q: %v", a, err)
	}
}
