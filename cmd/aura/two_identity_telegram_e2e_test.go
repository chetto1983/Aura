//go:build db_integration && garage_integration && authula_integration && musr_e2e && arcadedb_integration

// Two-identity cross-deny for the owner's Telegram account list and unlink (spec
// 2026-10-27-telegram-account-links, acceptance 9). The links are made through the real
// onboarding consume, and the requests go through the real AG-UI routes behind RequireAuth,
// on the live aura_app pool with the links' row security on. The mount behind governance.write
// is pinned by TestServeWebuiTelegramAccountRoutesAreGated.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/channels/telegram"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identity"
)

func TestTwoIdentityTelegramLinksCrossDeny(t *testing.T) {
	pool := musrMigratedPool(t)
	links := telegram.New(pool)
	idA, idB := musrProvisionIdentity(t, pool, "tg-alice"), musrProvisionIdentity(t, pool, "tg-bob")
	const linkA, linkB int64 = 9_000_080_001, 9_000_080_002
	ctx := context.Background()
	for owner, chat := range map[string]int64{idA: linkA, idB: linkB} {
		token := uuid.NewString()
		if err := links.InsertPending(ctx, telegram.InsertPendingParams{OnboardingToken: token, IdentityID: owner, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatalf("mint a token for %d: %v", chat, err)
		}
		if _, err := links.ConsumeOnboarding(ctx, telegram.ConsumeParams{OnboardingToken: token, TelegramUserID: chat}); err != nil {
			t.Fatalf("link %d: %v", chat, err)
		}
	}

	convStore := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	server := agui.NewServer(musrStubRunner{conv: convStore}, convStore, agui.ServerConfig{})
	server.SetTelegramAccountLinks(telegramAccountLinks{inner: links})
	srv := httptest.NewServer(agui.RequireAuth(server.Mux(), agui.AuthDeps{
		SecretConfigured: true,
		LocalIdentityID:  musrLocalIdentity,
		Identities:       identityCheckerAdapter{store: identity.New(pool)},
		SessionValidator: func(r *http.Request) (string, bool) {
			id := r.Header.Get(musrIdentityHeader)
			return id, id != ""
		},
	}))
	t.Cleanup(srv.Close)
	call := func(identityID, method, path string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(musrIdentityHeader, identityID)
		if method != http.MethodGet {
			req.Header.Set("Idempotency-Key", uuid.NewString())
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("%s %s as %s: %v", method, path, identityID, err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, body
	}
	listed := func(identityID string) []int64 {
		t.Helper()
		code, body := call(identityID, http.MethodGet, "/api/settings/telegram/accounts")
		var accounts []agui.TelegramAccount
		if code != http.StatusOK || json.Unmarshal(body, &accounts) != nil {
			t.Fatalf("list as %s = %d %s", identityID, code, body)
		}
		ids := make([]int64, 0, len(accounts))
		for _, a := range accounts {
			ids = append(ids, a.TelegramUserID)
		}
		return ids
	}
	unlinkPath := func(chat int64) string {
		return "/api/settings/telegram/accounts/" + strconv.FormatInt(chat, 10)
	}

	if got := listed(idA); len(got) != 1 || got[0] != linkA {
		t.Fatalf("A lists %v, want only its own %d", got, linkA)
	}
	if code, _ := call(idA, http.MethodDelete, unlinkPath(linkB)); code != http.StatusNotFound {
		t.Fatalf("A's DELETE of B's link = %d, want 404", code)
	}
	if acct, err := links.GetAccountByTelegramID(ctx, linkB); err != nil || acct.IdentityID != idB {
		t.Fatalf("B's link after A's DELETE = %+v, %v; want it still B's", acct, err)
	}
	if code, _ := call(idB, http.MethodDelete, unlinkPath(linkB)); code != http.StatusNoContent {
		t.Fatalf("B's DELETE of its own link = %d, want 204", code)
	}
	if _, err := links.GetAccountByTelegramID(ctx, linkB); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("B's unlinked account still resolves: %v", err)
	}
	if got := listed(idB); len(got) != 0 {
		t.Fatalf("B lists %v after unlinking, want none", got)
	}
	if got := listed(idA); len(got) != 1 || got[0] != linkA {
		t.Fatalf("A lists %v after B's unlink, want its own %d untouched", got, linkA)
	}
}
