package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/cron"
)

const notifyListing = `{"accounts":[
  {"accountId":"t__lavoro","provider":"imap","email":"tecnico@example.com","permissions":{"emailSend":true}},
  {"accountId":"t__personale","provider":"google","email":"me@gmail.example","permissions":{"emailSend":true}},
  {"accountId":"t__sola-lettura","provider":"microsoft365","email":"boss@example.org","permissions":{"emailSend":false}},
  {"accountId":"t__ferie","provider":"ics"}]}`

// The PIM refuses a send without accountId (aura-pim-mcp 63365df, 2026-11-23), and a scheduler
// notification goes to the identity's own address: it leaves from that same mailbox when one
// matches, else from the first account that may send.
func TestTheNotificationLeavesFromTheRecipientsOwnAccount(t *testing.T) {
	cases := []struct{ recipient, want string }{
		{"Me@Gmail.example", "t__personale"},
		{"someone@elsewhere.example", "t__lavoro"},
		{"boss@example.org", "t__lavoro"}, // its own account may not send
	}
	for _, tc := range cases {
		got, err := sendingAccountFor(notifyListing, tc.recipient)
		if err != nil || got != tc.want {
			t.Errorf("sendingAccountFor(%s) = %q, %v; want %q", tc.recipient, got, err, tc.want)
		}
	}
	if _, err := sendingAccountFor(`{"accounts":[{"accountId":"t__ferie","provider":"ics"}]}`, "me@gmail.example"); err == nil {
		t.Error("with no account that may send, the notification must fail, not leave unaddressed")
	}
	if _, err := sendingAccountFor("not json", "me@gmail.example"); err == nil {
		t.Error("an unreadable listing must be an error")
	}
}

// pimTool is one tool the bridge mounted from the PIM recipe. It records every call's raw
// arguments and answers each with the same text.
type pimTool struct {
	tool, answer string
	calls        []json.RawMessage
}

func (p *pimTool) Spec() tools.Spec {
	return tools.Spec{Name: "calendar__" + p.tool, TrustedRecipeSource: "recipe:calendar", TrustedRecipeTool: p.tool}
}

func (p *pimTool) Execute(ctx context.Context, args json.RawMessage) (tools.ToolResult, error) {
	p.calls = append(p.calls, args)
	return tools.NewResult(ctx, p.answer)
}

func (p *pimTool) call(t *testing.T, i int) map[string]any {
	t.Helper()
	var call map[string]any
	if err := json.Unmarshal(p.calls[i], &call); err != nil {
		t.Fatalf("%s call %d: %v", p.tool, i, err)
	}
	return call
}

// mountedPIM registers the given PIM tools and resolves the email route's sender over them.
func mountedPIM(t *testing.T, mounted ...*pimTool) cron.SelfSendTool {
	t.Helper()
	reg := tools.NewRegistry()
	for _, tool := range mounted {
		reg.Register(tool)
	}
	sender, ok := selfSendResolver{reg: reg, runDir: t.TempDir(), previewCap: 4096}.Resolve("recipe:calendar", "send_email")
	if !ok {
		t.Fatal("the send_email tool did not resolve")
	}
	return sender
}

const notificationEmail = `{"to":["me@gmail.example"],"subject":"Aura scheduled task","body":"fatto","bodyFormat":"text"}`

func TestNotificationListsAccountsThroughItsOwnTool(t *testing.T) {
	accounts := &pimTool{tool: "list_accounts", answer: notifyListing}
	send := &pimTool{tool: "send_email", answer: `{"success":true}`}
	sender := mountedPIM(t, accounts, send)

	if err := sender.Send(context.Background(), json.RawMessage(notificationEmail)); err != nil {
		t.Fatalf("Send = %v", err)
	}

	if len(accounts.calls) != 1 || string(accounts.calls[0]) != `{}` {
		t.Fatalf("list_accounts calls = %s, want one call with {}", accounts.calls)
	}
	if len(send.calls) != 1 {
		t.Fatalf("send_email calls = %s, want one", send.calls)
	}
	got := send.call(t, 0)
	if got["accountId"] != "t__personale" {
		t.Fatalf("send accountId = %v, want the recipient's own account", got["accountId"])
	}
	if _, multiplexed := got["action"]; multiplexed {
		t.Fatalf("send_email args carry an action: %v", got)
	}

	accounts.calls, send.calls = nil, nil
	named := `{"accountId":"t__lavoro","to":["me@gmail.example"],"subject":"s","body":"b"}`
	if err := sender.Send(context.Background(), json.RawMessage(named)); err != nil {
		t.Fatalf("Send with an account = %v", err)
	}
	if len(accounts.calls) != 0 || len(send.calls) != 1 || send.call(t, 0)["accountId"] != "t__lavoro" {
		t.Fatalf("list_accounts %s, send_email %s; want the named account kept and no listing", accounts.calls, send.calls)
	}
}

// Review Focus 5: a partial mount carries send_email without list_accounts. The notification
// cannot name its account, so it fails and sends nothing.
func TestNotificationWithoutListAccountsToolFails(t *testing.T) {
	send := &pimTool{tool: "send_email", answer: `{"success":true}`}
	sender := mountedPIM(t, send)

	err := sender.Send(context.Background(), json.RawMessage(notificationEmail))
	if err == nil || !strings.Contains(err.Error(), "list_accounts") {
		t.Fatalf("Send = %v, want an error naming list_accounts", err)
	}
	if len(send.calls) != 0 {
		t.Fatalf("send_email calls = %s, want none", send.calls)
	}
}
