package conversations

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func userTurn(seq int, content string) Turn {
	return Turn{Seq: seq, Role: llm.RoleUser, Content: content}
}

func assistantTurn(seq int, content string) Turn {
	return Turn{Seq: seq, Role: llm.RoleAssistant, Content: content}
}

func turnSeqs(turns []Turn) []int {
	out := make([]int, 0, len(turns))
	for _, turn := range turns {
		out = append(out, turn.Seq)
	}
	return out
}

func assertSeqs(t *testing.T, got []Turn, want ...int) {
	t.Helper()
	seqs := turnSeqs(got)
	if len(seqs) != len(want) {
		t.Fatalf("turns = %v, want %v", seqs, want)
	}
	for i := range want {
		if seqs[i] != want[i] {
			t.Fatalf("turns = %v, want %v", seqs, want)
		}
	}
}

// The measured case: conversation 019ffabe, seq 76 and 77, the same 42 characters three
// minutes apart because the first run answered nothing. The LATER turn survives — it is the
// one the assistant replied to, and the one the active round is anchored on.
func TestDropRepeatedUserTurnsKeepsTheLaterOfAnIdenticalPair(t *testing.T) {
	question := "cosa vedi in tutto il tuo contesto adesso?"
	turns := []Turn{
		userTurn(74, "cosa vedi adesso?"),
		assistantTurn(75, "Vedo il blocco memory_context aggiornato"),
		userTurn(76, question),
		userTurn(77, question),
		assistantTurn(78, "Ecco cosa vedo"),
	}

	assertSeqs(t, dropRepeatedUserTurns(turns), 74, 75, 77, 78)
}

// Two different messages in a row are a person typing twice before the agent answers. Real
// corpus, same deployment: merging them would put words in their mouth.
func TestDropRepeatedUserTurnsKeepsTwoDifferentMessages(t *testing.T) {
	turns := []Turn{
		userTurn(72, "via non capisci un cazzo"),
		userTurn(73, "percè????"),
	}

	assertSeqs(t, dropRepeatedUserTurns(turns), 72, 73)
}

func TestDropRepeatedUserTurnsCollapsesARunOfRepeats(t *testing.T) {
	turns := []Turn{
		userTurn(1, "riprova"),
		userTurn(2, "riprova"),
		userTurn(3, "riprova"),
		assistantTurn(4, "eccomi"),
	}

	assertSeqs(t, dropRepeatedUserTurns(turns), 3, 4)
}

// Identical questions asked again LATER are not a repeat: the person asked twice about a
// world that moved in between, and the assistant's answer sits between them.
func TestDropRepeatedUserTurnsKeepsAnAnsweredQuestionAskedAgain(t *testing.T) {
	turns := []Turn{
		userTurn(1, "che ore sono?"),
		assistantTurn(2, "le tre"),
		userTurn(3, "che ore sono?"),
	}

	assertSeqs(t, dropRepeatedUserTurns(turns), 1, 2, 3)
}

// The always-block rides as a user-role turn at messages[1]. Collapsing it into the question
// beside it would dislodge the cached prefix of every conversation, which is a far bigger
// loss than the repetition this function exists to remove.
func TestDropRepeatedUserTurnsNeverTouchesTheSyntheticHead(t *testing.T) {
	block := Turn{
		Seq: alwaysBlockSeq, Role: llm.RoleUser,
		Content: "ricorda", ToolCallID: alwaysBlockMarker,
	}
	turns := []Turn{block, userTurn(2, "ricorda")}

	got := dropRepeatedUserTurns(turns)
	assertSeqs(t, got, alwaysBlockSeq, 2)
	if !isAlwaysBlock(got[0]) {
		t.Fatalf("the always-block lost its marker: %#v", got[0])
	}
}

func contents(turns []Turn) []string {
	out := make([]string, 0, len(turns))
	for _, turn := range turns {
		out = append(out, turn.Content)
	}
	return out
}

// The half recordInterruptedRound cannot reach: a SIGKILL or a crash leaves no moment in
// which to write, so the record has to be reconstructed when the history is read back. By
// then the round that produced it is over by definition — no sweep, no grace period.
func TestMarkUnansweredUserTurnsRecordsAQuestionNothingAnswered(t *testing.T) {
	turns := []Turn{
		userTurn(1, "cosa vedi adesso?"),
		userTurn(2, "lascia stare, dimmi che ore sono"),
		assistantTurn(3, "le tre"),
	}

	got := contents(markUnansweredUserTurns(turns))
	want := []string{
		"cosa vedi adesso?", unansweredMarker, "lascia stare, dimmi che ore sono", "le tre",
	}
	if len(got) != len(want) {
		t.Fatalf("contents = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("contents = %#v, want %#v", got, want)
		}
	}
}

// An answered question is not unanswered, however the answer arrived.
func TestMarkUnansweredUserTurnsLeavesAnsweredRoundsAlone(t *testing.T) {
	turns := []Turn{
		userTurn(1, "che ore sono?"),
		assistantTurn(2, "le tre"),
		userTurn(3, "grazie"),
	}

	if got := markUnansweredUserTurns(turns); len(got) != 3 {
		t.Fatalf("turns = %#v, want the history untouched", contents(got))
	}
}

// The final user turn is the one being answered right now. Marking it would tell the model
// its own question had already failed.
func TestMarkUnansweredUserTurnsNeverMarksTheActiveQuestion(t *testing.T) {
	turns := []Turn{assistantTurn(1, "eccomi"), userTurn(2, "che ore sono?")}

	if got := markUnansweredUserTurns(turns); len(got) != 2 {
		t.Fatalf("turns = %#v, want the active question untouched", contents(got))
	}
}

// The always-block rides as a user-role turn and is bookkeeping, not anybody's message: no
// answer was ever owed to it, and a marker after it would sit between the cached prefix and
// the first real turn.
func TestMarkUnansweredUserTurnsIgnoresTheSyntheticHead(t *testing.T) {
	turns := []Turn{
		{Seq: alwaysBlockSeq, Role: llm.RoleUser, Content: "ricorda", ToolCallID: alwaysBlockMarker},
		userTurn(3, "che ore sono?"),
	}

	if got := markUnansweredUserTurns(turns); len(got) != 2 {
		t.Fatalf("turns = %#v, want no marker after the always-block", contents(got))
	}
}

// Composition with the collapse: a repeat is ONE unanswered question, not two.
func TestRepeatCollapseRunsBeforeTheUnansweredMarker(t *testing.T) {
	question := "cosa vedi in tutto il tuo contesto adesso?"
	turns := []Turn{
		userTurn(76, question), userTurn(77, question), assistantTurn(78, "ecco"),
	}

	got := markUnansweredUserTurns(dropRepeatedUserTurns(turns))
	if len(got) != 2 || got[0].Content != question {
		t.Fatalf("contents = %#v, want the repeat collapsed and no marker", contents(got))
	}
}

func TestDropRepeatedUserTurnsLeavesShortHistoriesAlone(t *testing.T) {
	assertSeqs(t, dropRepeatedUserTurns(nil))
	assertSeqs(t, dropRepeatedUserTurns([]Turn{userTurn(1, "ciao")}), 1)
}
