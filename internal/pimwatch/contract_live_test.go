//go:build pim_contract

// Holds a real PIM sidecar to testdata/pim_contract.json. It reads a real account, so it runs only
// when asked:
//
//	AURA_PIM_CONTRACT_URL=http://127.0.0.1:8090 AURA_PIM_CONTRACT_TOKEN=... \
//	AURA_PIM_CONTRACT_ACCOUNT=work go test -tags pim_contract -run TestLiveContract ./internal/pimwatch/
package pimwatch

import (
	"context"
	"os"
	"testing"
)

type envToken struct{ token string }

func (e envToken) AccessToken(context.Context, string) (string, error) { return e.token, nil }

func liveClient(t *testing.T) (*Client, string) {
	t.Helper()
	url, token, account := os.Getenv("AURA_PIM_CONTRACT_URL"), os.Getenv("AURA_PIM_CONTRACT_TOKEN"), os.Getenv("AURA_PIM_CONTRACT_ACCOUNT")
	if url == "" || token == "" || account == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("the pim_contract tier needs AURA_PIM_CONTRACT_URL, _TOKEN and _ACCOUNT")
		}
		t.Skip("set AURA_PIM_CONTRACT_URL, _TOKEN and _ACCOUNT to run against a sidecar")
	}
	return NewClient(url, envToken{token: token}), account
}

func TestLiveContractAnEmptyCursorStartsFromNow(t *testing.T) {
	client, account := liveClient(t)
	for _, resource := range Resources {
		page, err := client.Changes(t.Context(), account, resource, "", PageLimit)
		if class, _ := failureClass(err); err != nil && class == FailureUnsupported {
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", resource, err)
		}
		if len(page.Changes) != 0 || page.NextCursor == "" {
			t.Fatalf("%s: page = %+v, want no changes and a cursor", resource, page)
		}
	}
}

func TestLiveContractAGoneItemIsNamedGone(t *testing.T) {
	client, account := liveClient(t)
	got, err := client.Items(t.Context(), account, ResourceMail, []ItemRef{{ID: "no-such-mail"}})
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(got.Items) != 0 || len(got.Gone) != 1 || got.Gone[0].ID != "no-such-mail" {
		t.Fatalf("result = %+v", got)
	}
}
