package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
)

type fakeLinkedAccounts struct {
	linked []tools.TelegramAccount
	err    error
	asked  int
}

func (f *fakeLinkedAccounts) LinkedTelegramAccounts(context.Context) ([]tools.TelegramAccount, error) {
	f.asked++
	return f.linked, f.err
}

// `aura task schedule --notify=telegram` names its recipients the way the task tool does
// (docs/superpowers/specs/2026-10-28-telegram-push-recipients-design.md, R2); the CLI cannot ask,
// so two or more links with none named is refused and names the flag.
func TestCLITelegramRecipients(t *testing.T) {
	phone := tools.TelegramAccount{TelegramUserID: 4242, Username: "davide"}
	work := tools.TelegramAccount{TelegramUserID: 4343, FirstName: "Lavoro"}
	two := func() *fakeLinkedAccounts { return &fakeLinkedAccounts{linked: []tools.TelegramAccount{phone, work}} }

	for name, c := range map[string]struct {
		accounts *fakeLinkedAccounts
		flag     string
		want     []int64
	}{
		"named, a repeat stored once in the order given": {two(), "4343, 4242,4343", []int64{4343, 4242}},
		"all names every linked account":                 {two(), "all", []int64{4242, 4343}},
		"one link and none named stores that one":        {&fakeLinkedAccounts{linked: []tools.TelegramAccount{phone}}, "", []int64{4242}},
	} {
		got, err := cliTelegramRecipients(context.Background(), c.accounts, "telegram", c.flag)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("%s: recipients = %v, %v; want %v", name, got, err, c.want)
		}
	}

	for name, c := range map[string]struct {
		accounts *fakeLinkedAccounts
		flag     string
		want     []string
	}{
		"two links and none named": {two(), "", []string{"--telegram-recipients", "@davide (4242)", "Lavoro (4343)", "all"}},
		"no link":                  {&fakeLinkedAccounts{}, "", []string{"no Telegram account is linked"}},
		"an unlinked id":           {two(), "4242,9999", []string{"9999 is not linked"}},
		"an id that is no number":  {two(), "4242,abc", []string{`"abc"`}},
		"a failing lookup":         {&fakeLinkedAccounts{err: errors.New("connection refused")}, "4242", []string{"connection refused"}},
	} {
		got, err := cliTelegramRecipients(context.Background(), c.accounts, "telegram", c.flag)
		if err == nil {
			t.Errorf("%s: recipients = %v, want a refusal", name, got)
			continue
		}
		for _, want := range c.want {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error %q does not contain %q", name, err, want)
			}
		}
	}

	t.Run("another route takes no recipients and never asks", func(t *testing.T) {
		accounts := two()
		if got, err := cliTelegramRecipients(context.Background(), accounts, "stdout", ""); err != nil || got != nil || accounts.asked != 0 {
			t.Fatalf("stdout = (%v, %v) after %d lookups, want (nil, nil) and none", got, err, accounts.asked)
		}
		if _, err := cliTelegramRecipients(context.Background(), accounts, "stdout", "4242"); err == nil || !strings.Contains(err.Error(), "only with --notify=telegram") || accounts.asked != 0 {
			t.Fatalf("stdout with recipients = %v after %d lookups, want a refusal and none", err, accounts.asked)
		}
	})
}
