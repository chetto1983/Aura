package learning_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/learning"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/wiki"
)

// ---- slog capture -------------------------------------------------------

// warnCapture is a minimal slog.Handler that records Warn-level messages.
type warnCapture struct {
	mu   sync.Mutex
	msgs []string
}

func (w *warnCapture) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}
func (w *warnCapture) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		w.mu.Lock()
		w.msgs = append(w.msgs, r.Message)
		w.mu.Unlock()
	}
	return nil
}
func (w *warnCapture) WithAttrs(_ []slog.Attr) slog.Handler { return w }
func (w *warnCapture) WithGroup(_ string) slog.Handler      { return w }
func (w *warnCapture) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.msgs)
}

// ---- fakes for integration test -----------------------------------------

// guardEmitter implements summarizer.QuestionEmitter and records emitted candidates.
type guardEmitter struct {
	mu      sync.Mutex
	emitted []summarizer.Candidate
}

func (e *guardEmitter) EmitUserMemoryQuestion(_ context.Context, c summarizer.Candidate) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.emitted = append(e.emitted, c)
	return nil
}
func (e *guardEmitter) snapshot() []summarizer.Candidate {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]summarizer.Candidate, len(e.emitted))
	copy(out, e.emitted)
	return out
}
func (e *guardEmitter) reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.emitted = nil
}

// guardMemSearcher returns no hits for any query.
type guardMemSearcher struct{}

func (g *guardMemSearcher) SearchUserMemory(_ context.Context, _ string, _ int) ([]summarizer.UserMemoryHit, error) {
	return nil, nil
}

// guardWiki records wiki writes without touching the filesystem.
type guardWiki struct {
	writes []*wiki.Page
}

func (w *guardWiki) WritePage(_ context.Context, page *wiki.Page, _ ...string) error {
	w.writes = append(w.writes, page)
	return nil
}
func (w *guardWiki) ReadPage(_ string) (*wiki.Page, error) { return nil, nil }
func (w *guardWiki) AppendLog(_ context.Context, _, _ string) {}

// Compile-time interface checks.
var _ summarizer.QuestionEmitter = (*guardEmitter)(nil)
var _ summarizer.MemorySearcher  = (*guardMemSearcher)(nil)
var _ summarizer.WikiWriter      = (*guardWiki)(nil)
var _ slog.Handler               = (*warnCapture)(nil)

// ---- identity fixture helper --------------------------------------------

// seedGuardActors creates two actors on db:
//   - owner (PrincipalKindOwner) WITH CapabilityMemoryUserWrite
//   - dashboard (PrincipalKindService) WITHOUT any capability grant
//
// Both actors use ActorTypeSession with no parent, so they inherit principal grants.
func seedGuardActors(t *testing.T, db *sql.DB) (ownerActor, dashActor identity.Actor) {
	t.Helper()
	ctx := context.Background()
	idStore, err := identity.NewStore(db)
	if err != nil {
		t.Fatalf("identity.NewStore: %v", err)
	}

	// Owner principal + actor + grant
	if _, _, err := idStore.CreateOrResolvePrincipal(ctx, identity.PrincipalParams{
		ID:   "guard-principal-owner",
		Kind: identity.PrincipalKindOwner,
	}); err != nil {
		t.Fatalf("create owner principal: %v", err)
	}
	ownerActor, err = idStore.CreateActor(ctx, identity.ActorParams{
		ID:          "guard-actor-owner",
		PrincipalID: "guard-principal-owner",
		ActorType:   identity.ActorTypeSession,
	})
	if err != nil {
		t.Fatalf("create owner actor: %v", err)
	}
	if _, err := idStore.CreateGrant(ctx, identity.GrantParams{
		ID:          "guard-grant-owner-memwrite",
		SubjectType: identity.SubjectTypePrincipal,
		SubjectID:   "guard-principal-owner",
		Capability:  identity.CapabilityMemoryUserWrite,
	}); err != nil {
		t.Fatalf("create owner grant: %v", err)
	}

	// Dashboard principal + actor — NO capability grant
	if _, _, err := idStore.CreateOrResolvePrincipal(ctx, identity.PrincipalParams{
		ID:   "guard-principal-dashboard",
		Kind: identity.PrincipalKindService,
	}); err != nil {
		t.Fatalf("create dashboard principal: %v", err)
	}
	dashActor, err = idStore.CreateActor(ctx, identity.ActorParams{
		ID:          "guard-actor-dashboard",
		PrincipalID: "guard-principal-dashboard",
		ActorType:   identity.ActorTypeSession,
	})
	if err != nil {
		t.Fatalf("create dashboard actor: %v", err)
	}

	return ownerActor, dashActor
}

// ---- integration tests --------------------------------------------------

// TestUserMemoryWriteGuardsEndToEnd exercises both the Q01 (authorize gate) and
// Q02 (ambiguity question gate) paths end-to-end using in-memory SQLite.
func TestUserMemoryWriteGuardsEndToEnd(t *testing.T) {
	ctx := context.Background()

	// Step 1: shared DB + stores (in-memory SQLite via temp file + migrations).
	db := openTestDB(t) // defined in promoter_test.go — runs migrations.Run
	memStore, err := memoryindex.NewStore(db)
	if err != nil {
		t.Fatalf("memoryindex.NewStore: %v", err)
	}
	idStore, err := identity.NewStore(db)
	if err != nil {
		t.Fatalf("identity.NewStore: %v", err)
	}
	summStore := summarizer.NewSummariesStore(db)

	// Step 2-3: seed actors
	ownerActor, dashActor := seedGuardActors(t, db)

	// Step 4: capture slog.Warn output from WriteApprovedUserFactAs
	capture := &warnCapture{}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })

	// Step 5: build RoutingApplier with question gate
	emitter := &guardEmitter{}
	wikiStore := &guardWiki{}
	ra := summarizer.NewRoutingApplier(
		summarizer.NewAutoApplier(wikiStore),
		summStore,
		&guardMemSearcher{},
	).WithQuestionEmitter(emitter)

	// Step 6: apply 3 candidates
	highScorePref := summarizer.Candidate{
		Fact:     "User prefers dark mode in all editors",
		Category: "preference",
		Score:    0.9,
	}
	lowScorePref := summarizer.Candidate{
		Fact:     "User might enjoy morning runs occasionally",
		Category: "preference",
		Score:    0.5,
	}
	projectC := summarizer.Candidate{
		Fact:     "Aura backend is written in Go",
		Category: "project",
		Score:    0.9,
	}
	for _, c := range []summarizer.Candidate{highScorePref, lowScorePref, projectC} {
		if applyErr := ra.Apply(ctx, summarizer.Decision{Candidate: c, Action: summarizer.ActionNew}); applyErr != nil {
			t.Fatalf("Apply %q: %v", c.Fact, applyErr)
		}
	}

	// Step 7: verify routing results
	proposals, listErr := summStore.List(ctx, "pending", 100)
	if listErr != nil {
		t.Fatalf("List proposals: %v", listErr)
	}
	var userMemProposals []summarizer.ProposedUpdate
	for _, p := range proposals {
		if p.Kind == "user_memory" {
			userMemProposals = append(userMemProposals, p)
		}
	}
	if len(userMemProposals) != 1 {
		t.Fatalf("user_memory proposals after 3 candidates = %d, want 1 (high-score direct only)", len(userMemProposals))
	}
	if userMemProposals[0].Fact != highScorePref.Fact {
		t.Fatalf("direct proposal fact = %q, want %q", userMemProposals[0].Fact, highScorePref.Fact)
	}
	emittedSnapshot := emitter.snapshot()
	if len(emittedSnapshot) != 1 {
		t.Fatalf("questions emitted = %d, want 1 (low-score gated)", len(emittedSnapshot))
	}
	if len(wikiStore.writes) != 1 {
		t.Fatalf("wiki writes = %d, want 1 (project candidate)", len(wikiStore.writes))
	}

	// Step 8: simulate 'Sì, ricordalo' reply for the gated low-score candidate
	if err := summarizer.ApplyUserMemoryAnswer(ctx, summarizer.AnswerApprove, emittedSnapshot[0], summStore, &guardMemSearcher{}); err != nil {
		t.Fatalf("ApplyUserMemoryAnswer(approve): %v", err)
	}

	// Step 9: both user_memory proposals should now exist
	proposals2, listErr2 := summStore.List(ctx, "pending", 100)
	if listErr2 != nil {
		t.Fatalf("List proposals after approve: %v", listErr2)
	}
	var userMemProposals2 []summarizer.ProposedUpdate
	for _, p := range proposals2 {
		if p.Kind == "user_memory" {
			userMemProposals2 = append(userMemProposals2, p)
		}
	}
	if len(userMemProposals2) != 2 {
		t.Fatalf("user_memory proposals after approve reply = %d, want 2", len(userMemProposals2))
	}

	// Step 10: approve high-score proposal with owner actor → succeeds + doc in memoryindex
	highScoreProposal := userMemProposals[0]
	ownerWriteErr := learning.WriteApprovedUserFactAs(ctx, memStore, idStore, ownerActor, learning.Proposal{
		Kind:          "user_memory",
		Fact:          highScoreProposal.Fact,
		SignatureHash: highScoreProposal.SignatureHash,
		Category:      highScoreProposal.Category,
	})
	if ownerWriteErr != nil {
		t.Fatalf("WriteApprovedUserFactAs owner: %v", ownerWriteErr)
	}

	// Step 11: verify compact_memory_documents has the user_memory doc
	docs, searchErr := memStore.Search(ctx, highScoreProposal.SignatureHash, memoryindex.Filter{
		Kinds: []string{memoryindex.KindUserMemory},
	})
	if searchErr != nil {
		t.Fatalf("Search memoryindex: %v", searchErr)
	}
	var foundOwnerDoc bool
	for _, d := range docs {
		if d.Handle == highScoreProposal.SignatureHash {
			foundOwnerDoc = true
		}
	}
	if !foundOwnerDoc {
		t.Fatal("owner approval: user_memory doc not found in compact_memory_documents")
	}

	// Step 12: repeat with dashboard actor (no grant) → ErrPermissionDenied + no doc
	prevWarnCount := capture.count()
	dashHandle := "user_memory:guard-dash-sentinel-001"
	dashWriteErr := learning.WriteApprovedUserFactAs(ctx, memStore, idStore, dashActor, learning.Proposal{
		Kind:          "user_memory",
		Fact:          "Dashboard should not write this",
		SignatureHash: dashHandle,
		Category:      "preference",
	})
	if !errors.Is(dashWriteErr, identity.ErrPermissionDenied) {
		t.Fatalf("dashboard actor: expected ErrPermissionDenied, got %v", dashWriteErr)
	}
	// Doc must NOT appear in memoryindex
	docs2, searchErr2 := memStore.Search(ctx, dashHandle, memoryindex.Filter{})
	if searchErr2 != nil {
		t.Fatalf("Search dashboard doc: %v", searchErr2)
	}
	for _, d := range docs2 {
		if d.Handle == dashHandle {
			t.Error("dashboard actor: doc must NOT be written on denial")
		}
	}

	// Step 13: verify slog.Warn was emitted for the denial
	if capture.count() <= prevWarnCount {
		t.Fatal("slog.Warn for dashboard denial was not captured")
	}

	// AC7: free-text modify path — reply replaces Candidate.Fact then creates proposal
	emitter.reset()
	lowScoreC2 := summarizer.Candidate{
		Fact:     "User might drink decaf sometimes",
		Category: "preference",
		Score:    0.4,
	}
	if applyErr := ra.Apply(ctx, summarizer.Decision{Candidate: lowScoreC2, Action: summarizer.ActionNew}); applyErr != nil {
		t.Fatalf("Apply low-score for free-text gate: %v", applyErr)
	}
	emittedFreeText := emitter.snapshot()
	if len(emittedFreeText) != 1 {
		t.Fatalf("free-text gate: emitted = %d, want 1", len(emittedFreeText))
	}
	freeTextReply := "Mi chiamo Davide e amo il caffè"
	if err := summarizer.ApplyUserMemoryAnswer(ctx, freeTextReply, emittedFreeText[0], summStore, &guardMemSearcher{}); err != nil {
		t.Fatalf("ApplyUserMemoryAnswer(free-text): %v", err)
	}
	// Verify the proposal carries the modified fact, not the original
	proposals3, listErr3 := summStore.List(ctx, "pending", 100)
	if listErr3 != nil {
		t.Fatalf("List proposals after free-text: %v", listErr3)
	}
	var foundFreeText bool
	for _, p := range proposals3 {
		if p.Kind == "user_memory" && p.Fact == freeTextReply {
			foundFreeText = true
		}
	}
	if !foundFreeText {
		t.Fatal("free-text modify: proposal with modified fact not found in proposed_updates")
	}
}

// TestUserMemoryQuestionGateRejection verifies that 'No, scarta' discards the
// candidate: no proposed_updates row is created and no compact_memory_documents
// doc appears in the memoryindex.
func TestUserMemoryQuestionGateRejection(t *testing.T) {
	ctx := context.Background()

	db := openTestDB(t)
	summStore := summarizer.NewSummariesStore(db)
	memStore, err := memoryindex.NewStore(db)
	if err != nil {
		t.Fatalf("memoryindex.NewStore: %v", err)
	}

	emitter := &guardEmitter{}
	ra := summarizer.NewRoutingApplier(
		summarizer.NewAutoApplier(&guardWiki{}),
		summStore,
		&guardMemSearcher{},
	).WithQuestionEmitter(emitter)

	lowScoreC := summarizer.Candidate{
		Fact:     "User might like jazz music on Fridays",
		Category: "preference",
		Score:    0.45,
	}
	if applyErr := ra.Apply(ctx, summarizer.Decision{Candidate: lowScoreC, Action: summarizer.ActionNew}); applyErr != nil {
		t.Fatalf("Apply: %v", applyErr)
	}
	emittedSnapshot := emitter.snapshot()
	if len(emittedSnapshot) != 1 {
		t.Fatalf("questions emitted = %d, want 1", len(emittedSnapshot))
	}

	// User rejects
	if rejectErr := summarizer.ApplyUserMemoryAnswer(ctx, summarizer.AnswerReject, emittedSnapshot[0], summStore, &guardMemSearcher{}); rejectErr != nil {
		t.Fatalf("ApplyUserMemoryAnswer(reject): %v", rejectErr)
	}

	// No proposed_updates rows of kind user_memory
	proposals, listErr := summStore.List(ctx, "pending", 100)
	if listErr != nil {
		t.Fatalf("List: %v", listErr)
	}
	for _, p := range proposals {
		if p.Kind == "user_memory" {
			t.Errorf("rejection: unexpected user_memory proposal: %q", p.Fact)
		}
	}

	// No compact_memory_documents docs
	docs, searchErr := memStore.Search(ctx, lowScoreC.Fact, memoryindex.Filter{
		Kinds: []string{memoryindex.KindUserMemory},
	})
	if searchErr != nil {
		t.Fatalf("Search: %v", searchErr)
	}
	if len(docs) != 0 {
		t.Errorf("rejection: unexpected user_memory docs in memoryindex: %d", len(docs))
	}
}
