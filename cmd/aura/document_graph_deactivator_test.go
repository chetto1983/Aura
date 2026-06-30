package main

import (
	"context"
	"testing"
)

func TestRuntimeDocumentGraphDeactivatorRequiresConfig(t *testing.T) {
	err := (runtimeDocumentGraphDeactivator{}).DeactivateDocument(context.Background(), "doc-1")
	if err == nil {
		t.Fatal("expected missing config error")
	}
}
