//go:build db_integration

// share_cross_identity_test.go ships the WEBSHARE-04 SC4 acceptance vehicle: the ten-row
// cross-identity deny matrix proving no other identity's conversation, artifact, or
// capability state ever reaches a recipient, across every share trust boundary this phase
// built — owner-scoped CRUD (share_api.go), the D-10 bearer-within-auth internal reads
// (share_api_internal.go), and the unauthenticated public token routes
// (share_api_public.go).
//
// WHY HERE, NOT cmd/aura: cmd/aura already carries an older cross-identity E2E
// (two_identity_e2e_test.go) gated behind five build tags (see that file's own header) that
// confine it to the full live stack. The coverage gate (scripts/coverage_gate.sh) runs
// exactly two of those tags and measures ./internal/... only — cmd/aura is never measured,
// at any tag, so a file needing the full five-tag combo compiles-and-skips under the gate's
// two, contributing ZERO coverage (the WR-01 failure mode that let a real capability gap —
// see row 8 below — ride along latent through plan 37F-12). This file carries exactly ONE
// build tag and lives in internal/agui, so it sits inside both the measured package and the
// measured tag set. A future reader who "consolidates" this next to the older E2E deletes
// this file's entire coverage value — don't.
//
// Object store: objectstore.NewFake() only, wired transitively via newShareAPIEnv
// (share_api_test.go) — no live blob store of any kind backs this file, and no external
// dependency beyond Postgres is required.
//
// Requires a running Postgres with the migrations applied:
//
//	make db-up && aura db migrate
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race -p 1 -count=1 -run TestShareCrossIdentityDeny -v ./internal/agui/
//
// No-skip-as-green: migratedPool's envOrSkip (server_integration_test.go) t.Fatals under
// $CI when the DSN is unset — a skipped integration test must never pass green.
package agui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/share"
)

const (
	sc4TitleA            = "Aura-SC4-Owner-A-Weather-Briefing"
	sc4TitleB            = "Aura-SC4-Owner-B-Private-Budget"
	sc4ProseMarkerA      = "aura-sc4-marker-owner-A-prose-quarterly-roadmap"
	sc4ProseMarkerB      = "aura-sc4-marker-owner-B-prose-confidential-budget"
	sc4ArtifactNameA     = "a-report.txt"
	sc4ArtifactNameB     = "b-secret-report.pdf"
	sc4SigningKeyLiteral = "0123456789abcdef0123456789abcdef"
)

// TestShareCrossIdentityDeny is the WEBSHARE-04 SC4 acceptance vehicle: ten rows proving no
// other identity's data reaches a recipient across every share trust boundary. Two fresh,
// provisioned, NON-WILDCARD identities (R-13) are seeded per run via seedShareExportIdentity
// (share_export_test.go, same package/tag — name = "share-export-"+t.Name()+"-"+uuid, unique
// across parallel runs). Neither identity is ever granted the wildcard capability, and
// neither is the seeded operator identity — every capability assertion below is real, not
// vacuous.
func TestShareCrossIdentityDeny(t *testing.T) {
	pool := migratedPool(t)
	env := newShareAPIEnv(t, pool, true) // publicEnabled=true: the org kill-switch is OPEN,
	// so every 403 below (row 8) is the PER-USER capability gate, never the kill-switch.
	// Row 1 (GET /api/conversations/{id}/export) and row 10 (GET /api/assets/{id}/download)
	// both sit behind a Server.assets nil-check that runs BEFORE their own owner/auth gate —
	// newShareAPIEnv wires the share service only, not this one, so wire it here.
	env.server.SetAssetService(env.assetsF)
	ctx := context.Background()

	// --- Identity A: owns conv-A, the conversation every deny-row below targets. ---
	ownerA := seedShareExportIdentity(t, pool)
	convA := seedExportConversation(t, env.convStore, ownerA, sc4TitleA, []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "What should tomorrow's briefing cover?"},
		{Seq: 2, Role: llm.RoleAssistant, Content: sc4ProseMarkerA + ": cover the quarterly roadmap and nothing else."},
	})

	// --- Identity B: the prober/attacker in rows 1-2, 6, 9-10; the bearer in row 3; the
	// capability-holder in row 8. B owns her OWN conversation + artifact so rows 5/9 have
	// real, distinguishing "foreign" data to assert is absent from A's public snapshot. ---
	ownerB := seedShareExportIdentity(t, pool)
	convB := seedExportConversation(t, env.convStore, ownerB, sc4TitleB, []conversations.AppendTurnParams{
		{Seq: 1, Role: llm.RoleUser, Content: "Summarize my private budget."},
		{Seq: 2, Role: llm.RoleAssistant, Content: sc4ProseMarkerB + ": B's confidential Q3 numbers."},
	})

	// B mints her OWN agent-produced artifact + an internal share of her OWN conversation —
	// assetB below is a REAL, independently-resolvable asset id (not a random UUID row 9
	// invents), which is exactly what makes row 9's negative probe meaningful.
	assetB := seedBundledArtifact(env, sc4ArtifactNameB, []byte("B's confidential content"))
	createShare(t, env, ownerB, convB, "internal", http.StatusCreated)

	// Reset the shared single-slot fake asset service to A's OWN artifact before minting
	// anything for A: seedBundledArtifact overwrites env.assetsF wholesale, so B's artifact
	// above must not still be "current" when A's shares below call ListForThread — that would
	// make this suite's own fixture leak B's artifact into A's snapshot, a false positive
	// this file exists to catch as a real bug, not manufacture as a test artifact.
	assetA := seedBundledArtifact(env, sc4ArtifactNameA, []byte("A's own content"))

	linkAInternal := createShare(t, env, ownerA, convA, "internal", http.StatusCreated)

	// A's live public link (rows 5, 9): minted DIRECTLY over the share service, bypassing the
	// HTTP handler's share.public capability gate entirely — A is NEVER granted share.public
	// anywhere in this test (row 8's whole point), and the service layer itself never checked
	// capability (only the org kill-switch, ErrSharePublicDisabled); the capability check
	// lives exclusively in handleShareCreate (share_api.go). TestShareInternalRevokedExpired404
	// (share_api_test.go) sets the precedent for building fixtures directly over the
	// store/service instead of through HTTP in this file family.
	resPublic1, err := env.adapter.Create(ctx, share.CreateRequest{
		ConversationID: convA, OwnerIdentityID: ownerA, Tier: share.TierPublic, ExpiryOption: share.ExpiryDefault,
	})
	if err != nil {
		t.Fatalf("mint A's public link (row5/row9 fixture): %v", err)
	}

	// Row 1: A owns conv-A; B GETs the export of conv-A. A read must hide foreign existence
	// (36 D-06) — 404, never 403 — and the 404 body must not echo A's title back.
	t.Run("row1_export_foreign_conversation_hides_existence", func(t *testing.T) {
		rec := shareReq(env.server, http.MethodGet, "/api/conversations/"+convA+"/export", ownerB, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("B's export of A's conversation status = %d, want 404 (not 403 — reads hide foreign existence): %s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), sc4TitleA) {
			t.Fatalf("404 body leaks A's conversation title: %q", rec.Body.String())
		}
	})

	// Row 2: B cannot mint a link to A's thread — the owner gate inside share.Service.Create
	// runs before tier resolution or any capability check, so this row uses tier=internal to
	// isolate the OWNER-gate property under test from row 8's capability property.
	t.Run("row2_mint_share_for_foreign_conversation_hides_existence", func(t *testing.T) {
		rec := shareReq(env.server, http.MethodPost, "/api/shares", ownerB,
			`{"conversation_id":"`+convA+`","tier":"internal"}`)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("B minting a share for A's conversation status = %d, want 404: %s", rec.Code, rec.Body.String())
		}
	})

	// Row 3 is the ONE row in this suite whose expected answer is success: D-10
	// bearer-within-auth is intended BY DESIGN — share.Service.ResolveInternal runs NO owner
	// predicate on purpose, and the already-redacted snapshot (D-08) is the protection, not an
	// owner check. B here is deliberately the NON-owner: resolving as A would pass vacuously
	// and prove nothing. Do NOT "fix" this by adding an owner filter to
	// handleShareResolveInternal — that would silently reduce the internal tier to an
	// owner-only view and break D-10 (see that handler's own doc comment).
	//
	// This route (GET /api/shares/{id}/data) exists at all, rather than /s/{token}, because
	// migration 0040's shared_links_tier_shape CHECK forces token_hash IS NULL for
	// tier='internal' — an internal share has no token to put in a /s/ URL (PRD item 17 /
	// RESEARCH OQ#4).
	t.Run("row3_internal_bearer_within_auth_is_intended_200", func(t *testing.T) {
		rec := shareReq(env.server, http.MethodGet, "/api/shares/"+linkAInternal.ID+"/data", ownerB, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("bearer B resolving A's internal link status = %d, want 200 (D-10 bearer-within-auth is intended): %s", rec.Code, rec.Body.String())
		}
	})

	// Row 4 is row 3's authenticated-lane counterpart: the SAME route with NO principal at all
	// must 401/302, proving the internal tier is NOT on the /s/ public allowlist
	// (isPublicShareRoute, cmd/aura/serve_webui_share.go, admits every GET /s/... with no
	// session at all — this row is what fails the moment anyone "unifies" the internal tier
	// onto that lane). A bare handler call would bypass the very RequireAuth gate under test
	// and pass vacuously, so this wraps env.server.Mux() in the real RequireAuth chain with
	// the same shape production wires at the parent mux (cmd/aura/serve_webui.go).
	t.Run("row4_internal_anonymous_401_not_on_public_allowlist", func(t *testing.T) {
		deps := AuthDeps{SecretConfigured: true, SigningKey: []byte(sc4SigningKeyLiteral)}
		gated := RequireAuth(env.server.Mux(), deps)
		req := httptest.NewRequest(http.MethodGet, "/api/shares/"+linkAInternal.ID+"/data", nil)
		rec := httptest.NewRecorder()
		gated.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusFound {
			t.Fatalf("anonymous internal resolve status = %d, want 401 or 302", rec.Code)
		}
	})

	// Row 5: A's public link resolves anonymously with A's own prose present and ZERO trace
	// of B's data or any host/container path shape. A status-only assertion here would prove
	// nothing — Snapshot/SnapshotArtifact structurally cannot carry a path (snapshot.go), so
	// these substring checks are a regression guard against that structural guarantee ever
	// being weakened, not a redundant belt-and-suspenders check.
	t.Run("row5_public_anonymous_200_zero_foreign_data", func(t *testing.T) {
		rec := shareReq(env.server, http.MethodGet, "/s/"+resPublic1.Token+"/data", "", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("anonymous public resolve status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, sc4ProseMarkerA) {
			t.Fatalf("public snapshot body is missing A's own prose marker: %q", body)
		}
		for _, forbidden := range []string{sc4ProseMarkerB, sc4TitleB, sc4ArtifactNameB, "/abs/", "/etc/", "AURA_RUN_DIR"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("public snapshot body leaks forbidden substring %q: %s", forbidden, body)
			}
		}
	})

	// Row 6: B cannot revoke A's link — ANY error (foreign or absent) collapses to 404, never
	// 403 (D-06). The DELETE attempt fails before any store mutation, so linkAInternal stays
	// live; no other row depends on that, but it costs nothing to note.
	t.Run("row6_foreign_revoke_hides_existence", func(t *testing.T) {
		rec := shareReq(env.server, http.MethodDelete, "/api/shares/"+linkAInternal.ID, ownerB, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("B revoking A's link status = %d, want 404: %s", rec.Code, rec.Body.String())
		}
	})

	// Row 7: A mints a SECOND public link, revokes it herself (a legitimate owner-scoped
	// revoke, unlike row 6's denied one), and the anonymous resolve afterward must be a clean
	// 404 with no stale render (D-15) — the body must not still carry A's title. Self-contained
	// in this subtest: no other row depends on this second link.
	t.Run("row7_revoked_public_404_no_stale_render", func(t *testing.T) {
		resPublic2, err := env.adapter.Create(ctx, share.CreateRequest{
			ConversationID: convA, OwnerIdentityID: ownerA, Tier: share.TierPublic, ExpiryOption: share.ExpiryDefault,
		})
		if err != nil {
			t.Fatalf("mint A's second public link: %v", err)
		}
		if err := env.adapter.Revoke(ctx, resPublic2.Link.ID.String(), ownerA); err != nil {
			t.Fatalf("A revokes her own link: %v", err)
		}
		rec := shareReq(env.server, http.MethodGet, "/s/"+resPublic2.Token+"/data", "", "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("revoked public resolve status = %d, want 404: %s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), sc4TitleA) {
			t.Fatalf("404 body is a stale render — leaks A's title: %q", rec.Body.String())
		}
	})

	// Row 8 is the capability-gap closure this plan exists to prove (37F-13, discovered +
	// logged during plan 37F-12): handleShareCreate now checks the caller's share.public
	// capability for a public-tier mint, on top of the org kill-switch. Seeding the grant to
	// B (not A) — via a raw INSERT, the harness pattern, rather than GrantCapability — proves
	// share.public is a PER-IDENTITY grant, not a global flag: B holding it must have ZERO
	// effect on A's own request. A bare shareReq (not the createShare helper) is used
	// deliberately: createShare auto-grants share.public for a tier=public POST so every
	// pre-existing caller of that helper keeps minting (see its own doc comment) — that
	// auto-grant must NEVER touch ownerA here, or this row would pass vacuously.
	t.Run("row8_mint_public_without_capability_403", func(t *testing.T) {
		if _, err := pool.Exec(ctx,
			`INSERT INTO aura.capability_grants (identity_id, capability) VALUES ($1::uuid, $2)`,
			ownerB, sharePublicCapabilityName); err != nil {
			t.Fatalf("seed share.public for B: %v", err)
		}
		rec := shareReq(env.server, http.MethodPost, "/api/shares", ownerA,
			`{"conversation_id":"`+convA+`","tier":"public"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("A minting public without share.public status = %d, want 403: %s", rec.Code, rec.Body.String())
		}
	})

	// Row 9 is one of the two rows a naive "token authenticates, then fetch any asset id"
	// implementation fails: assetB is a REAL, independently-valid asset id minted under B's
	// OWN internal share above — proving A's public token 404s it shows the scope is THIS
	// snapshot's artifact list, not "any known asset id anywhere in the system".
	t.Run("row9_asset_scoped_to_its_own_snapshot_404", func(t *testing.T) {
		rec := shareReq(env.server, http.MethodGet, "/s/"+resPublic1.Token+"/asset/"+assetB, "", "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("A's public token resolving B's assetID status = %d, want 404: %s", rec.Code, rec.Body.String())
		}
	})

	// Row 10 is the other row a naive implementation fails: holding (or having anonymously
	// used) a public token must grant NO access to the identity-scoped /api/assets download
	// lane. There is no session-minting step anywhere on the /s/ path, so the bare anonymous
	// request below already demonstrates "the public session leaks into the authenticated
	// lane" cannot happen — exercised through the real RequireAuth chain exactly like row 4,
	// never a bare handler call.
	t.Run("row10_public_token_grants_no_identity_lane_access", func(t *testing.T) {
		deps := AuthDeps{SecretConfigured: true, SigningKey: []byte(sc4SigningKeyLiteral)}
		gated := RequireAuth(env.server.Mux(), deps)
		req := httptest.NewRequest(http.MethodGet, "/api/assets/"+assetA+"/download", nil)
		rec := httptest.NewRecorder()
		gated.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusFound {
			t.Fatalf("anonymous identity-scoped download status = %d, want 401 or 302", rec.Code)
		}
	})
}
