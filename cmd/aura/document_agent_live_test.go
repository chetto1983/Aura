//go:build document_live_e2e

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/cachemetrics"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

type recordingDocumentLibrary struct {
	inner tools.DocumentLibrary
	mu    sync.Mutex
	calls []documents.RetrievalRequest
}

func (l *recordingDocumentLibrary) Retrieve(
	ctx context.Context, request documents.RetrievalRequest,
) (documents.RetrievalResponse, error) {
	l.mu.Lock()
	l.calls = append(l.calls, request)
	l.mu.Unlock()
	return l.inner.Retrieve(ctx, request)
}

func (l *recordingDocumentLibrary) requests() []documents.RetrievalRequest {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]documents.RetrievalRequest(nil), l.calls...)
}

func requiredDocumentLiveEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("document_live_e2e requires %s", name)
	}
	return value
}

func TestDocumentProductionAgentE2E(t *testing.T) {
	identityA := requiredDocumentLiveEnv(t, "AURA_DOCUMENT_E2E_IDENTITY_A")
	identityB := requiredDocumentLiveEnv(t, "AURA_DOCUMENT_E2E_IDENTITY_B")
	markerA := requiredDocumentLiveEnv(t, "AURA_DOCUMENT_E2E_MARKER_A")
	markerB := requiredDocumentLiveEnv(t, "AURA_DOCUMENT_E2E_MARKER_B")
	requiredDocumentLiveEnv(t, "OPENROUTER_API_KEY")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	for label, identityID := range map[string]string{"a": identityA, "b": identityB} {
		name := fmt.Sprintf("document-e2e-%s-%s@example.test", label, identityID[:8])
		if _, err := pool.Exec(ctx,
			`INSERT INTO aura.identities (id, name, kind) VALUES ($1::uuid, $2, 'user')`,
			identityID, name,
		); err != nil {
			t.Fatalf("insert identity %s: %v", label, err)
		}
		if err := db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
			_, grantErr := tx.Exec(ctx,
				`INSERT INTO aura.capability_grants (identity_id, capability) VALUES ($1::uuid, 'agent.run')`,
				identityID,
			)
			return grantErr
		}); err != nil {
			t.Fatalf("grant identity %s: %v", label, err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM aura.identities WHERE id = ANY($1::uuid[])`, []string{identityA, identityB})
	})

	library := newDocumentLibrary(pool, cfg)
	assertDocumentMarker(t, library, identityA, "private launch code", markerA)
	assertDocumentMarker(t, library, identityB, "private launch code", markerB)
	foreign, err := library.Retrieve(identityctx.WithIdentityID(ctx, identityA), documents.RetrievalRequest{
		IdentityID: identityA, Query: markerB, Limit: 8,
	})
	if err != nil {
		t.Fatalf("cross-identity probe: %v", err)
	}
	if responseContains(foreign, markerB) {
		t.Fatalf("identity A recalled identity B marker %q", markerB)
	}

	recorded := &recordingDocumentLibrary{inner: library}
	registry := tools.NewRegistry()
	registry.Register(tools.TextResponse{})
	registry.Register(&tools.DocumentSearch{Library: recorded})
	runDir := t.TempDir()
	conv := conversations.New(pool, conversations.Config{RunDir: runDir, TurnCapBytes: 65_536})
	pause := askuser.New(pool)
	r := runner.New(runner.Deps{
		Conv: conv, Pause: pause, ApprovalExpiry: pause,
		Identity: identity.New(pool), CacheMetrics: cachemetrics.New(pool),
		ToolInvocations: toolinvocations.New(pool),
		Client:          openai_compat.New(cfg.LLM), Registry: registry, LLM: cfg.LLM,
		RunDir: runDir, PreviewCap: 4096, TitleTimeout: 30 * time.Second, StopTimeout: 45 * time.Second,
	})
	ownerCtx := identityctx.WithIdentityID(ctx, identityA)
	conversationID := uuid.Must(uuid.NewV7()).String()
	if _, err := r.NewConversationWithID(ownerCtx, conversationID); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	t.Cleanup(func() {
		_ = r.Stop(context.Background(), conversationID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.conversations WHERE id = $1`, conversationID)
	})

	prompt := "Use document_search to answer this question from my uploaded alpha document: " +
		"what is the private launch code? Return the code exactly."
	var reply strings.Builder
	sawSearch := false
	for event, turnErr := range r.Turn(ownerCtx, conversationID, &prompt) {
		if turnErr != nil {
			t.Fatalf("agent turn: %v", turnErr)
		}
		if event.LLMResponse != nil {
			reply.WriteString(event.LLMResponse.Content)
		}
		if invocation := event.Actions.ToolInvocation; invocation != nil &&
			invocation.Event == "end" && invocation.ToolName == "document_search" {
			sawSearch = true
		}
	}
	if !sawSearch {
		t.Fatal("real agent did not execute document_search")
	}
	if !strings.Contains(reply.String(), markerA) || strings.Contains(reply.String(), markerB) {
		t.Fatalf("agent reply did not stay in identity A: %q", reply.String())
	}
	calls := recorded.requests()
	if len(calls) == 0 {
		t.Fatal("document_search did not reach the production library")
	}
	for _, call := range calls {
		if call.IdentityID != identityA {
			t.Fatalf("agent search used identity %q, want %q", call.IdentityID, identityA)
		}
	}
}

func assertDocumentMarker(
	t *testing.T, library *documentLibrary, identityID, query, marker string,
) {
	t.Helper()
	response, err := library.Retrieve(
		identityctx.WithIdentityID(t.Context(), identityID),
		documents.RetrievalRequest{IdentityID: identityID, Query: query, Limit: 8},
	)
	if err != nil {
		t.Fatalf("retrieve for %s: %v", identityID, err)
	}
	if !responseContains(response, marker) {
		t.Fatalf("marker %q absent from identity %s response: %#v", marker, identityID, response)
	}
}

func responseContains(response documents.RetrievalResponse, marker string) bool {
	for _, document := range response.Documents {
		if strings.Contains(document.Card, marker) {
			return true
		}
		for _, passage := range document.Passages {
			if strings.Contains(passage.Text, marker) {
				return true
			}
		}
	}
	return false
}
