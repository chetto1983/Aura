package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	toolregistry "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/config"
	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/storage/freshness"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/telegram"
)

func TestRegisterMemoryRecallToolsWiresTypedTiersAndFreshness(t *testing.T) {
	ctx := context.Background()
	db, err := auradb.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Run(ctx, db); err != nil {
		t.Fatalf("migrations.Run: %v", err)
	}

	memoryStore, err := memoryindex.NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	freshnessStore := freshness.NewStore(db)
	if _, err := freshnessStore.Upsert(ctx, freshness.Row{
		ProjectionID:     "compact_memory_documents",
		Kind:             "compact_memory",
		EmbeddingModelID: "embeddinggemma-300m@256-mrl",
		EmbeddingDim:     256,
		IndexBuildID:     "phase07d-build",
		SchemaVersion:    1,
		Status:           "degraded",
	}); err != nil {
		t.Fatalf("freshness upsert: %v", err)
	}

	updatedAt := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	const (
		mixedQuery         = "phase07d mixedtier"
		userApprovedToken  = "PHASE07D_USER_APPROVED_ONLY"
		userPendingToken   = "PHASE07D_USER_PENDING_LEAK"
		operationalToken   = "PHASE07D_OPERATIONAL_APPROVED_ONLY"
		operationalPending = "PHASE07D_OPERATIONAL_PENDING_LEAK"
		sourceToken        = "PHASE07D_SOURCE_SHOULD_NOT_LEAK_TO_RECALL"
		archiveToken       = "PHASE07D_ARCHIVE_SHOULD_NOT_LEAK_TO_RECALL"
		proposalToken      = "PHASE07D_PROPOSAL_SHOULD_NOT_LEAK_TO_RECALL"
	)
	for _, doc := range []memoryindex.Document{
		{
			ID:               "user_memory:pref-dark-mode",
			Kind:             memoryindex.KindUserMemory,
			Title:            "Prefers dark mode in IDEs",
			Body:             "The operator prefers dark mode in IDEs. phase07d mixedtier " + userApprovedToken,
			Handle:           "user_memory:pref-dark-mode",
			Status:           "active",
			Tags:             []string{"preference"},
			UpdatedAt:        updatedAt,
			EmbeddingModelID: "embeddinggemma-300m@256-mrl",
			IndexBuildID:     "phase07d-build",
		},
		{
			ID:               "operational:web-search-io",
			Kind:             memoryindex.KindOperational,
			Title:            "Lesson: web_search io",
			Body:             "Tool `web_search` repeated `io` failures. Reason pattern: timeout. phase07d mixedtier " + operationalToken,
			Handle:           "operational:web-search-io",
			Status:           "active",
			UpdatedAt:        updatedAt,
			EmbeddingModelID: "embeddinggemma-300m@256-mrl",
			IndexBuildID:     "phase07d-build",
		},
		{
			ID:               "user_memory:pending-pref",
			Kind:             memoryindex.KindUserMemory,
			Title:            "Pending user memory should stay hidden",
			Body:             "phase07d mixedtier " + userPendingToken,
			Handle:           "user_memory:pending-pref",
			Status:           "pending",
			Tags:             []string{"preference"},
			UpdatedAt:        updatedAt.Add(time.Minute),
			EmbeddingModelID: "embeddinggemma-300m@256-mrl",
			IndexBuildID:     "phase07d-build",
		},
		{
			ID:               "operational:pending-io",
			Kind:             memoryindex.KindOperational,
			Title:            "Lesson: web_search pending",
			Body:             "phase07d mixedtier " + operationalPending,
			Handle:           "operational:pending-io",
			Status:           "pending",
			UpdatedAt:        updatedAt.Add(time.Minute),
			EmbeddingModelID: "embeddinggemma-300m@256-mrl",
			IndexBuildID:     "phase07d-build",
		},
		{
			ID:               "source:phase07d-mixedtier",
			Kind:             memoryindex.KindSource,
			Title:            "Source trap for Phase07D",
			Body:             "phase07d mixedtier " + sourceToken,
			Handle:           "source:phase07d-mixedtier#page=1",
			SourceID:         "src_phase07d",
			Page:             1,
			Status:           "active",
			UpdatedAt:        updatedAt,
			EmbeddingModelID: "embeddinggemma-300m@256-mrl",
			IndexBuildID:     "phase07d-build",
		},
		{
			ID:               "archive:phase07d-mixedtier",
			Kind:             memoryindex.KindArchive,
			Title:            "Archive trap for Phase07D",
			Body:             "phase07d mixedtier " + archiveToken,
			Handle:           "conversation:phase07d",
			Status:           "active",
			UpdatedAt:        updatedAt,
			EmbeddingModelID: "embeddinggemma-300m@256-mrl",
			IndexBuildID:     "phase07d-build",
		},
		{
			ID:               "proposal:phase07d-mixedtier",
			Kind:             memoryindex.KindProposal,
			Title:            "Proposal trap for Phase07D",
			Body:             "phase07d mixedtier " + proposalToken,
			Handle:           "proposal:phase07d",
			Status:           "pending",
			UpdatedAt:        updatedAt,
			EmbeddingModelID: "embeddinggemma-300m@256-mrl",
			IndexBuildID:     "phase07d-build",
		},
	} {
		if err := memoryStore.Upsert(ctx, doc); err != nil {
			t.Fatalf("Upsert %s: %v", doc.ID, err)
		}
	}

	registry := toolregistry.NewRegistry(slog.Default())
	app := &App{
		deps: telegram.Deps{
			Cfg:         &config.Config{MemorySearchTimeoutMS: 5000},
			Tools:       registry,
			MemoryStore: memoryStore,
		},
		freshnessStore: freshnessStore,
	}
	app.registerMemoryRecallTools(app.deps.Cfg)

	for _, name := range []string{"search"} {
		if registry.Get(name) == nil {
			t.Fatalf("expected %s to be registered", name)
		}
	}
	for _, name := range []string{"search_memory", "recall_operational", "recall_user_memory"} {
		if registry.Get(name) != nil {
			t.Fatalf("%s must NOT be registered (folded into search)", name)
		}
	}

	userOut, err := registry.Get("search").Execute(ctx, map[string]any{
		"action":   "user_facts",
		"query":    "dark mode",
		"category": "preference",
	})
	if err != nil {
		t.Fatalf("search user_facts: %v", err)
	}
	assertContainsAll(t, "user recall", userOut, []string{
		"[user_memory]",
		"user_memory:pref-dark-mode",
		userApprovedToken,
		"freshness=",
		"degraded_read=true",
	})
	assertContainsNone(t, "user recall", userOut, []string{
		userPendingToken,
		operationalToken,
		operationalPending,
		sourceToken,
		archiveToken,
		proposalToken,
	})

	operationalOut, err := registry.Get("search").Execute(ctx, map[string]any{
		"action":    "lessons",
		"tool_name": "web_search",
	})
	if err != nil {
		t.Fatalf("search lessons: %v", err)
	}
	assertContainsAll(t, "operational recall", operationalOut, []string{
		"[operational]",
		"operational:web-search-io",
		operationalToken,
		"freshness=",
		"degraded_read=true",
	})
	assertContainsNone(t, "operational recall", operationalOut, []string{
		operationalPending,
		userApprovedToken,
		userPendingToken,
		sourceToken,
		archiveToken,
		proposalToken,
	})

	searchOut, err := registry.Get("search").Execute(ctx, map[string]any{
		"action": "search",
		"query":  mixedQuery,
		"zone":   "all",
		"top_k":  10,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	assertContainsAll(t, "broad memory search", searchOut, []string{
		sourceToken,
		archiveToken,
		proposalToken,
	})
	assertContainsNone(t, "broad memory search", searchOut, []string{
		userApprovedToken,
		userPendingToken,
		operationalToken,
		operationalPending,
		"user_memory:pref-dark-mode",
		"operational:web-search-io",
	})
}

func assertContainsAll(t *testing.T, label, got string, wants []string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("%s missing %q:\n%s", label, want, got)
		}
	}
}

func assertContainsNone(t *testing.T, label, got string, rejects []string) {
	t.Helper()
	for _, reject := range rejects {
		if strings.Contains(got, reject) {
			t.Fatalf("%s leaked %q:\n%s", label, reject, got)
		}
	}
}
