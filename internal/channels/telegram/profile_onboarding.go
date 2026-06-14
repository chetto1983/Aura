package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/agent"
	profileflow "github.com/chetto1983/aura/internal/onboarding"
	"github.com/chetto1983/aura/internal/profile"
	"github.com/google/uuid"
)

const (
	profileCallbackUnique = "aura_profile"
	profileCallbackPrefix = "pob"

	profileActionConfirm = "confirm"
	profileActionEdit    = "edit"
	profileActionSkip    = "skip"
)

type profileAccountResolver interface {
	GetAccountByTelegramID(ctx context.Context, telegramUserID int64) (Account, error)
}

type profileOnboarding struct {
	store     *profile.Store
	accounts  profileAccountResolver
	extractor profileflow.AnswerExtractor

	mu       sync.Mutex
	sessions map[int64]*profileSession
}

type profileSession struct {
	mu           sync.Mutex
	account      Account
	session      *profileflow.Session
	awaitingEdit bool
}

type profileReply struct {
	text   string
	markup *tele.ReplyMarkup
	ack    string
}

func newProfileOnboarding(store *profile.Store, accounts profileAccountResolver) *profileOnboarding {
	return &profileOnboarding{
		store:    store,
		accounts: accounts,
		sessions: make(map[int64]*profileSession),
	}
}

func (p *profileOnboarding) maybeStart(ctx context.Context, chatID, telegramUserID int64) (profileReply, bool) {
	if p == nil {
		return profileReply{}, false
	}
	if p.accounts == nil {
		return profileReply{}, false
	}
	if p.store == nil {
		return profileReply{text: "Profilo non disponibile: configurazione profilo mancante."}, true
	}
	acct, err := p.accounts.GetAccountByTelegramID(ctx, telegramUserID)
	if err != nil {
		return profileReply{}, false
	}
	if _, err := p.store.ReadProfile(acct.IdentityID); err == nil {
		return profileReply{}, false
	} else if !errors.Is(err, profile.ErrProfileNotFound) && !errors.Is(err, profile.ErrInvalidIdentity) {
		slog.Warn("telegram profile onboarding: read profile", "identity", acct.IdentityID, "err", err)
		return profileReply{text: "Profilo non disponibile: non riesco a leggere il profilo ora."}, true
	}
	p.mu.Lock()
	ps := p.startLocked(chatID, acct)
	p.mu.Unlock()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return p.runOne(ctx, chatID, ps)
}

func (p *profileOnboarding) restart(ctx context.Context, chatID, telegramUserID int64) profileReply {
	if p == nil || p.store == nil {
		return profileReply{text: "Profilo non disponibile: configurazione profilo mancante."}
	}
	if p.accounts == nil {
		return profileReply{text: "Non trovo un account Telegram collegato."}
	}
	acct, err := p.accounts.GetAccountByTelegramID(ctx, telegramUserID)
	if err != nil {
		return profileReply{text: "Non trovo un account Telegram collegato."}
	}
	p.mu.Lock()
	ps := p.startLocked(chatID, acct)
	p.mu.Unlock()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	out, _ := p.runOne(ctx, chatID, ps)
	return out
}

func (p *profileOnboarding) handleText(ctx context.Context, chatID int64, text string) (profileReply, bool) {
	if p == nil {
		return profileReply{}, false
	}
	p.mu.Lock()
	ps := p.sessions[chatID]
	p.mu.Unlock()
	if ps == nil {
		return profileReply{}, false
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.awaitingEdit {
		ps.awaitingEdit = false
		ps.session.Queue(profileflow.Input{Intent: profileflow.IntentEdit, Answers: answersFromText(text)})
	} else {
		step := ps.session.Step
		var ans profileflow.Answers
		if p.extractor != nil && step != profileflow.StepStyle {
			if extracted, err := p.extractor.Extract(ctx, step, text); err == nil {
				ans = extracted
			} else {
				slog.Warn("telegram profile onboarding: extract", "step", step, "err", err)
				ans = answersFromText(text)
			}
		} else {
			ans = answersFromText(text)
		}
		if step == profileflow.StepIdentity && ans.Name == "" {
			ans.Name = strings.TrimSpace(text)
		}
		ps.session.Queue(profileflow.Input{Intent: profileflow.IntentAnswer, Text: text, Answers: ans})
	}
	out, handled := p.runOne(ctx, chatID, ps)
	return out, handled
}

func (p *profileOnboarding) handleCallback(ctx context.Context, chatID int64, data string) (profileReply, bool) {
	action, ok := parseProfileCallbackData(chatID, data)
	if !ok {
		return profileReply{ack: "Profilo scaduto o non disponibile."}, true
	}
	p.mu.Lock()
	ps := p.sessions[chatID]
	p.mu.Unlock()
	if ps == nil {
		return profileReply{ack: "Profilo scaduto o non disponibile."}, true
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	switch action {
	case profileActionEdit:
		ps.awaitingEdit = true
		return profileReply{text: "Scrivi le modifiche che vuoi applicare al profilo.", ack: "Modifica"}, true
	case profileActionSkip:
		ps.session.Queue(profileflow.Input{Intent: profileflow.IntentSkip})
		out, _ := p.runOne(ctx, chatID, ps)
		if err := p.writeSkipped(ps); err != nil {
			return profileReply{text: "Non sono riuscita a salvare la scelta. Riprova piu tardi.", ack: "Errore"}, true
		}
		p.deleteSession(chatID)
		out.text = "Onboarding profilo saltato. Riprendo la chat normale."
		out.markup = nil
		out.ack = "Saltato"
		return out, true
	case profileActionConfirm:
		ps.session.Queue(profileflow.Input{Intent: profileflow.IntentConfirm})
		out, handled := p.runOne(ctx, chatID, ps)
		if !handled {
			return profileReply{ack: "Profilo scaduto o non disponibile."}, true
		}
		if err := p.writeCompleted(ps, out); err != nil {
			return profileReply{text: "Non sono riuscita a salvare il profilo. Riprova piu tardi.", ack: "Errore"}, true
		}
		p.deleteSession(chatID)
		out.text = "Profilo salvato. Riprendo la chat normale."
		out.markup = nil
		out.ack = "Confermato"
		return out, true
	default:
		return profileReply{ack: "Profilo scaduto o non disponibile."}, true
	}
}

func (p *profileOnboarding) startLocked(chatID int64, acct Account) *profileSession {
	ps := &profileSession{
		account: acct,
		session: profileflow.NewSession(acct.IdentityID, identityLabel(acct)),
	}
	p.sessions[chatID] = ps
	return ps
}

func (p *profileOnboarding) deleteSession(chatID int64) {
	p.mu.Lock()
	delete(p.sessions, chatID)
	p.mu.Unlock()
}

func (p *profileOnboarding) runOne(ctx context.Context, chatID int64, ps *profileSession) (profileReply, bool) {
	maxSteps := 4
	budget, err := agent.NewBudget(agent.BudgetOptions{MaxSteps: &maxSteps})
	if err != nil {
		return profileReply{text: "Profilo non disponibile: errore interno."}, true
	}
	loop := profileflow.NewLoop(ps.session, 1)
	ic := agent.InvocationContext{
		Ctx:       ctx,
		Agent:     loop,
		RequestID: uuid.Must(uuid.NewV7()),
		Branch:    "telegram.profile_onboarding",
		Budget:    budget,
	}
	for ev, err := range loop.Run(ic) {
		if err != nil {
			slog.Warn("telegram profile onboarding: run", "chat", chatID, "err", err)
			return profileReply{text: "Profilo non disponibile: errore interno."}, true
		}
		if ev != nil {
			return replyFromEvent(chatID, ev), true
		}
	}
	return profileReply{}, false
}

func (p *profileOnboarding) writeCompleted(ps *profileSession, out profileReply) error {
	_ = out
	if strings.TrimSpace(ps.session.DraftAgentMD) == "" {
		return fmt.Errorf("empty profile draft")
	}
	return p.store.WriteProfile(ps.account.IdentityID, profile.Profile{
		AgentMD:     ps.session.DraftAgentMD,
		Preferences: ps.session.Preferences,
		Metadata: profile.Metadata{
			Version:             1,
			SchemaVersion:       1,
			OnboardingCompleted: true,
		},
		Change: "telegram profile onboarding confirmed",
	})
}

func (p *profileOnboarding) writeSkipped(ps *profileSession) error {
	return p.store.WriteProfile(ps.account.IdentityID, profile.Profile{
		AgentMD: "",
		Metadata: profile.Metadata{
			Version:             1,
			SchemaVersion:       1,
			OnboardingCompleted: false,
			OnboardingSkipped:   true,
		},
		Change: "telegram profile onboarding skipped",
	})
}

func replyFromEvent(chatID int64, ev *agent.Event) profileReply {
	if ev == nil || ev.LLMResponse == nil {
		return profileReply{}
	}
	state := ev.Actions.StateDelta
	step, _ := state["onboarding_step"].(string)
	switch profileflow.Step(step) {
	case profileflow.StepIdentity:
		return profileReply{text: "Come ti chiami, di cosa ti occupi (ruolo + azienda/team) e dove sei (fuso orario)?"}
	case profileflow.StepWork:
		return profileReply{text: "Quali sono le tue competenze principali e lo stack/strumenti che usi di solito?"}
	case profileflow.StepProjects:
		return profileReply{text: "A cosa stai lavorando in questo periodo e quali obiettivi hai?"}
	case profileflow.StepSocial:
		return profileReply{text: "Quali sono i tuoi interessi e le persone ricorrenti con cui collabori (nome + ruolo)?"}
	case profileflow.StepStyle:
		return profileReply{text: "Come preferisci che ti risponda? (tono: diretto/amichevole/formale; lunghezza: breve/normale/dettagliata; lingua; voce on/off)"}
	case profileflow.StepDraft:
		draft, _ := state["profile_draft"].(string)
		return profileReply{
			text:   "Ecco la bozza del profilo. Puoi confermare, modificare o saltare.\n\n" + draft,
			markup: profileDraftMarkup(chatID),
		}
	}
	if ev.Actions.Escalate {
		if skipped, _ := state["skipped"].(bool); skipped {
			return profileReply{text: "Onboarding profilo saltato. Riprendo la chat normale."}
		}
		if completed, _ := state["onboarding_completed"].(bool); completed {
			return profileReply{text: "Profilo salvato. Riprendo la chat normale."}
		}
	}
	return profileReply{text: ev.LLMResponse.Content}
}

func profileDraftMarkup(chatID int64) *tele.ReplyMarkup {
	mk := &tele.ReplyMarkup{}
	mk.InlineKeyboard = [][]tele.InlineButton{
		{
			{Unique: profileCallbackUnique, Text: "Conferma", Data: profileCallbackData(chatID, profileActionConfirm)},
			{Unique: profileCallbackUnique, Text: "Modifica", Data: profileCallbackData(chatID, profileActionEdit)},
		},
		{{Unique: profileCallbackUnique, Text: "Salta", Data: profileCallbackData(chatID, profileActionSkip)}},
	}
	return mk
}

func profileCallbackData(chatID int64, action string) string {
	return strings.Join([]string{profileCallbackPrefix, strconv.FormatInt(chatID, 10), action}, callbackSep)
}

func parseProfileCallbackData(chatID int64, data string) (string, bool) {
	parts := strings.Split(data, callbackSep)
	if len(parts) != 3 || parts[0] != profileCallbackPrefix {
		return "", false
	}
	gotChat, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || gotChat != chatID {
		return "", false
	}
	switch parts[2] {
	case profileActionConfirm, profileActionEdit, profileActionSkip:
		return parts[2], true
	default:
		return "", false
	}
}

func identityLabel(acct Account) string {
	if acct.Username != "" {
		return acct.Username
	}
	if acct.FirstName != "" {
		return acct.FirstName
	}
	return acct.IdentityID
}

func answersFromText(text string) profileflow.Answers {
	lower := strings.ToLower(text)
	var out profileflow.Answers
	if strings.Contains(lower, "ital") {
		out.Lang = "it"
	}
	if strings.Contains(lower, "europe/rome") || strings.Contains(lower, "roma") {
		out.Timezone = "Europe/Rome"
	}
	if strings.Contains(lower, "tecnic") || strings.Contains(lower, "technical") {
		out.TonePreference = "technical"
	}
	if strings.Contains(lower, "brevi") || strings.Contains(lower, "breve") || strings.Contains(lower, "short") {
		out.ResponseLength = "short"
	} else if strings.Contains(lower, "concise") || strings.Contains(lower, "concisa") {
		out.ResponseLength = "concise"
	}
	if strings.Contains(lower, "voce") || strings.Contains(lower, "voice") {
		voice := true
		out.VoiceMode = &voice
	}
	return out
}
