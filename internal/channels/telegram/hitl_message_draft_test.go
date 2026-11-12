package telegram

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/messagedrafts"
	"github.com/chetto1983/aura/internal/runner"
)

type resolveCall struct {
	draftID   string
	action    string
	overrides json.RawMessage
}

func (f *fakeResume) ListMessageDrafts(context.Context, string) ([]messagedrafts.Draft, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.drafts, nil
}

func (f *fakeResume) ResolveMessageDraft(_ context.Context, draftID, action string, overrides json.RawMessage) (runner.MessageDraftResolution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolved = append(f.resolved, resolveCall{draftID: draftID, action: action, overrides: overrides})
	if f.resolveErr != nil {
		return runner.MessageDraftResolution{}, f.resolveErr
	}
	for i := range f.drafts {
		if f.drafts[i].ID == draftID && f.resolvedStatus != "" {
			f.drafts[i].Status = f.resolvedStatus
		}
	}
	return runner.MessageDraftResolution{Directive: runner.ResolveDirective{Outcome: f.outcome}}, nil
}

func (f *fakeResume) Location(context.Context) *time.Location {
	if f.location != nil {
		return f.location
	}
	return time.UTC
}

// ownerReviewCtx is the linked identity every Telegram resolve runs under (hitlScope).
func ownerReviewCtx() context.Context {
	return identityctx.WithIdentityID(context.Background(), "10000000-0000-4000-8000-000000000001")
}

const reviewToken = "11112222-3333-4444-5555-666677778888"

func reviewPending(draftID string) askuser.Pending {
	return askuser.Pending{Token: reviewToken, Kind: "approval", Question: "Review this scheduled message before it is sent",
		ResumeContext: json.RawMessage(`{"type":"message_draft","draft_id":"` + draftID + `","task_id":"t"}`)}
}

func whatsappDraft(text string, sendAt time.Time) messagedrafts.Draft {
	return messagedrafts.Draft{
		ID: "draft-1", Status: messagedrafts.StatusPending, SendAt: sendAt,
		Target:       messagedrafts.Target{Recipe: "recipe:whatsapp", Tool: "send_message"},
		OriginalArgs: json.RawMessage(`{"recipient":"10000000000","message":` + strings.TrimSpace(mustJSON(text)) + `}`),
	}
}

func mustJSON(v string) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func reviewHitl(draft messagedrafts.Draft) (*hitl, *fakeResume, *hitlBot) {
	rome, _ := time.LoadLocation("Europe/Rome")
	resume := &fakeResume{pending: []askuser.Pending{reviewPending(draft.ID)}, drafts: []messagedrafts.Draft{draft}, location: rome}
	return newHitl(resume, resume.resumeTurn), resume, &hitlBot{}
}

func TestAScheduledReviewRendersTheMessageAndTheTime(t *testing.T) {
	h, _, bot := reviewHitl(whatsappDraft("Ciao", time.Date(2026, 11, 6, 7, 25, 0, 0, time.UTC)))
	h.prompt(context.Background(), bot, 42, "conv")
	sends := bot.recorded()
	if len(sends) != 1 {
		t.Fatalf("sends = %+v", sends)
	}
	for _, want := range []string{
		"A: 10000000000", "Quando: 06/11 08:25 (Europe/Rome)", "Testo:\nCiao",
		"Serve il tuo ok perché il messaggio va a un'altra persona. Senza risposta non parte nulla.",
		"Per cambiare il testo apri questa conversazione nel cockpit.",
	} {
		if !strings.Contains(sends[0].text, want) {
			t.Errorf("card lacks %q:\n%s", want, sends[0].text)
		}
	}
	row := sends[0].markup.InlineKeyboard[0]
	if len(row) != 2 || row[0].Text != "Approva" || row[1].Text != "Rifiuta" {
		t.Fatalf("keyboard = %+v", sends[0].markup.InlineKeyboard)
	}
}

func TestAnImmediateReviewHasNoTimeLine(t *testing.T) {
	h, _, bot := reviewHitl(whatsappDraft("Ciao", time.Time{}))
	h.prompt(context.Background(), bot, 42, "conv")
	if sends := bot.recorded(); len(sends) != 1 || strings.Contains(sends[0].text, "Quando:") {
		t.Fatalf("sends = %+v", sends)
	}
}

func TestApprovaResolvesTheDraftNotTheAnswer(t *testing.T) {
	h, resume, _ := reviewHitl(whatsappDraft("Ciao", time.Now().Add(time.Hour)))
	out := h.handleCallbackResult(ownerReviewCtx(), callbackData(reviewToken, askuser.ActionAccept, "yes"), "conv", nil)
	if len(resume.resolved) != 1 || resume.resolved[0].draftID != "draft-1" || resume.resolved[0].action != "send" || resume.resolved[0].overrides != nil {
		t.Fatalf("resolved = %+v", resume.resolved)
	}
	if len(resume.calls()) != 0 {
		t.Fatalf("SubmitAnswer was called: %+v", resume.calls())
	}
	if !out.submitted || !out.resumed || resume.resumes != 1 {
		t.Fatalf("out = %+v, resumes %d", out, resume.resumes)
	}
}

func TestRifiutaDeclinesTheDraft(t *testing.T) {
	h, resume, _ := reviewHitl(whatsappDraft("Ciao", time.Now().Add(time.Hour)))
	h.handleCallbackResult(ownerReviewCtx(), callbackData(reviewToken, askuser.ActionDecline, ""), "conv", nil)
	if len(resume.resolved) != 1 || resume.resolved[0].action != "decline" {
		t.Fatalf("resolved = %+v", resume.resolved)
	}
}

// The token is looked up in this chat's conversation only.
func TestAButtonFromAnotherChatIsIgnored(t *testing.T) {
	h, resume, _ := reviewHitl(whatsappDraft("Ciao", time.Now().Add(time.Hour)))
	resume.pending = nil
	h.handleCallbackResult(ownerReviewCtx(), callbackData(reviewToken, askuser.ActionAccept, "yes"), "conv", nil)
	if len(resume.resolved) != 0 {
		t.Fatalf("a foreign button resolved %+v", resume.resolved)
	}
}

func TestChiudiOnADispatchingCardRerendersIt(t *testing.T) {
	draft := whatsappDraft("Ciao", time.Time{})
	draft.Status = messagedrafts.StatusDispatching
	h, resume, _ := reviewHitl(draft)
	resume.resolveErr = messagedrafts.ErrUnavailable
	out := h.handleCallbackResult(ownerReviewCtx(), callbackData(reviewToken, askuser.ActionAccept, ""), "conv", nil)
	if out.submitted || !strings.Contains(out.rerender, "Invio in corso") || out.rerenderMarkup == nil {
		t.Fatalf("out = %+v", out)
	}
}

func TestATypedReplyNeverAnswersAReview(t *testing.T) {
	h, resume, bot := reviewHitl(whatsappDraft("Ciao", time.Now().Add(time.Hour)))
	resumed, err := h.handleTextReply(context.Background(), "conv", "sì, mandalo")
	if resumed || err != nil || len(resume.calls()) != 0 || len(resume.resolved) != 0 {
		t.Fatalf("typed reply: resumed %v, err %v, submits %+v, resolves %+v", resumed, err, resume.calls(), resume.resolved)
	}
	h.prompt(context.Background(), bot, 42, "conv")
	if sends := bot.recorded(); len(sends) != 1 || !strings.Contains(sends[0].text, "Testo:") {
		t.Fatalf("the card was not sent again: %+v", sends)
	}
}

func TestALongMessageIsCutWithAPointerToTheCockpit(t *testing.T) {
	h, resume, bot := reviewHitl(whatsappDraft(strings.Repeat("a", 5000), time.Now().Add(time.Hour)))
	h.prompt(context.Background(), bot, 42, "conv")
	card := bot.recorded()[0].text
	if utf8.RuneCountInString(card) > 4096 || !strings.Contains(card, "… (testo completo nel cockpit)") {
		t.Fatalf("card is %d runes, pointer present %v", utf8.RuneCountInString(card), strings.Contains(card, "testo completo"))
	}
	resume.outcome = runner.OutcomeContinue
	if out := h.handleCallbackResult(ownerReviewCtx(), callbackData(reviewToken, askuser.ActionAccept, "yes"), "conv", nil); !out.submitted {
		t.Fatal("Approva did not resolve a long message")
	}
}

// Measured 2026-11-12: the card named the recipient but not the account the email leaves from.
func TestTheReviewCardNamesTheSendingAccount(t *testing.T) {
	email := messagedrafts.Draft{
		ID: "draft-1", Status: messagedrafts.StatusPending,
		Target:       messagedrafts.Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"},
		OriginalArgs: json.RawMessage(`{"action":"send_email","accountId":"acc-1","to":["someone@example.com"],"subject":"Hi","body":"Text"}`),
	}
	if text := messageReviewText(email, time.UTC); !strings.Contains(text, "Da: acc-1\nA: someone@example.com") {
		t.Fatalf("card lacks the sending account:\n%s", text)
	}
	email.OriginalArgs = json.RawMessage(`{"action":"send_email","to":["someone@example.com"],"subject":"Hi","body":"Text"}`)
	if text := messageReviewText(email, time.UTC); strings.Contains(text, "Da:") {
		t.Fatalf("a draft with no account shows one:\n%s", text)
	}
}
