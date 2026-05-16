package summarizer

import (
	"context"
	"fmt"
	"log/slog"
)

// AmbiguityThreshold is the score below which a user_memory candidate is
// considered ambiguous and requires explicit user confirmation before a
// proposed_updates row is created.
// 0.7 was chosen as the empirical break-point between LLM-confident extractions
// and edge cases; export this const so callers can tune it without recompiling
// the summarizer package.
const AmbiguityThreshold = 0.7

// AnswerApprove, AnswerReject, AnswerModify are the canonical fixed options
// sent with every user-memory gate question. Matching is exact-string.
const (
	AnswerApprove = "Sì, ricordalo"
	AnswerReject  = "No, scarta"
	AnswerModify  = "Sì ma con modifica"
)

// QuestionEmitter emits an ambiguity gate question for a low-confidence
// user_memory candidate. Implementations persist a chat_questions row and
// notify the user channel; the pending Candidate is stored in metadata_json
// so the answer callback can resume proposal creation.
type QuestionEmitter interface {
	EmitUserMemoryQuestion(ctx context.Context, c Candidate) error
}

// ShouldGateUserMemoryWrite returns true when the candidate triages to
// TargetUserMemory AND its Score is below AmbiguityThreshold.
// Score==0 (unset) is below threshold and will trigger the gate when a
// QuestionEmitter is wired; callers that don't attach an emitter are unaffected.
func ShouldGateUserMemoryWrite(c Candidate) bool {
	return TriageCandidate(c) == TargetUserMemory && c.Score < AmbiguityThreshold
}

// UserMemoryQuestionText formats the canonical gate question text.
func UserMemoryQuestionText(c Candidate) string {
	return fmt.Sprintf("Vuoi che ricordi: %s? (categoria: %s, score: %.2f)", c.Fact, c.Category, c.Score)
}

// UserMemoryQuestionOptions returns the fixed ordered reply options.
func UserMemoryQuestionOptions() []string {
	return []string{AnswerApprove, AnswerReject, AnswerModify}
}

// ApplyUserMemoryAnswer processes a user's reply to a gated question and takes
// the appropriate action:
//   - AnswerReject ("No, scarta"): no-op + slog.Info.
//   - AnswerApprove or AnswerModify: proceeds with proposal creation using the
//     original candidate (AnswerModify without replacement text is treated as approval).
//   - Any other non-empty text: replaces Candidate.Fact with the user's text
//     then creates the proposal (free-text modification path).
func ApplyUserMemoryAnswer(
	ctx context.Context,
	answer string,
	c Candidate,
	proposals UserMemoryProposalWriter,
	memory MemorySearcher,
) error {
	if answer == AnswerReject {
		slog.InfoContext(ctx, "user memory gate: candidate rejected by user",
			"category", c.Category,
			"handle", UserFactHandle(c))
		return nil
	}
	if answer != AnswerApprove && answer != AnswerModify && answer != "" {
		// Free-text: user rewrote the fact — replace and proceed.
		c.Fact = answer
	}
	return submitUserMemoryProposal(ctx, c, proposals, memory)
}

// submitUserMemoryProposal is the shared proposal creation helper used by both
// RoutingApplier.createUserMemoryProposal and ApplyUserMemoryAnswer.
func submitUserMemoryProposal(
	ctx context.Context,
	c Candidate,
	proposals UserMemoryProposalWriter,
	memory MemorySearcher,
) error {
	handle := UserFactHandle(c)
	hits, err := memory.SearchUserMemory(ctx, c.Fact, 1)
	if err != nil {
		return fmt.Errorf("routing applier: user memory search: %w", err)
	}
	action := string(ActionNew)
	targetHandle := ""
	if len(hits) > 0 {
		action = string(ActionPatch)
		targetHandle = hits[0].Handle
	}
	_, err = proposals.CreateUserMemoryProposal(ctx, UserMemoryProposalInput{
		Handle:        handle,
		Fact:          c.Fact,
		Action:        action,
		TargetHandle:  targetHandle,
		SourceTurnIDs: c.SourceTurnIDs,
		Category:      c.Category,
	})
	return err
}
