package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Measured 2026-11-12 on the lab VM: four scheduled emails carried no accountId, so the PIM sent
// them from its first account while the operator has three. The sending account is part of the
// message, as its recipient is, and is asked for before anything is scheduled.
func TestAnEmailMessageNeedsItsSendingAccount(t *testing.T) {
	t.Parallel()
	for name, payload := range map[string]string{
		"no account":    `{"channel":"email","to":["someone@example.com"],"subject":"Hi","text":"Text"}`,
		"blank account": `{"channel":"email","account":"  ","to":["someone@example.com"],"subject":"Hi","text":"Text"}`,
	} {
		t.Run(name, func(t *testing.T) {
			tool, store, drafts := messageTool()
			a := messageArgs(t, time.Now().Add(time.Hour), payload, `,"notify":"none"`)
			if pause, ok := tool.hostReviewMessage(context.Background(), "conv-1", "call-1", a); ok || pause != nil {
				t.Fatalf("parked without a sending account: %+v", pause)
			}
			if store.creates != 0 || len(drafts.inputs) != 0 {
				t.Fatalf("created %d tasks, %d drafts", store.creates, len(drafts.inputs))
			}
			_, _, err := tool.messageSchedule(a, time.Now())
			if err == nil {
				t.Fatal("an email with no sending account was accepted")
			}
			for _, want := range []string{"payload.account", "list_accounts", "ask the operator", "Nothing was scheduled"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal %q does not contain %q", err, want)
				}
			}
		})
	}
}

func TestAWhatsAppMessageNeedsNoAccount(t *testing.T) {
	t.Parallel()
	tool, _, _ := messageTool()
	if _, _, err := tool.messageSchedule(messageArgs(t, time.Now().Add(time.Hour), whatsappPayload, ""), time.Now()); err != nil {
		t.Fatalf("a WhatsApp message was refused: %v", err)
	}
}
