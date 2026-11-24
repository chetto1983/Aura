package main

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// The PIM's list_accounts names each account's sending address (aura-pim-mcp #4). An account it
// does not list is unknown; one it lists without an address is known, with none.
func TestTheSenderIsReadFromTheAccountListing(t *testing.T) {
	listing := `{"accounts":[
	  {"accountId":"t__lavoro","provider":"imap","displayName":"tecnico","email":"tecnico@example.com"},
	  {"accountId":"t__calendario","provider":"ics","displayName":"Ferie"}]}`
	cases := []struct {
		account, address string
		known            bool
	}{
		{"t__lavoro", "tecnico@example.com", true},
		{"t__calendario", "", true},
		{"t__inventato", "", false},
	}
	for _, tc := range cases {
		address, known, err := senderIn(listing, tc.account)
		if err != nil || address != tc.address || known != tc.known {
			t.Errorf("%s: senderIn = %q, %v, %v", tc.account, address, known, err)
		}
	}
	if _, _, err := senderIn("not json", "t__lavoro"); err == nil {
		t.Fatal("an unreadable listing must be an error, not an unknown account")
	}
}

func TestSendingAddressUsesListAccountsTool(t *testing.T) {
	accounts := &pimTool{tool: "list_accounts", answer: notifyListing}
	reg := tools.NewRegistry()
	reg.Register(accounts)
	senders := pimSenders{reg: reg, runDir: t.TempDir(), previewCap: 4096}

	address, known, err := senders.SendingAddress(context.Background(), "t__lavoro")
	if err != nil || !known || address != "tecnico@example.com" {
		t.Fatalf("SendingAddress = %q, %v, %v; want tecnico@example.com", address, known, err)
	}
	if len(accounts.calls) != 1 || string(accounts.calls[0]) != `{}` {
		t.Fatalf("list_accounts calls = %s, want one call with {}", accounts.calls)
	}

	unmounted := pimSenders{reg: tools.NewRegistry(), runDir: t.TempDir(), previewCap: 4096}
	if _, _, err := unmounted.SendingAddress(context.Background(), "t__lavoro"); err == nil || !strings.Contains(err.Error(), "list_accounts") {
		t.Fatalf("SendingAddress with no list_accounts mounted = %v, want an error naming it", err)
	}
}
