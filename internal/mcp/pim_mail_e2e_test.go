//go:build pim_mail_e2e

// The published aura-pim-mcp sidecar against a fake IMAP/SMTP server (GreenMail), end to end
// through Aura's OAuth contract. scripts/pim_mail_e2e.sh brings both up, seeds three unread
// mails into operator@aura.test and runs this tier; it is not a CI job, because CI has no mail
// server. What it settles for the daily briefing and the 2026-10-31 audit:
//   - the shape of get_emails as the briefing and its baseline read it;
//   - that a send leaves the appliance and arrives;
//   - whether create_event with an attendee sends an invitation on these providers (A1);
//   - what bulk_mark_emails_read does to the unread mail (A3).
package mcp

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	pimOperatorTenant   = "00000000-0000-0000-0000-000000000001"
	pimColleagueTenant  = "00000000-0000-0000-0000-000000000002"
	pimOperatorMail     = "00000000000000000000000000000001__mail"
	pimOperatorCalendar = "00000000000000000000000000000001__fixture"
	pimColleagueMail    = "00000000000000000000000000000002__mail"
)

type pimEmail struct {
	ID        string          `json:"id"`
	AccountID string          `json:"accountId"`
	Subject   string          `json:"subject"`
	From      json.RawMessage `json:"from"`
	Received  string          `json:"receivedDateTime"`
}

func TestPIMMailEndToEnd(t *testing.T) {
	endpoint := strings.TrimSpace(os.Getenv("AURA_PIM_MCP_URL"))
	if endpoint == "" {
		t.Fatal("AURA_PIM_MCP_URL is unset: run this tier through scripts/pim_mail_e2e.sh")
	}
	issuer := newOAuthResourceIssuer(t)
	reapIdleHTTPConns(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	operator := openOAuthResourceSession(t, ctx, issuer, "pim-operator", endpoint, issuer.token(t, endpoint, pimOperatorTenant))
	colleague := openOAuthResourceSession(t, ctx, issuer, "pim-colleague", endpoint, issuer.token(t, endpoint, pimColleagueTenant))

	accounts, isErr := callCalendar(t, ctx, operator, map[string]any{"action": "list_accounts"})
	if isErr || !strings.Contains(accounts, pimOperatorMail) || !strings.Contains(accounts, pimOperatorCalendar) || strings.Contains(accounts, pimColleagueMail) {
		t.Fatalf("operator's accounts = %s, want its mailbox and calendar and not the colleague's", accounts)
	}

	// The briefing's two reads.
	unreadText, unread := awaitMail(t, ctx, operator, pimOperatorMail, func(m []pimEmail) bool { return len(m) >= 3 })
	t.Logf("get_emails unreadOnly, first item as the PIM returns it: %s", firstItem(unreadText))
	if !slices.ContainsFunc(unread, func(m pimEmail) bool { return m.Subject == "Fattura 2026-118 in scadenza" }) {
		t.Errorf("unread = %+v, want the seeded invoice reminder", unread)
	}
	eventsText, isErr := callCalendar(t, ctx, operator, map[string]any{
		"action": "get_calendar_events", "accountId": pimOperatorCalendar, "timeZone": "Europe/Rome",
		"startDate": "2026-08-24", "endDate": "2026-08-24",
	})
	if isErr || !strings.Contains(eventsText, "Phase 46 tracer") {
		t.Errorf("get_calendar_events = %s (isError=%v), want the fixture event", eventsText, isErr)
	}
	writeBaselineBundle(t, eventsText, unreadText)

	// A send leaves the appliance and arrives.
	sent, isErr := callCalendar(t, ctx, operator, map[string]any{
		"action": "send_email", "accountId": pimOperatorMail, "to": []string{"colleague@aura.test"},
		"subject": "E2E: delivered", "body": "Sent by the pim_mail_e2e tier.",
	})
	if isErr {
		t.Fatalf("send_email = %s", sent)
	}
	awaitMail(t, ctx, colleague, pimColleagueMail, func(m []pimEmail) bool { return hasSubject(m, "E2E: delivered") })

	// A1: an event with an outside attendee, on each provider this fake setup has.
	for _, account := range []string{pimOperatorMail, pimOperatorCalendar} {
		out, isErr := callCalendar(t, ctx, operator, map[string]any{
			"action": "create_event", "accountId": account, "subject": "E2E invitation " + account,
			"start": "2026-11-03T10:00:00", "end": "2026-11-03T11:00:00", "timeZone": "Europe/Rome",
			"attendees": []string{"colleague@aura.test"}, "body": "Would this reach the attendee?",
		})
		t.Logf("A1 create_event with an attendee on %s: isError=%v result=%s", account, isErr, clip(out))
	}
	time.Sleep(3 * time.Second)
	if text, _ := callCalendar(t, ctx, colleague, map[string]any{"action": "get_emails", "accountId": pimColleagueMail, "count": 20}); strings.Contains(text, "E2E invitation") {
		t.Errorf("an invitation reached the attendee on a provider that has no scheduling: %s", text)
	}

	// A3: one bulk call hides every unread mail.
	items := make([]map[string]string, 0, len(unread))
	for _, m := range unread {
		items = append(items, map[string]string{"accountId": m.AccountID, "emailId": m.ID})
	}
	if out, isErr := callCalendar(t, ctx, operator, map[string]any{"action": "bulk_mark_emails_read", "items": items, "isRead": true}); isErr {
		t.Fatalf("bulk_mark_emails_read = %s", out)
	}
	_, after := awaitMail(t, ctx, operator, pimOperatorMail, func(m []pimEmail) bool { return len(m) == 0 })
	t.Logf("A3: after one bulk_mark_emails_read the operator has %d unread mails", len(after))
}

// awaitMail polls get_emails (unread only for the operator's mailbox, everything for the
// colleague's) until done holds.
func awaitMail(t *testing.T, ctx context.Context, session *sdkmcp.ClientSession, account string, done func([]pimEmail) bool) (string, []pimEmail) {
	t.Helper()
	args := map[string]any{"action": "get_emails", "accountId": account, "count": 20}
	if account == pimOperatorMail {
		args["unreadOnly"] = true
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		text, isErr := callCalendar(t, ctx, session, args)
		if isErr {
			t.Fatalf("get_emails(%s) = %s", account, text)
		}
		var payload struct {
			Emails []pimEmail `json:"emails"`
		}
		if err := json.Unmarshal([]byte(text), &payload); err != nil {
			t.Fatalf("get_emails(%s) is not the JSON the briefing reads: %v\n%s", account, err, text)
		}
		if done(payload.Emails) {
			return text, payload.Emails
		}
		if time.Now().After(deadline) {
			t.Fatalf("get_emails(%s) never reached the expected state: %s", account, text)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func hasSubject(mails []pimEmail, subject string) bool {
	return slices.ContainsFunc(mails, func(m pimEmail) bool { return m.Subject == subject })
}

func firstItem(text string) string {
	var payload struct {
		Emails []json.RawMessage `json:"emails"`
	}
	if json.Unmarshal([]byte(text), &payload) != nil || len(payload.Emails) == 0 {
		return clip(text)
	}
	return string(payload.Emails[0])
}

func clip(s string) string {
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

// writeBaselineBundle hands the PIM's real payloads to scripts/briefing_baseline.py when the
// script asks for them, so the baseline is checked against what the sidecar returns.
func writeBaselineBundle(t *testing.T, events, emails string) {
	t.Helper()
	path := os.Getenv("PIM_E2E_BUNDLE")
	if path == "" {
		return
	}
	bundle, err := json.Marshal(map[string]any{
		"now": time.Now().UTC().Format(time.RFC3339), "zone": "Europe/Rome", "board": "",
		"events": events, "emails": emails,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bundle, 0o600); err != nil {
		t.Fatal(err)
	}
}
