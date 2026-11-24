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

// ListMessageDrafts lists, as the Runner does, only the drafts whose review pause is still
// pending: a resolved review is no longer listed (measured live on 2026-11-12, when a fake that
// listed every draft hid a card that was never redrawn).
func (f *fakeResume) ListMessageDrafts(context.Context, string) ([]messagedrafts.Draft, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var listed []messagedrafts.Draft
	for _, d := range f.drafts {
		for _, p := range f.pending {
			if id, ok := messagedrafts.PauseDraftID(p.ResumeContext); ok && id == d.ID {
				listed = append(listed, d)
			}
		}
	}
	return listed, nil
}

// ResolveMessageDraft, when a resolvedStatus is scripted, moves the draft to it and closes its
// review pause, as the Runner's resolve does.
func (f *fakeResume) ResolveMessageDraft(_ context.Context, draftID, action string, overrides json.RawMessage) (runner.MessageDraftResolution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolved = append(f.resolved, resolveCall{draftID: draftID, action: action, overrides: overrides})
	if f.resolveErr != nil {
		return runner.MessageDraftResolution{}, f.resolveErr
	}
	if f.resolvedStatus == "" {
		return runner.MessageDraftResolution{Directive: runner.ResolveDirective{Outcome: f.outcome}}, nil
	}
	for i := range f.drafts {
		if f.drafts[i].ID == draftID {
			f.drafts[i].Status = f.resolvedStatus
		}
	}
	open := f.pending[:0]
	for _, p := range f.pending {
		if id, ok := messagedrafts.PauseDraftID(p.ResumeContext); !ok || id != draftID {
			open = append(open, p)
		}
	}
	f.pending = open
	return runner.MessageDraftResolution{Status: f.resolvedStatus, Directive: runner.ResolveDirective{Outcome: f.outcome}}, nil
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
	h, resume, bot := reviewHitl(whatsappDraft("Ciao", time.Now().Add(time.Hour)))
	h.prompt(ownerReviewCtx(), bot, 42, "conv")
	out := h.handleCallbackResult(ownerReviewCtx(), bot.recorded()[0].markup.InlineKeyboard[0][0].Data, "conv", nil)
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

func emailReviewDraft(args string) messagedrafts.Draft {
	return messagedrafts.Draft{
		ID: "draft-1", Status: messagedrafts.StatusPending,
		Target:       messagedrafts.Target{Recipe: "recipe:calendar", Tool: "send_email"},
		OriginalArgs: json.RawMessage(args),
	}
}

func TestImmediateEmailReviewShowsCopiesBeforeSending(t *testing.T) {
	draft := emailReviewDraft(`{"to":["to@example.com"],"subject":"Subject","body":"Body","bodyFormat":"text","cc":["first@example.com","second@example.com"],"attachments":[]}`)
	h, resume, bot := reviewHitl(draft)
	h.prompt(ownerReviewCtx(), bot, 42, "conv")
	card := bot.recorded()[0]
	for _, line := range []string{"A: to@example.com", "Cc: first@example.com, second@example.com", "Oggetto: Subject", "Testo:\nBody"} {
		if !strings.Contains(card.text, line) {
			t.Errorf("missing %q in %q", line, card.text)
		}
	}
	// Old keyboards used the generic approval value and omitted the copies.
	old := h.handleCallbackResult(ownerReviewCtx(), callbackData(reviewToken, askuser.ActionAccept, "yes"), "conv", nil)
	if old.submitted || len(resume.resolved) != 0 || !strings.Contains(old.rerender, "first@example.com, second@example.com") {
		t.Fatalf("old keyboard sent or failed to show the full card: %+v", old)
	}
	row := card.markup.InlineKeyboard[0]
	if len(row) != 2 || row[0].Text != "Approva" {
		t.Fatalf("complete card keyboard = %+v", row)
	}
	out := h.handleCallbackResult(ownerReviewCtx(), row[0].Data, "conv", nil)
	if !out.submitted || len(resume.resolved) != 1 || resume.resolved[0].action != "send" {
		t.Fatalf("full card did not send: %+v, %+v", out, resume.resolved)
	}
}

func TestCompleteReviewFitsWithoutTheOptionalInstructions(t *testing.T) {
	body := strings.Repeat("è", 4020)
	h, _, bot := reviewHitl(whatsappDraft(body, time.Time{}))
	h.prompt(ownerReviewCtx(), bot, 42, "conv")
	card := bot.recorded()[0]
	row := card.markup.InlineKeyboard[0]
	if !strings.Contains(card.text, body) || utf8.RuneCountInString(card.text) > 4096 || len(row) != 2 || row[0].Text != "Approva" {
		t.Fatal("a complete message that fits on Telegram must be readable and approvable")
	}
}

func TestIncompleteReviewKeepsOnlyDeclineAndRefusesStaleAccept(t *testing.T) {
	const fields = `"to":["to@example.com"],"subject":"Subject","body":"Body"`
	longRecipients := strings.Repeat(mustJSON(strings.Repeat("a", 50)+"@example.com")+",", 49)
	quoted := whatsappDraft("Ciao", time.Time{})
	quoted.OriginalArgs = json.RawMessage(`{"recipient":"10000000000","message":"Ciao","quoted_content":"Previous message"}`)
	for name, draft := range map[string]messagedrafts.Draft{
		"attachment":                emailReviewDraft(`{` + fields + `,"attachments":[{"name":"memo","base64Content":"c2VjcmV0"}]}`),
		"multipart":                 emailReviewDraft(`{` + fields + `,"bodyFormat":"multipart","textBody":"plain","htmlBody":"<b>HTML</b>"}`),
		"alternative body":          emailReviewDraft(`{` + fields + `,"textBody":"alternate"}`),
		"oversize body":             whatsappDraft(strings.Repeat("ò", 5000), time.Time{}),
		"oversize headers and body": emailReviewDraft(`{"to":[` + longRecipients + `"last@example.com"],"subject":"Subject","body":` + mustJSON(strings.Repeat("b", 1600)) + `}`),
		"quoted message":            quoted,
	} {
		t.Run(name, func(t *testing.T) {
			// Keep an accept button issued when the draft was fully visible.
			h, resume, bot := reviewHitl(whatsappDraft("Ciao", time.Time{}))
			h.prompt(ownerReviewCtx(), bot, 42, "conv")
			oldAccept := bot.recorded()[0].markup.InlineKeyboard[0][0].Data
			resume.drafts[0] = draft
			h.prompt(ownerReviewCtx(), bot, 42, "conv")
			card := bot.recorded()[1]
			if utf8.RuneCountInString(card.text) > 4096 || !strings.Contains(card.text, "cockpit") || strings.Contains(card.text, "c2VjcmV0") {
				t.Fatalf("incomplete review is oversized, missing the cockpit link or contains attachment data: %d runes", utf8.RuneCountInString(card.text))
			}
			row := card.markup.InlineKeyboard[0]
			if len(row) != 1 || row[0].Text != "Rifiuta" {
				t.Errorf("incomplete review keyboard = %+v", row)
			}
			out := h.handleCallbackResult(ownerReviewCtx(), oldAccept, "conv", nil)
			if out.submitted || len(resume.resolved) != 0 || out.rerender != card.text {
				t.Fatalf("stale accept resolved or failed to refresh the card, submitted=%v resolves=%+v", out.submitted, resume.resolved)
			}
			out = h.handleCallbackResult(ownerReviewCtx(), row[0].Data, "conv", nil)
			if !out.submitted || len(resume.resolved) != 1 || resume.resolved[0].action != "decline" {
				t.Fatalf("cannot decline an incomplete review: %+v", resume.resolved)
			}
		})
	}
}

// Measured 2026-11-12: the card named the recipient but not the account the email leaves from.
func TestTheReviewCardNamesTheSendingAccount(t *testing.T) {
	email := messagedrafts.Draft{
		ID: "draft-1", Status: messagedrafts.StatusPending,
		Target:       messagedrafts.Target{Recipe: "recipe:calendar", Tool: "send_email"},
		OriginalArgs: json.RawMessage(`{"accountId":"acc-1","to":["someone@example.com"],"subject":"Hi","body":"Text"}`),
	}
	if text := renderMessageReview(email, time.UTC).text; !strings.Contains(text, "Da: acc-1\nA: someone@example.com") {
		t.Fatalf("card lacks the sending account:\n%s", text)
	}
	email.OriginalArgs = json.RawMessage(`{"to":["someone@example.com"],"subject":"Hi","body":"Text"}`)
	if text := renderMessageReview(email, time.UTC).text; strings.Contains(text, "Da:") {
		t.Fatalf("a draft with no account shows one:\n%s", text)
	}
}

// The operator asked on 2026-11-12 for the account's name and address, not its id.
func TestTheCardShowsTheAccountsNameAndAddress(t *testing.T) {
	email := messagedrafts.Draft{
		ID: "draft-1", Status: messagedrafts.StatusPending, SenderAddress: "someone@gmail.com",
		Target:       messagedrafts.Target{Recipe: "recipe:calendar", Tool: "send_email"},
		OriginalArgs: json.RawMessage(`{"accountId":"448ddbe1__personale","to":["a@example.com"],"subject":"Hi","body":"Text"}`),
	}
	if text := renderMessageReview(email, time.UTC).text; !strings.Contains(text, "Da: personale <someone@gmail.com>\n") {
		t.Fatalf("card lacks the name and address:\n%s", text)
	}
	email.SenderAddress = ""
	if text := renderMessageReview(email, time.UTC).text; !strings.Contains(text, "Da: personale\n") {
		t.Fatalf("card without an address lacks the name:\n%s", text)
	}
}
