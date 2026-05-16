package summarizer_test

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/conversation/summarizer"
)

// fakeQuestionEmitter records emitted candidates for assertions.
type fakeQuestionEmitter struct {
	emitted []summarizer.Candidate
	err     error
}

func (f *fakeQuestionEmitter) EmitUserMemoryQuestion(_ context.Context, c summarizer.Candidate) error {
	if f.err != nil {
		return f.err
	}
	f.emitted = append(f.emitted, c)
	return nil
}

// TestShouldGateUserMemoryWrite_AboveThreshold verifies that a high-confidence
// candidate (Score >= AmbiguityThreshold) is NOT gated.
func TestShouldGateUserMemoryWrite_AboveThreshold(t *testing.T) {
	c := summarizer.Candidate{
		Fact:     "User prefers dark mode",
		Category: "preference",
		Score:    summarizer.AmbiguityThreshold, // exactly at threshold = not gated
	}
	if summarizer.ShouldGateUserMemoryWrite(c) {
		t.Fatal("ShouldGateUserMemoryWrite = true for Score == AmbiguityThreshold, want false")
	}
	c.Score = 0.9
	if summarizer.ShouldGateUserMemoryWrite(c) {
		t.Fatal("ShouldGateUserMemoryWrite = true for Score 0.9, want false")
	}
}

// TestShouldGateUserMemoryWrite_BelowThreshold verifies that a low-confidence
// user_memory candidate IS gated, and a wiki candidate is never gated.
func TestShouldGateUserMemoryWrite_BelowThreshold(t *testing.T) {
	c := summarizer.Candidate{
		Fact:     "User might like tea",
		Category: "preference",
		Score:    0.5,
	}
	if !summarizer.ShouldGateUserMemoryWrite(c) {
		t.Fatal("ShouldGateUserMemoryWrite = false for Score 0.5 user_memory, want true")
	}
	// Wiki category is never gated regardless of score.
	cWiki := summarizer.Candidate{
		Fact:     "Project uses Go backend",
		Category: "project",
		Score:    0.1,
	}
	if summarizer.ShouldGateUserMemoryWrite(cWiki) {
		t.Fatal("ShouldGateUserMemoryWrite = true for wiki category, want false")
	}
}

// TestRoutingApplier_BelowThresholdEmitsQuestion verifies that when a
// QuestionEmitter is wired and a candidate has Score < AmbiguityThreshold,
// the applier calls EmitUserMemoryQuestion and does NOT create a proposal.
func TestRoutingApplier_BelowThresholdEmitsQuestion(t *testing.T) {
	emitter := &fakeQuestionEmitter{}
	writer := &fakeUserMemoryWriter{}
	wiki := &fakeWikiStore{}
	ra := summarizer.NewRoutingApplier(summarizer.NewAutoApplier(wiki), writer, &fakeMemorySearcher{}).
		WithQuestionEmitter(emitter)

	err := ra.Apply(context.Background(), summarizer.Decision{
		Candidate: summarizer.Candidate{
			Fact:     "User might prefer coffee over tea",
			Category: "preference",
			Score:    0.5, // below threshold
		},
		Action: summarizer.ActionNew,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(writer.rows) != 0 {
		t.Fatalf("proposed_updates rows = %d, want 0 (question emitted instead)", len(writer.rows))
	}
	if len(emitter.emitted) != 1 {
		t.Fatalf("questions emitted = %d, want 1", len(emitter.emitted))
	}
}

// TestApplyUserMemoryAnswer_Approve verifies that AnswerApprove creates a proposal.
func TestApplyUserMemoryAnswer_Approve(t *testing.T) {
	writer := &fakeUserMemoryWriter{}
	c := summarizer.Candidate{
		Fact:     "User likes espresso",
		Category: "preference",
		Score:    0.5,
	}
	err := summarizer.ApplyUserMemoryAnswer(context.Background(), summarizer.AnswerApprove, c, writer, &fakeMemorySearcher{})
	if err != nil {
		t.Fatalf("ApplyUserMemoryAnswer(approve): %v", err)
	}
	if len(writer.rows) != 1 {
		t.Fatalf("proposed_updates rows = %d, want 1", len(writer.rows))
	}
	if writer.rows[0].Fact != c.Fact {
		t.Fatalf("fact = %q, want %q", writer.rows[0].Fact, c.Fact)
	}
}

// TestApplyUserMemoryAnswer_Reject verifies that AnswerReject discards the candidate.
func TestApplyUserMemoryAnswer_Reject(t *testing.T) {
	writer := &fakeUserMemoryWriter{}
	c := summarizer.Candidate{
		Fact:     "User might like sushi",
		Category: "preference",
		Score:    0.4,
	}
	err := summarizer.ApplyUserMemoryAnswer(context.Background(), summarizer.AnswerReject, c, writer, &fakeMemorySearcher{})
	if err != nil {
		t.Fatalf("ApplyUserMemoryAnswer(reject): %v", err)
	}
	if len(writer.rows) != 0 {
		t.Fatalf("proposed_updates rows = %d, want 0 (rejected)", len(writer.rows))
	}
}

// TestApplyUserMemoryAnswer_FreeTextModify verifies that a free-text reply
// replaces Candidate.Fact and then creates a proposal with the new text.
func TestApplyUserMemoryAnswer_FreeTextModify(t *testing.T) {
	writer := &fakeUserMemoryWriter{}
	c := summarizer.Candidate{
		Fact:     "User maybe enjoys hiking",
		Category: "preference",
		Score:    0.45,
	}
	newFact := "Mi chiamo Davide e amo il caffè"
	err := summarizer.ApplyUserMemoryAnswer(context.Background(), newFact, c, writer, &fakeMemorySearcher{})
	if err != nil {
		t.Fatalf("ApplyUserMemoryAnswer(free-text): %v", err)
	}
	if len(writer.rows) != 1 {
		t.Fatalf("proposed_updates rows = %d, want 1", len(writer.rows))
	}
	if writer.rows[0].Fact != newFact {
		t.Fatalf("fact = %q, want %q (free-text replacement)", writer.rows[0].Fact, newFact)
	}
}

// TestRoutingApplier_AboveThresholdRoutesDirect verifies that a Score≥0.7
// candidate bypasses the question gate even when a QuestionEmitter is wired.
func TestRoutingApplier_AboveThresholdRoutesDirect(t *testing.T) {
	emitter := &fakeQuestionEmitter{}
	writer := &fakeUserMemoryWriter{}
	wiki := &fakeWikiStore{}
	ra := summarizer.NewRoutingApplier(summarizer.NewAutoApplier(wiki), writer, &fakeMemorySearcher{}).
		WithQuestionEmitter(emitter)

	err := ra.Apply(context.Background(), summarizer.Decision{
		Candidate: summarizer.Candidate{
			Fact:     "User prefers dark mode",
			Category: "preference",
			Score:    0.9, // above threshold — direct route
		},
		Action: summarizer.ActionNew,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(writer.rows) != 1 {
		t.Fatalf("proposed_updates rows = %d, want 1 (above threshold = direct)", len(writer.rows))
	}
	if len(emitter.emitted) != 0 {
		t.Fatalf("questions emitted = %d, want 0 (above threshold)", len(emitter.emitted))
	}
}

// Ensure fakeQuestionEmitter satisfies the interface (compile-time check).
var _ summarizer.QuestionEmitter = (*fakeQuestionEmitter)(nil)
