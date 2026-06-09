package agenttest_test

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
)

func TestFakeClientLastRequestEmptyIsZero(t *testing.T) {
	fc := agenttest.NewFakeClient()
	got := fc.LastRequest()
	if got.Model != "" || len(got.Messages) != 0 {
		t.Fatalf("empty LastRequest = %+v, want zero request", got)
	}
}
