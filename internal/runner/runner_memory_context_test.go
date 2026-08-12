package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

type memoryContextFake struct {
	content     string
	err         error
	identityID  string
	searchOut   string
	searchErr   error
	searchQuery string
	searchCalls int
}

func (f *memoryContextFake) Context(_ context.Context, identityID string) (string, error) {
	f.identityID = identityID
	return f.content, f.err
}

func (f *memoryContextFake) Search(_ context.Context, identityID, query string) (string, error) {
	f.identityID = identityID
	f.searchQuery = query
	f.searchCalls++
	return f.searchOut, f.searchErr
}

func strptr(s string) *string { return &s }

func TestLoadMemoryContextFencesAuthenticatedIdentityFacts(t *testing.T) {
	provider := &memoryContextFake{content: "Davide located_in Caraglio"}
	r := &Runner{memoryContext: provider}
	ctx := identityctx.WithIdentityID(context.Background(), "identity-a")

	got := r.loadMemoryContext(ctx, strptr("hi"))
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
	// Preload is off by default: no search call, no recall fence.
	if provider.searchCalls != 0 {
		t.Fatalf("preload disabled must not call Search, got %d", provider.searchCalls)
	}
	if strings.Contains(got.Content, memoryRecallHeader) {
		t.Fatalf("recall fence must be absent when preload is off: %q", got.Content)
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
			if got := r.loadMemoryContext(setup.ctx, strptr("hi")); got != nil {
				t.Fatalf("context = %+v, want omitted memory", got)
			}
		})
	}
}

func TestLoadMemoryContext_PreloadInjectsRecall(t *testing.T) {
	provider := &memoryContextFake{content: "digest fact", searchOut: "recalled fact"}
	r := &Runner{memoryContext: provider, memoryPreloadEnabled: true}
	ctx := identityctx.WithIdentityID(context.Background(), "identity-a")

	got := r.loadMemoryContext(ctx, strptr("what do I prefer"))
	if got == nil {
		t.Fatal("memory context is nil")
	}
	if provider.searchCalls != 1 || provider.searchQuery != "what do I prefer" {
		t.Fatalf("preload must search the user text once: calls=%d q=%q", provider.searchCalls, provider.searchQuery)
	}
	if !strings.Contains(got.Content, memoryContextHeader) || !strings.Contains(got.Content, "digest fact") {
		t.Fatalf("digest section missing: %q", got.Content)
	}
	if !strings.Contains(got.Content, memoryRecallHeader) || !strings.Contains(got.Content, "recalled fact") {
		t.Fatalf("recall section missing: %q", got.Content)
	}
}

func TestLoadMemoryContext_PreloadFailSoftKeepsDigest(t *testing.T) {
	provider := &memoryContextFake{content: "digest fact", searchErr: errors.New("search offline")}
	r := &Runner{memoryContext: provider, memoryPreloadEnabled: true}
	ctx := identityctx.WithIdentityID(context.Background(), "identity-a")

	got := r.loadMemoryContext(ctx, strptr("q"))
	if got == nil {
		t.Fatal("a preload failure must not drop the digest")
	}
	if !strings.Contains(got.Content, "digest fact") {
		t.Fatalf("digest must survive a preload error: %q", got.Content)
	}
	if strings.Contains(got.Content, memoryRecallHeader) {
		t.Fatalf("no recall fence on preload error: %q", got.Content)
	}
}

func TestLoadMemoryContext_PreloadOnlyWhenDigestEmpty(t *testing.T) {
	provider := &memoryContextFake{content: "", searchOut: "recalled fact"}
	r := &Runner{memoryContext: provider, memoryPreloadEnabled: true}
	ctx := identityctx.WithIdentityID(context.Background(), "identity-a")

	got := r.loadMemoryContext(ctx, strptr("q"))
	if got == nil {
		t.Fatal("a recall-only result must still inject memory")
	}
	if strings.Contains(got.Content, memoryContextHeader) {
		t.Fatalf("no digest fence when the digest is empty: %q", got.Content)
	}
	if !strings.Contains(got.Content, memoryRecallHeader) || !strings.Contains(got.Content, "recalled fact") {
		t.Fatalf("recall section missing: %q", got.Content)
	}
}

func TestLoadMemoryContext_NilUserMsgSkipsPreload(t *testing.T) {
	provider := &memoryContextFake{content: "digest fact", searchOut: "recalled fact"}
	r := &Runner{memoryContext: provider, memoryPreloadEnabled: true}
	ctx := identityctx.WithIdentityID(context.Background(), "identity-a")

	got := r.loadMemoryContext(ctx, nil) // resume/branch continuation
	if got == nil {
		t.Fatal("digest must still load on a nil user message")
	}
	if provider.searchCalls != 0 {
		t.Fatalf("a nil user message must skip the preload search, got %d", provider.searchCalls)
	}
	if got.BeforeCurrentUser {
		t.Fatal("a nil user message must not set BeforeCurrentUser")
	}
}
