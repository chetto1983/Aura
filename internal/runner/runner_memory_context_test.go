package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

type memoryContextFake struct {
	content    string
	err        error
	identityID string
}

func (f *memoryContextFake) Context(_ context.Context, identityID string) (string, error) {
	f.identityID = identityID
	return f.content, f.err
}

func TestLoadMemoryContextFencesAuthenticatedIdentityFacts(t *testing.T) {
	provider := &memoryContextFake{content: "Davide located_in Caraglio"}
	r := &Runner{memoryContext: provider}
	ctx := identityctx.WithIdentityID(context.Background(), "identity-a")

	got := r.loadMemoryContext(ctx, true)
	if got == nil {
		t.Fatal("memory context is nil")
	}
	if provider.identityID != "identity-a" {
		t.Fatalf("provider identity = %q", provider.identityID)
	}
	if !got.BeforeCurrentUser {
		t.Fatal("memory must be inserted immediately before the current user")
	}
	if strings.Count(got.Content, memoryContextHeader) != 1 ||
		strings.Count(got.Content, memoryContextFooter) != 1 ||
		!strings.Contains(got.Content, provider.content) {
		t.Fatalf("fenced content = %q", got.Content)
	}
}

func TestLoadMemoryContextFailureDoesNotBlockTheTurn(t *testing.T) {
	for name, setup := range map[string]struct {
		ctx      context.Context
		provider *memoryContextFake
	}{
		"missing identity": {ctx: context.Background(), provider: &memoryContextFake{content: "fact"}},
		"provider error": {
			ctx:      identityctx.WithIdentityID(context.Background(), "identity-a"),
			provider: &memoryContextFake{err: errors.New("offline")},
		},
		"empty digest": {
			ctx:      identityctx.WithIdentityID(context.Background(), "identity-a"),
			provider: &memoryContextFake{content: "  "},
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := &Runner{memoryContext: setup.provider}
			if got := r.loadMemoryContext(setup.ctx, true); got != nil {
				t.Fatalf("context = %+v, want omitted memory", got)
			}
		})
	}
}
