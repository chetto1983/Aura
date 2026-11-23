package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
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

// pimTool answers list_accounts with a listing and records every call, as the bridged PIM tool does.
type pimTool struct {
	listing string
	calls   []map[string]any
}

func (p *pimTool) Spec() tools.Spec {
	return tools.Spec{Name: "calendar__calendar", TrustedRecipeSource: "recipe:calendar", TrustedRecipeTool: "calendar"}
}

func (p *pimTool) Execute(ctx context.Context, args json.RawMessage) (tools.ToolResult, error) {
	var call map[string]any
	_ = json.Unmarshal(args, &call)
	p.calls = append(p.calls, call)
	if call["action"] == "list_accounts" {
		return tools.NewResult(ctx, p.listing)
	}
	return tools.NewResult(ctx, `{"success":true}`)
}

func TestAPIMNotificationNamesItsSendingAccount(t *testing.T) {
	reg := tools.NewRegistry()
	pim := &pimTool{listing: notifyListing}
	reg.Register(pim)
	sender, ok := selfSendResolver{reg: reg, runDir: t.TempDir(), previewCap: 4096}.Resolve("recipe:calendar", "calendar")
	if !ok {
		t.Fatal("the PIM tool did not resolve")
	}

	err := sender.Send(context.Background(), json.RawMessage(`{"action":"send_email","to":["me@gmail.example"],"subject":"Aura scheduled task","body":"fatto","bodyFormat":"text"}`))
	if err != nil {
		t.Fatalf("Send = %v", err)
	}

	if len(pim.calls) != 2 || pim.calls[0]["action"] != "list_accounts" {
		t.Fatalf("calls = %v, want list_accounts then the send", pim.calls)
	}
	if got := pim.calls[1]["accountId"]; got != "t__personale" {
		t.Fatalf("send accountId = %v, want the recipient's own account", got)
	}
}
