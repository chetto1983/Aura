//go:build pim_mail_e2e

// The change feed internal/pimwatch reads (its testdata/pim_contract.json), held to the real
// sidecar over the GreenMail mailbox. It runs inside TestPIMMailEndToEnd, on that test's token:
// the PIM accepts only tokens from this package's OAuth test issuer, and it caches the issuer's
// JWKS, so a second issuer in the same run would be refused.
package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/pimwatch"
)

type pimContractToken string

func (p pimContractToken) AccessToken(context.Context, string) (string, error) {
	return string(p), nil
}

func testPIMChangeFeedContract(t *testing.T, endpoint, token, account string) {
	t.Helper()
	client := pimwatch.NewClient(endpoint, pimContractToken(token))

	t.Run("an empty cursor starts from now", func(t *testing.T) {
		supported := 0
		for _, resource := range pimwatch.Resources {
			page, err := client.Changes(t.Context(), account, resource, "", pimwatch.PageLimit)
			if pimErr, ok := errors.AsType[*pimwatch.PIMError](err); ok && pimErr.Class == pimwatch.FailureUnsupported {
				continue
			}
			if err != nil {
				t.Fatalf("%s: %v", resource, err)
			}
			if len(page.Changes) != 0 || page.NextCursor == "" {
				t.Fatalf("%s: page = %+v, want no changes and a cursor", resource, page)
			}
			supported++
		}
		if supported == 0 {
			t.Fatalf("account %s supports none of %v: the contract was checked on nothing", account, pimwatch.Resources)
		}
	})

	t.Run("a gone item is named gone", func(t *testing.T) {
		got, err := client.Items(t.Context(), account, pimwatch.ResourceMail, []pimwatch.ItemRef{{ID: "no-such-mail"}})
		if err != nil {
			t.Fatalf("Items: %v", err)
		}
		if len(got.Items) != 0 || len(got.Gone) != 1 || got.Gone[0].ID != "no-such-mail" {
			t.Fatalf("result = %+v, want no items and no-such-mail gone", got)
		}
	})
}
