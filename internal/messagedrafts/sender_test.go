package messagedrafts

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeSenders struct {
	addresses map[string]string
	err       error
	asked     []string
}

func (f *fakeSenders) SendingAddress(_ context.Context, accountID string) (string, bool, error) {
	f.asked = append(f.asked, accountID)
	if f.err != nil {
		return "", false, f.err
	}
	address, known := f.addresses[accountID]
	return address, known, nil
}

var calendarSend = Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"}

// Measured 2026-11-12: the review showed only the account id. The host reads the address the
// account sends from when the draft is created; the model's word is never taken for it.
func TestTheSenderIsTheAccountsOwnAddress(t *testing.T) {
	t.Parallel()
	senders := &fakeSenders{addresses: map[string]string{"t__personale": "someone@gmail.com"}}
	args := json.RawMessage(`{"action":"send_email","accountId":"t__personale","to":["a@example.com"]}`)
	got, err := resolveSender(context.Background(), senders, calendarSend, args)
	if err != nil || got != "someone@gmail.com" {
		t.Fatalf("resolveSender = %q, %v", got, err)
	}
}

func TestAnAccountThePIMDoesNotKnowIsRefused(t *testing.T) {
	t.Parallel()
	senders := &fakeSenders{addresses: map[string]string{"t__personale": "someone@gmail.com"}}
	args := json.RawMessage(`{"action":"send_email","accountId":"t__invented","to":["a@example.com"]}`)
	if _, err := resolveSender(context.Background(), senders, calendarSend, args); !errors.Is(err, ErrUnknownAccount) {
		t.Fatalf("err = %v, want ErrUnknownAccount", err)
	}
}

// A PIM that cannot be asked, or a draft that names no account, still makes a draft: the review then
// shows the account's name alone, as before.
func TestNoSenderIsLookedUpWhenNothingCanBeFound(t *testing.T) {
	t.Parallel()
	email := json.RawMessage(`{"action":"send_email","accountId":"t__personale","to":["a@example.com"]}`)
	cases := map[string]struct {
		senders SenderDirectory
		target  Target
		args    json.RawMessage
	}{
		"no directory":           {nil, calendarSend, email},
		"WhatsApp":               {&fakeSenders{}, Target{Recipe: "recipe:whatsapp", Tool: "send_message"}, json.RawMessage(`{"recipient":"1","message":"m"}`)},
		"no account named":       {&fakeSenders{}, calendarSend, json.RawMessage(`{"action":"send_email","to":["a@example.com"]}`)},
		"the PIM is unreachable": {&fakeSenders{err: errors.New("dial tcp: refused")}, calendarSend, email},
	}
	for name, tc := range cases {
		got, err := resolveSender(context.Background(), tc.senders, tc.target, tc.args)
		if err != nil || got != "" {
			t.Errorf("%s: resolveSender = %q, %v", name, got, err)
		}
	}
}
