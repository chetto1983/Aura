package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// pimSelfSendTool names the account a scheduler email leaves from: the PIM refuses a send
// without accountId (aura-pim-mcp 63365df, 2026-11-23). A notification goes to the identity's
// own address, so it leaves from that same mailbox when one of the accounts owns it. The listing
// is looked up per send: a mount without list_accounts fails the send, naming the missing tool.
type pimSelfSendTool struct {
	send selfSendTool
	reg  *tools.Registry
}

func (p pimSelfSendTool) Send(ctx context.Context, args json.RawMessage) error {
	var call map[string]json.RawMessage
	if err := json.Unmarshal(args, &call); err != nil {
		return fmt.Errorf("read the email notification: %w", err)
	}
	if _, named := call["accountId"]; named {
		return p.send.Send(ctx, args)
	}
	var to []string
	if raw, ok := call["to"]; ok {
		if err := json.Unmarshal(raw, &to); err != nil {
			return fmt.Errorf("read the notification's recipients: %w", err)
		}
	}
	listing, err := pimAccountListing(p.send.callContext(ctx), p.reg)
	if err != nil {
		return fmt.Errorf("list the PIM accounts: %w", err)
	}
	recipient := ""
	if len(to) > 0 {
		recipient = to[0]
	}
	account, err := sendingAccountFor(listing, recipient)
	if err != nil {
		return err
	}
	call["accountId"], _ = json.Marshal(account)
	named, err := json.Marshal(call)
	if err != nil {
		return err
	}
	return p.send.Send(ctx, named)
}

// sendingAccountFor is the account whose address is the recipient's, when it may send; else the
// first account that may send.
func sendingAccountFor(listing, recipient string) (string, error) {
	accounts, err := pimAccountsIn(listing)
	if err != nil {
		return "", err
	}
	first := ""
	for _, account := range accounts {
		if !account.Permissions.EmailSend {
			continue
		}
		if strings.EqualFold(account.Email, recipient) {
			return account.AccountID, nil
		}
		if first == "" {
			first = account.AccountID
		}
	}
	if first == "" {
		return "", errors.New("no PIM account may send email")
	}
	return first, nil
}
