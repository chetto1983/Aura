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

// pimSenders is the drafts store's SenderDirectory: it asks the PIM mounted from the calendar recipe,
// found by recipe and never by name, which address an account sends from.
type pimSenders struct {
	reg        *tools.Registry
	runDir     string
	previewCap int
}

func (p pimSenders) SendingAddress(ctx context.Context, accountID string) (string, bool, error) {
	tool, ok := p.reg.ByTrustedRecipe("recipe:calendar", "calendar")
	if !ok {
		return "", false, errors.New("email is not connected")
	}
	callID := fmt.Sprintf("pim-senders-%d", pimSendersCallSeq.Add(1))
	ctx = tools.WithToolCallContext(ctx, pimSendersSessionID, callID, p.runDir, p.previewCap)
	result, err := tool.Execute(ctx, json.RawMessage(`{"action":"list_accounts"}`))
	if err != nil {
		return "", false, err
	}
	if result.Truncated {
		return "", false, errors.New("the account listing was truncated")
	}
	return senderIn(result.Preview, accountID)
}

// senderIn finds an account in a list_accounts answer. An account the listing does not name is
// unknown; a listing that cannot be read is an error, so a PIM fault never reads as "no such account".
func senderIn(listing, accountID string) (string, bool, error) {
	var answer struct {
		Accounts []struct {
			AccountID string `json:"accountId"`
			Email     string `json:"email"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal([]byte(listing), &answer); err != nil {
		return "", false, fmt.Errorf("read the account listing: %w", err)
	}
	for _, account := range answer.Accounts {
		if account.AccountID == accountID {
			return account.Email, true, nil
		}
	}
	return "", false, nil
}

func newPIMSenders(cfg *config.Config, reg *tools.Registry) pimSenders {
	return pimSenders{reg: reg, runDir: cfg.RunDir, previewCap: cfg.ToolPreviewCap}
}
