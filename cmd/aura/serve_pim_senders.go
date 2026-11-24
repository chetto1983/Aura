package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
)

// pimSendersSessionID is the synthetic session a host-side list_accounts call runs under, as the
// scheduler's self-sends run under theirs (serve_notify.go).
const pimSendersSessionID = "00000000-0000-0000-0000-000000000045"

var pimSendersCallSeq atomic.Uint64

// pimSenders is the drafts store's SenderDirectory: it asks the PIM mounted from the calendar recipe
// which address an account sends from.
type pimSenders struct {
	reg        *tools.Registry
	runDir     string
	previewCap int
}

func (p pimSenders) SendingAddress(ctx context.Context, accountID string) (string, bool, error) {
	callID := fmt.Sprintf("pim-senders-%d", pimSendersCallSeq.Add(1))
	listing, err := pimAccountListing(tools.WithToolCallContext(ctx, pimSendersSessionID, callID, p.runDir, p.previewCap), p.reg)
	if err != nil {
		return "", false, err
	}
	return senderIn(listing, accountID)
}

// pimAccountListing runs the list_accounts tool the calendar recipe mounted, found by recipe and
// never by name. ctx must already carry a tool-call context.
func pimAccountListing(ctx context.Context, reg *tools.Registry) (string, error) {
	tool, ok := reg.ByTrustedRecipe("recipe:calendar", "list_accounts")
	if !ok {
		return "", errors.New("the PIM's list_accounts tool is not mounted")
	}
	result, err := tool.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		return "", err
	}
	if result.Truncated {
		return "", errors.New("the account listing was truncated")
	}
	return result.Preview, nil
}

type pimAccount struct {
	AccountID   string `json:"accountId"`
	Email       string `json:"email"`
	Permissions struct {
		EmailSend bool `json:"emailSend"`
	} `json:"permissions"`
}

// pimAccountsIn reads a list_accounts answer. A listing that cannot be read is an error, so a PIM
// fault never reads as "no such account".
func pimAccountsIn(listing string) ([]pimAccount, error) {
	var answer struct {
		Accounts []pimAccount `json:"accounts"`
	}
	if err := json.Unmarshal([]byte(listing), &answer); err != nil {
		return nil, fmt.Errorf("read the account listing: %w", err)
	}
	return answer.Accounts, nil
}

// senderIn finds an account in a list_accounts answer. An account the listing does not name is unknown.
func senderIn(listing, accountID string) (string, bool, error) {
	accounts, err := pimAccountsIn(listing)
	if err != nil {
		return "", false, err
	}
	for _, account := range accounts {
		if account.AccountID == accountID {
			return account.Email, true, nil
		}
	}
	return "", false, nil
}

func newPIMSenders(cfg *config.Config, reg *tools.Registry) pimSenders {
	return pimSenders{reg: reg, runDir: cfg.RunDir, previewCap: cfg.ToolPreviewCap}
}
