# Management Key — Plan A (Daemon) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The daemon needs only the OpenRouter management key: it mints and caps every other key itself, keeps secret settings encrypted and out of the process environment, and bills web and Telegram turns to the key of the identity that owns them.

**Architecture:** A cap that can be "no limit" runs end to end: provider wire, `identitykey`, credit policy, credit and spend APIs. Secret `aura.settings` rows become AES-GCM ciphertext behind an `enc:v1:` prefix and are read through `settings.Store.Secret`, never the environment. The management key is read at call time. One `agui.IdentityKeyMinter` mints a person's key for both the provisioning saga and an idempotent `Server.EnsureOpenRouterKeys` reconciler; the reconciler also mints the `aura-services` key and aligns each key's limit with its owner's role. The per-identity resolver follows the live runtime and is finally wired into the serve-path Runner.

**Tech Stack:** Go 1.26, pgx/v5, sqlc v1.31.1, golang-migrate, `crypto/hkdf` + AES-256-GCM, OpenRouter Provisioning API.

**Spec:** `docs/superpowers/specs/2026-09-10-management-key-onboarding-design.md` (approved, 0937ece8e). This plan is the spec's Delivery item A. Plans B (UI), C (installer, compose, `.env`) and D (reinstall + E2E) follow.

## Global Constraints

- Work on `master`, one commit per task. Message: imperative subject, body with the why, last line `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`. Leave the `.planning/*` changes in the tree alone: they are not this plan's.
- A migration number is `ls internal/db/migrations/ | tail -1` plus one at the moment the task runs (0125 today). Never copy it from a document.
- No file above 600 lines. A file that would cross the cap is split in the same task.
- After every Go edit: `go vet ./...`, `go build ./...`, `go test` on the touched packages, then the race detector in WSL from Git Bash:
  `MSYS_NO_PATHCONV=1 wsl.exe -- bash -lc 'cd /mnt/d/Repo/Aura && export PATH="$HOME/go/bin:/usr/local/go/bin:$PATH" && go test -race -count=1 ./<each touched package>/'`.
  The lefthook pre-commit hook runs gofmt, file-size, vet and golangci-lint.
- A test changes only when it encodes behaviour this plan deliberately changes; the commit body names the test and says why.
- Never log, echo or wrap a credential in an error. Comments only for a non-obvious why.
- Values copied from the spec: HKDF info string `aura-settings-secret-v1`; ciphertext prefix `enc:v1:`; services key name `aura-services`; an admin is an identity holding `identity.CapIdentityCreate`; service identities (`kind = 'service'`) get no key; nothing falls back to the services key (CRED-07); member keys mint at a zero cap with a monthly reset.
- New setting row `AURA_OPENROUTER_SERVICES_CAP_USD`: the services key's monthly cap in USD. Plan B's wizard writes it.
- Coverage: every touched package keeps its `scripts/coverage_package_policy.json` floor (85% target).
- Integration tests (`-tags db_integration`) run against the disposable database (`bash scripts/coverage_docker.sh`), with `AURA_DB_URL`, `AURA_DB_MIGRATE_URL`, `POSTGRES_PASSWORD` and `AURA_AUTHULA_SECRET` exported from `docker exec aura printenv` without printing them.

## Deviations from the spec, found while reading the code

1. The spec hooks role changes into `handleGrantCapability` and `handleRevokeCapability`. Those handlers refuse administrative capabilities for every caller (`internal/identity/capability_policy.go:42-55`, D-02), so `identity.create` changes only through `aura identity grant|revoke` on the host. Instead, the reconciler aligns every key's limit with its owner's role on each run (Task 10); no handler hook is added.
2. The spec revokes a replaced services key. Once the settings API refuses to write `OPENROUTER_API_KEY` (Task 9), nothing can replace it, so there is nothing to revoke and no code for it.
3. The spec re-enables a key on reactivation. No reactivation path exists (`Deprovisioner` deactivates, then purges), so only deactivation is handled (Task 11).
4. The spec splits `chat_boot.go` before wiring `IdentityLLM`. The wiring goes through a setter called from `serve.go` (Task 12), and the settings split of Task 5 already takes `chat_boot.go` well under the cap.
5. `aura identity create` needs no mint path of its own: it builds the same onboarding service (`cmd/aura/identity_create.go:177` → `cmd/aura/serve_onboarding.go:278`).

## File map

| Responsibility | Files |
|---|---|
| Provider wire: null limit | `internal/openrouterprovision/{wire,client,errors}.go` |
| Stored cap: NULL = no limit | migration `0125_*`, `internal/identitykey/{store,policy}.go`, `internal/db/queries/identity_llm_key.sql` |
| Credit and spend readers | `internal/agui/{credit_api,spend_overview_api}.go` |
| Live route and keyless classifier | `internal/llm/{keyless,runtime}.go`, `internal/runner/runner_identity_llm.go`, `cmd/aura/llm_client.go` |
| Encrypted secret settings | `internal/settings/{secrets,settings}.go`, `cmd/aura/chat_boot_settings.go` (new, split from `chat_boot.go`) |
| Secrets out of the environment | `cmd/aura/{boot_secret_settings,serve_channels,serve_onboarding,config,doctor}.go`, `cmd/aura-media-index/main.go`, `internal/skills/installer.go` |
| Management key at call time | `cmd/aura/serve_provisioning_openrouter.go` |
| Minting and reconciling | `internal/agui/{openrouter_keys,openrouter_reconcile}.go` |
| Admin-only settings | `internal/agui/{settings_api_authz,settings_api_validate}.go` |
| Deactivation disables the key | `internal/agui/deprovision.go`, `cmd/aura/serve_provisioning.go` |
| Serve-path turns bill the identity | `internal/runner/runner_llm_runtime.go`, `cmd/aura/serve.go` |

---

### Task 1: Measure `"limit": null` on the provider

**Files:** none in the repo. The result goes to aura-memory, or into the Task 2 commit body if the MCP is down.

- [x] **Step 1: Ask the operator for the management key** in one line (never search `.env` or the containers for it). Save it to the session scratchpad as `$SCRATCH/mkey`, outside the repo.

- [x] **Step 2: Mint a throwaway key with no limit**

```bash
curl -sS -X POST https://openrouter.ai/api/v1/keys \
  -H "Authorization: Bearer $(cat "$SCRATCH/mkey")" -H "Content-Type: application/json" \
  -d '{"name":"aura-limit-null-probe","limit":null}' -o "$SCRATCH/mint.json" -w "%{http_code}\n"
python -c "import json;d=json.load(open('$SCRATCH/mint.json'))['data'];print(d['hash'], d['limit'])"
```

Expected: `201`, then the hash followed by `None`.

- [x] **Step 3: Put a cap on, then take it off**

```bash
HASH=$(python -c "import json;print(json.load(open('$SCRATCH/mint.json'))['data']['hash'])")
for body in '{"limit":5}' '{"limit":null}'; do
  curl -sS -X PATCH "https://openrouter.ai/api/v1/keys/$HASH" \
    -H "Authorization: Bearer $(cat "$SCRATCH/mkey")" -H "Content-Type: application/json" -d "$body" \
    | python -c "import json,sys;print(json.load(sys.stdin)['data']['limit'])"
done
```

Expected: `5`, then `None`.

- [x] **Step 4: Delete the probe key**

```bash
curl -sS -X DELETE "https://openrouter.ai/api/v1/keys/$HASH" -H "Authorization: Bearer $(cat "$SCRATCH/mkey")" -w "%{http_code}\n"
rm -f "$SCRATCH/mint.json"
```

Keep `$SCRATCH/mkey` for Task 13, then delete it.

- [x] **Step 5: Record or stop.** Record the three measured answers (`memory_upsert_fact`, subject `OpenRouter Provisioning API`, predicate `accepts_null_limit`). If the provider refused `null` in either call, STOP: "no limit" needs a different representation. Bring the response body to the operator.

---

### Task 2: The provider wire can say "no limit"

**Files:**
- Modify: `internal/openrouterprovision/wire.go` (`MintRequest.Limit`, `mintRequestWire.Limit`, `KeyPatch`, `patchRequestWire`)
- Modify: `internal/openrouterprovision/client.go` (`PatchKey`, the `patchRequestWire(patch)` conversion)
- Modify: `internal/openrouterprovision/errors.go`
- Modify: `cmd/aura/serve_provisioning_openrouter.go` (the `MintKey` adapter's `Limit: 0`)
- Test: `internal/openrouterprovision/client_test.go`

**Interfaces:**
- Produces: `MintRequest.Limit *USDCap` (nil = no limit, sent as `"limit": null`); `KeyPatch.ClearLimit bool` (sends `"limit": null`); `ErrConflictingLimitPatch`.

- [x] **Step 1: Write the failing tests.** Append to `client_test.go`:

```go
func TestMintWithNoLimitSendsNull(t *testing.T) {
	var body []byte
	srv := mintServer(t, http.StatusCreated, mintResponsePayload, &body, nil)

	req := openrouterprovision.MintRequest{IdentityID: "id", Name: "id", LimitReset: openrouterprovision.LimitResetMonthly}
	if _, err := openrouterprovision.MintKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", req); err != nil {
		t.Fatalf("MintKey: %v", err)
	}
	if !bytes.Contains(body, []byte(`"limit":null`)) {
		t.Errorf("captured mint body = %s, want \"limit\":null for a key with no limit", body)
	}
}

func TestPatchClearLimitSendsNull(t *testing.T) {
	var body []byte
	srv := patchServer(t, http.StatusOK, patchResponsePayload, &body)

	patch := openrouterprovision.KeyPatch{ClearLimit: true}
	if _, err := openrouterprovision.PatchKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "h1", patch); err != nil {
		t.Fatalf("PatchKey: %v", err)
	}
	if string(body) != `{"limit":null}` {
		t.Errorf("captured patch body = %s, want {\"limit\":null}", body)
	}
}

func TestPatchRefusesSettingAndClearingTheLimit(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(patchResponsePayload))
	}))
	defer srv.Close()

	five := openrouterprovision.USDCap(500)
	patch := openrouterprovision.KeyPatch{Limit: &five, ClearLimit: true}
	_, err := openrouterprovision.PatchKey(context.Background(), srv.Client(), srv.URL, "sk-mgmt", "h1", patch)
	if !errors.Is(err, openrouterprovision.ErrConflictingLimitPatch) {
		t.Fatalf("err = %v, want ErrConflictingLimitPatch", err)
	}
	if requests != 0 {
		t.Errorf("requests = %d, want 0: a contradictory patch is refused before any request", requests)
	}
}
```

In the same file, every `MintRequest` literal that sets `Limit: 0` (lines 55, 70, 84, 102, 116, 133, 451, 462, 474) becomes `Limit: new(openrouterprovision.USDCap)`. A pointer to zero still marshals as `0.00`, so `TestMintAtZeroCap` keeps asserting a literal `"limit":0`.

- [x] **Step 2: Run the tests and watch them fail**

Run: `go test ./internal/openrouterprovision/`
Expected: build failure (`ClearLimit` undefined, `Limit` type mismatch).

- [x] **Step 3: Implement.** In `errors.go`:

```go
// ErrConflictingLimitPatch marks a KeyPatch that both sets and clears the limit. It is
// refused locally, before any request goes out.
var ErrConflictingLimitPatch = errors.New("openrouterprovision: a patch cannot both set and clear the limit")
```

In `wire.go`, replace `MintRequest`'s `Limit` field and its comment with:

```go
	// Limit is the spending cap. nil mints a key with no limit, sent as "limit": null (the
	// admin's own key); a pointer to zero is a member's default state (D-09).
	Limit *USDCap
```

Replace `mintRequestWire`'s `Limit` field and the comment block above it with:

```go
	// Limit deliberately carries NO omitempty. nil marshals as "limit": null, a key with no
	// limit, and a zero cap as "limit": 0.00. The old value field under omitempty dropped a
	// real zero and minted an uncapped key (TestMintAtZeroCap pins the bytes).
	Limit      *USDCap          `json:"limit"`
```

Add this field to `KeyPatch`, right after `Limit`:

```go
	// ClearLimit removes the cap, sent as "limit": null, so the key has no limit. It cannot
	// be combined with Limit.
	ClearLimit bool
```

Replace `patchRequestWire` and its doc comment with:

```go
// patchRequestWire is the PATCH /api/v1/keys/{hash} body: "every field optional; send only
// what changes" (02-OPENROUTER-API.md). Limit is raw JSON because a PATCH has three things to
// say about it: leave it alone (omitted), set it, or clear it with an explicit null, which a
// *USDCap under omitempty cannot send.
type patchRequestWire struct {
	Limit              json.RawMessage `json:"limit,omitempty"`
	LimitReset         *LimitReset     `json:"limit_reset,omitempty"`
	Disabled           *bool           `json:"disabled,omitempty"`
	Name               *string         `json:"name,omitempty"`
	IncludeBYOKInLimit *bool           `json:"include_byok_in_limit,omitempty"`
}

// wireLimit encodes the patch's limit for patchRequestWire.
func (p KeyPatch) wireLimit() (json.RawMessage, error) {
	switch {
	case p.ClearLimit && p.Limit != nil:
		return nil, ErrConflictingLimitPatch
	case p.ClearLimit:
		return json.RawMessage("null"), nil
	case p.Limit != nil:
		return json.Marshal(*p.Limit)
	default:
		return nil, nil
	}
}
```

Add `"encoding/json"` to `wire.go`'s imports. In `client.go`'s `PatchKey`, replace the three-line conversion comment and `wireReq := patchRequestWire(patch)` with:

```go
	limit, err := patch.wireLimit()
	if err != nil {
		return KeyRecord{}, err
	}
	wireReq := patchRequestWire{
		Limit: limit, LimitReset: patch.LimitReset, Disabled: patch.Disabled,
		Name: patch.Name, IncludeBYOKInLimit: patch.IncludeBYOKInLimit,
	}
```

In `cmd/aura/serve_provisioning_openrouter.go`, the mint adapter's `Limit: 0,` becomes `Limit: new(openrouterprovision.USDCap),`.

- [x] **Step 4: Run the tests and watch them pass**

Run: `go test ./internal/openrouterprovision/ && go build ./...`
Expected: PASS.

- [x] **Step 5: Race, lint, commit** (`feat(openrouterprovision): send an explicit null limit for keys with no cap`). Body: the admin's key and the services key mint with no limit, a PATCH must be able to clear a cap, and `patchRequestWire`'s omitempty could not send null; include the Task 1 measurement if aura-memory was down.

---

### Task 3: A NULL cap means no limit, in the store and in every reader

**Files:**
- Create: `internal/db/migrations/0125_identity_llm_key_no_limit.up.sql`, `.down.sql` (number per the Global Constraints)
- Modify: `internal/db/db_unit_test.go` (`TestMigrationHeadMatchesEmbeddedCatalog` pin)
- Modify: `internal/identitykey/store.go`, `internal/identitykey/policy.go`
- Modify: `internal/agui/credit_api.go`, `internal/agui/spend_overview_api.go`
- Modify: `cmd/aura/serve_provisioning_openrouter.go` (the adapter's `LimitUSD: 0`)
- Test: `internal/identitykey/policy_test.go`, `internal/identitykey/store_integration_test.go`, `internal/agui/credit_api_test.go`, `internal/agui/spend_overview_api_test.go`, `internal/runner/runner_identity_llm_test.go`, `cmd/aura/two_role_tracer_e2e_test.go`

**Interfaces:**
- Produces: `identitykey.Record.LimitUSD *float64` and `identitykey.Summary.LimitUSD *float64` (nil = no limit); `identitykey.DecisionInput.LimitUSD *float64`; the credit POST body field `clear_cap`; `unlimited` (bool) in the credit GET and POST responses, with `cap` null when unlimited; `over_allocation.uncapped_keys` in the spend overview.

**The trap to handle explicitly.** A `Record` literal that omits `LimitUSD` used to mean a zero cap; it now means no limit. In every test file listed, write `LimitUSD: capUSD(0)` wherever the test expects a refusal or a zero cap.

- [x] **Step 1: Check the migration number**

Run: `ls internal/db/migrations/ | tail -1`
Expected: `0124_widen_cost_usd_scale.up.sql`, so this migration is 0125. If it is higher, use the next free number and adjust the pin in Step 4.

- [x] **Step 2: Write the failing tests.** In `policy_test.go` add the helper and the test, and change the existing literals (lines 23, 34, 47, 73, 80) from `LimitUSD: 0`, `5`, `0.01` to `capUSD(0)`, `capUSD(5)`, `capUSD(0.01)`:

```go
func capUSD(v float64) *float64 { return &v }

func TestDecideAllowsAKeyWithNoLimit(t *testing.T) {
	decision, err := Decide(DecisionInput{IdentityID: "identity-a", HasKey: true, LimitUSD: nil, BackendBills: true})
	if err != nil || decision != DecisionAllow {
		t.Fatalf("no limit: decision = %v, err = %v, want DecisionAllow and nil", decision, err)
	}
}
```

In `store_integration_test.go`, the two `Save` literals `LimitUSD: 10` and `LimitUSD: 5` become `capUSD(10)` and `capUSD(5)`, and add:

```go
// TestIdentityLLMKeyNoLimitRoundTrips proves a key saved with no limit reads back with no
// limit, never as a zero cap that would refuse every turn.
func TestIdentityLLMKeyNoLimitRoundTrips(t *testing.T) {
	pool := migratedKeyPool(t)
	store := keyStore(t, pool)
	owner := seedKeyIdentity(t, pool)
	ctx := identityctx.WithIdentityID(context.Background(), owner)

	if err := store.Save(ctx, Record{
		Key: "sk-or-v1-admin-" + uuid.NewString(), Hash: "hash-admin", Label: "sk-or-v1-adm...in1", LimitReset: "monthly",
	}); err != nil {
		t.Fatalf("Save with no limit: %v", err)
	}
	got, err := store.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.LimitUSD != nil {
		t.Fatalf("LimitUSD = %v, want nil (no limit)", *got.LimitUSD)
	}
	list, err := store.List(ctx)
	if err != nil || len(list) != 1 || list[0].LimitUSD != nil {
		t.Fatalf("List = %+v (err %v), want one summary with no limit", list, err)
	}
}
```

Append these helpers to the end of `credit_api_test.go` (add `strconv` to its imports):

```go
func capUSD(v float64) *float64 { return &v }

func capString(p *float64) string {
	if p == nil {
		return "nil"
	}
	return strconv.FormatFloat(*p, 'f', -1, 64)
}
```

Change the `Record` literals (`grep -n LimitUSD internal/agui/credit_api_test.go`) at lines 127, 188, 222, 256, 279, 302, 323, 340, 367 and 411 to `LimitUSD: capUSD(<same number>)`. Rewrite the four cap assertions:

```go
	// line 236
	if got := keys.savedRec.LimitUSD; got == nil || *got != 5.00 || keys.savedRec.LimitReset != "weekly" {
		t.Errorf("saved cap %s reset %q, want 5.00 and weekly", capString(got), keys.savedRec.LimitReset)
	}
	// line 268
	if got := keys.savedRec.LimitUSD; got == nil || *got != 9.00 {
		t.Errorf("cap = %s, want 9.00", capString(got))
	}
	// line 288
	if got := keys.savedRec.LimitUSD; got == nil || *got != 3.00 {
		t.Errorf("cap changed to %s, want unchanged 3.00", capString(got))
	}
	// line 349
	if got := keys.savedRec.LimitUSD; got == nil || *got != 5.13 {
		t.Errorf("stored cap = %s, want 5.13", capString(got))
	}
```

Append the new credit tests:

```go
func TestAdminGetCreditForAKeyWithNoLimit(t *testing.T) {
	keys := &fakeCreditKeyStore{hasKey: true, rec: identitykey.Record{Hash: "h", LimitReset: "monthly"}}
	spend := &fakeCreditSpendReader{spend: map[string]float64{testLocalID: 2.5}}
	s := newTestCreditServer(spend, keys, &fakeCreditProvider{}, &fakeCreditInvalidator{}, true)

	rec := httptest.NewRecorder()
	s.handleGetCredit(rec, creditRequest(http.MethodGet, "/api/admin/identities/"+testLocalID+"/credit", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["unlimited"] != true || got["cap"] != nil || got["remaining"] != nil || got["percent_used"] != nil || got["spend"] != 2.5 {
		t.Fatalf("body = %v, want unlimited, a null cap, remaining and percent_used, and the real spend", got)
	}
}

func TestAdminSetCreditClearsTheCap(t *testing.T) {
	keys := &fakeCreditKeyStore{hasKey: true, rec: identitykey.Record{Key: "sk-x", Hash: "hash-1", Label: "l", LimitUSD: capUSD(5), LimitReset: "monthly"}}
	provider := &fakeCreditProvider{}
	s := newTestCreditServer(&fakeCreditSpendReader{}, keys, provider, &fakeCreditInvalidator{}, true)

	rec := httptest.NewRecorder()
	s.handleSetCredit(rec, creditRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/credit", `{"clear_cap":true}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !provider.lastPatch.ClearLimit || provider.lastPatch.Limit != nil {
		t.Fatalf("provider patch = %+v, want ClearLimit and no Limit", provider.lastPatch)
	}
	if keys.savedRec.LimitUSD != nil {
		t.Fatalf("stored cap = %s, want nil (no limit)", capString(keys.savedRec.LimitUSD))
	}
	if !strings.Contains(rec.Body.String(), `"unlimited":true`) {
		t.Fatalf("body = %s, want \"unlimited\":true", rec.Body.String())
	}
}

func TestAdminSetCreditRefusesCapAndClearCapTogether(t *testing.T) {
	keys := &fakeCreditKeyStore{hasKey: true, rec: identitykey.Record{Hash: "h", LimitUSD: capUSD(1), LimitReset: "monthly"}}
	provider := &fakeCreditProvider{}
	s := newTestCreditServer(&fakeCreditSpendReader{}, keys, provider, &fakeCreditInvalidator{}, true)

	rec := httptest.NewRecorder()
	s.handleSetCredit(rec, creditRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/credit", `{"cap":"5","clear_cap":true}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if keys.saveCalls != 0 || provider.calls != 0 {
		t.Fatalf("a contradictory request wrote something: saves=%d patches=%d", keys.saveCalls, provider.calls)
	}
}
```

In `spend_overview_api_test.go`, give `fakeSpendCapReader` an `uncapped map[string]bool` field, change line 341's literal to `capUSD(5)`, and replace its `Load` with:

```go
func (f *fakeSpendCapReader) Load(ctx context.Context) (identitykey.Record, error) {
	id := identityctx.IdentityID(ctx)
	if f.uncapped[id] {
		return identitykey.Record{}, nil
	}
	cap, ok := f.caps[id]
	if !ok {
		return identitykey.Record{}, identitykey.ErrNoKey
	}
	return identitykey.Record{LimitUSD: &cap}, nil
}
```

and add:

```go
func TestSpendOverviewCountsUncappedKeys(t *testing.T) {
	ids := []identity.Identity{{ID: "id-admin", Name: "admin"}, {ID: "id-member", Name: "member"}}
	recon := &fakeSpendReconciliation{tiles: spendFiveTiles(), credits: openrouterprovision.Credits{TotalCredits: 100, TotalUsage: 10}}
	caps := &fakeSpendCapReader{caps: map[string]float64{"id-member": 5}, uncapped: map[string]bool{"id-admin": true}}
	s := newTestSpendServer(recon, caps, ids)

	rec := httptest.NewRecorder()
	s.handleSpendOverview(rec, spendRequest())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out spendOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.OverAllocation.SumCaps != 5 || out.OverAllocation.UncappedKeys != 1 {
		t.Fatalf("over_allocation = %+v, want sum_caps 5 and uncapped_keys 1", out.OverAllocation)
	}
}
```

In `runner_identity_llm_test.go`, add `func capUSD(v float64) *float64 { return &v }` and turn every `LimitUSD: <n>` into `LimitUSD: capUSD(<n>)`. In `cmd/aura/two_role_tracer_e2e_test.go:249`, declare `memberCap := 15.0` before the literal and write `LimitUSD: &memberCap`.

- [x] **Step 3: Run the tests and watch them fail**

Run: `go test ./internal/identitykey/ ./internal/agui/ ./internal/runner/`
Expected: build failures on the `LimitUSD` type and the missing fields.

- [x] **Step 4: Implement.** `0125_identity_llm_key_no_limit.up.sql`:

```sql
-- A NULL limit_usd is a key with no spending limit: the admin's own key is minted that way
-- (docs/superpowers/specs/2026-09-10-management-key-onboarding-design.md). Zero stays the
-- member default (CRED-02).
ALTER TABLE aura.identity_llm_key ALTER COLUMN limit_usd DROP NOT NULL;

COMMENT ON COLUMN aura.identity_llm_key.limit_usd IS
    'Spending cap in USD. NULL is a key with no limit (the admin key); 0 refuses until an admin tops it up.';
```

`0125_identity_llm_key_no_limit.down.sql`:

```sql
-- Going back turns a key with no limit into a zero cap: Aura then refuses that identity's
-- turns until an admin sets a cap. Failing closed is the right side to fail on.
UPDATE aura.identity_llm_key SET limit_usd = 0 WHERE limit_usd IS NULL;
ALTER TABLE aura.identity_llm_key ALTER COLUMN limit_usd SET NOT NULL;
COMMENT ON COLUMN aura.identity_llm_key.limit_usd IS NULL;
```

In `db_unit_test.go`, change the pin to `if head != 125 {` and `"MigrationHead=%d, want embedded head 125"`, and add to the comment above it: "0125 lets identity_llm_key.limit_usd be NULL, a key with no limit (the admin's own)."

In `identitykey/store.go`, the `Record.LimitUSD` and `Summary.LimitUSD` fields become:

```go
	// LimitUSD is the spending cap; nil is a key with no limit (the admin's own).
	LimitUSD   *float64
```

In `Save`, `pgnumeric.NumericFromFloat(r.LimitUSD)` becomes `numericCap(r.LimitUSD)`. In `decodeRow` and `List`, `pgnumeric.FloatFromNumeric(...)` becomes `capFromNumeric(...)`. Add at the end of the file:

```go
// numericCap maps a cap onto the limit_usd column: nil is SQL NULL, a key with no limit.
func numericCap(limit *float64) (pgtype.Numeric, error) {
	if limit == nil {
		return pgtype.Numeric{}, nil
	}
	return pgnumeric.NumericFromFloat(*limit)
}

// capFromNumeric reads limit_usd back. pgnumeric.FloatFromNumeric reads NULL as 0, which
// would turn a key with no limit into one that refuses every turn, so NULL is checked first.
func capFromNumeric(n pgtype.Numeric) *float64 {
	if !n.Valid {
		return nil
	}
	f := pgnumeric.FloatFromNumeric(n)
	return &f
}
```

In `policy.go`, replace the `LimitUSD` field of `DecisionInput` and its comment with:

```go
	// LimitUSD is the stored key's cap. nil is a key with no limit and never refuses for
	// credit. A non-nil cap is compared directly — never narrowed or rounded through a
	// display formatter first: a cap of 0.004 is above zero and must stay above zero here,
	// or a real sub-cent balance rounds into a refusal.
	LimitUSD *float64
```

and in `Decide`, `if in.LimitUSD <= 0 {` becomes `if in.LimitUSD != nil && *in.LimitUSD <= 0 {`.

In `credit_api.go`'s `handleGetCredit`, replace everything from `cap, err := usdCapFromFloat(rec.LimitUSD)` to the end of the function with:

```go
	if rec.LimitUSD == nil {
		writeJSON(w, unlimitedCreditResponse(targetID, rec.LimitReset, spendFloat))
		return
	}
	cap, err := usdCapFromFloat(*rec.LimitUSD)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "credit figures unavailable"})
		return
	}
	writeJSON(w, creditGetResponse(targetID, rec.LimitReset, cap, spendFloat))
}

// unlimitedCreditResponse is the GET body for a key with no limit: no cap, so no remaining
// and no percentage either — a zero there would read as exhausted.
func unlimitedCreditResponse(identityID, limitReset string, spend float64) map[string]any {
	return map[string]any{
		"identity_id": identityID, "exempt": false, "unlimited": true, "cap": nil,
		"reset_interval": limitReset, "spend": spend, "remaining": nil, "percent_used": nil,
	}
}
```

Add `"unlimited": false,` to the map `creditGetResponse` returns. Add `ClearCap bool \`json:"clear_cap"\`` to `creditSetRequest`, with the comment line "ClearCap removes the cap, giving the key no limit; it cannot be combined with Cap." In `handleSetCredit`, replace the "at least one" check with:

```go
	if body.Cap == nil && body.ResetInterval == nil && !body.ClearCap {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "at least one of cap, clear_cap or reset_interval is required"})
		return
	}
	if body.ClearCap && body.Cap != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "cap and clear_cap cannot both be set"})
		return
	}
```

After the `body.Cap != nil` block, add `patch.ClearLimit = body.ClearCap`. Replace the `finalLimitUSD` computation with:

```go
	finalLimitUSD := rec.LimitUSD
	switch {
	case body.ClearCap:
		finalLimitUSD = nil
	case appliedCapPtr != nil:
		dollars := float64(*appliedCapPtr) / 100
		finalLimitUSD = &dollars
	}
```

and replace everything from `displayCap, err := usdCapFromFloat(finalLimitUSD)` to the end of the function with:

```go
	resp := map[string]any{
		"identity_id": targetID, "cap": nil, "unlimited": finalLimitUSD == nil,
		"reset_interval": finalLimitReset, "store_applied": true, "provider_applied": true,
	}
	if finalLimitUSD != nil {
		displayCap, err := usdCapFromFloat(*finalLimitUSD)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": "credit figures unavailable"})
			return
		}
		resp["cap"] = displayCap
	}
	writeJSON(w, resp)
}
```

In `spend_overview_api.go`, add `UncappedKeys int \`json:"uncapped_keys"\`` to `spendOverAllocationDTO`, change the handler to `sumCaps, uncapped, err := s.sumIdentityCaps(ctx, identities)` and `OverAllocation: buildOverAllocation(sumCaps, uncapped, credits)`, and replace `sumIdentityCaps` and `buildOverAllocation` with:

```go
// sumIdentityCaps sums every roster identity's own stored OpenRouter cap and counts the keys
// with no limit, which have no amount to add. Each read is scoped to that ONE identity
// (identityctx.WithIdentityID): aura.identity_llm_key's RLS floor (migration 0122) admits
// only the identity named by app.current_identity, so this is N scoped reads, not one
// unscoped enumeration. An identity with no key contributes nothing rather than failing the
// sum (identitykey.ErrNoKey is a normal answer: a local backend, or an identity provisioned
// before minting existed).
func (s *Server) sumIdentityCaps(ctx context.Context, identities []identity.Identity) (float64, int, error) {
	var sum float64
	var uncapped int
	for _, idn := range identities {
		rec, err := s.spendOverview.caps.Load(identityctx.WithIdentityID(ctx, idn.ID))
		if errors.Is(err, identitykey.ErrNoKey) {
			continue
		}
		if err != nil {
			return 0, 0, err
		}
		if rec.LimitUSD == nil {
			uncapped++
			continue
		}
		sum += *rec.LimitUSD
	}
	return sum, uncapped, nil
}

// buildOverAllocation decides the M-12 trigger by STRICT inequality (backstop decision,
// UI-SPEC "Over-allocation advisory banner · zero-one-many"): Σ(cap) EXCEEDING the available
// pool, not merely reaching it — at exact equality every cap can still be honored in full.
// Keys with no limit are reported beside the sum, because they draw on the same pool
// without an amount to add to it.
func buildOverAllocation(sumCaps float64, uncapped int, credits openrouterprovision.Credits) spendOverAllocationDTO {
	available := credits.TotalCredits - credits.TotalUsage
	return spendOverAllocationDTO{
		Triggered: sumCaps > available, SumCaps: sumCaps, Available: available, UncappedKeys: uncapped,
	}
}
```

In `cmd/aura/serve_provisioning_openrouter.go`, the adapter's `LimitUSD: 0,` becomes `LimitUSD: new(float64),`.

- [x] **Step 5: Confirm sqlc output is unchanged**

Run: `make sqlc && git diff --stat internal/db/sqlc`
Expected: no changes, because a nullable `numeric` is still `pgtype.Numeric`.

- [x] **Step 6: Run the tests and watch them pass**

Run: `go test ./internal/identitykey/ ./internal/agui/ ./internal/runner/ ./internal/db/ && go build ./...`, then with the stack up `go test -tags db_integration -race -run 'TestIdentityLLMKey' ./internal/identitykey/`
Expected: PASS.

- [x] **Step 7: Race, lint, commit** (`feat(identitykey): let a key's cap be NULL, meaning no limit`). In the body, name the rewritten credit assertions and say why: the cap is now a pointer.

---

### Task 4: One keyless classifier, and the resolver follows the live route

**Files:**
- Create: `internal/llm/keyless.go`, `internal/llm/keyless_test.go`, `internal/llm/runtime_version_test.go`
- Modify: `internal/llm/runtime.go`
- Modify: `internal/runner/runner_identity_llm.go` (drop its copy of the classifier; build on the live config; versioned cache)
- Modify: `cmd/aura/llm_client.go` (drop its copy of the classifier), `cmd/aura/serve_agui.go:251` (bill check per call)
- Modify: `internal/agui/credit_api.go` (`backendBills func() bool`)
- Test: `internal/runner/runner_identity_llm_test.go`, `internal/agui/credit_api_test.go`, `internal/agui/spend_overview_api_test.go:342`

**Interfaces:**
- Produces: `llm.IsKeylessLocalBaseURL(raw string) bool`; `llm.RuntimeSnapshot.Version uint64` (grows by one on every `Replace`); `(*agui.Server).SetCreditAPI(spend, keys, provider, invalidate, backendBills func() bool)`.

- [x] **Step 1: Write the failing tests.** `internal/llm/keyless_test.go`:

```go
package llm

import "testing"

func TestIsKeylessLocalBaseURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://openrouter.ai/api/v1":         false,
		"https://api.example.com/v1":           false,
		"http://localhost:8080/v1":             true,
		"http://127.0.0.1:9000":                true,
		"http://host.docker.internal:11434/v1": true,
		"http://aura-llm:8084/v1":              true,
		"http://192.168.1.20:11434/v1":         true,
		"http://nas.local:11434/v1":            true,
		"":                                     false,
		"://not-a-url":                         false,
	} {
		if got := IsKeylessLocalBaseURL(raw); got != want {
			t.Errorf("IsKeylessLocalBaseURL(%q) = %v, want %v", raw, got, want)
		}
	}
}
```

`internal/llm/runtime_version_test.go`:

```go
package llm

import "testing"

func TestRuntimeVersionAdvancesOnReplace(t *testing.T) {
	rt := NewRuntime(nil, Config{Model: "a"})
	first := rt.Snapshot().Version
	rt.Replace(nil, Config{Model: "b"})
	second := rt.Snapshot()
	if second.Version != first+1 || second.Config.Model != "b" {
		t.Fatalf("after Replace: version %d model %q, want %d and b", second.Version, second.Config.Model, first+1)
	}
}
```

Append to `runner_identity_llm_test.go`:

```go
func TestResolverFollowsTheRuntimeModel(t *testing.T) {
	t.Parallel()
	loader := newFakeKeyLoader(map[string]identitykey.Record{"identity-a": {Key: "key-for-a", LimitUSD: capUSD(5)}})
	cloud := llm.Config{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", Model: "model-one", APIKey: "services-key"}
	runtime := llm.NewRuntime(&fakeIdentityScopedClient{label: "services"}, cloud)
	rs := NewIdentityLLMResolver(loader, runtime, llm.Config{Provider: "openrouter", Model: "boot-model"}, fakeClientFactory(), nil)

	first, err := rs.SnapshotFor(context.Background(), "identity-a")
	if err != nil {
		t.Fatalf("SnapshotFor: %v", err)
	}
	if first.Config.Model != "model-one" || first.Config.APIKey != "key-for-a" {
		t.Fatalf("snapshot = model %q key %q, want the live model-one with the identity's key", first.Config.Model, first.Config.APIKey)
	}

	cloud.Model = "model-two"
	runtime.Replace(&fakeIdentityScopedClient{label: "services"}, cloud)
	second, err := rs.SnapshotFor(context.Background(), "identity-a")
	if err != nil {
		t.Fatalf("SnapshotFor after Replace: %v", err)
	}
	if second.Config.Model != "model-two" || second.Config.APIKey != "key-for-a" {
		t.Fatalf("after a model change: model %q key %q, want model-two with the identity's key", second.Config.Model, second.Config.APIKey)
	}
	if second.Client == first.Client {
		t.Fatal("a model change served the client cached for the old model")
	}
}

func TestResolverExemptsOnceTheRouteTurnsLocal(t *testing.T) {
	t.Parallel()
	loader := newFakeKeyLoader(nil)
	cloud := llm.Config{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", Model: "m"}
	runtime := llm.NewRuntime(&fakeIdentityScopedClient{label: "cloud"}, cloud)
	rs := NewIdentityLLMResolver(loader, runtime, cloud, fakeClientFactory(), nil)

	if _, err := rs.SnapshotFor(context.Background(), "identity-nokey"); !errors.Is(err, ErrNoIdentityLLMKey) {
		t.Fatalf("cloud route with no key: err = %v, want ErrNoIdentityLLMKey", err)
	}
	local := &fakeIdentityScopedClient{label: "local"}
	runtime.Replace(local, llm.Config{Provider: "ollama", BaseURL: "http://host.docker.internal:11434/v1", Model: "gemma"})
	snap, err := rs.SnapshotFor(context.Background(), "identity-nokey")
	if err != nil {
		t.Fatalf("local route: %v", err)
	}
	if snap.Client != local {
		t.Fatal("after the switch to a local route the resolver did not serve the process runtime's client")
	}
}

func TestResolverRefusalCarriesNoServicesKey(t *testing.T) {
	t.Parallel()
	loader := newFakeKeyLoader(map[string]identitykey.Record{"identity-broke": {Key: "key-broke", LimitUSD: capUSD(0)}})
	runtime := llm.NewRuntime(nil, llm.Config{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", Model: "m", APIKey: "services-key"})
	rs := NewIdentityLLMResolver(loader, runtime, llm.Config{}, fakeClientFactory(), fakeExhaustedClient{})

	snap, err := rs.SnapshotFor(context.Background(), "identity-broke")
	if err != nil {
		t.Fatalf("SnapshotFor: %v", err)
	}
	if snap.Config.APIKey != "" {
		t.Fatal("a refusal snapshot carries the services key (CRED-07)")
	}
}
```

- [x] **Step 2: Run the tests and watch them fail**

Run: `go test ./internal/llm/ ./internal/runner/`
Expected: build failure (`IsKeylessLocalBaseURL`, `Version` undefined); once those exist, `TestResolverFollowsTheRuntimeModel` fails on `model-one` against `boot-model`.

- [x] **Step 3: Implement.** `internal/llm/keyless.go` — the body is `cmd/aura/llm_client.go`'s `allowsKeylessLLMBaseURL`, which `internal/runner` could only copy because it cannot import `cmd/aura`:

```go
package llm

import (
	"net"
	"net/url"
	"strings"
)

// IsKeylessLocalBaseURL reports whether raw points at a backend that bills nothing and takes
// no credential: vLLM, llama.cpp or Ollama on this box or the LAN (D-13).
func IsKeylessLocalBaseURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "host.docker.internal" || strings.HasSuffix(host, ".local") {
		return true
	}
	switch host {
	case "ollama", "vllm", "llama", "llama-cpp", "aura-llm", "aura-vllm-chat", "aura-llama-chat", "aura-llama":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}
```

In `runtime.go`, add the field and the counter, and stamp the version in `Replace`:

```go
// RuntimeSnapshot is one immutable client/config pair used for a complete LLM run. Version
// grows by one on every Replace, so a cache built from a snapshot can tell it is stale.
type RuntimeSnapshot struct {
	Client  Client
	Config  Config
	Version uint64
}

// Runtime owns the primary LLM client and model profile selected by the operator.
type Runtime struct {
	current atomic.Pointer[RuntimeSnapshot]
	version atomic.Uint64
}
```

```go
	r.current.Store(&RuntimeSnapshot{Client: client, Config: cfg, Version: r.version.Add(1)})
```

Delete `allowsKeylessLLMBaseURL` from `cmd/aura/llm_client.go`; `newLLMClient` calls `llm.IsKeylessLocalBaseURL(cfg.BaseURL)`; drop the now-unused `net` and `net/url` imports. Delete `allowsKeylessLocalLLMBaseURL` and its doc comment from `runner_identity_llm.go`, with the same two imports.

In `runner_identity_llm.go`, add the cache entry type and change the `cache` field (and its `make` in `NewIdentityLLMResolver`) to `map[string]cachedSnapshot`:

```go
// cachedSnapshot is one identity's resolved snapshot and the runtime version it was built
// on; a newer runtime (the operator switched route or model) makes it stale.
type cachedSnapshot struct {
	snapshot llm.RuntimeSnapshot
	version  uint64
}
```

Rewrite the `runtime` and `base` field comments and the matching sentences of `NewIdentityLLMResolver`'s doc: `runtime` is the live route — every identity-scoped client is built on its config with only the API key swapped, and its client serves the D-13 exemption; it is never a fallback credential on the OpenRouter path. `base` is the route used when no runtime is wired (tests, headless callers). Replace `SnapshotFor`, `cachedSnapshot` and `cacheSnapshot` with:

```go
func (rs *IdentityLLMResolver) SnapshotFor(ctx context.Context, identityID string) (llm.RuntimeSnapshot, error) {
	if rs == nil || rs.loader == nil {
		return llm.RuntimeSnapshot{}, errors.New("runner: nil identity LLM resolver")
	}
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return llm.RuntimeSnapshot{}, errors.New("runner: empty identity id")
	}

	base, version := rs.liveBase()
	if cached, ok := rs.cachedSnapshot(identityID, version); ok {
		return cached, nil
	}

	scoped := identityctx.WithIdentityID(ctx, identityID)
	rec, loadErr := rs.loader.Load(scoped)
	hasKey := loadErr == nil
	if loadErr != nil && !errors.Is(loadErr, identitykey.ErrNoKey) {
		return llm.RuntimeSnapshot{}, fmt.Errorf("runner: load identity llm key for %s: %w", identityID, loadErr)
	}

	decision, decErr := identitykey.Decide(identitykey.DecisionInput{
		IdentityID:   identityID,
		HasKey:       hasKey,
		LimitUSD:     rec.LimitUSD,
		BackendBills: !llm.IsKeylessLocalBaseURL(base.BaseURL),
	})

	// A refusal keeps the route but never the services key the runtime holds (CRED-07).
	refusal := base
	refusal.APIKey = ""
	switch decision {
	case identitykey.DecisionExemptLocal:
		return rs.exemptionSnapshot(), nil
	case identitykey.DecisionAllow:
		cfg := base
		cfg.APIKey = rec.Key
		snapshot := llm.RuntimeSnapshot{Client: rs.newClient(cfg), Config: cfg}
		rs.cacheSnapshot(identityID, snapshot, version)
		return snapshot, nil
	case identitykey.DecisionRefuseNoCredit:
		if rs.exhaustedClient == nil {
			return llm.RuntimeSnapshot{}, fmt.Errorf("runner: %s: %w", identityID, decErr)
		}
		snapshot := llm.RuntimeSnapshot{Client: rs.exhaustedClient, Config: refusal}
		rs.cacheSnapshot(identityID, snapshot, version)
		return snapshot, nil
	case identitykey.DecisionRefuseNoKey:
		return llm.RuntimeSnapshot{}, fmt.Errorf("%w: %s", ErrNoIdentityLLMKey, identityID)
	default:
		// Deny by default (RBAC-09's discipline): an unrecognized Decision is a REFUSAL, never
		// treated as Allow. Decision is an int, not an enum, so the compiler would not catch a
		// fifth value added to identitykey.Decide without this switch.
		return llm.RuntimeSnapshot{}, fmt.Errorf("runner: %s: unrecognized credit decision %d", identityID, decision)
	}
}

// liveBase is the route every identity-scoped client is built on: the runtime the Settings
// API republishes when the operator switches route or model, and the boot config only when no
// runtime is wired.
func (rs *IdentityLLMResolver) liveBase() (llm.Config, uint64) {
	if rs.runtime == nil {
		return rs.base, 0
	}
	snap := rs.runtime.Snapshot()
	return snap.Config, snap.Version
}

func (rs *IdentityLLMResolver) cachedSnapshot(identityID string, version uint64) (llm.RuntimeSnapshot, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	entry, ok := rs.cache[identityID]
	if !ok || entry.version != version {
		return llm.RuntimeSnapshot{}, false
	}
	return entry.snapshot, true
}

func (rs *IdentityLLMResolver) cacheSnapshot(identityID string, snapshot llm.RuntimeSnapshot, version uint64) {
	rs.mu.Lock()
	rs.cache[identityID] = cachedSnapshot{snapshot: snapshot, version: version}
	rs.mu.Unlock()
}
```

Keep `SnapshotFor`'s existing doc comment, adding one sentence: a cached client is reused only while the runtime version it was built on is still current.

In `credit_api.go`, `creditPorts.backendBills` becomes `func() bool`, `SetCreditAPI`'s last parameter becomes `backendBills func() bool`, and both handlers test `!s.credit.backendBills()`. Add to the `SetCreditAPI` doc: "backendBills is asked on every request, because the operator can switch route while the daemon runs." In `credit_api_test.go`, `newTestCreditServer` keeps its `bool` parameter and passes `func() bool { return backendBills }`. In `spend_overview_api_test.go:342`, the last argument `true` becomes `func() bool { return true }`.

In `cmd/aura/serve_agui.go`, replace line 251 with

```go
	creditBackendBills := func() bool { return !llm.IsKeylessLocalBaseURL(chat.llmRuntime.Snapshot().Config.BaseURL) }
```

and change `} else if !creditBackendBills {` to `} else if !creditBackendBills() {` (add the `internal/llm` import if the file lacks it).

- [x] **Step 4: Run the tests and watch them pass**

Run: `go test ./internal/llm/ ./internal/runner/ ./internal/agui/ ./cmd/aura/ && go build ./...`
Expected: PASS.

- [x] **Step 5: Race, lint, commit** (`fix(runner): resolve identity clients on the live route, not the boot config`). Body: the resolver was built from the boot config and cached clients forever, so a route or model chosen in the wizard never reached identity turns; the two copies of the keyless classifier become one.

---

### Task 5: Secret settings are encrypted at rest

This task changes only the storage: `OverlayEnv` still copies the decrypted secrets into the environment, so every reader keeps working. Task 6 takes them out of the environment.

**Files:**
- Create: `internal/settings/secrets.go`, `internal/settings/secrets_test.go`
- Modify: `internal/settings/settings.go` (`Store`, `NewStore`, `List`, `Upsert`, `ReplaceMany`, plus new `Secret` and `EncryptPlaintextSecrets`)
- Modify: `internal/settings/store_db_test.go` (every `NewStore(pool)` call; two new integration tests)
- Create: `cmd/aura/chat_boot_settings.go`, moving out of `cmd/aura/chat_boot.go` `bootSettingsOps`, `resolveConfigAndPool`, `resolveConfigAndPoolWithSettings`, `overlayBootSettings` and `openSettingsOverlayPool`, unchanged except for the overlay closure below
- Modify: `cmd/aura/config.go` (`settingsListerForCLI`), `cmd/aura/serve_onboarding_botname.go` (`newBotUsernameResolver`) and its caller `cmd/aura/serve_onboarding.go:288`, `cmd/aura/serve_settings.go` (`wireSettingsProviders`), `cmd/aura-media-index/main.go` (`loadEffectiveConfig`)

**Interfaces:**
- Produces: `settings.NewStore(pool *pgxpool.Pool, authulaSecretHex string) (*settings.Store, error)`; `(*settings.Store).Secret(ctx context.Context, key string) (string, error)`; `(*settings.Store).EncryptPlaintextSecrets(ctx context.Context) (int, error)`; `settings.ErrSecretsUnavailable`; `newBotUsernameResolver(pool *pgxpool.Pool, authulaSecret string)`.

- [ ] **Step 1: Write the failing unit tests.** `internal/settings/secrets_test.go`:

```go
package settings

import (
	"errors"
	"strings"
	"testing"
)

const testAuthulaSecret = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

func TestSecretRoundTrip(t *testing.T) {
	aead, err := newSecretAEAD(testAuthulaSecret)
	if err != nil {
		t.Fatalf("newSecretAEAD: %v", err)
	}
	sealed, err := sealSecret(aead, "sk-or-v1-secret")
	if err != nil {
		t.Fatalf("sealSecret: %v", err)
	}
	if !strings.HasPrefix(sealed, secretPrefix) || strings.Contains(sealed, "sk-or-v1-secret") {
		t.Fatalf("sealed = %q, want an enc:v1: value that does not contain the plaintext", sealed)
	}
	if again, _ := sealSecret(aead, "sk-or-v1-secret"); again == sealed {
		t.Fatal("two seals of the same value are identical: the nonce is not random")
	}
	if plain, err := openSecret(aead, sealed); err != nil || plain != "sk-or-v1-secret" {
		t.Fatalf("openSecret = %q, %v; want the plaintext back", plain, err)
	}
}

func TestOpenSecretPassesPlaintextRowsThrough(t *testing.T) {
	aead, _ := newSecretAEAD(testAuthulaSecret)
	if plain, err := openSecret(aead, "legacy-plaintext"); err != nil || plain != "legacy-plaintext" {
		t.Fatalf("openSecret(legacy) = %q, %v; want the row unchanged", plain, err)
	}
}

func TestSecretsNeedTheAuthulaSecret(t *testing.T) {
	if _, err := sealSecret(nil, "value"); !errors.Is(err, ErrSecretsUnavailable) {
		t.Fatalf("sealSecret without a key: err = %v, want ErrSecretsUnavailable", err)
	}
	if _, err := openSecret(nil, secretPrefix+"00:00"); !errors.Is(err, ErrSecretsUnavailable) {
		t.Fatalf("openSecret without a key: err = %v, want ErrSecretsUnavailable", err)
	}
	if sealed, err := sealSecret(nil, ""); err != nil || sealed != "" {
		t.Fatalf("an empty value is not a secret: sealSecret = %q, %v", sealed, err)
	}
}

func TestSettingsKeyIsDomainSeparated(t *testing.T) {
	aead, _ := newSecretAEAD(testAuthulaSecret)
	sealed, _ := sealSecret(aead, "value")
	other, err := aeadWithInfo(testAuthulaSecret, "aura-identity-llm-key-v1")
	if err != nil {
		t.Fatalf("aeadWithInfo: %v", err)
	}
	if _, err := openSecret(other, sealed); err == nil {
		t.Fatal("a key derived under identitykey's info string opened a settings secret")
	}
}

func TestNewSecretAEADInputs(t *testing.T) {
	if _, err := newSecretAEAD("not-hex"); err == nil {
		t.Fatal("a malformed AURA_AUTHULA_SECRET was accepted")
	}
	if aead, err := newSecretAEAD(""); err != nil || aead != nil {
		t.Fatalf("an empty secret = (%v, %v), want (nil, nil): secrets unavailable, not an error", aead, err)
	}
}

func TestOpenSecretRejectsDamagedValues(t *testing.T) {
	aead, _ := newSecretAEAD(testAuthulaSecret)
	sealed, _ := sealSecret(aead, "value")
	flipped := byte('0')
	if sealed[len(sealed)-1] == '0' {
		flipped = '1'
	}
	for _, bad := range []string{
		secretPrefix + "zz:00",
		secretPrefix + "no-separator",
		sealed[:len(sealed)-1] + string(flipped),
	} {
		if _, err := openSecret(aead, bad); err == nil {
			t.Errorf("openSecret(%q) succeeded on a damaged value", bad)
		}
	}
}
```

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./internal/settings/`
Expected: build failure (`newSecretAEAD` undefined).

- [ ] **Step 3: Implement the cipher.** `internal/settings/secrets.go`:

```go
package settings

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// secretDerivationInfo domain-separates the settings wrapping key from every other key
// derived from AURA_AUTHULA_SECRET; identitykey uses "aura-identity-llm-key-v1".
const secretDerivationInfo = "aura-settings-secret-v1"

// secretPrefix marks a value this package encrypted. A secret row without it is a plaintext
// row from before encryption, which EncryptPlaintextSecrets converts at boot.
const secretPrefix = "enc:v1:"

// ErrSecretsUnavailable reports a secret row written or read through a store built without
// AURA_AUTHULA_SECRET. Nothing is stored in the clear in its place.
var ErrSecretsUnavailable = errors.New("settings: secret rows need AURA_AUTHULA_SECRET")

var errMalformedSecret = errors.New("settings: malformed secret value")

// newSecretAEAD derives the settings wrapping key. An empty secret yields no cipher (secret
// rows unavailable); a malformed one is an error, so a mis-provisioned deployment never
// stores a credential in the clear.
func newSecretAEAD(authulaSecretHex string) (cipher.AEAD, error) {
	if strings.TrimSpace(authulaSecretHex) == "" {
		return nil, nil
	}
	return aeadWithInfo(authulaSecretHex, secretDerivationInfo)
}

// aeadWithInfo takes the info string as a parameter so a test can derive a key under another
// store's info and assert the two cannot open each other's values.
func aeadWithInfo(authulaSecretHex, info string) (cipher.AEAD, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(authulaSecretHex))
	if err != nil || len(raw) != 32 {
		return nil, errors.New("settings: AURA_AUTHULA_SECRET must be 64 hex characters (32 bytes)")
	}
	key, err := hkdf.Key(sha256.New, raw, nil, info, 32)
	if err != nil {
		return nil, fmt.Errorf("settings: derive key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("settings: cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

// sealSecret encrypts one value as enc:v1:<nonce hex>:<ciphertext hex>. An empty value is
// stored empty: clearing a setting is not a secret.
func sealSecret(aead cipher.AEAD, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if aead == nil {
		return "", ErrSecretsUnavailable
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("settings: nonce: %w", err)
	}
	sealed := aead.Seal(nil, nonce, []byte(plaintext), nil)
	return secretPrefix + hex.EncodeToString(nonce) + ":" + hex.EncodeToString(sealed), nil
}

// openSecret decrypts a value sealSecret wrote and passes a legacy plaintext value through.
func openSecret(aead cipher.AEAD, stored string) (string, error) {
	rest, encrypted := strings.CutPrefix(stored, secretPrefix)
	if !encrypted {
		return stored, nil
	}
	if aead == nil {
		return "", ErrSecretsUnavailable
	}
	nonceHex, sealedHex, ok := strings.Cut(rest, ":")
	if !ok {
		return "", errMalformedSecret
	}
	nonce, err := hex.DecodeString(nonceHex)
	if err != nil || len(nonce) != aead.NonceSize() {
		return "", errMalformedSecret
	}
	sealed, err := hex.DecodeString(sealedHex)
	if err != nil {
		return "", errMalformedSecret
	}
	plain, err := aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("settings: decrypt: %w", err)
	}
	return string(plain), nil
}
```

- [ ] **Step 4: Implement the store.** In `settings.go`, add `"crypto/cipher"`, `"log/slog"` and `"strings"` to the imports and replace `Store`, `NewStore` and `List` with:

```go
// Store is the aura.settings CRUD over a pgx pool. Secret rows are encrypted on the way in
// and decrypted on the way out, so every caller sees plaintext and the table never holds it.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
	aead cipher.AEAD // nil when built without AURA_AUTHULA_SECRET: secret rows unavailable
}

// NewStore builds a settings store over the pool. authulaSecretHex keys the secret rows (see
// secrets.go): empty leaves them unavailable, malformed fails.
func NewStore(pool *pgxpool.Pool, authulaSecretHex string) (*Store, error) {
	aead, err := newSecretAEAD(authulaSecretHex)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool, q: sqlc.New(pool), aead: aead}, nil
}

// List returns all settings rows ordered by key, secret values decrypted. A secret this store
// cannot open reads as empty and is logged, rather than failing the whole list: the Settings
// page and the overlay still work, and Secret reports the error to the reader who needs it.
func (s *Store) List(ctx context.Context) ([]sqlc.AuraSettings, error) {
	rows, err := s.q.ListSettings(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if !AllowedKeys[rows[i].Key].Secret {
			continue
		}
		plain, err := openSecret(s.aead, rows[i].Value)
		if err != nil {
			slog.Warn("settings: secret row unreadable", "key", rows[i].Key, "err", err)
			plain = ""
		}
		rows[i].Value = plain
	}
	return rows, nil
}

// Secret returns the decrypted value of one secret row, "" when the row is absent.
func (s *Store) Secret(ctx context.Context, key string) (string, error) {
	rows, err := s.q.ListSettings(ctx)
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		if row.Key == key {
			return openSecret(s.aead, row.Value)
		}
	}
	return "", nil
}

// storedValue encrypts a secret key's value; every other key is stored as given.
func (s *Store) storedValue(key, value string) (string, error) {
	if !AllowedKeys[key].Secret {
		return value, nil
	}
	return sealSecret(s.aead, value)
}

// EncryptPlaintextSecrets rewrites every secret row still stored in the clear, so an install
// upgraded from before encryption converges at boot. It returns how many rows it rewrote.
func (s *Store) EncryptPlaintextSecrets(ctx context.Context) (int, error) {
	if s.aead == nil {
		return 0, nil
	}
	rewritten := 0
	err := s.withWriteLock(ctx, func(q *sqlc.Queries) error {
		rows, err := q.ListSettings(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if !AllowedKeys[row.Key].Secret || row.Value == "" || strings.HasPrefix(row.Value, secretPrefix) {
				continue
			}
			sealed, err := sealSecret(s.aead, row.Value)
			if err != nil {
				return err
			}
			if _, err := q.UpsertSetting(ctx, sqlc.UpsertSettingParams{
				Key: row.Key, Value: sealed, IsSecret: true, UpdatedBy: row.UpdatedBy,
			}); err != nil {
				return err
			}
			rewritten++
		}
		return nil
	})
	return rewritten, err
}
```

In `Upsert`, before the write lock:

```go
	stored, err := s.storedValue(key, value)
	if err != nil {
		return sqlc.AuraSettings{}, err
	}
```

pass `Value: stored` to `UpsertSetting`, and after the transaction set `row.Value = value` so the caller gets back the plaintext it wrote. In `ReplaceMany`'s loop, do the same per key: `stored, err := s.storedValue(key, values[key])`, return the error, `Value: stored`, then `row.Value = values[key]` before the append.

Update the package doc's second sentence: "Secret rows are AES-GCM ciphertext (secrets.go); the Store decrypts them for its callers."

- [ ] **Step 5: Update every constructor call.**

`cmd/aura/chat_boot_settings.go` (new; the moved functions keep their doc comments) replaces the overlay closure in `resolveConfigAndPool` with `overlay: overlayStoreSettings,` and adds:

```go
// overlayStoreSettings converts any secret row still stored in the clear, then overlays the
// allowlisted rows onto the environment for config.Load to read.
func overlayStoreSettings(ctx context.Context, pool *pgxpool.Pool) error {
	store, err := settings.NewStore(pool, os.Getenv("AURA_AUTHULA_SECRET"))
	if err != nil {
		return err
	}
	if _, err := store.EncryptPlaintextSecrets(ctx); err != nil {
		return err
	}
	return settings.OverlayEnv(ctx, store)
}
```

`cmd/aura/config.go`, the last line of `settingsListerForCLI`:

```go
	store, err := settings.NewStore(pool, os.Getenv("AURA_AUTHULA_SECRET"))
	if err != nil {
		pool.Close()
		return nil, nil, "aura.settings unreadable: " + err.Error()
	}
	return store, pool.Close, ""
```

`cmd/aura/serve_onboarding_botname.go`: the signature becomes `func newBotUsernameResolver(pool *pgxpool.Pool, authulaSecret string) func(context.Context) string` (its doc names the new parameter), and the store block becomes

```go
	var store *settings.Store
	if pool != nil {
		built, err := settings.NewStore(pool, authulaSecret)
		if err != nil {
			slog.Warn("onboarding: settings store unavailable; the bot name comes from the environment token only", "err", err)
		} else {
			store = built
		}
	}
```

The caller at `serve_onboarding.go:288` passes `chat.cfg.AuthulaSecret`. Update the tests that call `newBotUsernameResolver(` (`grep -rn "newBotUsernameResolver(" cmd/aura`), passing `""`.

`cmd/aura/serve_settings.go`:

```go
func wireSettingsProviders(server *agui.Server, chat *chatEnv) {
	store, err := settings.NewStore(chat.pool, chat.cfg.AuthulaSecret)
	if err != nil {
		slog.Error("settings store unavailable; the Settings routes answer 503", "err", err)
	} else {
		server.SetSettingsStore(store)
		// Same store, second seam: aura.llm_provider_routes is a different table with a
		// different meaning (the memory of each provider's route, not the active one).
		server.SetLLMRouteStore(store)
	}
	server.SetTelegramBotProbe(telegramGetMeProbe)
	server.SetLLMRuntime(chat.llmRuntime)
	server.SetLLMRouteReloader(&primaryLLMRouteReloader{fallback: chat.llmFallback, runtime: chat.llmRuntime, server: server})
}
```

`cmd/aura-media-index/main.go`, inside `loadEffectiveConfig`:

```go
		store, err := settings.NewStore(pool, os.Getenv("AURA_AUTHULA_SECRET"))
		if err != nil {
			return nil, fmt.Errorf("settings store: %w", err)
		}
		if err = settings.OverlayEnv(ctx, store); err != nil {
			return nil, fmt.Errorf("settings overlay: %w", err)
		}
```

- [ ] **Step 6: Write the integration tests.** In `store_db_test.go`, the two `NewStore(migratedPool(t))` calls (lines 99 and 164) become `mustStore(t, migratedPool(t))`, and add:

```go
func mustStore(t *testing.T, pool *pgxpool.Pool) *Store {
	t.Helper()
	store, err := NewStore(pool, envOrSkip(t, "AURA_AUTHULA_SECRET"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

// TestSecretRowsAreStoredEncrypted proves the table holds ciphertext while every reader of
// the store gets the plaintext back.
func TestSecretRowsAreStoredEncrypted(t *testing.T) {
	pool := migratedPool(t)
	store := mustStore(t, pool)
	ctx := context.Background()
	t.Cleanup(func() { _ = store.Delete(context.Background(), "TELEGRAM_BOT_TOKEN") })

	if _, err := store.Upsert(ctx, "TELEGRAM_BOT_TOKEN", "123:plain-token", "test"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	var stored string
	if err := pool.QueryRow(ctx, `SELECT value FROM aura.settings WHERE key = 'TELEGRAM_BOT_TOKEN'`).Scan(&stored); err != nil {
		t.Fatalf("read the raw row: %v", err)
	}
	if !strings.HasPrefix(stored, "enc:v1:") || strings.Contains(stored, "plain-token") {
		t.Fatalf("stored value = %q, want ciphertext", stored)
	}
	if got, err := store.Secret(ctx, "TELEGRAM_BOT_TOKEN"); err != nil || got != "123:plain-token" {
		t.Fatalf("Secret = %q, %v; want the plaintext", got, err)
	}
}

// TestBootEncryptsPlaintextSecretRows proves an install from before encryption converges, and
// that a second pass finds nothing left to do.
func TestBootEncryptsPlaintextSecretRows(t *testing.T) {
	pool := migratedPool(t)
	store := mustStore(t, pool)
	ctx := context.Background()
	t.Cleanup(func() { _ = store.Delete(context.Background(), "TELEGRAM_BOT_TOKEN") })

	if _, err := pool.Exec(ctx, `INSERT INTO aura.settings (key, value, is_secret) VALUES ('TELEGRAM_BOT_TOKEN', '123:legacy', true)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`); err != nil {
		t.Fatalf("seed a plaintext row: %v", err)
	}
	if n, err := store.EncryptPlaintextSecrets(ctx); err != nil || n != 1 {
		t.Fatalf("EncryptPlaintextSecrets = %d, %v; want 1 row", n, err)
	}
	var stored string
	if err := pool.QueryRow(ctx, `SELECT value FROM aura.settings WHERE key = 'TELEGRAM_BOT_TOKEN'`).Scan(&stored); err != nil {
		t.Fatalf("read the raw row: %v", err)
	}
	if !strings.HasPrefix(stored, "enc:v1:") {
		t.Fatalf("stored value = %q, want ciphertext after the boot pass", stored)
	}
	if again, err := store.EncryptPlaintextSecrets(ctx); err != nil || again != 0 {
		t.Fatalf("second pass = %d, %v; want 0", again, err)
	}
}
```


- [ ] **Step 7: Run the tests and watch them pass**

Run: `go test ./internal/settings/ ./cmd/aura/ ./cmd/aura-media-index/ && go build ./... && wc -l cmd/aura/chat_boot.go cmd/aura/chat_boot_settings.go`, then with the stack up `go test -tags db_integration -race ./internal/settings/`
Expected: PASS; `chat_boot.go` under 600 lines.

- [ ] **Step 8: Race, lint, commit** (`feat(settings): encrypt secret rows at rest behind an enc:v1: prefix`). Body: `aura.settings` held the OpenRouter keys and the Telegram token in plaintext; the key is derived from `AURA_AUTHULA_SECRET` under its own info string, so no new secret is needed; `chat_boot.go` loses its settings functions to stay under the cap.

---

### Task 6: Secret settings stay out of the process environment

**Files:**
- Modify: `internal/settings/settings.go` (`OverlayEnv` skips secret rows; package doc)
- Modify: `internal/llm/config.go` (`requiresAPIKey` becomes the exported `RequiresAPIKey`)
- Modify: `cmd/aura/chat_boot_settings.go` (a `secrets` op and `applySecretSettings`), `cmd/aura/chat_boot.go` (`bootChatEnv`, `bootServeChatEnv`, `bootChatEnvWithConfig`)
- Create: `cmd/aura/boot_secret_settings.go` (`settingsSecret`, `effectiveTelegramToken`)
- Modify: `cmd/aura/serve_channels.go:65`, `cmd/aura/serve_onboarding.go:285`, `cmd/aura/config.go` (`effectiveLLMKeyForCLI`, `loadLLMConfigAndOverlayNote`), `cmd/aura/doctor.go:35`, `cmd/aura-media-index/main.go` (`loadEffectiveConfig`), `internal/skills/installer.go` (`execCommandEnv`)
- Test: `internal/settings/settings_test.go`, `cmd/aura/chat_boot_settings_test.go` (new), `internal/skills/installer_env_test.go` (new)

**Interfaces:**
- Consumes: `(*settings.Store).Secret` (Task 5).
- Produces: `llm.RequiresAPIKey(provider string) bool`; `bootChatEnvWithConfig(ctx, loadConfig, requireLLMKey bool)`; `applySecretSettings(ctx, secretReader, *config.Config) error`; `effectiveTelegramToken(ctx, *chatEnv) string`; `effectiveLLMKeyForCLI(ctx) string`.

Every consumer of the services key reads `cfg.LLM.APIKey`: cloud embeddings (`config_routes.go:27`), web TTS/STT (`serve_voice.go:70,90`), Telegram TTS (`serve_channels.go:143`). Setting that one field at boot covers them all.

- [ ] **Step 1: Write the failing tests.** In `settings_test.go`, replace `TestOverlayEnvAppliesTelegramBotToken` with:

```go
// TestOverlayEnvSkipsSecretRows proves no credential reaches the process environment, where
// every child process would inherit it: secret rows are read through Store.Secret instead.
func TestOverlayEnvSkipsSecretRows(t *testing.T) {
	for _, key := range []string{"TELEGRAM_BOT_TOKEN", "OPENROUTER_API_KEY", "AURA_OPENROUTER_MANAGEMENT_KEY", "AURA_TTS_MODEL"} {
		t.Setenv(key, "")
	}
	l := fakeLister{rows: []sqlc.AuraSettings{
		{Key: "TELEGRAM_BOT_TOKEN", Value: "123456:settings-telegram-token"},
		{Key: "OPENROUTER_API_KEY", Value: "sk-settings-services"},
		{Key: "AURA_OPENROUTER_MANAGEMENT_KEY", Value: "sk-settings-management"},
		{Key: "AURA_TTS_MODEL", Value: "tts-from-settings"},
	}}
	if err := OverlayEnv(t.Context(), l); err != nil {
		t.Fatalf("OverlayEnv: %v", err)
	}
	for _, key := range []string{"TELEGRAM_BOT_TOKEN", "OPENROUTER_API_KEY", "AURA_OPENROUTER_MANAGEMENT_KEY"} {
		if got := os.Getenv(key); got != "" {
			t.Errorf("%s = %q, want unset: a secret row must not enter the environment", key, got)
		}
	}
	if got := os.Getenv("AURA_TTS_MODEL"); got != "tts-from-settings" {
		t.Errorf("AURA_TTS_MODEL = %q, want the overlaid value", got)
	}
}
```

In `TestOverlayEnvFeedsRuntimeConfig`, right after `clearRuntimeConfigEnvForOverlayTest(t)`, add `t.Setenv("OPENROUTER_API_KEY", "sk-from-environment")` and `t.Setenv("AURA_OPENROUTER_MANAGEMENT_KEY", "")`. Keep both secret rows in the fixture, and replace the `LLM.APIKey`, `OpenRouterManagementKey` and `EmbedRoute` assertions with:

```go
	if got := cfg.LLM.APIKey; got != "sk-from-environment" {
		t.Errorf("LLM.APIKey = %q, want the environment's key: the settings secret must not be overlaid", got)
	}
	if got := cfg.OpenRouterManagementKey; got != "" {
		t.Errorf("OpenRouterManagementKey = %q, want empty: the settings secret must not be overlaid", got)
	}
```

```go
	embedBase, embedKey, embedModel := cfg.EmbedRoute()
	if embedBase != "https://settings-embed.example" || embedKey != "sk-from-environment" || embedModel != "settings/embed-model" {
		t.Errorf("EmbedRoute() = (%q, %q, %q), want overlaid base and model with the environment's key", embedBase, embedKey, embedModel)
	}
```

`cmd/aura/chat_boot_settings_test.go`:

```go
package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

type fakeSecretReader map[string]string

func (f fakeSecretReader) Secret(_ context.Context, key string) (string, error) { return f[key], nil }

type failingSecretReader struct{}

func (failingSecretReader) Secret(context.Context, string) (string, error) {
	return "", errors.New("decrypt failed")
}

func TestApplySecretSettingsLetsTheStoredKeysWin(t *testing.T) {
	cfg := &config.Config{OpenRouterManagementKey: "from-env"}
	cfg.LLM.APIKey = "from-env"
	secrets := fakeSecretReader{"OPENROUTER_API_KEY": "sk-services", "AURA_OPENROUTER_MANAGEMENT_KEY": "sk-management"}
	if err := applySecretSettings(context.Background(), secrets, cfg); err != nil {
		t.Fatalf("applySecretSettings: %v", err)
	}
	if cfg.LLM.APIKey != "sk-services" || cfg.OpenRouterManagementKey != "sk-management" {
		t.Fatal("the stored keys did not replace the environment's")
	}
}

func TestApplySecretSettingsKeepsTheEnvironmentWhenNothingIsStored(t *testing.T) {
	cfg := &config.Config{OpenRouterManagementKey: "from-env"}
	cfg.LLM.APIKey = "from-env"
	if err := applySecretSettings(context.Background(), fakeSecretReader{}, cfg); err != nil {
		t.Fatalf("applySecretSettings: %v", err)
	}
	if cfg.LLM.APIKey != "from-env" || cfg.OpenRouterManagementKey != "from-env" {
		t.Fatal("an empty store erased the environment's keys")
	}
}

func TestApplySecretSettingsReportsAnUnreadableSecret(t *testing.T) {
	if err := applySecretSettings(context.Background(), failingSecretReader{}, &config.Config{}); err == nil {
		t.Fatal("an unreadable secret passed silently")
	}
}
```

`internal/skills/installer_env_test.go`:

```go
package skills

import (
	"slices"
	"strings"
	"testing"
)

func TestExecCommandEnvCarriesNoCredential(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-must-not-leak")
	t.Setenv("AURA_OPENROUTER_MANAGEMENT_KEY", "sk-or-v1-management-must-not-leak")
	env := execCommandEnv()
	for _, kv := range env {
		if strings.Contains(kv, "must-not-leak") {
			t.Fatalf("npx environment carries a credential under %s", strings.SplitN(kv, "=", 2)[0])
		}
	}
	if !slices.Contains(env, "GIT_TERMINAL_PROMPT=0") {
		t.Fatal("npx environment lost GIT_TERMINAL_PROMPT=0")
	}
	if !slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "PATH=") }) {
		t.Fatal("npx environment lost PATH, so npx itself cannot be found")
	}
}
```

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./internal/settings/ ./cmd/aura/ ./internal/skills/`
Expected: `TestOverlayEnvSkipsSecretRows` fails (the secrets are overlaid); `applySecretSettings` is undefined; the npx environment leaks the key.

- [ ] **Step 3: Implement.** In `settings.go`, replace `OverlayEnv` and its doc with:

```go
// OverlayEnv applies the allowlisted, non-secret aura.settings rows onto the process
// environment so a subsequent config.Load reads them. Call at daemon boot BEFORE config.Load,
// after the pool is open. Secret rows are skipped: a credential in the environment reaches
// every child process the daemon starts, so their readers call Store.Secret instead.
func OverlayEnv(ctx context.Context, l Lister) error {
	rows, err := l.List(ctx)
	if err != nil {
		return err
	}
	for _, r := range rows {
		meta, ok := AllowedKeys[r.Key]
		if !ok || meta.Secret {
			continue
		}
		_ = os.Setenv(r.Key, r.Value)
	}
	return nil
}
```

and in the package doc, "OverlayEnv applies them onto the process environment" becomes "OverlayEnv applies the non-secret rows onto the process environment; secret rows never reach it".

In `internal/llm/config.go`, rename `requiresAPIKey` to `RequiresAPIKey` (doc: "RequiresAPIKey answers whether an empty APIKey should stop startup…"), and update its caller in `load` and any other caller (`grep -rn requiresAPIKey internal/llm`).

In `chat_boot_settings.go`, add `secrets func(context.Context, *pgxpool.Pool, *config.Config) error` to `bootSettingsOps`, with the comment "secrets hands the loaded config the credentials aura.settings holds; nil in tests that do not exercise it". Set `secrets: applyStoreSecrets,` in `resolveConfigAndPool`. In `resolveConfigAndPoolWithSettings`, right after each successful post-overlay `cfg, err = loadConfig()`, call `applyBootSecrets(ctx, pool, cfg, settingsOps)`. Add:

```go
func applyBootSecrets(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, settingsOps bootSettingsOps) {
	if settingsOps.secrets == nil {
		return
	}
	if err := settingsOps.secrets(ctx, pool, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "warn: settings secrets:", err)
	}
}

// secretReader is settings.Store.Secret, narrowed so applySecretSettings is testable.
type secretReader interface {
	Secret(ctx context.Context, key string) (string, error)
}

func applyStoreSecrets(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) error {
	store, err := settings.NewStore(pool, cfg.AuthulaSecret)
	if err != nil {
		return err
	}
	return applySecretSettings(ctx, store, cfg)
}

// applySecretSettings puts the credentials aura.settings holds into the loaded config. They
// never pass through the environment (settings.OverlayEnv skips them), so this is how the
// daemon's LLM client, and every backend that reuses its key, and the management key see them.
// A stored row wins over the environment.
func applySecretSettings(ctx context.Context, secrets secretReader, cfg *config.Config) error {
	llmKey, err := secrets.Secret(ctx, "OPENROUTER_API_KEY")
	if err != nil {
		return err
	}
	if llmKey != "" {
		cfg.LLM.APIKey = llmKey
	}
	managementKey, err := secrets.Secret(ctx, "AURA_OPENROUTER_MANAGEMENT_KEY")
	if err != nil {
		return err
	}
	if managementKey != "" {
		cfg.OpenRouterManagementKey = managementKey
	}
	return nil
}
```

In `chat_boot.go`, both boots load with `config.LoadServe`, and the CLI's fail-fast moves after the settings are applied:

```go
func bootChatEnv(ctx context.Context) (*chatEnv, error) {
	return bootChatEnvWithConfig(ctx, config.LoadServe, true)
}

func bootServeChatEnv(ctx context.Context) (*chatEnv, error) {
	return bootChatEnvWithConfig(deferOAuthMountsUntilListener(ctx), config.LoadServe, false)
}

func bootChatEnvWithConfig(ctx context.Context, loadConfig func() (*config.Config, error), requireLLMKey bool) (*chatEnv, error) {
	// Capture the pre-settings route once. DELETE on a hot route must restore this
	// deployment fallback, not the DB value OverlayEnv copies into process env below.
	baseline, baselineErr := loadConfig()
	cfg, pool, err := resolveConfigAndPool(ctx, loadConfig, db.Open)
	if err != nil {
		return nil, err
	}
	// The key may live only in aura.settings, which config loading cannot see, so the CLI's
	// fail-fast runs here, after the stored secrets are applied.
	if requireLLMKey && strings.TrimSpace(cfg.LLM.APIKey) == "" && llm.RequiresAPIKey(cfg.LLM.Provider) {
		releaseBootResources(pool, nil)
		return nil, llm.ErrMissingAPIKey
	}
```

(the rest of `bootChatEnvWithConfig` is unchanged). In `bootChatEnv`'s doc, "It uses fail-fast config.Load" becomes "It fails fast on a missing LLM key once aura.settings is applied". Update the tests that call `bootChatEnvWithConfig(` to pass the new argument (`grep -rn "bootChatEnvWithConfig(" cmd/aura`).

With both boots on `config.LoadServe`, `resolveConfigAndPoolWithSettings`'s keyless branch (the `llm.ErrMissingAPIKey` path through `openKeyless`) can no longer be reached from production. Delete that branch and the `openKeyless` op, keep `openSettingsOverlayPool` (still used by `settingsListerForCLI`), and delete the tests that drive only that branch, naming them in the commit body.

`cmd/aura/boot_secret_settings.go`:

```go
package main

// boot_secret_settings.go reads the secret aura.settings rows the daemon needs outside its LLM
// config. They never reach the process environment (settings.OverlayEnv skips them).

import (
	"context"
	"log/slog"
	"strings"

	"github.com/chetto1983/aura/internal/channels/telegram"
	"github.com/chetto1983/aura/internal/settings"
)

// settingsSecret reads one secret row, or "" when the store cannot be built or read. That is
// logged, never fatal: the caller falls back to the environment.
func settingsSecret(ctx context.Context, chat *chatEnv, key string) string {
	if chat == nil || chat.pool == nil || chat.cfg == nil {
		return ""
	}
	store, err := settings.NewStore(chat.pool, chat.cfg.AuthulaSecret)
	if err != nil {
		slog.Warn("settings secret unavailable", "key", key, "err", err)
		return ""
	}
	value, err := store.Secret(ctx, key)
	if err != nil {
		slog.Warn("settings secret unreadable", "key", key, "err", err)
		return ""
	}
	return strings.TrimSpace(value)
}

// effectiveTelegramToken is the saved token when there is one, else the environment's.
func effectiveTelegramToken(ctx context.Context, chat *chatEnv) string {
	if token := settingsSecret(ctx, chat, "TELEGRAM_BOT_TOKEN"); token != "" {
		return token
	}
	return strings.TrimSpace(telegram.LoadConfig().BotToken)
}
```

In `serve_channels.go`, right after `tgCfg := telegram.LoadConfig()`, add `tgCfg.BotToken = effectiveTelegramToken(ctx, chat)`. In `serve_onboarding.go:285`, `resolveBotUsername(ctx, telegram.LoadConfig().BotToken)` becomes `resolveBotUsername(ctx, effectiveTelegramToken(ctx, chat))`.

In `cmd/aura/config.go`, add

```go
// effectiveLLMKeyForCLI is the key the daemon would use: the aura.settings row, which never
// reaches the environment, then the environment.
func effectiveLLMKeyForCLI(ctx context.Context) string {
	if lister, closeLister, _ := settingsListerForCLI(ctx); lister != nil {
		defer closeLister()
		if rows, err := lister.List(ctx); err == nil {
			for _, row := range rows {
				if row.Key == "OPENROUTER_API_KEY" && strings.TrimSpace(row.Value) != "" {
					return row.Value
				}
			}
		}
	}
	return os.Getenv("OPENROUTER_API_KEY")
}
```

and make `loadLLMConfigAndOverlayNote` apply it:

```go
func loadLLMConfigAndOverlayNote() (*llm.Config, string, error) {
	ctx := context.Background()
	note := applySettingsOverlay(ctx)
	cfg, err := resolveLLMConfigTiers()
	if err == nil {
		if key := effectiveLLMKeyForCLI(ctx); key != "" {
			cfg.APIKey = key
		}
	}
	return cfg, note, err
}
```

In `doctor.go:35`, the default becomes `func() string { return effectiveLLMKeyForCLI(context.Background()) }` (keep the `//nolint:gosec` note).

In `cmd/aura-media-index/main.go`, replace `loadEffectiveConfig` with:

```go
func loadEffectiveConfig(ctx context.Context) (*config.Config, error) {
	dbURL := strings.TrimSpace(os.Getenv("AURA_DB_URL"))
	if dbURL == "" {
		return config.LoadServe()
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, fmt.Errorf("settings database: %w", err)
	}
	defer pool.Close()
	store, err := settings.NewStore(pool, os.Getenv("AURA_AUTHULA_SECRET"))
	if err != nil {
		return nil, fmt.Errorf("settings store: %w", err)
	}
	if err = settings.OverlayEnv(ctx, store); err != nil {
		return nil, fmt.Errorf("settings overlay: %w", err)
	}
	cfg, err := config.LoadServe()
	if err != nil {
		return nil, err
	}
	// The services key never reaches the environment, and the vision call on the OpenRouter
	// route needs it.
	key, err := store.Secret(ctx, "OPENROUTER_API_KEY")
	if err != nil {
		return nil, fmt.Errorf("settings secret: %w", err)
	}
	if key != "" {
		cfg.LLM.APIKey = key
	}
	return cfg, nil
}
```

In `internal/skills/installer.go`:

```go
// execCommandEnv is npx's environment. npx runs third-party install scripts, so it gets the
// same credential-free environment an MCP install resolver does, never Aura's own.
func execCommandEnv() []string {
	return append(mcp.InstallerEnv(), "GIT_TERMINAL_PROMPT=0", "DO_NOT_TRACK=1")
}
```

(import `github.com/chetto1983/aura/internal/mcp`; `internal/mcp` does not import `internal/skills`, so there is no cycle).

- [ ] **Step 4: Run the tests and watch them pass**

Run: `go test ./internal/settings/ ./internal/llm/ ./internal/skills/ ./cmd/aura/ ./cmd/aura-media-index/ && go build ./...`
Expected: PASS.

- [ ] **Step 5: Race, lint, commit** (`feat: keep secret settings out of the process environment`). Body: every child process inherited the OpenRouter keys and the Telegram token, npx included; the daemon now reads them from `aura.settings` at boot and at call time. Name the two rewritten `OverlayEnv` tests and the deleted keyless-branch tests, with why.

---

### Task 7: The management key is read at call time

**Files:**
- Modify: `internal/openrouterprovision/errors.go` (`ErrManagementKeyUnset`)
- Modify: `cmd/aura/serve_provisioning_openrouter.go` (`openRouterKeyConfig`, every adapter, `resolveOpenRouterKeyConfig`)
- Modify: `internal/agui/spend_overview_api.go` (`failSpendReconciliation`)
- Modify: `cmd/aura/serve_agui.go:246-288` (comments only: the ports are now always wired)
- Test: `cmd/aura/serve_provisioning_openrouter_test.go`, `cmd/aura/serve_provisioning_openrouter_integration_test.go:146`, `internal/agui/spend_overview_api_test.go`

**Interfaces:**
- Consumes: `(*settings.Store).Secret` (Task 5), `cfg.OpenRouterManagementKey` filled at boot (Task 6).
- Produces: `openrouterprovision.ErrManagementKeyUnset`; `openRouterKeyConfig.managementKey func(context.Context) (string, error)` and `(openRouterKeyConfig).key(ctx) (string, error)`.

- [ ] **Step 1: Write the failing tests.** In `serve_provisioning_openrouter_test.go`, delete `TestProvisionerForNilWhenCredentialAbsent` and `TestOpenRouterManagementKeyAbsentLogsDegradedBoot` (they pin the boot-time capture this task removes) and add:

```go
// TestOpenRouterPortsAreWiredBeforeTheManagementKeyExists proves the ports no longer hang on a
// key captured at boot: they exist whenever the pool and AURA_AUTHULA_SECRET do.
func TestOpenRouterPortsAreWiredBeforeTheManagementKeyExists(t *testing.T) {
	chat := &chatEnv{pool: newLazyPool(t), cfg: &config.Config{AuthulaSecret: validProvisioningAuthulaSecret}}
	if openRouterKeyMinterFor(chat) == nil || openRouterKeyRevokerFor(chat) == nil {
		t.Fatal("ports are nil without a management key; they must be wired and decide at call time")
	}
	if openRouterKeyMinterFor(nil) != nil || openRouterKeyRevokerFor(&chatEnv{cfg: chat.cfg}) != nil {
		t.Fatal("a nil chat or a nil pool must still yield nil ports")
	}
}

func TestOpenRouterKeyConfigRefusesABlankManagementKey(t *testing.T) {
	cfg := openRouterKeyConfig{managementKey: func(context.Context) (string, error) { return "  ", nil }}
	if _, err := cfg.key(context.Background()); !errors.Is(err, openrouterprovision.ErrManagementKeyUnset) {
		t.Fatalf("key() error = %v, want ErrManagementKeyUnset", err)
	}
}

func TestMintSkipsWhileTheManagementKeyIsUnset(t *testing.T) {
	cfg := openRouterKeyConfig{managementKey: func(context.Context) (string, error) { return "", nil }}
	minted, err := openRouterKeyMintAdapter{cfg}.MintKey(context.Background(), "id", "id")
	if err != nil || minted != (agui.MintedKey{}) {
		t.Fatalf("MintKey without a management key = %+v, %v; want an empty key and no error", minted, err)
	}
	if err := (openRouterKeyMintAdapter{cfg}).RevokeKey(context.Background(), ""); err != nil {
		t.Fatalf("RevokeKey(\"\") = %v, want nil: nothing was minted", err)
	}
}
```

Drop the imports this leaves unused (`bytes`, `log/slog`, `strings`) and add `context`, `errors`, `internal/agui` and `internal/openrouterprovision`. In the integration test, the `cfg` literal at line 146 becomes:

```go
	cfg := openRouterKeyConfig{
		client: srv.Client(), baseURL: srv.URL, store: store,
		managementKey: func(context.Context) (string, error) { return "test-management-key", nil },
	}
```

Append to `spend_overview_api_test.go` (add `fmt` to its imports):

```go
func TestSpendOverviewAnswers503WithoutAManagementKey(t *testing.T) {
	recon := &fakeSpendReconciliation{tilesErr: fmt.Errorf("kpi windows: %w", openrouterprovision.ErrManagementKeyUnset)}
	s := newTestSpendServer(recon, &fakeSpendCapReader{caps: map[string]float64{}}, []identity.Identity{{ID: testLocalID, Name: "local"}})

	rec := httptest.NewRecorder()
	s.handleSpendOverview(rec, spendRequest())
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "management key not set") {
		t.Fatalf("status = %d body = %s, want 503 \"management key not set\"", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./cmd/aura/ -run 'OpenRouter|Mint' && go test ./internal/agui/ -run SpendOverview`
Expected: build failure (`managementKey` is a string, `ErrManagementKeyUnset` undefined).

- [ ] **Step 3: Implement.** In `errors.go`:

```go
// ErrManagementKeyUnset reports a provisioning call made before an admin set the management
// key. The ports are wired at boot either way and decide at call time.
var ErrManagementKeyUnset = errors.New("openrouterprovision: management key not set")
```

In `serve_provisioning_openrouter.go`, replace the `openRouterKeyConfig` struct and its doc with:

```go
// openRouterKeyConfig is the dial info every adapter below shares: the HTTP client, the
// Provisioning-API base URL (always OpenRouter's own, whatever the primary route — D-13's
// local exemption never applies to this call), the management key, and the encrypted
// per-identity key store.
type openRouterKeyConfig struct {
	client  *http.Client
	baseURL string
	// managementKey reads the credential on every call: an admin sets it in the wizard long
	// after boot, and a key captured at boot is why the first admin never got one.
	managementKey func(context.Context) (string, error)
	store         *identitykey.Store
}

// key returns the management key, or ErrManagementKeyUnset while no admin has set one.
func (c openRouterKeyConfig) key(ctx context.Context) (string, error) {
	key, err := c.managementKey(ctx)
	if err != nil {
		return "", err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", openrouterprovision.ErrManagementKeyUnset
	}
	return key, nil
}
```

Every adapter method starts by resolving the key and passes it where it used to pass `a.managementKey`:

```go
	managementKey, err := a.key(ctx)
	if err != nil {
		return <zero value>, err
	}
```

This covers `openRouterKeyMintAdapter.MintKey` and `.RevokeKey`, `openRouterKeyPatchAdapter.PatchCap`, `openRouterKeyRevokeAdapter.RevokeKey`, and `openRouterSpendAdapter.ListKeys`, `.GetCredits` and `.KPIWindows`. Two of them differ:

- `openRouterKeyMintAdapter.MintKey` treats an unset key as "nothing to mint yet", so the saga still provisions the identity. Its key gets minted once an admin sets the management key:

```go
	managementKey, err := a.key(ctx)
	if errors.Is(err, openrouterprovision.ErrManagementKeyUnset) {
		return agui.MintedKey{}, nil
	}
	if err != nil {
		return agui.MintedKey{}, err
	}
```

  and its revoke-after-failed-persist uses the same `managementKey`.
- `openRouterKeyMintAdapter.RevokeKey` returns nil for an empty hash before anything else: the saga's compensation passes the hash of a mint that never happened.

Replace `resolveOpenRouterKeyConfig` and its doc with:

```go
// resolveOpenRouterKeyConfig builds the dial info every OpenRouter adapter shares. The
// management key is read from aura.settings on each call, falling back to the value the
// daemon booted with, so the ports are wired as soon as the pool and AURA_AUTHULA_SECRET
// exist, and a missing key surfaces as ErrManagementKeyUnset at call time. ok is false only
// when a store cannot be built (a malformed AURA_AUTHULA_SECRET, logged rather than a boot
// panic, as buildIdentityLLMResolver does).
func resolveOpenRouterKeyConfig(chat *chatEnv) (openRouterKeyConfig, bool) {
	if chat == nil || chat.pool == nil || chat.cfg == nil {
		return openRouterKeyConfig{}, false
	}
	keys, err := identitykey.NewStore(chat.pool, chat.cfg.AuthulaSecret)
	if err != nil {
		slog.Warn("aura serve: identitykey store unavailable — openrouter key mint/revoke disabled", "err", err)
		return openRouterKeyConfig{}, false
	}
	secrets, err := settings.NewStore(chat.pool, chat.cfg.AuthulaSecret)
	if err != nil {
		slog.Warn("aura serve: settings store unavailable — openrouter key mint/revoke disabled", "err", err)
		return openRouterKeyConfig{}, false
	}
	bootKey := chat.cfg.OpenRouterManagementKey
	return openRouterKeyConfig{
		client:  http.DefaultClient,
		baseURL: openrouterprovision.DefaultBaseURL,
		store:   keys,
		managementKey: func(ctx context.Context) (string, error) {
			stored, err := secrets.Secret(ctx, "AURA_OPENROUTER_MANAGEMENT_KEY")
			if err != nil || stored != "" {
				return stored, err
			}
			return bootKey, nil
		},
	}, true
}
```

In `spend_overview_api.go`, `failSpendReconciliation` answers the unset key before the generic path (add the `openrouterprovision` import if missing):

```go
func failSpendReconciliation(w http.ResponseWriter, err error) {
	if errors.Is(err, openrouterprovision.ErrManagementKeyUnset) {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "management key not set"})
		return
	}
	slog.Error("aura admin: spend overview reconciliation failed", "err", err)
	writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "couldn't load the spend overview"})
}
```

In `serve_agui.go`, rewrite the comments above the two `resolveOpenRouterKeyConfig` calls: the ports are wired whenever the stores build, and each call reads the management key, answering "management key not set" until an admin sets it.

- [ ] **Step 4: Run the tests and watch them pass**

Run: `go test ./cmd/aura/ ./internal/agui/ ./internal/openrouterprovision/ && go build ./...`, then with the stack up `go test -tags db_integration -race -run OpenRouter ./cmd/aura/`
Expected: PASS.

- [ ] **Step 5: Race, lint, commit** (`fix(openrouter): read the management key at call time`). Body: the key was captured at boot, so an admin who set it in the wizard got no key until a restart, and the first admin never got one at all. Name the two deleted tests and why.

---

### Task 8: One minter for every identity's key

**Files:**
- Modify: `internal/db/queries/identity_llm_key.sql` (+ regenerated `internal/db/sqlc/identity_llm_key.sql.go`)
- Modify: `internal/identitykey/store.go` (`rowParams`, `Save`, `InsertIfAbsent`)
- Create: `internal/agui/openrouter_keys.go`, `internal/agui/openrouter_keys_test.go`
- Modify: `cmd/aura/serve_provisioning_openrouter.go` (`openRouterMintingAdapter` replaces `openRouterKeyMintAdapter`; `openRouterKeyMinterFor`; `liveRouteBills`), `cmd/aura/serve_agui.go` (`creditBackendBills := liveRouteBills(chat)`)
- Test: `internal/identitykey/store_integration_test.go`, `cmd/aura/serve_provisioning_openrouter_test.go`, `cmd/aura/serve_provisioning_openrouter_integration_test.go`

**Interfaces:**
- Consumes: `MintRequest.Limit *USDCap` (Task 2), `Record.LimitUSD *float64` (Task 3), `openRouterKeyConfig.key` (Task 7).
- Produces: `(*identitykey.Store).InsertIfAbsent(ctx, Record) (bool, error)`; `agui.OpenRouterMinting`; `agui.NewIdentityKeyMinter(minting OpenRouterMinting, keys identityKeyStore, caps capabilityChecker, routeBills func() bool) *IdentityKeyMinter`; its methods `MintKey`, `RevokeKey` (it satisfies `OpenRouterKeyMinter`), `readiness(ctx) (string, error)`, `ensure(ctx, identityID, keyName) (MintedKey, bool, error)`, `revokeUnrecorded(ctx, hash)`; the constants `skipLocalRoute`, `skipManagementKeyUnset`; `liveRouteBills(*chatEnv) func() bool`; the test helper `adminCaps(admins ...string) *fakeIdentityAdmin`.

- [ ] **Step 1: Add the query and regenerate.** Append to `internal/db/queries/identity_llm_key.sql`:

```sql
-- name: InsertIdentityLLMKeyIfAbsent :execrows
-- Writes a key only when the identity has none. The reconciler and the provisioning saga can
-- mint for the same new identity at once; the one that loses sees 0 rows and revokes its own
-- key instead of overwriting the winner's.
INSERT INTO aura.identity_llm_key (identity_id, key_ciphertext, key_hash, key_label, limit_usd, limit_reset)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (identity_id) DO NOTHING;
```

Run: `make sqlc && git diff --stat internal/db/sqlc`
Expected: `identity_llm_key.sql.go` gains `InsertIdentityLLMKeyIfAbsent(ctx, InsertIdentityLLMKeyIfAbsentParams) (int64, error)`, whose params have the same fields as `UpsertIdentityLLMKeyParams`.

- [ ] **Step 2: Write the failing tests.** Append to `store_integration_test.go`:

```go
// TestInsertIfAbsentKeepsTheFirstKey proves the second of two mints for one identity is told
// it lost, and that the stored key stays the first one.
func TestInsertIfAbsentKeepsTheFirstKey(t *testing.T) {
	pool := migratedKeyPool(t)
	store := keyStore(t, pool)
	owner := seedKeyIdentity(t, pool)
	ctx := identityctx.WithIdentityID(context.Background(), owner)

	first := Record{Key: "sk-or-v1-first-" + uuid.NewString(), Hash: "hash-first", Label: "first", LimitUSD: capUSD(0), LimitReset: "monthly"}
	if inserted, err := store.InsertIfAbsent(ctx, first); err != nil || !inserted {
		t.Fatalf("first InsertIfAbsent = %v, %v; want inserted", inserted, err)
	}
	second := Record{Key: "sk-or-v1-second-" + uuid.NewString(), Hash: "hash-second", Label: "second", LimitUSD: capUSD(0), LimitReset: "monthly"}
	if inserted, err := store.InsertIfAbsent(ctx, second); err != nil || inserted {
		t.Fatalf("second InsertIfAbsent = %v, %v; want not inserted", inserted, err)
	}
	got, err := store.Load(ctx)
	if err != nil || got.Hash != "hash-first" || got.Key != first.Key {
		t.Fatalf("stored key = %+v (err %v), want the first one", got, err)
	}
}
```

`internal/agui/openrouter_keys_test.go`:

```go
package agui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/openrouterprovision"
)

// adminCaps is an identity admin whose listed identities hold identity.create.
func adminCaps(admins ...string) *fakeIdentityAdmin {
	caps := map[string][]string{}
	for _, id := range admins {
		caps[id] = []string{identity.CapIdentityCreate}
	}
	return &fakeIdentityAdmin{caps: caps}
}

type fakeMinting struct {
	mu      sync.Mutex
	keySet  bool
	failFor map[string]error
	minted  []openrouterprovision.MintRequest
	revoked []string
	next    int
}

func (f *fakeMinting) ManagementKeySet(context.Context) (bool, error) { return f.keySet, nil }

func (f *fakeMinting) Mint(_ context.Context, req openrouterprovision.MintRequest) (openrouterprovision.MintResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.minted = append(f.minted, req)
	if err := f.failFor[req.IdentityID]; err != nil {
		return openrouterprovision.MintResult{}, err
	}
	f.next++
	hash := fmt.Sprintf("hash-%d", f.next)
	return openrouterprovision.MintResult{Key: "sk-or-v1-" + hash, Record: openrouterprovision.KeyRecord{Hash: hash, Label: "sk-or-v1-..." + hash}}, nil
}

func (f *fakeMinting) Revoke(_ context.Context, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = append(f.revoked, hash)
	return nil
}

type fakeIdentityKeys struct {
	mu        sync.Mutex
	records   map[string]identitykey.Record
	insertErr error
	// raceWinner is written by "someone else" just before the next InsertIfAbsent runs.
	raceWinner *identitykey.Record
}

func newFakeIdentityKeys() *fakeIdentityKeys {
	return &fakeIdentityKeys{records: map[string]identitykey.Record{}}
}

func (f *fakeIdentityKeys) Load(ctx context.Context) (identitykey.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[identityctx.IdentityID(ctx)]
	if !ok {
		return identitykey.Record{}, identitykey.ErrNoKey
	}
	return rec, nil
}

func (f *fakeIdentityKeys) InsertIfAbsent(ctx context.Context, r identitykey.Record) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertErr != nil {
		return false, f.insertErr
	}
	id := identityctx.IdentityID(ctx)
	if f.raceWinner != nil {
		f.records[id], f.raceWinner = *f.raceWinner, nil
	}
	if _, taken := f.records[id]; taken {
		return false, nil
	}
	f.records[id] = r
	return true, nil
}

func billing() bool { return true }

func TestMinterGivesAnAdminAKeyWithNoLimit(t *testing.T) {
	minting, keys := &fakeMinting{keySet: true}, newFakeIdentityKeys()
	minter := NewIdentityKeyMinter(minting, keys, adminCaps("id-admin"), billing)

	minted, err := minter.MintKey(context.Background(), "id-admin", "id-admin")
	if err != nil || minted.Hash != "hash-1" {
		t.Fatalf("MintKey = %+v, %v; want hash-1", minted, err)
	}
	if len(minting.minted) != 1 || minting.minted[0].Limit != nil {
		t.Fatalf("mint requests = %+v, want one with no limit", minting.minted)
	}
	if keys.records["id-admin"].LimitUSD != nil {
		t.Fatal("the admin's stored key has a cap, want none")
	}
}

func TestMinterGivesAMemberAZeroCap(t *testing.T) {
	minting, keys := &fakeMinting{keySet: true}, newFakeIdentityKeys()
	minter := NewIdentityKeyMinter(minting, keys, adminCaps(), billing)

	if _, err := minter.MintKey(context.Background(), "id-member", "id-member"); err != nil {
		t.Fatalf("MintKey: %v", err)
	}
	if req := minting.minted[0]; req.Limit == nil || *req.Limit != 0 || req.IdentityID != "id-member" {
		t.Fatalf("mint request = %+v, want a zero cap for id-member", req)
	}
	if got := keys.records["id-member"].LimitUSD; got == nil || *got != 0 {
		t.Fatal("the member's stored key is not at a zero cap")
	}
}

func TestMinterMintsNothingYet(t *testing.T) {
	for name, tc := range map[string]struct{ keySet, routeBills bool }{
		"local route":           {keySet: true, routeBills: false},
		"no management key yet": {keySet: false, routeBills: true},
	} {
		t.Run(name, func(t *testing.T) {
			minting := &fakeMinting{keySet: tc.keySet}
			minter := NewIdentityKeyMinter(minting, newFakeIdentityKeys(), adminCaps(), func() bool { return tc.routeBills })
			minted, err := minter.MintKey(context.Background(), "id", "id")
			if err != nil || minted != (MintedKey{}) || len(minting.minted) != 0 {
				t.Fatalf("MintKey = %+v, %v with %d mints; want nothing minted and no error", minted, err, len(minting.minted))
			}
		})
	}
}

func TestMinterKeepsAnExistingKey(t *testing.T) {
	minting, keys := &fakeMinting{keySet: true}, newFakeIdentityKeys()
	keys.records["id"] = identitykey.Record{Key: "sk-existing", Hash: "hash-existing", Label: "existing"}
	minter := NewIdentityKeyMinter(minting, keys, adminCaps(), billing)

	minted, err := minter.MintKey(context.Background(), "id", "id")
	if err != nil || minted.Hash != "hash-existing" || len(minting.minted) != 0 {
		t.Fatalf("MintKey = %+v, %v with %d mints; want the existing key and no mint", minted, err, len(minting.minted))
	}
}

func TestMinterRevokesTheKeyThatLostTheRace(t *testing.T) {
	minting, keys := &fakeMinting{keySet: true}, newFakeIdentityKeys()
	keys.raceWinner = &identitykey.Record{Key: "sk-winner", Hash: "hash-winner", Label: "winner"}
	minter := NewIdentityKeyMinter(minting, keys, adminCaps(), billing)

	minted, err := minter.MintKey(context.Background(), "id", "id")
	if err != nil || minted.Hash != "hash-winner" {
		t.Fatalf("MintKey = %+v, %v; want the winner's key", minted, err)
	}
	if len(minting.revoked) != 1 || minting.revoked[0] != "hash-1" {
		t.Fatalf("revoked = %v, want the losing mint hash-1", minting.revoked)
	}
}

func TestMinterRevokesAKeyItCouldNotRecord(t *testing.T) {
	minting, keys := &fakeMinting{keySet: true}, newFakeIdentityKeys()
	keys.insertErr = errors.New("store down")
	minter := NewIdentityKeyMinter(minting, keys, adminCaps(), billing)

	if _, err := minter.MintKey(context.Background(), "id", "id"); err == nil {
		t.Fatal("a failed store write passed silently")
	}
	if len(minting.revoked) != 1 || minting.revoked[0] != "hash-1" {
		t.Fatalf("revoked = %v, want the unrecorded hash-1", minting.revoked)
	}
}

func TestMinterRevokeIgnoresAMintThatNeverHappened(t *testing.T) {
	minting := &fakeMinting{keySet: true}
	minter := NewIdentityKeyMinter(minting, newFakeIdentityKeys(), adminCaps(), billing)
	if err := minter.RevokeKey(context.Background(), ""); err != nil || len(minting.revoked) != 0 {
		t.Fatalf("RevokeKey(\"\") = %v with %d revokes; want nil and none", err, len(minting.revoked))
	}
}
```

In `serve_provisioning_openrouter_test.go`, replace Task 7's `TestMintSkipsWhileTheManagementKeyIsUnset` with:

```go
func TestMintingAdapterWithoutAManagementKey(t *testing.T) {
	adapter := openRouterMintingAdapter{openRouterKeyConfig{managementKey: func(context.Context) (string, error) { return "", nil }}}
	if set, err := adapter.ManagementKeySet(context.Background()); set || err != nil {
		t.Fatalf("ManagementKeySet = %v, %v; want false, nil", set, err)
	}
	if _, err := adapter.Mint(context.Background(), openrouterprovision.MintRequest{}); !errors.Is(err, openrouterprovision.ErrManagementKeyUnset) {
		t.Fatalf("Mint error = %v, want ErrManagementKeyUnset", err)
	}
}
```

In the integration test, build the minter instead of the removed adapter, and check the member's zero cap:

```go
	mint := agui.NewIdentityKeyMinter(openRouterMintingAdapter{cfg}, store, memberCapabilities{}, func() bool { return true })
```

```go
	if rec.LimitUSD == nil || *rec.LimitUSD != 0 {
		t.Fatalf("stored cap = %v, want a member's zero cap", rec.LimitUSD)
	}
```

```go
// memberCapabilities answers "not an admin" for every identity.
type memberCapabilities struct{}

func (memberCapabilities) HasCapability(context.Context, string, string) (bool, error) { return false, nil }
```

- [ ] **Step 3: Run the tests and watch them fail**

Run: `go test ./internal/agui/ -run Minter && go test ./cmd/aura/ -run MintingAdapter`
Expected: build failure (`NewIdentityKeyMinter`, `openRouterMintingAdapter` undefined).

- [ ] **Step 4: Implement the store.** In `identitykey/store.go`, replace `Save` with `rowParams` + `Save` + `InsertIfAbsent`:

```go
// rowParams validates r and encodes it for the identity on ctx; Save and InsertIfAbsent
// share it.
func (s *Store) rowParams(ctx context.Context, r Record) (string, sqlc.UpsertIdentityLLMKeyParams, error) {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return "", sqlc.UpsertIdentityLLMKeyParams{}, err
	}
	id, err := parseUUID(identity)
	if err != nil {
		return "", sqlc.UpsertIdentityLLMKeyParams{}, err
	}
	if strings.TrimSpace(r.Key) == "" {
		return "", sqlc.UpsertIdentityLLMKeyParams{}, errors.New("identitykey: save needs a key")
	}
	ciphertext, err := s.seal([]byte(r.Key))
	if err != nil {
		return "", sqlc.UpsertIdentityLLMKeyParams{}, err
	}
	limitUSD, err := numericCap(r.LimitUSD)
	if err != nil {
		return "", sqlc.UpsertIdentityLLMKeyParams{}, fmt.Errorf("identitykey: save: %w", err)
	}
	limitReset := strings.TrimSpace(r.LimitReset)
	if limitReset == "" {
		limitReset = "monthly"
	}
	return identity, sqlc.UpsertIdentityLLMKeyParams{
		IdentityID: id, KeyCiphertext: ciphertext, KeyHash: r.Hash, KeyLabel: r.Label,
		LimitUsd: limitUSD, LimitReset: limitReset,
	}, nil
}

// Save writes the key, replacing any earlier one for the same identity (ON CONFLICT DO
// UPDATE — a rotation rewrites the same row rather than accumulating history).
func (s *Store) Save(ctx context.Context, r Record) error {
	identity, params, err := s.rowParams(ctx, r)
	if err != nil {
		return err
	}
	err = db.WithIdentityTx(ctx, s.pool, identity, func(q *sqlc.Queries) error {
		return q.UpsertIdentityLLMKey(ctx, params)
	})
	if err != nil {
		return fmt.Errorf("identitykey: save: %w", err)
	}
	return nil
}

// InsertIfAbsent writes the key only when the identity has none, and reports whether it did.
func (s *Store) InsertIfAbsent(ctx context.Context, r Record) (bool, error) {
	identity, params, err := s.rowParams(ctx, r)
	if err != nil {
		return false, err
	}
	var inserted int64
	err = db.WithIdentityTx(ctx, s.pool, identity, func(q *sqlc.Queries) error {
		var qerr error
		inserted, qerr = q.InsertIdentityLLMKeyIfAbsent(ctx, sqlc.InsertIdentityLLMKeyIfAbsentParams(params))
		return qerr
	})
	if err != nil {
		return false, fmt.Errorf("identitykey: insert: %w", err)
	}
	return inserted == 1, nil
}
```

- [ ] **Step 5: Implement the minter.** `internal/agui/openrouter_keys.go`:

```go
package agui

// openrouter_keys.go mints a person's own OpenRouter key. The provisioning saga (as its credit
// port) and the reconciler (openrouter_reconcile.go) both go through IdentityKeyMinter, so the
// rules live in one place: nothing on a local route or before a management key exists, no
// limit for an admin, a zero cap for everyone else, and never two live keys for one identity.

import (
	"context"
	"errors"
	"log/slog"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/openrouterprovision"
)

// OpenRouterMinting is the provider side of minting, implemented in cmd/aura over the
// management key it reads at call time.
type OpenRouterMinting interface {
	ManagementKeySet(ctx context.Context) (bool, error)
	Mint(ctx context.Context, req openrouterprovision.MintRequest) (openrouterprovision.MintResult, error)
	Revoke(ctx context.Context, hash string) error
}

// identityKeyStore is identitykey.Store narrowed to what minting needs; the caller scopes ctx
// to the identity.
type identityKeyStore interface {
	Load(ctx context.Context) (identitykey.Record, error)
	InsertIfAbsent(ctx context.Context, r identitykey.Record) (bool, error)
}

// capabilityChecker answers whether an identity is an admin.
type capabilityChecker interface {
	HasCapability(ctx context.Context, identityID, capability string) (bool, error)
}

// The reasons minting waits, reported by the reconciler.
const (
	skipLocalRoute         = "local_route"
	skipManagementKeyUnset = "management_key_unset"
)

// IdentityKeyMinter mints identities' OpenRouter keys.
type IdentityKeyMinter struct {
	minting    OpenRouterMinting
	keys       identityKeyStore
	caps       capabilityChecker
	routeBills func() bool
}

// NewIdentityKeyMinter builds the minter. routeBills reports whether the live primary route
// bills; a local route bills nothing, so it gets no keys (D-13).
func NewIdentityKeyMinter(minting OpenRouterMinting, keys identityKeyStore, caps capabilityChecker, routeBills func() bool) *IdentityKeyMinter {
	return &IdentityKeyMinter{minting: minting, keys: keys, caps: caps, routeBills: routeBills}
}

var _ OpenRouterKeyMinter = (*IdentityKeyMinter)(nil)

// MintKey is the provisioning saga's credit leg. On a local route, or before an admin set the
// management key, it mints nothing and returns an empty key; the reconciler mints it later.
func (m *IdentityKeyMinter) MintKey(ctx context.Context, identityID, keyName string) (MintedKey, error) {
	skip, err := m.readiness(ctx)
	if err != nil || skip != "" {
		return MintedKey{}, err
	}
	minted, _, err := m.ensure(ctx, identityID, keyName)
	return minted, err
}

// RevokeKey is the saga's compensation. An empty hash is a mint that never happened.
func (m *IdentityKeyMinter) RevokeKey(ctx context.Context, hash string) error {
	if hash == "" {
		return nil
	}
	return m.minting.Revoke(ctx, hash)
}

// readiness is "" when keys can be minted now, else the reason they cannot.
func (m *IdentityKeyMinter) readiness(ctx context.Context) (string, error) {
	if m.routeBills != nil && !m.routeBills() {
		return skipLocalRoute, nil
	}
	set, err := m.minting.ManagementKeySet(ctx)
	if err != nil {
		return "", err
	}
	if !set {
		return skipManagementKeyUnset, nil
	}
	return "", nil
}

// ensure mints identityID's key unless it already has one, and reports whether it minted. An
// admin's key has no limit; everyone else's starts at zero (CRED-02). The row is written only
// if still absent: a mint that loses that race is revoked and the winner's key returned.
func (m *IdentityKeyMinter) ensure(ctx context.Context, identityID, keyName string) (MintedKey, bool, error) {
	scoped := identityctx.WithIdentityID(ctx, identityID)
	existing, err := m.keys.Load(scoped)
	if err == nil {
		return MintedKey{Hash: existing.Hash, Label: existing.Label}, false, nil
	}
	if !errors.Is(err, identitykey.ErrNoKey) {
		return MintedKey{}, false, err
	}
	admin, err := m.caps.HasCapability(ctx, identityID, identity.CapIdentityCreate)
	if err != nil {
		return MintedKey{}, false, err
	}
	var limit *openrouterprovision.USDCap
	var limitUSD *float64
	if !admin {
		limit, limitUSD = new(openrouterprovision.USDCap), new(float64)
	}
	res, err := m.minting.Mint(ctx, openrouterprovision.MintRequest{
		IdentityID: identityID, Name: keyName, Limit: limit, LimitReset: openrouterprovision.LimitResetMonthly,
	})
	if err != nil {
		return MintedKey{}, false, err
	}
	inserted, err := m.keys.InsertIfAbsent(scoped, identitykey.Record{
		Key: res.Key, Hash: res.Record.Hash, Label: res.Record.Label,
		LimitUSD: limitUSD, LimitReset: string(openrouterprovision.LimitResetMonthly),
	})
	if err != nil {
		m.revokeUnrecorded(ctx, res.Record.Hash)
		return MintedKey{}, false, err
	}
	if !inserted {
		m.revokeUnrecorded(ctx, res.Record.Hash)
		winner, err := m.keys.Load(scoped)
		if err != nil {
			return MintedKey{}, false, err
		}
		return MintedKey{Hash: winner.Hash, Label: winner.Label}, false, nil
	}
	return MintedKey{Hash: res.Record.Hash, Label: res.Record.Label}, true, nil
}

// revokeUnrecorded revokes a key Aura minted but did not record, so it does not stay live and
// billable at the provider. Its own failure is logged, not returned: the caller is already
// reporting the error that left the key unrecorded.
func (m *IdentityKeyMinter) revokeUnrecorded(ctx context.Context, hash string) {
	if err := m.minting.Revoke(context.WithoutCancel(ctx), hash); err != nil {
		slog.Error("openrouter keys: revoking an unrecorded key failed; an orphan key may be live at the provider", "err", err)
	}
}
```

Update `onboarding_provision_credit.go`'s file header: the composition-root adapter it describes is now `agui.IdentityKeyMinter` over `cmd/aura`'s `openRouterMintingAdapter`.

- [ ] **Step 6: Wire it.** In `serve_provisioning_openrouter.go`, delete `openRouterKeyMintAdapter` and its two methods, and add:

```go
// openRouterMintingAdapter satisfies agui.OpenRouterMinting over the management key it reads
// at call time.
type openRouterMintingAdapter struct{ openRouterKeyConfig }

var _ agui.OpenRouterMinting = openRouterMintingAdapter{}

func (a openRouterMintingAdapter) ManagementKeySet(ctx context.Context) (bool, error) {
	_, err := a.key(ctx)
	if errors.Is(err, openrouterprovision.ErrManagementKeyUnset) {
		return false, nil
	}
	return err == nil, err
}

func (a openRouterMintingAdapter) Mint(ctx context.Context, req openrouterprovision.MintRequest) (openrouterprovision.MintResult, error) {
	managementKey, err := a.key(ctx)
	if err != nil {
		return openrouterprovision.MintResult{}, err
	}
	return openrouterprovision.MintKey(ctx, a.client, a.baseURL, managementKey, req)
}

func (a openRouterMintingAdapter) Revoke(ctx context.Context, hash string) error {
	managementKey, err := a.key(ctx)
	if err != nil {
		return err
	}
	return openrouterprovision.RevokeKey(ctx, a.client, a.baseURL, managementKey, hash)
}

// liveRouteBills reports whether the primary route the operator runs right now bills.
func liveRouteBills(chat *chatEnv) func() bool {
	return func() bool { return !llm.IsKeylessLocalBaseURL(chat.llmRuntime.Snapshot().Config.BaseURL) }
}
```

`openRouterKeyMinterFor` returns `agui.NewIdentityKeyMinter(openRouterMintingAdapter{cfg}, cfg.store, chat.identity, liveRouteBills(chat))` when `resolveOpenRouterKeyConfig` succeeds, and nil otherwise. In `serve_agui.go`, Task 4's inline `creditBackendBills` func becomes `creditBackendBills := liveRouteBills(chat)`.

- [ ] **Step 7: Run the tests and watch them pass**

Run: `go test ./internal/identitykey/ ./internal/agui/ ./cmd/aura/ && go build ./...`, then with the stack up `go test -tags db_integration -race -run 'InsertIfAbsent|OpenRouter' ./internal/identitykey/ ./cmd/aura/`
Expected: PASS.

- [ ] **Step 8: Race, lint, commit** (`feat(agui): one minter for every identity's OpenRouter key`). Body: the saga minted every key at a zero cap and overwrote any existing row; the minter gives an admin no limit, skips a local route or a missing management key, and never leaves two live keys for one identity.

---

### Task 9: Only an admin changes the deployment's credential, route and model

**Files:**
- Modify: `internal/settings/settings.go` (`AllowedKeys` gains `AURA_OPENROUTER_SERVICES_CAP_USD`), `internal/settings/settings_test.go`
- Create: `internal/agui/settings_api_validate.go`, moving `validateSettingValue`, `isLLMTokenSetting`, `validatePendingLLMTokenSetting` and `applyLLMTokenSetting` out of `settings_api.go` unchanged (the file would cross 600 lines)
- Create: `internal/agui/settings_api_authz.go`, `internal/agui/settings_api_authz_test.go`
- Modify: `internal/agui/settings_api.go` (`handlePutSetting`, `handleDeleteSetting`, `handlePutLLMProfile`, `handleListSettings`)

**Interfaces:**
- Consumes: `adminCaps` (Task 8 test helper), `fakeIdentityAdmin` (`audit_api_test.go`).
- Produces: `(*Server).authorizeSettingWrite(w, r, actor string, requireAdmin bool, keys ...string) bool`; `isCallTimeSetting(key string) bool`; the setting `AURA_OPENROUTER_SERVICES_CAP_USD`.

- [ ] **Step 1: Write the failing tests.** Add to `settings_test.go`'s `TestAllowed`:

```go
	if m, ok := Allowed("AURA_OPENROUTER_SERVICES_CAP_USD"); !ok || m.Secret || m.Kind != KindString {
		t.Errorf("AURA_OPENROUTER_SERVICES_CAP_USD should be an allowlisted non-secret string setting, got ok=%v meta=%+v", ok, m)
	}
```

`internal/agui/settings_api_authz_test.go`:

```go
package agui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

func TestMemberCannotChangeTheRoute(t *testing.T) {
	store := &fakeSettingsStore{}
	s := &Server{settings: store, idAdmin: adminCaps("admin-1")}
	rr, r := putReq(t, "AURA_LLM_MODEL", "other/model", "member-1")
	s.handlePutSetting(rr, r)
	if rr.Code != http.StatusForbidden || len(store.upserted) != 0 {
		t.Fatalf("status = %d upserted = %v, want 403 and nothing written", rr.Code, store.upserted)
	}
}

func TestAdminSetsTheManagementKey(t *testing.T) {
	store := &fakeSettingsStore{}
	s := &Server{settings: store, idAdmin: adminCaps("admin-1")}
	rr, r := putReq(t, "AURA_OPENROUTER_MANAGEMENT_KEY", "sk-or-v1-mgmt", "admin-1")
	s.handlePutSetting(rr, r)
	if rr.Code != http.StatusOK || store.upserted["AURA_OPENROUTER_MANAGEMENT_KEY"] != "sk-or-v1-mgmt" {
		t.Fatalf("status = %d upserted = %v, want 200 and the key stored", rr.Code, store.upserted)
	}
	if strings.Contains(rr.Body.String(), "sk-or-v1-mgmt") {
		t.Fatal("the response echoes the management key")
	}
}

func TestNobodyWritesTheServicesKey(t *testing.T) {
	store := &fakeSettingsStore{}
	s := &Server{settings: store, idAdmin: adminCaps("admin-1")}
	rr, r := putReq(t, "OPENROUTER_API_KEY", "sk-or-v1-pasted", "admin-1")
	s.handlePutSetting(rr, r)
	if rr.Code != http.StatusForbidden || len(store.upserted) != 0 {
		t.Fatalf("status = %d upserted = %v, want 403 and nothing written", rr.Code, store.upserted)
	}
}

func TestMemberCannotDeleteTheManagementKey(t *testing.T) {
	store := &fakeSettingsStore{}
	s := &Server{settings: store, idAdmin: adminCaps("admin-1")}
	r := withPrincipal(httptest.NewRequest(http.MethodDelete, "/api/settings/AURA_OPENROUTER_MANAGEMENT_KEY", nil), "member-1")
	r.SetPathValue("key", "AURA_OPENROUTER_MANAGEMENT_KEY")
	rr := httptest.NewRecorder()
	s.handleDeleteSetting(rr, r)
	if rr.Code != http.StatusForbidden || len(store.deleted) != 0 {
		t.Fatalf("status = %d deleted = %v, want 403 and nothing deleted", rr.Code, store.deleted)
	}
}

func TestMemberCannotPutAnLLMProfile(t *testing.T) {
	store := &fakeSettingsStore{}
	s := &Server{settings: store, llmRouteReloader: &fakeLLMRouteReloader{}, idAdmin: adminCaps("admin-1")}
	body := `{"settings":{"AURA_LOOP_MAX_STEPS":"40"}}`
	r := withPrincipal(httptest.NewRequest(http.MethodPut, "/api/settings/llm-profile", strings.NewReader(body)), "member-1")
	rr := httptest.NewRecorder()
	s.handlePutLLMProfile(rr, r)
	if rr.Code != http.StatusForbidden || len(store.upserted) != 0 {
		t.Fatalf("status = %d upserted = %v, want 403 and nothing written", rr.Code, store.upserted)
	}
}

func TestMemberStillTunesAnOrdinarySetting(t *testing.T) {
	store := &fakeSettingsStore{}
	s := &Server{settings: store, idAdmin: adminCaps("admin-1")}
	rr, r := putReq(t, "AURA_TTS_MODEL", "tts-x", "member-1")
	s.handlePutSetting(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: members keep governance.write for the rest", rr.Code)
	}
}

func TestCallTimeSettingsReadAsLive(t *testing.T) {
	store := &fakeSettingsStore{rows: []sqlc.AuraSettings{{Key: "AURA_OPENROUTER_MANAGEMENT_KEY", Value: "sk-or-v1-mgmt", IsSecret: true}}}
	s := &Server{settings: store}
	rr := httptest.NewRecorder()
	s.handleListSettings(rr, httptest.NewRequest(http.MethodGet, "/api/settings", nil))
	if item := settingItemByKey(t, rr.Body.Bytes(), "AURA_OPENROUTER_MANAGEMENT_KEY"); item.Applied != appliedLive {
		t.Fatalf("applied = %q, want live: the key is read on every call", item.Applied)
	}
}
```

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./internal/agui/ -run 'Member|Admin|Nobody|CallTime' && go test ./internal/settings/ -run TestAllowed`
Expected: the member writes succeed (want 403), and the new key is not allowlisted.

- [ ] **Step 3: Implement.** In `settings.go`'s `AllowedKeys`, after the management key:

```go
	// The monthly cap of the aura-services key the reconciler mints. It is read when the key is
	// minted, never overlaid into a running config.
	"AURA_OPENROUTER_SERVICES_CAP_USD": {Kind: KindString, Label: "OpenRouter services key monthly cap (USD)"},
```

`internal/agui/settings_api_authz.go`:

```go
package agui

import (
	"net/http"

	"github.com/chetto1983/aura/internal/identity"
)

// adminOnlySettingKeys decide which credential, which route and which model the whole
// deployment runs on. Every identity holds governance.write (D-01), so writing them also
// takes identity.create, the capability that makes an identity an admin.
var adminOnlySettingKeys = map[string]struct{}{
	"AURA_OPENROUTER_MANAGEMENT_KEY":   {},
	"AURA_OPENROUTER_SERVICES_CAP_USD": {},
	"AURA_LLM_PROVIDER":                {},
	"AURA_LLM_MODEL":                   {},
	"AURA_LLM_BASE_URL":                {},
}

// mintedSettingKeys are written only by the reconciler: nobody types the services key.
var mintedSettingKeys = map[string]struct{}{"OPENROUTER_API_KEY": {}}

// callTimeSettingKeys are read from the store on every use, so a saved value is live at once.
var callTimeSettingKeys = map[string]struct{}{
	"AURA_OPENROUTER_MANAGEMENT_KEY":   {},
	"AURA_OPENROUTER_SERVICES_CAP_USD": {},
}

func isCallTimeSetting(key string) bool {
	_, ok := callTimeSettingKeys[key]
	return ok
}

// authorizeSettingWrite refuses, and answers for, a write the caller may not make: a minted key
// from anyone, an admin-only key — or, with requireAdmin, any key — from a member. It returns
// true when the write may go ahead.
func (s *Server) authorizeSettingWrite(w http.ResponseWriter, r *http.Request, actor string, requireAdmin bool, keys ...string) bool {
	for _, key := range keys {
		if _, minted := mintedSettingKeys[key]; minted {
			writeJSONStatus(w, http.StatusForbidden, map[string]string{"error": key + " is minted by Aura and cannot be set"})
			return false
		}
		if _, adminOnly := adminOnlySettingKeys[key]; adminOnly {
			requireAdmin = true
		}
	}
	if !requireAdmin {
		return true
	}
	if s.idAdmin == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "identity admin not configured"})
		return false
	}
	isAdmin, err := s.idAdmin.HasCapability(r.Context(), actor, identity.CapIdentityCreate)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": "capability store unavailable"})
		return false
	}
	if !isAdmin {
		writeJSONStatus(w, http.StatusForbidden, map[string]string{"error": "only an admin can change this setting"})
		return false
	}
	return true
}
```

In `settings_api.go`:

- `handlePutLLMProfile`: after the key-validation loop and before `s.settingsMu.Lock()`, add

```go
	if !s.authorizeSettingWrite(w, r, actor, true, slices.Collect(maps.Keys(body.Settings))...) {
		return
	}
```

- `handlePutSetting`: after the `principalIdentityID` check, add `if !s.authorizeSettingWrite(w, r, actor, false, key) { return }`.
- `handleDeleteSetting`: `if _, ok := principalIdentityID(r); !ok {` becomes `actor, ok := principalIdentityID(r); if !ok {`, followed by the same authorization call.
- `handleListSettings`: in the `switch` inside `if overridden`, add as its second case

```go
			case isCallTimeSetting(key):
				item.Applied = appliedLive
```

and update the file header: PUT and DELETE of the credential, route and model keys, and the whole `llm-profile` route, also require `identity.create`; `OPENROUTER_API_KEY` cannot be written through the API.

- [ ] **Step 4: Bring the existing settings tests in line.** Run: `go test ./internal/agui/ -run 'Setting|LLMProfile|LLMModel|Telegram'`. For each failure:
  - a test that writes an admin-only key gets `idAdmin: adminCaps("op-1")` (or its own principal) on its `Server`;
  - a test that writes `OPENROUTER_API_KEY` to exercise hot publishing switches to another hot key (`AURA_LOOP_MAX_STEPS`); a test that exercised the key itself is replaced by `TestNobodyWritesTheServicesKey`.

  Name every changed test in the commit body.

- [ ] **Step 5: Run the tests and watch them pass**

Run: `go test ./internal/agui/ ./internal/settings/ && go build ./... && wc -l internal/agui/settings_api.go`
Expected: PASS; `settings_api.go` under 600 lines.

- [ ] **Step 6: Race, lint, commit** (`fix(agui): only an admin changes the deployment's credential and route`). Body: every identity holds `governance.write`, so any member could replace the management key or switch to a local route that exempts everyone from billing; nobody can type the services key any more.

---

### Task 10: The reconciler mints what is missing and aligns limits with roles

**Files:**
- Modify: `internal/agui/openrouter_keys.go` (`OpenRouterMinting` gains `Patch`; `identityKeyStore` gains `Save`; new `alignLimit`), `internal/agui/openrouter_keys_test.go` (the fakes gain `Patch` and `Save`)
- Create: `internal/agui/openrouter_reconcile.go`, `internal/agui/openrouter_reconcile_test.go`
- Modify: `internal/agui/server.go` (the `keyMinter` field; route registration after `registerSpendOverviewRoutes`), `internal/agui/idempotency_http.go` (inventory), `internal/agui/settings_api.go` (`settingPutDTO`; the reconcile call in `handlePutSetting` and `handlePutLLMProfile`)
- Modify: `cmd/aura/serve_provisioning_openrouter.go` (`openRouterMintingAdapter.Patch`), `cmd/aura/serve_agui.go` (`SetOpenRouterKeys`), `cmd/aura/serve.go` (reconcile at boot), `cmd/aura/serve_webui_musr.go` (mount)

**Interfaces:**
- Consumes: `IdentityKeyMinter` and its fakes (Task 8), `authorizeSettingWrite` (Task 9), `llmProfileOverrides` and `settingsMu` (`settings_api.go`).
- Produces: `(*Server).SetOpenRouterKeys(*IdentityKeyMinter)`; `(*Server).EnsureOpenRouterKeys(ctx) (OpenRouterKeysResult, error)`; `OpenRouterKeysResult`; `ErrServicesCapUnset`; `POST /api/admin/openrouter/reconcile` (mounted behind `identity.CapIdentityCreate`); the `openrouter_keys` field in PUT `/api/settings/{key}` and PUT `/api/settings/llm-profile` responses.

- [ ] **Step 1: Write the failing tests.** Add to the fakes in `openrouter_keys_test.go`: the field `patched map[string]openrouterprovision.KeyPatch` on `fakeMinting`, and

```go
func (f *fakeMinting) Patch(_ context.Context, hash string, patch openrouterprovision.KeyPatch) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.patched == nil {
		f.patched = map[string]openrouterprovision.KeyPatch{}
	}
	f.patched[hash] = patch
	return nil
}

func (f *fakeIdentityKeys) Save(ctx context.Context, r identitykey.Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[identityctx.IdentityID(ctx)] = r
	return nil
}
```

`internal/agui/openrouter_reconcile_test.go`:

```go
package agui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identitykey"
)

var routeRows = []sqlc.AuraSettings{
	{Key: "AURA_LLM_PROVIDER", Value: "openrouter"},
	{Key: "AURA_LLM_MODEL", Value: "z-ai/glm-5.3-flash"},
	{Key: "AURA_OPENROUTER_SERVICES_CAP_USD", Value: "20"},
}

func reconcileServer(rows []sqlc.AuraSettings, ids []identity.Identity, admins ...string) (*Server, *fakeMinting, *fakeIdentityKeys, *fakeSettingsStore, *fakeLLMRouteReloader) {
	minting, keys := &fakeMinting{keySet: true}, newFakeIdentityKeys()
	store, reloader := &fakeSettingsStore{rows: rows}, &fakeLLMRouteReloader{}
	caps := adminCaps(admins...)
	caps.identities = ids
	s := &Server{settings: store, llmRouteReloader: reloader, idAdmin: caps}
	s.SetOpenRouterKeys(NewIdentityKeyMinter(minting, keys, caps, billing))
	return s, minting, keys, store, reloader
}

func withServicesKey(rows []sqlc.AuraSettings) []sqlc.AuraSettings {
	return append(slices.Clone(rows), sqlc.AuraSettings{Key: "OPENROUTER_API_KEY", Value: "sk-or-v1-existing", IsSecret: true})
}

func TestReconcileMintsTheServicesKeyAndKeepsTheRoute(t *testing.T) {
	s, minting, _, store, reloader := reconcileServer(routeRows, nil)
	res, err := s.EnsureOpenRouterKeys(context.Background())
	if err != nil {
		t.Fatalf("EnsureOpenRouterKeys: %v", err)
	}
	if res.ServicesLabel != "sk-or-v1-...hash-1" {
		t.Fatalf("services label = %q, want the minted key's label", res.ServicesLabel)
	}
	if req := minting.minted[0]; req.Name != "aura-services" || req.Limit == nil || *req.Limit != 2000 {
		t.Fatalf("services mint = %+v, want aura-services with a 20.00 cap", req)
	}
	prepared := reloader.validated[0]
	if prepared["AURA_LLM_MODEL"] != "z-ai/glm-5.3-flash" || prepared["OPENROUTER_API_KEY"] != "sk-or-v1-hash-1" {
		t.Fatalf("prepared profile = %v, want the persisted route plus the new key", prepared)
	}
	if store.upserted["OPENROUTER_API_KEY"] != "sk-or-v1-hash-1" || len(reloader.applied) != 1 {
		t.Fatalf("stored = %v applied = %d, want the key stored and the profile published once", store.upserted, len(reloader.applied))
	}
}

func TestReconcileWaitsForTheServicesCap(t *testing.T) {
	s, minting, _, _, _ := reconcileServer(routeRows[:2], nil)
	res, err := s.EnsureOpenRouterKeys(context.Background())
	if !errors.Is(err, ErrServicesCapUnset) || len(res.Errors) != 1 || len(minting.minted) != 0 {
		t.Fatalf("result = %+v err = %v mints = %d; want ErrServicesCapUnset and no mint", res, err, len(minting.minted))
	}
}

func TestReconcileRevokesTheServicesKeyWhenTheProfileIsRejected(t *testing.T) {
	s, minting, _, store, reloader := reconcileServer(routeRows, nil)
	reloader.err = errors.New("model not found")
	if _, err := s.EnsureOpenRouterKeys(context.Background()); err == nil {
		t.Fatal("a rejected profile passed silently")
	}
	if len(minting.revoked) != 1 || minting.revoked[0] != "hash-1" {
		t.Fatalf("revoked = %v, want the unrecorded services key", minting.revoked)
	}
	if _, stored := store.upserted["OPENROUTER_API_KEY"]; stored {
		t.Fatal("the services key was stored although the profile was rejected")
	}
}

func TestReconcileLeavesAnExistingServicesKey(t *testing.T) {
	s, minting, _, _, _ := reconcileServer(withServicesKey(routeRows), nil)
	if _, err := s.EnsureOpenRouterKeys(context.Background()); err != nil {
		t.Fatalf("EnsureOpenRouterKeys: %v", err)
	}
	if len(minting.minted) != 0 {
		t.Fatalf("mints = %+v, want none", minting.minted)
	}
}

func TestReconcileMintsEveryActiveUserIdentity(t *testing.T) {
	ids := []identity.Identity{
		{ID: "admin-1", Kind: "user"}, {ID: "member-1", Kind: "user"},
		{ID: "aura-cli", Kind: "service"}, {ID: "gone-1", Kind: "user", Deactivated: true},
	}
	s, _, keys, _, _ := reconcileServer(withServicesKey(routeRows), ids, "admin-1")
	res, err := s.EnsureOpenRouterKeys(context.Background())
	if err != nil {
		t.Fatalf("EnsureOpenRouterKeys: %v", err)
	}
	if !slices.Equal(res.IdentitiesMinted, []string{"admin-1", "member-1"}) {
		t.Fatalf("identities minted = %v, want admin-1 and member-1 only", res.IdentitiesMinted)
	}
	if keys.records["admin-1"].LimitUSD != nil {
		t.Fatal("the admin's key has a cap, want none")
	}
	if got := keys.records["member-1"].LimitUSD; got == nil || *got != 0 {
		t.Fatal("the member's key is not at a zero cap")
	}
}

func TestReconcileAlignsLimitsWithRoles(t *testing.T) {
	ids := []identity.Identity{{ID: "admin-1", Kind: "user"}, {ID: "demoted-1", Kind: "user"}}
	s, minting, keys, _, _ := reconcileServer(withServicesKey(routeRows), ids, "admin-1")
	keys.records["admin-1"] = identitykey.Record{Key: "k1", Hash: "hash-admin", LimitUSD: capUSD(0)}
	keys.records["demoted-1"] = identitykey.Record{Key: "k2", Hash: "hash-demoted"}

	res, err := s.EnsureOpenRouterKeys(context.Background())
	if err != nil {
		t.Fatalf("EnsureOpenRouterKeys: %v", err)
	}
	if !minting.patched["hash-admin"].ClearLimit || keys.records["admin-1"].LimitUSD != nil {
		t.Fatal("the admin's zero cap was not cleared")
	}
	if patch := minting.patched["hash-demoted"]; patch.Limit == nil || *patch.Limit != 0 {
		t.Fatal("the demoted identity's key was not put back to a zero cap")
	}
	if !slices.Equal(res.LimitsAligned, []string{"admin-1", "demoted-1"}) {
		t.Fatalf("limits aligned = %v, want both", res.LimitsAligned)
	}
}

func TestReconcileWaits(t *testing.T) {
	for name, tc := range map[string]struct {
		keySet, routeBills bool
		want               string
	}{
		"local route":           {keySet: true, routeBills: false, want: skipLocalRoute},
		"no management key yet": {keySet: false, routeBills: true, want: skipManagementKeyUnset},
	} {
		t.Run(name, func(t *testing.T) {
			s, minting, _, _, _ := reconcileServer(routeRows, []identity.Identity{{ID: "admin-1", Kind: "user"}}, "admin-1")
			minting.keySet = tc.keySet
			s.keyMinter.routeBills = func() bool { return tc.routeBills }
			res, err := s.EnsureOpenRouterKeys(context.Background())
			if err != nil || res.Skipped != tc.want || len(minting.minted) != 0 {
				t.Fatalf("result = %+v err = %v mints = %d; want skipped %q and nothing minted", res, err, len(minting.minted), tc.want)
			}
		})
	}
}

func TestReconcileKeepsGoingAfterOneIdentityFails(t *testing.T) {
	ids := []identity.Identity{{ID: "broken-1", Kind: "user"}, {ID: "member-1", Kind: "user"}}
	s, minting, _, _, _ := reconcileServer(withServicesKey(routeRows), ids)
	minting.failFor = map[string]error{"broken-1": errors.New("provider 500")}
	res, err := s.EnsureOpenRouterKeys(context.Background())
	if err == nil || len(res.Errors) != 1 {
		t.Fatalf("result = %+v err = %v, want one reported error", res, err)
	}
	if !slices.Equal(res.IdentitiesMinted, []string{"member-1"}) {
		t.Fatalf("identities minted = %v, want member-1 despite broken-1", res.IdentitiesMinted)
	}
}

func TestSavingTheManagementKeyRunsTheReconciler(t *testing.T) {
	s, _, keys, _, _ := reconcileServer(withServicesKey(routeRows), []identity.Identity{{ID: "admin-1", Kind: "user"}}, "admin-1")
	rr, r := putReq(t, "AURA_OPENROUTER_MANAGEMENT_KEY", "sk-or-v1-mgmt", "admin-1")
	s.handlePutSetting(rr, r)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"openrouter_keys"`) {
		t.Fatalf("status = %d body = %s, want 200 with the reconcile result", rr.Code, rr.Body.String())
	}
	if _, minted := keys.records["admin-1"]; !minted {
		t.Fatal("saving the management key did not mint the admin's key")
	}
}

func TestReconcileEndpointReportsTheResult(t *testing.T) {
	s, _, _, _, _ := reconcileServer(routeRows, nil)
	rr := httptest.NewRecorder()
	s.handleReconcileOpenRouterKeys(rr, httptest.NewRequest(http.MethodPost, "/api/admin/openrouter/reconcile", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"services_label":"sk-or-v1-...hash-1"`) {
		t.Fatalf("status = %d body = %s, want 200 with the services label", rr.Code, rr.Body.String())
	}
}
```

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./internal/agui/ -run 'Reconcile|SavingTheManagementKey'`
Expected: build failure (`EnsureOpenRouterKeys`, `alignLimit` undefined).

- [ ] **Step 3: Extend the minter.** In `openrouter_keys.go`, add `Patch(ctx context.Context, hash string, patch openrouterprovision.KeyPatch) error` to `OpenRouterMinting`, add `Save(ctx context.Context, r identitykey.Record) error` to `identityKeyStore`, and add:

```go
// alignLimit keeps a key's limit in step with its owner's role: an admin's key has no limit,
// and an identity that is no longer an admin goes back to a zero cap (CRED-02). The role
// changes only through `aura identity grant|revoke` on the host — the admin API refuses
// administrative capabilities — so the reconciler converges it rather than a handler. The
// provider is patched first because it is what enforces the limit; a store write that then
// fails is corrected by the next run.
func (m *IdentityKeyMinter) alignLimit(ctx context.Context, identityID string) (bool, error) {
	scoped := identityctx.WithIdentityID(ctx, identityID)
	rec, err := m.keys.Load(scoped)
	if err != nil {
		return false, err
	}
	admin, err := m.caps.HasCapability(ctx, identityID, identity.CapIdentityCreate)
	if err != nil {
		return false, err
	}
	var patch openrouterprovision.KeyPatch
	switch {
	case admin && rec.LimitUSD != nil:
		patch.ClearLimit = true
		rec.LimitUSD = nil
	case !admin && rec.LimitUSD == nil:
		patch.Limit = new(openrouterprovision.USDCap)
		rec.LimitUSD = new(float64)
	default:
		return false, nil
	}
	if err := m.minting.Patch(ctx, rec.Hash, patch); err != nil {
		return false, err
	}
	return true, m.keys.Save(scoped, rec)
}
```

In `cmd/aura/serve_provisioning_openrouter.go`:

```go
func (a openRouterMintingAdapter) Patch(ctx context.Context, hash string, patch openrouterprovision.KeyPatch) error {
	managementKey, err := a.key(ctx)
	if err != nil {
		return err
	}
	_, err = openrouterprovision.PatchKey(ctx, a.client, a.baseURL, managementKey, hash, patch)
	return err
}
```

- [ ] **Step 4: Implement the reconciler.** `internal/agui/openrouter_reconcile.go`:

```go
package agui

// openrouter_reconcile.go is EnsureOpenRouterKeys: it mints every OpenRouter key the
// deployment is missing and aligns each key's limit with its owner's role. It is idempotent,
// so boot, the settings writes that can make minting possible, and the admin endpoint all run
// the same thing.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/chetto1983/aura/internal/openrouterprovision"
)

const (
	servicesKeySetting  = "OPENROUTER_API_KEY"
	servicesCapSetting  = "AURA_OPENROUTER_SERVICES_CAP_USD"
	servicesKeyName     = "aura-services"
	reconcileActor      = "aura-reconciler"
	serviceIdentityKind = "service" // migration 0049: principals that can never log in
)

// reconcileTriggerKeys are the settings whose write can make minting possible.
var reconcileTriggerKeys = map[string]struct{}{
	"AURA_OPENROUTER_MANAGEMENT_KEY": {},
	servicesCapSetting:               {},
	"AURA_LLM_PROVIDER":              {},
	"AURA_LLM_BASE_URL":              {},
}

// ErrServicesCapUnset keeps the services key unminted until the admin picks its monthly cap.
var ErrServicesCapUnset = errors.New("openrouter reconcile: the services key's monthly cap is not set")

// OpenRouterKeysResult is what one run did, for the first-run wizard, the settings writes and
// the log.
type OpenRouterKeysResult struct {
	Skipped          string   `json:"skipped,omitempty"`
	ServicesLabel    string   `json:"services_label,omitempty"`
	IdentitiesMinted []string `json:"identities_minted"`
	LimitsAligned    []string `json:"limits_aligned"`
	Errors           []string `json:"errors,omitempty"`
}

// SetOpenRouterKeys wires the reconciler. Until it is set, nothing is minted.
func (s *Server) SetOpenRouterKeys(minter *IdentityKeyMinter) { s.keyMinter = minter }

func (s *Server) registerOpenRouterKeysRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/admin/openrouter/reconcile", s.handleReconcileOpenRouterKeys)
}

// EnsureOpenRouterKeys runs the reconciler under settingsMu.
func (s *Server) EnsureOpenRouterKeys(ctx context.Context) (OpenRouterKeysResult, error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return s.ensureOpenRouterKeysLocked(ctx)
}

// ensureOpenRouterKeysLocked is the body. The settings handlers call it while they still hold
// settingsMu, right after a write that can make minting possible.
func (s *Server) ensureOpenRouterKeysLocked(ctx context.Context) (OpenRouterKeysResult, error) {
	res := OpenRouterKeysResult{IdentitiesMinted: []string{}, LimitsAligned: []string{}}
	if s.keyMinter == nil || s.settings == nil || s.llmRouteReloader == nil || s.idAdmin == nil {
		return res, nil
	}
	skip, err := s.keyMinter.readiness(ctx)
	if err != nil || skip != "" {
		res.Skipped = skip
		return res, err
	}
	var errs []error
	label, err := s.ensureServicesKeyLocked(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("services key: %w", err))
	}
	res.ServicesLabel = label
	ids, err := s.idAdmin.ListIdentities(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("list identities: %w", err))
	}
	for _, idn := range ids {
		if idn.Kind == serviceIdentityKind || idn.Deactivated {
			continue
		}
		if err := s.reconcileIdentity(ctx, idn.ID, &res); err != nil {
			errs = append(errs, fmt.Errorf("identity %s: %w", idn.ID, err))
		}
	}
	for _, err := range errs {
		res.Errors = append(res.Errors, SanitizeString(err.Error()))
	}
	return res, errors.Join(errs...)
}

// reconcileIdentity mints the identity's key, or aligns an existing one with its role.
func (s *Server) reconcileIdentity(ctx context.Context, identityID string, res *OpenRouterKeysResult) error {
	_, created, err := s.keyMinter.ensure(ctx, identityID, identityID)
	if err != nil {
		return err
	}
	if created {
		res.IdentitiesMinted = append(res.IdentitiesMinted, identityID)
		return nil
	}
	aligned, err := s.keyMinter.alignLimit(ctx, identityID)
	if err != nil {
		return err
	}
	if aligned {
		res.LimitsAligned = append(res.LimitsAligned, identityID)
		if s.credit != nil && s.credit.invalidate != nil {
			s.credit.invalidate.Invalidate(identityID)
		}
	}
	return nil
}

// ensureServicesKeyLocked mints the aura-services key when the settings hold none. It writes
// the key the way a settings PUT does: Prepare with the whole persisted profile, so the route
// is kept (Prepare resets every profile key it is not given), then ReplaceMany, then apply.
// The new key is revoked if either step fails, so a key the deployment never recorded does not
// stay live at the provider.
func (s *Server) ensureServicesKeyLocked(ctx context.Context) (string, error) {
	rows, err := s.settings.List(ctx)
	if err != nil {
		return "", err
	}
	values := make(map[string]string, len(rows))
	for _, row := range rows {
		values[row.Key] = strings.TrimSpace(row.Value)
	}
	if values[servicesKeySetting] != "" {
		return "", nil
	}
	if values[servicesCapSetting] == "" {
		return "", ErrServicesCapUnset
	}
	limit, err := openrouterprovision.NewUSDCapFromString(values[servicesCapSetting])
	if err != nil {
		return "", fmt.Errorf("services cap: %w", err)
	}
	minted, err := s.keyMinter.minting.Mint(ctx, openrouterprovision.MintRequest{
		IdentityID: servicesKeyName, Name: servicesKeyName, Limit: &limit, LimitReset: openrouterprovision.LimitResetMonthly,
	})
	if err != nil {
		return "", err
	}
	overrides := llmProfileOverrides(rows)
	overrides[servicesKeySetting] = minted.Key
	apply, err := s.llmRouteReloader.Prepare(ctx, overrides, nil)
	if err == nil {
		_, err = s.settings.ReplaceMany(ctx, map[string]string{servicesKeySetting: minted.Key}, nil, reconcileActor)
	}
	if err != nil {
		s.keyMinter.revokeUnrecorded(ctx, minted.Record.Hash)
		return "", err
	}
	apply()
	return minted.Record.Label, nil
}

// handleReconcileOpenRouterKeys runs the reconciler on demand and returns what it did, so the
// first-run wizard can show the new keys or the provider's error. The parent mux gates it on
// identity.create.
func (s *Server) handleReconcileOpenRouterKeys(w http.ResponseWriter, r *http.Request) {
	if s.keyMinter == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "OpenRouter key minting not configured"})
		return
	}
	res, err := s.EnsureOpenRouterKeys(r.Context())
	if err != nil {
		slog.Warn("openrouter reconcile", "err", err)
	}
	writeJSON(w, res)
}

// reconcileAfterSettingsWrite runs the reconciler inside a settings handler, which already holds
// settingsMu, when one of keys can make minting possible. A failure is logged and reported in
// the result; the write itself stands.
func (s *Server) reconcileAfterSettingsWrite(ctx context.Context, keys ...string) *OpenRouterKeysResult {
	if s.keyMinter == nil || !touchesReconcileTrigger(keys) {
		return nil
	}
	res, err := s.ensureOpenRouterKeysLocked(ctx)
	if err != nil {
		slog.Warn("openrouter reconcile after a settings write", "err", err)
	}
	return &res
}

func touchesReconcileTrigger(keys []string) bool {
	for _, key := range keys {
		if _, ok := reconcileTriggerKeys[key]; ok {
			return true
		}
	}
	return false
}
```

In `settings_api.go`, add next to the other DTOs:

```go
// settingPutDTO is a PUT's answer: the row, plus what the reconciler did when the write could
// make minting possible (the management key, the services cap, the route).
type settingPutDTO struct {
	settingItemDTO
	OpenRouterKeys *OpenRouterKeysResult `json:"openrouter_keys,omitempty"`
}
```

`handlePutSetting`'s last line becomes `writeJSON(w, settingPutDTO{settingItemDTO: item, OpenRouterKeys: s.reconcileAfterSettingsWrite(r.Context(), key)})`. `handlePutLLMProfile`'s last statement becomes:

```go
	resp := map[string]any{"updated": len(body.Settings), "restart_required": false}
	if keys := s.reconcileAfterSettingsWrite(r.Context(), slices.Collect(maps.Keys(body.Settings))...); keys != nil {
		resp["openrouter_keys"] = keys
	}
	writeJSONStatus(w, http.StatusOK, resp)
```

In `server.go`, add the field `keyMinter *IdentityKeyMinter // nil until SetOpenRouterKeys (openrouter_reconcile.go)` next to `credit`, and call `s.registerOpenRouterKeysRoutes(mux)` right after `s.registerSpendOverviewRoutes(mux)`. In `idempotency_http.go`, next to the credit entry: `"POST /api/admin/openrouter/reconcile": httpMutationMeta("openrouter_reconcile"),`.

- [ ] **Step 5: Wire it.** In `cmd/aura/serve_agui.go`, inside the `resolveOpenRouterKeyConfig` block that sets the spend overview:

```go
		aguiServer.SetOpenRouterKeys(agui.NewIdentityKeyMinter(openRouterMintingAdapter{orCfg}, orCfg.store, chat.identity, liveRouteBills(chat)))
```

In `cmd/aura/serve.go`, right after `wireRestartTrigger(aguiServer, requestShutdown)`:

```go
	// Mint whatever keys the deployment is missing: an admin bootstrapped before the management
	// key existed, a route switched while the daemon was down. Off the boot path, because the
	// provider is a network call and a failure only leaves the Credit panel's no-key state.
	go func() {
		if res, err := aguiServer.EnsureOpenRouterKeys(ctx); err != nil {
			slog.Warn("openrouter reconcile at boot", "err", err, "identities_minted", len(res.IdentitiesMinted))
		}
	}()
```

In `cmd/aura/serve_webui_musr.go`, add `adminOpenRouterReconcileRoute = "POST /api/admin/openrouter/reconcile"` to the route constants, and mount it after the remove route:

```go
	// Minting keys spends the deployment's money: identity.create, like the remove route above.
	mux.Handle(adminOpenRouterReconcileRoute, agui.RequireCapability(aguiHandler, auth, identity.CapIdentityCreate))
```

- [ ] **Step 6: Run the tests and watch them pass**

Run: `go test ./internal/agui/ ./cmd/aura/ && go build ./... && wc -l internal/agui/settings_api.go internal/agui/openrouter_reconcile.go`
Expected: PASS, including `TestEveryRegisteredUnsafeHTTPRouteIsClassified`; both files under 600 lines.

- [ ] **Step 7: Race, lint, commit** (`feat(agui): reconcile the deployment's OpenRouter keys`). Body: one idempotent run mints the services key and every missing identity key and aligns limits with roles. It runs at boot, on the settings writes that can make minting possible, and from the admin endpoint the wizard calls; the admin API cannot change `identity.create`, so a role change made on the host is picked up here.

---

### Task 11: A deactivated identity's key stops spending

**Files:**
- Modify: `internal/agui/deprovision.go` (port `OpenRouterKeyDisabler`, field `DeprovisionDeps.KeyDisabler`, a step in `Deactivate`)
- Modify: `cmd/aura/serve_provisioning_openrouter.go` (`openRouterKeyDisableAdapter`, `openRouterKeyDisablerFor`), `cmd/aura/serve_provisioning.go` (`deprovisionDeps`)
- Test: `internal/agui/deprovision_test.go`, `cmd/aura/serve_provisioning_openrouter_test.go`

**Interfaces:**
- Produces: `agui.OpenRouterKeyDisabler{ DisableKey(ctx, identityID string) error }`; `DeprovisionDeps.KeyDisabler`; `openRouterKeyDisablerFor(*chatEnv) agui.OpenRouterKeyDisabler`.

- [ ] **Step 1: Write the failing tests.** Append to `deprovision_test.go`:

```go
type fakeKeyDisabler struct{ disabled []string }

func (f *fakeKeyDisabler) DisableKey(_ context.Context, identityID string) error {
	f.disabled = append(f.disabled, identityID)
	return nil
}

// TestDeprovisionDeactivateDisablesTheOpenRouterKey proves a deactivated identity cannot spend
// through the grace window before the purge revokes its key.
func TestDeprovisionDeactivateDisablesTheOpenRouterKey(t *testing.T) {
	deps, f := fullDeprovisionDeps()
	f.deact.targets[testIdentityID] = targetFor(testIdentityID)
	disabler := &fakeKeyDisabler{}
	deps.KeyDisabler = disabler

	if err := NewDeprovisioner(deps).Deactivate(context.Background(), testIdentityID); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if len(disabler.disabled) != 1 || disabler.disabled[0] != testIdentityID {
		t.Fatalf("disabled = %v, want [%s]", disabler.disabled, testIdentityID)
	}
}
```

Append to `serve_provisioning_openrouter_test.go`:

```go
func TestDisablerIsWiredWithThePool(t *testing.T) {
	chat := &chatEnv{pool: newLazyPool(t), cfg: &config.Config{AuthulaSecret: validProvisioningAuthulaSecret}}
	if openRouterKeyDisablerFor(chat) == nil {
		t.Fatal("openRouterKeyDisablerFor: want a port whenever the pool and AURA_AUTHULA_SECRET exist")
	}
	if openRouterKeyDisablerFor(nil) != nil {
		t.Fatal("openRouterKeyDisablerFor(nil): want nil")
	}
}
```

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./internal/agui/ -run Deactivate && go test ./cmd/aura/ -run Disabler`
Expected: build failure (`KeyDisabler`, `openRouterKeyDisablerFor` undefined).

- [ ] **Step 3: Implement.** In `deprovision.go`, next to `OpenRouterKeyRevoker`:

```go
// OpenRouterKeyDisabler switches off a deactivated identity's OpenRouter key at the provider,
// so it cannot spend through the grace window before the purge revokes it. An identity with no
// key is a success.
type OpenRouterKeyDisabler interface {
	DisableKey(ctx context.Context, identityID string) error
}
```

Add `KeyDisabler OpenRouterKeyDisabler` to `DeprovisionDeps` after `OpenRouterKey`. Inside `Deactivate`'s step function, after the `Jobs` block:

```go
		if d.deps.KeyDisabler != nil {
			if err := d.deps.KeyDisabler.DisableKey(ctx, identityID); err != nil {
				return err
			}
		}
```

Add to `Deactivate`'s doc comment: "…terminates the identity's background jobs and disables its OpenRouter key." In `serve_provisioning_openrouter.go`:

```go
// openRouterKeyDisableAdapter satisfies agui.OpenRouterKeyDisabler: it looks the identity's
// key up and PATCHes it disabled. No stored key means nothing to disable.
type openRouterKeyDisableAdapter struct{ openRouterKeyConfig }

func (a openRouterKeyDisableAdapter) DisableKey(ctx context.Context, identityID string) error {
	rec, err := a.store.Load(identityctx.WithIdentityID(ctx, identityID))
	if errors.Is(err, identitykey.ErrNoKey) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("openrouter key disabler: load key for %s: %w", identityID, err)
	}
	managementKey, err := a.key(ctx)
	if err != nil {
		return err
	}
	disabled := true
	_, err = openrouterprovision.PatchKey(ctx, a.client, a.baseURL, managementKey, rec.Hash, openrouterprovision.KeyPatch{Disabled: &disabled})
	return err
}

// openRouterKeyDisablerFor builds the deactivation port; nil under the same conditions as the
// revoker.
func openRouterKeyDisablerFor(chat *chatEnv) agui.OpenRouterKeyDisabler {
	cfg, ok := resolveOpenRouterKeyConfig(chat)
	if !ok {
		return nil
	}
	return openRouterKeyDisableAdapter{cfg}
}
```

In `serve_provisioning.go`'s `deprovisionDeps`, add `KeyDisabler: openRouterKeyDisablerFor(chat),` after `OpenRouterKey: revoker,`.

- [ ] **Step 4: Run the tests and watch them pass**

Run: `go test ./internal/agui/ ./cmd/aura/ && go build ./...`
Expected: PASS.

- [ ] **Step 5: Race, lint, commit** (`feat(agui): disable a deactivated identity's OpenRouter key`). Body: a deactivated identity's key stayed live through the grace window; there is no reactivation path, so nothing re-enables it.

---

### Task 12: Web and Telegram turns are billed to the identity

**Files:**
- Modify: `internal/runner/runner_llm_runtime.go` (`SetIdentityLLM`), `cmd/aura/serve.go` (the call right after `bootServeChatEnv`)
- Modify: `internal/runner/runner_identity_llm.go` (`ErrNoIdentityLLMKey` text), `cmd/aura/llm_client.go` (`llmNotConfiguredHint` and the literal payload beside it), `internal/llm/config.go` (`ErrMissingAPIKey` text)
- Test: `internal/runner/runner_llm_runtime_test.go`

**Interfaces:**
- Consumes: the live-route resolver (Task 4), keys minted by Tasks 8 and 10.
- Produces: `(*runner.Runner).SetIdentityLLM(*runner.IdentityLLMResolver)`.

- [ ] **Step 1: Write the failing tests.** Append to `runner_llm_runtime_test.go`:

```go
func TestSetIdentityLLMRoutesTurnsThroughTheResolver(t *testing.T) {
	t.Parallel()
	loader := newFakeKeyLoader(map[string]identitykey.Record{"id-a": {Key: "key-a", LimitUSD: capUSD(5)}})
	runtime := llm.NewRuntime(&fakeIdentityScopedClient{label: "services"}, llm.Config{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", Model: "m", APIKey: "services-key"})
	r := &Runner{runtime: runtime}
	r.SetIdentityLLM(NewIdentityLLMResolver(loader, runtime, llm.Config{}, fakeClientFactory(), nil))

	snap, err := r.turnLLMSnapshot(identityctx.WithIdentityID(context.Background(), "id-a"))
	if err != nil || snap.Config.APIKey != "key-a" {
		t.Fatalf("turn key = %q, err %v; want the identity's own key", snap.Config.APIKey, err)
	}
}

func TestSetIdentityLLMWithNilKeepsTheProcessClient(t *testing.T) {
	t.Parallel()
	r := &Runner{runtime: llm.NewRuntime(nil, llm.Config{Model: "process"})}
	r.SetIdentityLLM(nil)
	if r.identityLLM != nil {
		t.Fatal("a nil resolver was boxed into a non-nil interface")
	}
	snap, err := r.turnLLMSnapshot(identityctx.WithIdentityID(context.Background(), "anyone"))
	if err != nil || snap.Config.Model != "process" {
		t.Fatalf("snapshot model = %q, err %v; want the process runtime", snap.Config.Model, err)
	}
}
```

(add the `identitykey` import to the test file if it lacks it).

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./internal/runner/ -run SetIdentityLLM`
Expected: build failure (`SetIdentityLLM` undefined).

- [ ] **Step 3: Implement.** In `runner_llm_runtime.go`:

```go
// SetIdentityLLM makes every later turn resolve its credential from the identity that owns it.
// The serve composition root calls it once, before the listener opens; `aura chat` never does,
// so the REPL keeps the process-wide client (runner_deps.go). A nil resolver is stored as a nil
// interface, never a non-nil interface around a nil pointer.
func (r *Runner) SetIdentityLLM(resolver *IdentityLLMResolver) {
	if resolver == nil {
		r.identityLLM = nil
		return
	}
	r.identityLLM = resolver
}
```

In `cmd/aura/serve.go`, right after the `bootServeChatEnv` error check:

```go
	// Web and Telegram turns are billed to the identity that owns them, never to the services
	// key; `aura chat` keeps the process-wide client because it never comes through here.
	chat.run.SetIdentityLLM(buildIdentityLLMResolver(chat))
```

Point the three refusals at the setup instead of `.env`:

```go
// runner_identity_llm.go
var ErrNoIdentityLLMKey = errors.New("runner: identity has no OpenRouter key yet; an admin connects OpenRouter in the first-run setup to mint it")
```

```go
// cmd/aura/llm_client.go — the const and the literal payload next to it
llmNotConfiguredHint = "connect OpenRouter or pick a local route in Settings, then retry"
```

```go
// internal/llm/config.go
var ErrMissingAPIKey = errors.New("llm: API key is empty (connect OpenRouter in the first-run setup, or pick a local route)")
```

Then run `grep -rn "set OPENROUTER_API_KEY in .env" --include=*.go --include=*.ts --include=*.tsx cmd internal web/src` and update every remaining copy.

- [ ] **Step 4: Run the tests and watch them pass**

Run: `go test ./internal/runner/ ./internal/llm/ ./cmd/aura/ && go build ./...`
Expected: PASS.

- [ ] **Step 5: Race, lint, commit** (`feat(runner): bill web and Telegram turns to the identity's own key`). Body: `runner.Deps.IdentityLLM` was never assigned, so every web and Telegram turn spent the deployment key and spend could not be split per person; the refusal texts stop telling the operator to edit `.env`.

---

### Task 13: Gates, then the E2E on the running stack

Plan D reinstalls this PC. This E2E runs on the current stack first, so Plan A is proven before anything is wiped. The first-run wizard does not exist yet (Plan B), so the admin works through the existing Settings page and the browser console.

- [ ] **Step 1: Quality gates** (WSL, stack up):

```bash
MSYS_NO_PATHCONV=1 wsl.exe -- bash -lc 'cd /mnt/d/Repo/Aura && export PATH="$HOME/.local/bin:$HOME/go/bin:/usr/local/go/bin:$PATH" && make quality'
```

Expected: vet, file-size, lint, deadcode, race and vuln all green.

- [ ] **Step 2: Integration tiers and coverage**

```bash
MSYS_NO_PATHCONV=1 wsl.exe -- bash -lc 'cd /mnt/d/Repo/Aura && export PATH="$HOME/.local/bin:$HOME/go/bin:/usr/local/go/bin:$PATH" && bash scripts/coverage_docker.sh'
```

Expected: the aggregate floor of 85% and every package policy pass. If a touched package's denominator changed, follow the script's instructions to re-pin its baseline, never lowering a floor, and say so in the commit.

- [ ] **Step 3: Mutation spot-check** (≥70% killed per critical file). Run `go-mutesting` in WSL on `internal/identitykey/policy.go` (the `credit_policy` scope of `scripts/critical_mutation_gate.py`) and on `internal/agui/openrouter_keys.go`. Record the killed/total counts for the final report.

- [ ] **Step 4: Rebuild and restart `aura`**

```bash
docker compose build aura && docker compose up -d --no-deps aura
docker inspect -f '{{.State.Health.Status}}' aura
docker logs --since 5m aura 2>&1 | grep -iE 'openrouter reconcile|settings secrets|panic'
```

Expected: `healthy`, and no `panic`. A pre-existing plaintext secret row is converted silently by the boot pass.

- [ ] **Step 5: The admin sets the management key.** The operator, logged in as the admin: Settings → Model routing → paste the management key → save (the field still exists until Plan B). In the browser's network tab, the PUT response carries `openrouter_keys` with the admin's id in `identities_minted`, and `errors` naming the missing services cap.

- [ ] **Step 6: Set the services cap and reconcile.** In the same browser tab's console:

```js
const put = (key, value) => fetch(`/api/settings/${key}`, {method: 'PUT', headers: {'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID()}, body: JSON.stringify({value})}).then(r => r.json());
await put('AURA_OPENROUTER_SERVICES_CAP_USD', '10');
await fetch('/api/admin/openrouter/reconcile', {method: 'POST', headers: {'Idempotency-Key': crypto.randomUUID()}}).then(r => r.json());
```

Expected: `services_label` is set and `errors` is absent. Then restart Aura (Settings banner, or `POST /api/admin/restart`) so TTS/STT and cloud embeddings pick up the services key (decision 8).

- [ ] **Step 7: Check the provider**

```bash
curl -sS https://openrouter.ai/api/v1/keys -H "Authorization: Bearer $(cat "$SCRATCH/mkey")" \
  | python -c "import json,sys;[print(k['name'], k.get('external_user'), k['limit'], k.get('limit_reset')) for k in json.load(sys.stdin)['data']]"
```

Expected: one row named after the admin's identity id, with `external_user` equal to that id and `limit` `None`; one row `aura-services` with `limit` `10`.

- [ ] **Step 8: Check the credit API.** In the console, with `id` set to the `identity_id` that `GET /api/me` returns: `await fetch('/api/admin/identities/' + id + '/credit').then(r => r.json())` returns 200 with `unlimited: true`. The panel's rendering of "no limit" is Plan B's job.

- [ ] **Step 9: One chat turn, billed to the admin.** Send one message in the web chat as the admin. Wait about 60 seconds, then:

```bash
curl -sS -X POST https://openrouter.ai/api/v1/analytics/query -H "Authorization: Bearer $(cat "$SCRATCH/mkey")" -H "Content-Type: application/json" \
  -d "{\"metrics\":[\"request_count\",\"total_usage\"],\"dimensions\":[\"api_key_id\"],\"time_range\":{\"start\":\"$(date -u +%Y-%m-%dT00:00:00Z)\",\"end\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}}"
```

Expected: a row whose `api_key_id` is the admin's key name (the identity id) with at least one request. Analytics lag 30-40 seconds (M-07); retry once before concluding anything.

- [ ] **Step 10: Record and clean up.** Upsert the measured facts to aura-memory (or put them in the push summary if the MCP is down), each with what it does NOT prove: the wizard, the installer, the reinstall and the `.env` changes are Plans B-D. Delete `$SCRATCH/mkey`.

- [ ] **Step 11: Push and watch CI**

```bash
git push
gh run list --limit 5
gh run watch "$(gh run list --limit 1 --json databaseId --jq '.[0].databaseId')" --exit-status
```

Expected: every workflow green. A red job is fixed at its root and pushed again, never retried blind.

---

## Spec coverage (Plan A)

| Spec item | Task |
|---|---|
| Management key read at call time; the five boot captures | 4 (`creditBackendBills`), 7 |
| Services key minted by Aura, monthly cap, API refuses it | 10, 9 |
| Per-identity keys; admin no limit, members zero; service identities skipped | 8, 10 |
| Secret settings encrypted in place (`enc:v1:`), boot convergence | 5 |
| `OverlayEnv` skips secrets; readers use the store; npx env scrubbed | 6 |
| Admin-only credential, route and model writes | 9 |
| Resolver follows the live route and model | 4 |
| Reconciler: triggers, insert-if-absent, revoke unrecorded keys, errors reported | 8, 10 |
| Role and limit alignment (deviation 1) | 10 |
| Deactivation disables the key (deviation 3) | 11 |
| `IdentityLLM` on the serve path; `aura chat` unchanged | 12 |
| NULL cap: migration, `Decide`, wire types, credit clear-cap, over-allocation | 2, 3 |
| Refusal texts point at the setup | 12 |
| `"limit": null` measured first | 1 |
| Wizard, Credit panel UI | Plan B |
| Installer, compose, `.env.example`, `AURA_EMBED_DIMENSIONS`, payload manifest, `.env` guard test | Plan C |
| Reinstall and the full E2E | Plan D |

