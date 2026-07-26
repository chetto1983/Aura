package main

import (
	"testing"

	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/google/uuid"
)

func TestRunAdaptiveBenchmarkInChatEnvironmentRejectsIncompleteRuntime(
	t *testing.T,
) {
	t.Parallel()
	if draft, err := runAdaptiveBenchmarkInChatEnvironmentWithOps(
		t.Context(),
		nil,
		adaptiveBenchmarkRunRequest{},
		uuid.Nil,
		auraeval.AdaptiveBenchmarkModel{},
		adaptiveBenchmarkReportOps{},
	); err == nil || draft != nil {
		t.Fatalf("draft=%#v error=%v", draft, err)
	}
}
