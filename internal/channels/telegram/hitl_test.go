package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/runner"
)

// fakeResume is the resumeRunner double. It records SubmitAnswer calls and the
// resume Turn drive, and serves a scripted PendingFor list. It NEVER writes
// paused_states — proving the channel is render-only over the Runner (the Runner
// is the sole writer; the double just models its surface).
type fakeResume struct {
	mu sync.Mutex

	pending    []askuser.Pending
	submitted  []submitCall
	remaining  int                   // returned by SubmitAnswer
	outcome    runner.ResolveOutcome // scripted verdict when neither cancel nor remaining applies
	resumes    int                   // Turn(convID,nil) drives
	submitErr  error
	pendingErr error

	clearPendingOnSubmit bool
}

type submitCall struct {
	token   string
	action  string
	content string
}

func (f *fakeResume) PendingFor(_ context.Context, _ string) ([]askuser.Pending, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pendingErr != nil {
		return nil, f.pendingErr
	}
	return f.pending, nil
}

func (f *fakeResume) SubmitAnswer(_ context.Context, token string, resp runner.ResponseInput) (runner.ResolveDirective, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submitted = append(f.submitted, submitCall{token: token, action: resp.Action, content: resp.Content})
	if f.submitErr != nil {
		return runner.ResolveDirective{}, f.submitErr
	}
	if f.clearPendingOnSubmit {
		f.pending = nil
	}
	return f.directiveFor(resp.Action), nil
}

// directiveFor mirrors runner.classifyResolve's precedence (cancel → Terminated; remaining>0 →
// Pending; else the scripted outcome, default Continue). The double MUST be faithful here: the
// channel now decides whether to resume from the directive alone, so a double that returned a
// directive the real runner never emits would test a contract that does not exist.
func (f *fakeResume) directiveFor(action string) runner.ResolveDirective {
	switch {
	case action == askuser.ActionCancel:
		return runner.ResolveDirective{Outcome: runner.OutcomeTerminated}
	case f.remaining > 0:
		return runner.ResolveDirective{Outcome: runner.OutcomePending, Remaining: f.remaining}
	default:
		return runner.ResolveDirective{Outcome: f.outcome}
	}
}

func (f *fakeResume) resumeTurn(_ context.Context, _ string) {
	f.mu.Lock()
	f.resumes++
	f.mu.Unlock()
}

func (f *fakeResume) calls() []submitCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]submitCall, len(f.submitted))
	copy(out, f.submitted)
	return out
}

func optsJSON(t *testing.T, opts ...[2]string) json.RawMessage {
	t.Helper()
	type lv struct {
		Label string `json:"label"`
		Value string `json:"value"`
	}
	arr := make([]lv, 0, len(opts))
	for _, o := range opts {
		arr = append(arr, lv{Label: o[0], Value: o[1]})
	}
	b, err := json.Marshal(arr)
	if err != nil {
		t.Fatalf("marshal opts: %v", err)
	}
	return b
}

// markupOf extracts the *tele.ReplyMarkup from a recorded send's opts.
func markupOf(opts []any) *tele.ReplyMarkup {
	for _, o := range opts {
		if so, ok := o.(*tele.SendOptions); ok && so.ReplyMarkup != nil {
			return so.ReplyMarkup
		}
		if rm, ok := o.(*tele.ReplyMarkup); ok {
			return rm
		}
	}
	return nil
}

// hitlBot records sends with their full opts so the markup (InlineKeyboard /
// ForceReply) can be asserted on the Send RESPONSE.
type hitlBot struct {
	mu    sync.Mutex
	sends []hitlSend
}

type hitlSend struct {
	text   string
	markup *tele.ReplyMarkup
}

func (b *hitlBot) Send(_ tele.Recipient, what any, opts ...any) (*tele.Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := hitlSend{markup: markupOf(opts)}
	if t, ok := what.(string); ok {
		s.text = t
	}
	b.sends = append(b.sends, s)
	return &tele.Message{ID: len(b.sends)}, nil
}

func (b *hitlBot) Edit(_ tele.Editable, what any, opts ...any) (*tele.Message, error) {
	return b.Send(nil, what, opts...)
}

func (b *hitlBot) recorded() []hitlSend {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]hitlSend, len(b.sends))
	copy(out, b.sends)
	return out
}

// TestApprovalMarkupMultiRow pins the enriched native approval: Approva/Rifiuta on row 1, a
// non-resolving Dettagli on row 2, and every button within Telegram's 64-byte on-wire budget
// (framing included) for a real 36-char UUID token.
func TestApprovalMarkupMultiRow(t *testing.T) {
	t.Parallel()
	const token = "11112222-3333-4444-5555-666677778888"
	mk := approvalMarkup(token)
	if len(mk.InlineKeyboard) != 2 {
		t.Fatalf("want 2 rows (Approva/Rifiuta, Dettagli), got %d", len(mk.InlineKeyboard))
	}
	if len(mk.InlineKeyboard[0]) != 2 || len(mk.InlineKeyboard[1]) != 1 {
		t.Fatalf("unexpected keyboard shape: %+v", mk.InlineKeyboard)
	}
	if got := mk.InlineKeyboard[1][0].Text; got != "Dettagli" {
		t.Errorf("row 2 button = %q, want Dettagli", got)
	}
	for _, row := range mk.InlineKeyboard {
		for _, b := range row {
			if !strings.Contains(b.Data, token) {
				t.Errorf("button %q must carry the pause token, data=%q", b.Text, b.Data)
			}
			if wire := len("\f") + len(b.Unique) + len(callbackSep) + len(b.Data); wire > callbackDataMaxBytes {
				t.Errorf("button %q on-wire %d > %d", b.Text, wire, callbackDataMaxBytes)
			}
		}
	}
}

// TestDetailsCallbackIsNonResolving proves a Dettagli tap never answers the pause: it only
// reveals. Resolving on a reveal would decide for the operator.
func TestDetailsCallbackIsNonResolving(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{}
	h := newHitl(rs, rs.resumeTurn)

	out := h.handleCallbackResult(context.Background(), callbackData("tok-1", actionDetails, ""), "conv-1", nil)
	if len(rs.calls()) != 0 {
		t.Errorf("details must not reach SubmitAnswer, got %+v", rs.calls())
	}
	if out.submitted || out.resumed || rs.resumes != 0 {
		t.Errorf("details must neither resolve nor resume, got %+v (resumes=%d)", out, rs.resumes)
	}
}

// TestQuestionForMatchesToken proves the reveal reads the pause's own server-sanitized question
// and returns "" for a stale token, so a resolved prompt is never blanked.
func TestQuestionForMatchesToken(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{pending: []askuser.Pending{
		{Token: "tok-1", Kind: "approval", Question: "Attivo il job giornaliero delle 7:00?"},
	}}
	h := newHitl(rs, rs.resumeTurn)

	if got := h.questionFor(context.Background(), "conv-1", "tok-1"); got != "Attivo il job giornaliero delle 7:00?" {
		t.Errorf("questionFor = %q", got)
	}
	if got := h.questionFor(context.Background(), "conv-1", "stale"); got != "" {
		t.Errorf("stale token must yield \"\", got %q", got)
	}
}

// TestHITLChoiceRendersInlineKeyboard: a pause WITH options renders an
// InlineKeyboard, one button per option, each callback carrying the pause token
// (VALIDATION "options→InlineKeyboard").
func TestHITLChoiceRendersInlineKeyboard(t *testing.T) {
	t.Parallel()
	bot := &hitlBot{}
	rs := &fakeResume{pending: []askuser.Pending{{
		Token: "tok-1", Kind: "choice", Question: "Quale città?",
		Options:    optsJSON(t, [2]string{"Cuneo", "cuneo"}, [2]string{"Torino", "torino"}),
		ToolCallID: "tc-1",
	}}}
	h := newHitl(rs, rs.resumeTurn)

	h.prompt(context.Background(), bot, 42, "conv-1")

	sends := bot.recorded()
	if len(sends) != 1 {
		t.Fatalf("want 1 prompt send, got %d", len(sends))
	}
	mk := sends[0].markup
	if mk == nil || len(mk.InlineKeyboard) == 0 {
		t.Fatalf("choice pause must render an InlineKeyboard, got %+v", mk)
	}
	// Count the discrete option buttons (an accept action) vs the trailing cancel
	// safety button so the user is never stuck.
	var optBtns, cancelBtns int
	for _, row := range mk.InlineKeyboard {
		for _, b := range row {
			if !strings.Contains(b.Data, "tok-1") {
				t.Errorf("callback data %q must carry the pause token", b.Data)
			}
			if strings.Contains(b.Data, askuser.ActionCancel) {
				cancelBtns++
			} else if strings.Contains(b.Data, askuser.ActionAccept) {
				optBtns++
			}
		}
	}
	if optBtns != 2 {
		t.Errorf("want 2 option (accept) buttons, got %d", optBtns)
	}
	if cancelBtns != 1 {
		t.Errorf("want 1 cancel safety button, got %d", cancelBtns)
	}
}

// TestHITLApprovalRendersInlineKeyboard: an approval pause renders accept/decline
// buttons (an InlineKeyboard), not a ForceReply.
func TestHITLApprovalRendersInlineKeyboard(t *testing.T) {
	t.Parallel()
	bot := &hitlBot{}
	rs := &fakeResume{pending: []askuser.Pending{{
		Token: "tok-a", Kind: "approval", Question: "Procedo?", ToolCallID: "tc-a",
	}}}
	h := newHitl(rs, rs.resumeTurn)
	h.prompt(context.Background(), bot, 42, "conv-1")

	mk := bot.recorded()[0].markup
	if mk == nil || len(mk.InlineKeyboard) == 0 {
		t.Fatalf("approval pause must render an InlineKeyboard, got %+v", mk)
	}
}

// TestHITLClarificationRendersForceReply: a free-text pause (no options) renders a
// ForceReply, not an InlineKeyboard (VALIDATION "none→ForceReply").
func TestHITLClarificationRendersForceReply(t *testing.T) {
	t.Parallel()
	bot := &hitlBot{}
	rs := &fakeResume{pending: []askuser.Pending{{
		Token: "tok-c", Kind: "clarification", Question: "Come ti chiami?", ToolCallID: "tc-c",
	}}}
	h := newHitl(rs, rs.resumeTurn)
	h.prompt(context.Background(), bot, 42, "conv-1")

	mk := bot.recorded()[0].markup
	if mk == nil || !mk.ForceReply {
		t.Fatalf("clarification pause must render a ForceReply, got %+v", mk)
	}
	if len(mk.InlineKeyboard) != 0 {
		t.Error("clarification must NOT carry an InlineKeyboard")
	}
}

// TestHITLCallbackResumesWhenRemainingZero: an accept callback submits via the
// Runner and drives Turn(convID,nil) ONLY when remaining==0 (VALIDATION
// "callback→resume").
func TestHITLCallbackResumesWhenRemainingZero(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{remaining: 0}
	h := newHitl(rs, rs.resumeTurn)

	resumed := h.handleCallback(context.Background(), callbackData("tok-1", askuser.ActionAccept, "cuneo"), "conv-1")
	calls := rs.calls()
	if len(calls) != 1 {
		t.Fatalf("want 1 SubmitAnswer, got %d", len(calls))
	}
	if calls[0].token != "tok-1" || calls[0].action != askuser.ActionAccept || calls[0].content != "cuneo" {
		t.Errorf("SubmitAnswer call = %+v, want {tok-1 accept cuneo}", calls[0])
	}
	if !resumed {
		t.Error("remaining==0 must drive a resume Turn")
	}
	if rs.resumes != 1 {
		t.Errorf("resume drives = %d, want 1", rs.resumes)
	}
}

// TestHITLCallbackScheduledApprovalRendersOutcomeWithoutResume is the Phase A behavioral pin:
// a scheduled_task_approval resolves to a deterministic Approved/Rejected verdict, and the
// channel must NOT drive a continuation turn for it — the ResumeHook activating the task IS the
// whole intent, and resuming is what made the agent re-schedule in a loop (fix-plan 1.7 defect A).
func TestHITLCallbackScheduledApprovalRendersOutcomeWithoutResume(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		action  string
		outcome runner.ResolveOutcome
	}{
		{"approve", askuser.ActionAccept, runner.OutcomeApproved},
		{"reject", askuser.ActionDecline, runner.OutcomeRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rs := &fakeResume{outcome: tc.outcome}
			h := newHitl(rs, rs.resumeTurn)

			out := h.handleCallbackResult(context.Background(),
				callbackData("tok-1", tc.action, ""), "conv-1", nil)
			if !out.submitted {
				t.Fatal("the pause must still be submitted through the Runner")
			}
			if out.outcome != tc.outcome {
				t.Errorf("outcome = %v, want %v", out.outcome, tc.outcome)
			}
			if out.resumed || rs.resumes != 0 {
				t.Errorf("a scheduled approval must NOT drive a continuation turn (resumes=%d)", rs.resumes)
			}
		})
	}
}

// TestHITLCallbackWaitsWhenRemaining: remaining>0 submits but does NOT resume
// (more pauses to answer first — FIFO).
func TestHITLCallbackWaitsWhenRemaining(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{remaining: 1}
	h := newHitl(rs, rs.resumeTurn)
	resumed := h.handleCallback(context.Background(), callbackData("tok-1", askuser.ActionAccept, "x"), "conv-1")
	if resumed {
		t.Error("remaining>0 must NOT resume yet")
	}
	if rs.resumes != 0 {
		t.Errorf("resume drives = %d, want 0", rs.resumes)
	}
}

// TestHITLDeclineSubmitsDecline: a decline button submits the decline action.
func TestHITLDeclineSubmitsDecline(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{remaining: 0}
	h := newHitl(rs, rs.resumeTurn)
	h.handleCallback(context.Background(), callbackData("tok-1", askuser.ActionDecline, ""), "conv-1")
	if got := rs.calls()[0].action; got != askuser.ActionDecline {
		t.Errorf("action = %q, want decline", got)
	}
}

// TestHITLCancelDoesNotResume: a cancel terminates the turn — it submits cancel
// and does NOT drive a resume Turn even when remaining==0.
func TestHITLCancelDoesNotResume(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{remaining: 0}
	h := newHitl(rs, rs.resumeTurn)
	resumed := h.handleCallback(context.Background(), callbackData("tok-1", askuser.ActionCancel, ""), "conv-1")
	if resumed {
		t.Error("a cancel must NOT resume the turn")
	}
	if rs.resumes != 0 {
		t.Errorf("resume drives = %d, want 0 on cancel", rs.resumes)
	}
	if got := rs.calls()[0].action; got != askuser.ActionCancel {
		t.Errorf("action = %q, want cancel", got)
	}
}

// TestHITLForceReplyAnswerResumes: a free-text ForceReply answer round-trips back
// to the Runner via SubmitAnswer(accept) keyed by the pending token, then resumes.
func TestHITLForceReplyAnswerResumes(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{
		remaining: 0,
		pending:   []askuser.Pending{{Token: "tok-c", Kind: "clarification", Question: "Nome?", ToolCallID: "tc-c"}},
	}
	h := newHitl(rs, rs.resumeTurn)

	resumed, _ := h.handleTextReply(context.Background(), "conv-1", "Davide")
	calls := rs.calls()
	if len(calls) != 1 {
		t.Fatalf("ForceReply answer must submit exactly once, got %d", len(calls))
	}
	if calls[0].token != "tok-c" || calls[0].action != askuser.ActionAccept || calls[0].content != "Davide" {
		t.Errorf("ForceReply submit = %+v, want {tok-c accept Davide}", calls[0])
	}
	if !resumed {
		t.Error("a ForceReply answer with remaining==0 must resume")
	}
}

// TestHITLTextReplySubmitErrorSurfaced: when SubmitAnswer fails, handleTextReply
// returns the error (no longer swallowed) so the channel can notify the user to
// retry, and no resume is driven while the pause stays open.
func TestHITLTextReplySubmitErrorSurfaced(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{
		pending:   []askuser.Pending{{Token: "tok-e", Kind: "clarification", Question: "Nome?", ToolCallID: "tc-e"}},
		submitErr: errors.New("submit boom"),
	}
	h := newHitl(rs, rs.resumeTurn)

	resumed, err := h.handleTextReply(context.Background(), "conv-1", "Davide")
	if err == nil {
		t.Fatal("a SubmitAnswer error must be surfaced, not silently swallowed")
	}
	if resumed {
		t.Error("a failed submit must not drive a resume")
	}
}

// TestHITLNoPendingTextReplyIgnored: a text reply with NO pending pause is a
// no-op (handled==false), so an ordinary message is not stolen by HITL.
func TestHITLNoPendingTextReplyIgnored(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{} // no pending
	h := newHitl(rs, rs.resumeTurn)
	if resumed, _ := h.handleTextReply(context.Background(), "conv-1", "hello"); resumed {
		t.Error("a text reply with no pending pause must not resume")
	}
	if len(rs.calls()) != 0 {
		t.Error("no pending → no SubmitAnswer")
	}
}

func TestHITLMalformedCallbackIgnored(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{}
	h := newHitl(rs, rs.resumeTurn)
	if resumed := h.handleCallback(context.Background(), "garbage-no-delim", "conv-1"); resumed {
		t.Error("a malformed callback must not resume")
	}
	if len(rs.calls()) != 0 {
		t.Error("malformed callback must not submit")
	}
}

func TestHITLSubmitErrorNoResume(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{submitErr: errors.New("db down")}
	h := newHitl(rs, rs.resumeTurn)
	if resumed := h.handleCallback(context.Background(), callbackData("t", askuser.ActionAccept, "x"), "conv-1"); resumed {
		t.Error("a SubmitAnswer error must not resume")
	}
}

// TestHITLChoiceCallbackDataUnder64Bytes guards Telegram's 64-byte callback_data cap:
// a choice with long, free-prose option values must still render buttons whose
// callback_data fits — the option INDEX, not the value, is embedded. Without it a long
// value 400s BUTTON_DATA_INVALID on send (the live-found bug).
func TestHITLChoiceCallbackDataUnder64Bytes(t *testing.T) {
	t.Parallel()
	long1 := "Tamone — dal Monte Tamone, originale e robusto per un cane da montagna"
	long2 := "Kai — corto, giapponese, facilissimo da richiamare sui sentieri di Valle Grana"
	bot := &hitlBot{}
	rs := &fakeResume{pending: []askuser.Pending{{
		Token: "019ea841-fa1f-7885-9e46-179928f3fa28", Kind: "choice", Question: "Quale nome?",
		Options:    optsJSON(t, [2]string{long1, long1}, [2]string{long2, long2}),
		ToolCallID: "tc-1",
	}}}
	h := newHitl(rs, rs.resumeTurn)
	h.prompt(context.Background(), bot, 42, "conv-1")

	mk := bot.recorded()[0].markup
	if mk == nil || len(mk.InlineKeyboard) == 0 {
		t.Fatalf("choice must render an InlineKeyboard")
	}
	for _, row := range mk.InlineKeyboard {
		for _, b := range row {
			if n := len([]byte(b.Data)); n > 64 {
				t.Errorf("callback_data %q is %d bytes > 64 (Telegram BUTTON_DATA_INVALID)", b.Data, n)
			}
		}
	}
}

// TestHITLChoiceCallbackResolvesIndexToValue proves a choice button tap (callback_data
// carries the option INDEX) submits the option's full VALUE to the Runner, not the raw
// index — the 64-byte fix must not lose the answer.
func TestHITLChoiceCallbackResolvesIndexToValue(t *testing.T) {
	t.Parallel()
	val := "Tamone — dal Monte Tamone, originale e robusto"
	rs := &fakeResume{remaining: 0, pending: []askuser.Pending{{
		Token: "tok-1", Kind: "choice", Question: "Quale nome?",
		Options:    optsJSON(t, [2]string{"Kai", "Kai"}, [2]string{val, val}),
		ToolCallID: "tc-1",
	}}}
	h := newHitl(rs, rs.resumeTurn)
	// The user tapped the SECOND option (index 1) → callback_data token|accept|1.
	h.handleCallback(context.Background(), callbackData("tok-1", askuser.ActionAccept, "1"), "conv-1")
	calls := rs.calls()
	if len(calls) != 1 {
		t.Fatalf("want 1 SubmitAnswer, got %d", len(calls))
	}
	if calls[0].content != val {
		t.Errorf("submitted content = %q, want the resolved option value %q", calls[0].content, val)
	}
}

func TestCallbackDataPanicsOverTelegramLimit(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Fatal("callbackData must fail loudly when the internal payload exceeds Telegram's 64-byte limit")
		}
	}()
	callbackData(strings.Repeat("x", 65), askuser.ActionAccept, "")
}
