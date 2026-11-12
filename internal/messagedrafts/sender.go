package messagedrafts

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
)

// ErrUnknownAccount is an email draft naming an account the PIM does not list.
var ErrUnknownAccount = errors.New("message draft: the PIM lists no account with that id; list the accounts and ask the operator which to send from")

// SenderDirectory names the address a PIM account sends from, as the PIM itself reports it. known is
// false when the PIM lists no such account; an empty address with known true is an account whose
// address the PIM does not report.
type SenderDirectory interface {
	SendingAddress(ctx context.Context, accountID string) (address string, known bool, err error)
}

// WithSenders makes Create record each email draft's sending address (measured 2026-11-12: the review
// showed only the account id). The host reads it, never the model: a typed address could differ from
// the account that actually sends.
func (s *Store) WithSenders(senders SenderDirectory) *Store {
	s.senders = senders
	return s
}

// resolveSender is the address an email draft leaves from, "" when there is nothing to look up or the
// PIM cannot be asked, in which case the review shows the account's name alone.
func resolveSender(ctx context.Context, senders SenderDirectory, target Target, args json.RawMessage) (string, error) {
	if senders == nil || target.Recipe != "recipe:calendar" {
		return "", nil
	}
	var named struct {
		AccountID string `json:"accountId"`
	}
	if json.Unmarshal(args, &named) != nil || named.AccountID == "" {
		return "", nil
	}
	address, known, err := senders.SendingAddress(ctx, named.AccountID)
	if err != nil {
		slog.Warn("message draft: sending address not read", "error_type", reflect.TypeOf(err).String())
		return "", nil
	}
	if !known {
		return "", ErrUnknownAccount
	}
	return address, nil
}
