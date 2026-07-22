# Phase 40: Security & Supply-Chain Pack - Pattern Map

**Mapped:** 2026-07-22
**Files analyzed:** 43 (11 new, 32 modified — 6 of which are markdown/YAML config, not Go)
**Analogs found:** 43 / 43 (every file has at least a role-match analog; most touched Go files are their OWN best style-analog via a sibling function in the same file)

This phase is seven independent hardening seams (SEC-01..06, SEC-09). There is no single dominant analog — each seam gets its own 1-3 closest matches. Where CONTEXT.md/RESEARCH.md already pinned an exact analog with verified line numbers, this document re-confirms it against HEAD and extracts the excerpt; it does not re-litigate the choice.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `internal/gateway/injection_suite_test.go` (new) | test | request-response (deny) | `internal/gateway/classify_test.go` (`TestClassifyTable`) | exact |
| `internal/eval/injection_cot_eval.go` + `_test.go` (new) | service/dataset | event-driven (LLM eval) | `internal/eval/dataset_cot_eval.go` + `skills_cot_eval_test.go` | exact |
| `internal/agui/strict_decode.go` + `_test.go` (new) | middleware/utility | request-response | `internal/agui/governance_write_api.go` (`decodeMCPBody`) — **anti-pattern**, do not copy verbatim | role-match, corrected |
| `internal/tracesink/sink.go` + `_test.go` (new) | service/utility | file-I/O | `internal/objectstore/identity_store.go` (`encrypt`/`decrypt`/`deriveObjectStoreKey`) | exact (unexported → rewrite) |
| `internal/secret/redact_exact.go` + `_test.go` (new) | utility | transform | `internal/secret/envkey.go` (`IsSecretEnvKey`/`IsSecretEnvVar`) | exact (sibling in same package) |
| `scripts/workflow_pin_gate.sh` + `_test.sh` (new) | config/CI-gate | batch | `scripts/quality_snapshot_gate.sh` + `_test.sh` | exact |
| `internal/agui/recovery_hash.go` | utility/crypto | transform | itself — sibling `hashArgon2id`/`HashOpaqueSecret` in the same file; keying idiom from `internal/objectstore/identity_store.go` | exact |
| `internal/agui/recovery_hash_test.go` | test | transform | itself — `TestRecoveryHasherHashVerify` in the same file | exact |
| `cmd/aura/identity.go` | CLI/controller | request-response | itself — `identityRecoverOperator` sibling call already passes `cfg` (:63) | exact |
| `cmd/aura/recovery.go` | CLI/service | CRUD | `cmd/aura/serve_password_reset.go` (`mintPasswordResetToken`, same challenge/token shape) | exact |
| `cmd/aura/serve_password_reset.go` | service/wiring | CRUD | itself — `recoveryStoreAdapter`/`wirePasswordResetService` | exact |
| `internal/agui/password_reset.go` | service | CRUD | itself — `PasswordResetDeps`/`PasswordResetService` struct | exact |
| `cmd/aura/serve.go` | controller/wiring | request-response | itself — `wirePasswordResetService(...)` call + `agui.ServerConfig{...}` literal | exact |
| `internal/conversations/store_append.go` | service/store | CRUD | itself — `appendTurnWrites`/`postgresTextSafe` | exact |
| `internal/agent/tools/result.go` | utility | file-I/O | itself — `writeSidecar` | exact |
| `internal/reasoningtrace/reasoningtrace.go` | service | event-driven / file-I/O | itself — `redactString`/`Record`; outbound pattern from `internal/redact/string.go` | exact (bug confirmed) |
| `internal/config/config_validate.go` | config/validation | batch | itself — `gateDestructiveShell` (D-12 template), `gateCORS` (D-14 removal target) | exact |
| `internal/config/config_knobs.go` | config/registry | batch | itself — `AURA_AGUI_CORS_PERMISSIVE` row (removal template + insertion point) | exact |
| `internal/config/config.go` | config | batch | itself — `AGUICORSPermissive`/`AuthulaSecret` fields | exact |
| `internal/agui/server_cors.go` | middleware | request-response | **DELETE** — no replacement analog needed | exact (deletion) |
| `internal/agui/server.go` | controller/router | request-response | itself — `Mux()` withCORS call sites | exact |
| `internal/agui/server_test.go` | test | request-response | itself — `TestServer_CORSPermissive` "off" sub-case becomes `TestNoCORSHeaders` | exact |
| `cmd/aura/integrations_console.go` | CLI/controller | request-response | `internal/config/config.go` (`GuardWebBind`) — the loopback-guard precedent | role-match, strong |
| `cmd/aura/integrations_proxy.go` | middleware | request-response | itself — `pimAdminToken()`/`injectAuth` closure shape | exact |
| `internal/agui/server_run_request.go` | utility/decoder | request-response | new `strict_decode.go` helper (below) | new-helper |
| `internal/agui/approvals_api.go` | controller | request-response | `internal/agui/assets_api.go` (identical `MaxBytesReader`+`Decode` shape) | exact |
| `internal/agui/onboarding_api.go` | controller | request-response | itself — `handleOnboardingMutation[Req,Resp]` generic + `prepareOnboardingMutation` | exact |
| `internal/agui/assets_api.go` | controller | request-response | `internal/agui/approvals_api.go` (identical shape) | exact |
| `internal/agui/governance_write_api.go` | controller | request-response | itself — `decodeMCPBody` (**the bug to fix**, D-16b) | exact, corrected |
| `internal/agui/governance_write_skills_api.go` | controller | request-response | itself — `decodeSkillsBody` (**the bug to fix**, D-16b) | exact, corrected |
| `.github/workflows/ci.yml` | config/CI | batch | itself — already-pinned `golangci-lint@v2.12.2`/`sqlc@v1.31.1` version-pin style | exact |
| `.github/workflows/codeql.yml` | config/CI | batch | `ci.yml`'s `actions/checkout@v7`/`setup-go@v7` pattern | exact |
| `.github/workflows/release.yml` | config/CI | batch | itself — existing `docker/*`/`goreleaser-action` step shape | exact |
| `.github/workflows/skills.yml` | config/CI | batch | `ci.yml`'s `go install …@latest` tool-install pattern (the anti-pattern to fix) | exact |
| `.goreleaser.yaml` | config | batch | GoReleaser official `sboms:` docs (no in-repo analog — `sboms:` block is net-new) | none in-repo |
| `.github/dependabot.yml` | config | batch | itself — `github-actions` group already configured, **no change needed** | exact (no-op) |
| `.planning/REQUIREMENTS.md` | docs | batch | itself — SEC-01/SEC-02/SEC-09 bullet lines (:131,132,139) | exact |
| `.planning/ROADMAP.md` | docs | batch | itself — Phase 40 success criteria 2 and 5 (:734,737) | exact |

## Pattern Assignments

### SEC-04 — `internal/gateway/injection_suite_test.go` (new) (test, deterministic table-driven)

**Analog:** `internal/gateway/classify_test.go` (`TestClassifyTable`) + `internal/gateway/decide_test.go` (`fakeStore`, profile-construction helpers) + `internal/gateway/main_test.go` (goleak wiring).

Write this file **in-package** (`package gateway`, not a sub-package) so it inherits `main_test.go`'s `goleak.VerifyTestMain(m)` for free and matches the existing `decide_test.go`/`approve_test.go`/`classify_test.go` naming convention.

**goleak wiring already covers the package** (`internal/gateway/main_test.go`, full file, 14 lines):
```go
package gateway

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
```

**Table-driven cases shape to copy** (`internal/gateway/classify_test.go:52-90`, `TestClassifyTable`):
```go
cases := []struct {
	name string
	spec tools.Spec
	args json.RawMessage
	want scoring.RiskTier
}{
	{"skill/list", skill, mustArgs(t, map[string]string{"action": "list"}), scoring.Safe},
	{"skill/restore", skill, mustArgs(t, map[string]string{"action": "restore", "name": "x"}), scoring.Normal},
	{"skill/create", skill, mustArgs(t, map[string]string{"action": "create", "name": "x"}), scoring.Risky},
	// ...
}
for _, tc := range cases {
	t.Run(tc.name, func(t *testing.T) {
		got := classify(tc.spec, tc.args)
		if got != tc.want { t.Errorf(...) }
	})
}
```
D-01 drives the SAME shape through `gateway.Decide`, not `classify` directly: build `g := New(config.ProfileServerProduction, nil)`, call `g.Decide(context.Background(), spec, rawArgs, key)`, assert `v.Decision == Deny` (deny corpus) or `Allow` (negative controls).

**Constructor + context-escape-hatch to AVOID in the deny corpus** (`internal/gateway/gateway.go:128-130`, `internal/gateway/approve.go:92-98,103-105`):
```go
func New(profile config.RuntimeProfile, store reservationStore) *Gateway {
	return &Gateway{profile: profile, store: store, approvals: NewGatewayApprovals()}
}
```
```go
// approve.go:92 — a WithResolvedApproval ctx short-circuits to Allow REGARDLESS of profile.
// The deny suite must drive every case with a PLAIN context.Background() to avoid this.
if r, ok := resolvedApproval(ctx); ok && r.Approved {
	return Verdict{Decision: Allow, Tier: tier, OperatorID: r.OperatorID}, nil
}
// approve.go:103 — the actual fail-closed deny the suite asserts:
if g.profile == config.ProfileServerProduction || !responderPresent(ctx) {
	g.recordDegradedDeny(ctx, spec, key, tier)
	return Verdict{Decision: Deny, Tier: tier, Reason: "no interactive approver — action declined"}, nil
}
```

**Mutating-bit ground truth for corpus shapes** (verified zero `Mutating` matches in `web_fetch.go`/`web_search.go`; confirmed present elsewhere):
```go
// internal/agent/tools/shell_exec.go:102-106 — the DENY-able "network" shape (D-03b):
// Conservatively Mutating (D-43): a command line can write files or mutate
// state and the agent cannot tell `ls` from `python build.py` statically.
Mutating:       true,
OperationScope: OperationScopeAgent, OperationNormalizer: OperationNormalizerCanonical,

// internal/agent/mcptools/bridge.go:72,242 — MCP mutating classification:
spec.Mutating = !d.Annotations.ReadOnlyHint
```
```go
// internal/scoring/scoring.go:140 (package internal/scoring, NOT internal/gateway —
// CONTEXT.md's citation "internal/gateway/scoring.go:140" is a path mislabel, line+content are correct)
func GateRecommended(t RiskTier) bool { return t == Risky || t == Destructive }
```

**Runtime profile enum for the `New(...)` argument** (`internal/config/config_runtimeprofile.go:22-32,56-58`):
```go
const (
	ProfileDev                RuntimeProfile = "dev"
	ProfileLocalTrusted       RuntimeProfile = "local_trusted"
	ProfileSingleUserHardened RuntimeProfile = "single_user_hardened"
	ProfileServerProduction   RuntimeProfile = "server_production"
)
func (p RuntimeProfile) Strict() bool {
	return p == ProfileSingleUserHardened || p == ProfileServerProduction
}
```

**Do NOT use as a deny-case:** `web_fetch`/`web_search` are Safe→Allow (SSRF defense lives elsewhere, Phase 31 CAP-05). A Normal-tier-mutating shape (`skill{restore}`, `task{cancel}`) also returns Allow — `reserve.go:36-37` (`beginOperation`, `g.operations == nil` → Allow) and `:172-176` (`reserve`, `g.store == nil` → Allow) are nil-guarded, never panic:
```go
// internal/gateway/reserve.go:35-38
func (g *Gateway) beginOperation(...) (Verdict, bool) {
	if g.operations == nil {
		return Verdict{Decision: Allow, Tier: tier}, true
	}
// internal/gateway/reserve.go:171-176
func (g *Gateway) reserve(...) (Verdict, error) {
	if g.store == nil {
		return Verdict{Decision: Allow, Tier: tier, OperatorID: operatorID}, nil
	}
```

---

### SEC-04 — `internal/eval/injection_cot_eval.go` + `_test.go` (new) (service/dataset, LLM eval tier, D-04)

**Analog:** `internal/eval/dataset_cot_eval.go` (dimension/dataset shape) + `internal/eval/skills_cot_eval_test.go` (live-harness `TestXxxE2E` shape) + `internal/eval/harness_cot_eval_test.go` (the skip idiom) + `internal/eval/doc.go` (package-level tag isolation doc).

**Build-tag + no-CI isolation** (`internal/eval/doc.go`, full file):
```go
// This doc.go carries NO build tag so the package is valid under the default build...
package eval
```
Every dataset/test file in the package itself DOES carry the tag:
```go
//go:build cot_eval
```

**Skip idiom to copy exactly** (`internal/eval/harness_cot_eval_test.go:41-46`):
```go
func TestCoTEval(t *testing.T) {
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		t.Skip("cot_eval: OPENROUTER_API_KEY unset — this is a MANUAL paid gate, NOT a CI job. " +
			"Run locally: set -a; . ./.env; set +a; go test -tags cot_eval -run TestCoTEval -timeout 600s -v ./internal/eval/")
	}
```

**Dataset/dimension shape to mirror** (`internal/eval/dataset_cot_eval.go:28-42`):
```go
type dimension string

const (
	dimSecretRedaction   dimension = "secret_redaction"       // Critical, release-blocking
	dimGuardrail         dimension = "guardrail_refusal"      // asserted
	// injection-resistance tier: add e.g. dimInjectionResistance dimension = "injection_resistance"
)
```

**Source payloads per D-04** (not invented — from `garak promptinject`/`latentinjection` or promptfoo redteam plugins; the deterministic-gate corpus in `injection_suite_test.go` is hand-written and payload-inert, this tier is the opposite: payload TEXT is exactly what is scored).

**NOT CI-blocking** — confirmed nowhere in Makefile/CI: `grep -rn cot_eval Makefile .github/workflows/` returns no hits outside doc comments; this tier stays a human-run gate honoring no-unsolicited-paid-runs.

---

### SEC-09 — `internal/agui/recovery_hash.go` (utility/crypto, edit in place)

**Analog:** itself (the argon2id sibling functions in the same file establish the file's style) + `internal/objectstore/identity_store.go` (the HKDF-subkey idiom to mirror for the pepper derivation).

**Current sink to replace** (`internal/agui/recovery_hash.go:66-70`, full function):
```go
// HashLookupToken hashes only high-entropy random reset tokens, not user-chosen secrets.
func HashLookupToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
```
D-06 requires threading a `pepper []byte` parameter (HMAC-SHA-256 keyed hash) — this changes the signature everywhere it's called (4 call sites below).

**HKDF subkey derivation idiom to mirror exactly** (`internal/objectstore/identity_store.go:192-208`, already stdlib `crypto/hkdf`, Go 1.24+, zero new deps):
```go
const keyDerivationInfo = "aura-objectstore-identity-key-v1" // convention: "aura-<domain>-<purpose>-v1"

func deriveObjectStoreKey(authulaSecretHex string) ([]byte, error) {
	secret := strings.TrimSpace(authulaSecretHex)
	if len(secret) != 64 { // fail CLOSED on malformed input
		return nil, fmt.Errorf("objectstore identity: AURA_AUTHULA_SECRET must be 64 hex characters (32 bytes)")
	}
	raw, err := hex.DecodeString(secret)
	if err != nil {
		return nil, fmt.Errorf("objectstore identity: AURA_AUTHULA_SECRET must be valid hex: %w", err)
	}
	key, err := hkdf.Key(sha256.New, raw, nil, keyDerivationInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("objectstore identity: derive key: %w", err)
	}
	return key, nil
}
```
Recommended label for this phase (domain-separated, `-v1` suffix convention): `"aura-reset-token-pepper-v1"`. Use `crypto/hmac` + `crypto/sha256` for the keyed hash itself (`hmac.New(sha256.New, pepper)`), NOT a second HKDF call — HKDF derives the pepper KEY once at boot; HMAC uses it per-token.

**Existing file style to preserve** (constant-time compare, versioned encoding — `recovery_hash.go:72-90`, argon2id sibling):
```go
func hashArgon2id(secret string) (string, error) {
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(secret), salt[:], 1, 64*1024, 4, 32)
	return fmt.Sprintf("$aura$argon2id$v=19$m=65536,t=1,p=4$%s$%s", ...), nil
}
func verifyArgon2id(secret, encoded string) bool {
	...
	return subtle.ConstantTimeCompare(got, want) == 1
}
```
`HashLookupToken` stays deterministic (unsalted-per-call) BY DESIGN — it backs a `token_hash text PRIMARY KEY` lookup (`internal/db/migrations/0023_identity_recovery.up.sql:28`), which a randomly-salted KDF cannot serve. The pepper is what makes it un-forgeable from a DB leak alone, not a per-call salt.

**All 4 call sites needing the new pepper parameter** (verified at HEAD):
```go
// cmd/aura/serve_password_reset.go:149 (online mint, inside recoveryStoreAdapter.StartPasswordReset-ish tx)
TokenHash:   agui.HashLookupToken(token),

// internal/agui/password_reset.go:318 (online lookup, PasswordResetService.Complete)
tokenHash := HashLookupToken(in.ResetToken)

// cmd/aura/recovery.go:96,104 (break-glass CLI mint — D-06b, the pitfall site)
challenge, err := tq.InsertPasswordResetChallenge(ctx, breakGlassChallengeParams(pgID, agui.HashLookupToken(challengeSecret), now))
if _, err := tq.InsertPasswordResetToken(ctx, breakGlassTokenParams(agui.HashLookupToken(token), consumed.ID, consumed.IdentityID, now)); err != nil {
```

---

### SEC-09 — `cmd/aura/identity.go` + `cmd/aura/recovery.go` (D-06b pepper threading + break-glass guard)

**Analog:** itself — the sibling `recover-operator` case (`identity.go:63`) already threads `cfg` where `recover` (:61) does not; that asymmetry IS the bug and the fix template.

**The gap** (`cmd/aura/identity.go:33-67`, `runIdentity`):
```go
func runIdentity(args []string) {
	...
	cfg := config.LoadDB()          // AuthulaSecret IS populated here via loadBase()
	...
	switch args[0] {
	...
	case "recover":
		identityRecover(ctx, store, pool, args[1:])              // cfg NOT passed — the bug
	case "recover-operator":
		identityRecoverOperator(ctx, pool, cfg, args[1:])         // cfg IS passed — the pattern to mirror
	...
```
`config.LoadDB()` DOES populate the field (`internal/config/config.go:158,471`):
```go
AuthulaSecret string // AURA_AUTHULA_SECRET — 32-byte hex secret Authula derives its HMAC/token keys from
...
AuthulaSecret: os.Getenv("AURA_AUTHULA_SECRET"),
```
Fix: pass `cfg` (or `cfg.AuthulaSecret`) into `identityRecover`, thread it to `mintBreakGlassToken` (`cmd/aura/recovery.go:37,63`), and add a presence guard in `runIdentity`'s `"recover"` branch (`strings.TrimSpace(cfg.AuthulaSecret) == ""` → exit non-zero with a clear message) — mirroring `gateWebAuth`'s existing shape (`internal/config/config_validate.go:252-260`):
```go
func (c *Config) gateWebAuth(p RuntimeProfile) []Violation {
	if !p.Strict() { return nil }
	if strings.TrimSpace(c.AuthulaSecret) == "" {
		return []Violation{{Knob: "AURA_AUTHULA_SECRET", Sev: Fatal, Msg: "web-auth secret is required under " + string(p)}}
	}
	return nil
}
```
Note: there is no way to cryptographically verify cross-process "same value serve uses" — the guard is necessarily "fail if unset", not a true equality check (document this limitation, do not over-engineer).

**`mintBreakGlassToken` signature to extend** (`cmd/aura/recovery.go:63-74`):
```go
func mintBreakGlassToken(ctx context.Context, pool *pgxpool.Pool, identityID string) (string, error) {
	...
	token, err := newPasswordResetToken()
	...
	// The plaintext token handed to the operator; only HashLookupToken(token) is stored.
```
Add a `pepper []byte` parameter here, threaded from `identityRecover`'s new `cfg`/pepper argument.

---

### SEC-09 — `cmd/aura/serve_password_reset.go` + `internal/agui/password_reset.go` (online mint/lookup pepper threading)

**Analog:** itself — `recoveryStoreAdapter`/`wirePasswordResetService`/`PasswordResetDeps`/`PasswordResetService` currently hold only a `*pgxpool.Pool` / store-messenger-resetter-clock; add a `pepper []byte` field alongside.

**Current struct shapes** (`internal/agui/password_reset.go:94-107`):
```go
type PasswordResetDeps struct {
	Store     PasswordResetStore
	Messenger RecoveryMessenger
	Resetter  PasswordResetter
	Clock     func() time.Time
}
type PasswordResetService struct {
	store     PasswordResetStore
	messenger RecoveryMessenger
	resetter  PasswordResetter
	clock     func() time.Time
}
```
**Wiring call site** (`cmd/aura/serve_password_reset.go:467-481`, `wirePasswordResetService`):
```go
func wirePasswordResetService(server passwordResetServer, pool *pgxpool.Pool, deliverer recoveryCodeDeliverer, provider passwordResetCoreProvider) bool {
	...
	server.SetPasswordResetService(agui.NewPasswordResetService(agui.PasswordResetDeps{
		Store:     recoveryStoreAdapter{pool: pool},
		Messenger: telegramRecoveryMessenger{deliverer: deliverer},
		Resetter:  authulaPasswordResetter{core: core, pool: pool},
	}))
	return true
}
```
Called from `cmd/aura/serve.go:517` where `chat.cfg.AuthulaSecret` is already in scope (confirmed via `chat.cfg.AGUICORSPermissive` at `serve.go:359` in the SAME function, `bootServe`) — thread `chat.cfg.AuthulaSecret` (or the derived pepper) into `wirePasswordResetService(...)`'s new parameter, and into `recoveryStoreAdapter{pool: pool}` if the mint (`serve_password_reset.go:149`) needs it there instead of on the service.

**Lookup call site** (`internal/agui/password_reset.go:308-319`, `Complete`):
```go
func (s *PasswordResetService) Complete(ctx context.Context, in PasswordResetCompleteRequest) (PasswordResetCompleteResponse, error) {
	...
	tokenHash := HashLookupToken(in.ResetToken)
	claim, err := s.store.ClaimResetTokenHash(ctx, tokenHash)
```
Becomes `tokenHash := HashLookupToken(in.ResetToken, s.pepper)`.

---

### SEC-01 — `internal/conversations/store_append.go` (D-09/D-11, service/store, edit in place)

**Analog:** itself — `appendTurnWrites` is the verified SINGLE choke-point (one sqlc caller `insertTurnAndAggregates:252`; every `AppendTurn`/`AppendTurnTx`/`AppendAssistantTurnWithCacheMetric`/`ForkBranch` routes through it).

**Insertion point** (`internal/conversations/store_append.go:213-222`, `appendTurnWrites`):
```go
func (s *Store) appendTurnWrites(p AppendTurnParams) (sqlc.InsertConversationTurnParams, sqlc.UpdateConversationAggregatesParams, error) {
	convID, err := parseUUID("conversation_id", p.ConversationID)
	if err != nil {
		return sqlc.InsertConversationTurnParams{}, sqlc.UpdateConversationAggregatesParams{}, fmt.Errorf("append turn: %w", err)
	}
	safeContent := postgresTextSafe(p.Content)   // <-- INSERT THE REDACTOR HERE, wrapping p.Content
	content, sidecarPath, err := s.maybeSpill(p.ConversationID, p.Seq, safeContent)
	...
```
Wrap `p.Content` with the new `internal/secret` exact-match detector BEFORE `postgresTextSafe` (or immediately after — order with the NUL-safety pass is not security-relevant, but doing it once here covers BOTH the inline `content` branch and the `maybeSpill` sidecar branch, per D-09's explicit warning against redacting only one branch).

**The independent length-check that MUST mirror the same redaction** (`internal/conversations/store_append.go:120-124`, `AppendTurnTx`'s pre-write spill-guard):
```go
// Reject a would-be spill BEFORE appendTurnWrites so maybeSpill never writes a sidecar
// this no-cleanup tx path could orphan on rollback (WR-01). The length test mirrors
// maybeSpill exactly (postgresTextSafe applied, compared against turnCapBytes).
if len(postgresTextSafe(p.Content)) > s.turnCapBytes {
	return fmt.Errorf("append turn tx %s seq %d: %w", p.ConversationID, p.Seq, ErrContentSpillUnsupported)
}
```
D-11 fix-on-touch coupling: when the redactor changes the effective byte length at `:218`, this independent recompute at `:123` disagrees at the cap boundary unless mirrored — apply the SAME redaction call here too (or compute length post-redaction once and thread it, whichever keeps the file simplest).

**`maybeSpill`/`postgresTextSafe` being wrapped** (`internal/conversations/store_helpers.go:123-142`):
```go
func (s *Store) maybeSpill(conversationID string, seq int, content string) (pgtype.Text, pgtype.Text, error) {
	if len(content) <= s.turnCapBytes {
		return pgtype.Text{String: content, Valid: true}, pgtype.Text{}, nil
	}
	path, err := s.turnSidecarPath(conversationID, seq)
	...
	if err := writeTurnSidecar(path, content); err != nil { ... }
	return pgtype.Text{}, pgtype.Text{String: path, Valid: true}, nil
}
func postgresTextSafe(s string) string {
	if !strings.ContainsRune(s, '\x00') { return s }
	return strings.ReplaceAll(s, "\x00", postgresTextNULReplacement)
}
```

---

### SEC-01 — `internal/secret/redact_exact.go` (new) (utility, inbound exact-match detector, D-10)

**Analog:** `internal/secret/envkey.go` (sibling in the SAME package — the canonical predicate this new file directly builds on, avoiding a fourth divergent list).

**Predicate to reuse for the boot-harvested set** (`internal/secret/envkey.go:65-99`, full functions):
```go
func IsSecretEnvKey(name string) bool {
	lower := strings.ToLower(name)
	for _, marker := range secretEnvMarkers {
		if strings.Contains(lower, marker) { return true }
	}
	return false
}
func IsSecretEnvVar(name, value string) bool {
	lower := strings.ToLower(name)
	for _, marker := range alwaysSecretEnvMarkers {
		if strings.Contains(lower, marker) { return true }
	}
	if ContainsCredentialURL(value) { return true }
	...
}
```
D-10's inbound detector: at boot, iterate `os.Environ()`, keep entries where `IsSecretEnvVar(name, value)` is true AND `len(value) >= <length floor, Claude's Discretion>`, build a `map[string]struct{}` or slice of VALUES (not names — the replace target is the value), then `strings.ReplaceAll` each configured secret value with a placeholder at every at-rest write. This is exact-match (zero false positives on agent-discovered secrets), NOT the pattern-based `internal/redact` approach — keep them as two distinct files/functions never merged.

**Contrast — the OUTBOUND pattern-based redactor this file must NOT duplicate** (`internal/redact/string.go`, full file, package `redact`):
```go
var secretPattern = regexp.MustCompile(`(?i)(postgres(?:ql)?|mysql|...)://[^\s"']*`)
var tokenPattern = regexp.MustCompile(`(?i)(bearer\s+|(?:api[_-]?key|token|password|...)\s*[:=]\s*)[^\s,;"']+`)
func String(message string) string { ... }
```
This stays wired at the OUTBOUND surfaces only (`internal/toolinvocations/store.go`'s `RedactForLedger`, below).

---

### SEC-01 — `internal/agent/tools/result.go` (D-09, utility, edit in place — the `.result` sidecar leak)

**Analog:** itself — `writeSidecar`, the write path `NewResult` funnels through.

**The unredacted write to fix** (`internal/agent/tools/result.go:158-165,234-244`):
```go
path, err := sidecarPath(tc.runDir, tc.sessionID, spillID)
...
if werr := writeSidecar(path, content); werr != nil {   // content is the FULL, unredacted tool output
	...
}
...
// writeSidecar persists the full content to path, creating the per-session dir lazily on first persist.
func writeSidecar(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { return err }
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { return err }
	return os.Chmod(path, 0o600)
}
```
Apply the SAME `internal/secret` exact-match detector (imported from the new `redact_exact.go`) to `content` before the `writeSidecar(path, content)` call — this is the second, independent at-rest surface D-09 requires (the ledger's `ResultSidecarPath` column, `internal/toolinvocations/store.go:154`, only stores the PATH and never sees the bytes, so nothing downstream can catch this).

**Existing test file to extend** (`internal/agent/tools/result_test.go` — `TestNewResult_LargeSpills` and siblings already assert sidecar byte-for-byte content and 0600 permissions; add a redaction-focused sibling test in the same style, e.g. `TestNewResult_RedactsConfiguredSecrets`).

---

### SEC-01 — `internal/reasoningtrace/reasoningtrace.go` (D-11, service, edit in place — confirmed real bug)

**Analog:** itself — `Record`/`redactString`/`redactValueForKey`; the outbound pattern pass to FOLD IN is `internal/redact.String`.

**The bug** (`internal/reasoningtrace/reasoningtrace.go:232-248`, full `redactString`):
```go
func redactString(s string) string {
	for _, env := range os.Environ() {
		name, value, ok := strings.Cut(env, "=")
		if !ok || len(value) < 8 {
			continue
		}
		upper := strings.ToUpper(name)
		if !strings.Contains(upper, "KEY") &&
			!strings.Contains(upper, "TOKEN") &&
			!strings.Contains(upper, "PASSWORD") &&
			!strings.Contains(upper, "SECRET") {
			continue   // AURA_DB_URL's key has NONE of these 4 markers — leaks in full
		}
		s = strings.ReplaceAll(s, value, "[REDACTED_"+upper+"]")
	}
	return s
}
```
`secret.IsSecretEnvVar` is not imported/called anywhere in this file (confirmed: zero `internal/secret` import). Fix: replace the 4-marker inline check with `secret.IsSecretEnvKey(name)` / `secret.IsSecretEnvVar(name, value)`, AND fold a `redact.String(...)` pattern pass into `Record` (`internal/reasoningtrace/reasoningtrace.go:90-106`) so a literal `postgres://user:pass@host` DSN typed into a free-text field is also caught — there is currently NO pattern pass on this path at all:
```go
func Record(stage string, fields map[string]any) {
	if !Enabled() { return }
	row := make(map[string]any, traceRowCap(len(fields)))
	...
	for k, v := range fields {
		row[k] = redactValueForKey(k, v)
	}
	line, err := json.Marshal(row)
	...
	line = []byte(redactString(string(line)))   // <-- fold redact.String in here too, ONE pass, no double-redaction of existing [REDACTED_*]/[capped] markers
	...
```

**Outbound pattern redactor to fold in** (`internal/redact/string.go`, already shown above under SEC-01 redact_exact.go section).

---

### SEC-01 — `internal/config/config_validate.go` + `config_knobs.go` (D-12, config, edit in place)

**Analog:** itself — `gateDestructiveShell` is the EXACT template CONTEXT.md names for `gateReasoningTraceFull`.

**Template to mirror** (`internal/config/config_validate.go:232-247`, full function):
```go
// gateDestructiveShell forbids an explicitly DISABLED destructive-shell gate under
// server_production ONLY: single_user_hardened ALLOWS `off` (appliance-operator
// flexibility, A3). It reads the RAW env value directly...
func (c *Config) gateDestructiveShell(p RuntimeProfile) []Violation {
	if p != ProfileServerProduction {
		return nil
	}
	raw := strings.TrimSpace(os.Getenv("AURA_SHELL_DESTRUCTIVE_PATTERNS"))
	if strings.EqualFold(raw, "off") {
		return []Violation{{Knob: "AURA_SHELL_DESTRUCTIVE_PATTERNS", Sev: Fatal, Msg: "destructive-shell gate must not be disabled (off) under server_production"}}
	}
	return nil
}
```
`gateReasoningTraceFull(p)` mirrors this shape: `p.Strict()` (not `p != ProfileServerProduction` — D-12 says BOTH hardened tiers, matching `gateWebAuth`'s `!p.Strict()` early-return instead), read `AURA_REASONING_TRACE` raw, if `== "full"` require `AURA_TRACE_FULL_ACK == "1"` else Fatal. Register the call in the aggregator (`internal/config/config_validate.go:88-105`, `ValidateProfile`):
```go
func (c *Config) ValidateProfile(p RuntimeProfile) []Violation {
	var vs []Violation
	...
	vs = append(vs, c.gateDestructiveShell(p)...)
	vs = append(vs, c.gateWebAuth(p)...)
	...
	return vs
}
```

**KnobSpec registry entries to add** (`internal/config/config_knobs.go:44-50,72,77` — `KnobSpec` shape + neighboring `Secret: true` rows):
```go
type KnobSpec struct {
	Name    string
	Kind    KnobKind
	Default string
	Enum    []string
	Secret  bool
}
// existing neighbors to pattern-match:
{Name: "AURA_AUTHULA_SECRET", Kind: KindString, Default: "", Secret: true},
{Name: "AURA_AGUI_CORS_PERMISSIVE", Kind: KindBool, Default: "false"},   // <- REMOVE this row (SEC-02)
```
Add three new rows: `AURA_REASONING_TRACE` (KindEnum if a fixed set like `off|summary|full`, else KindString), `AURA_TRACE_FULL_ACK` (KindBool, Default "false"), `AURA_TRACE_ENCRYPT_KEY` (KindString, Default "", Secret: true — matches the `AURA_AUTHULA_SECRET` row shape exactly).

**Test template to mirror** (`internal/config/config_validate_test.go:157-182`, `TestGateDestructiveShell`, table-driven with `t.Setenv`):
```go
func TestGateDestructiveShell(t *testing.T) {
	tests := []struct {
		name      string
		profile   RuntimeProfile
		env       string
		wantFatal bool
	}{
		{name: "prod off lowercase", profile: ProfileServerProduction, env: "off", wantFatal: true},
		{name: "hardened allows off", profile: ProfileSingleUserHardened, env: "off", wantFatal: false},
		...
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AURA_SHELL_DESTRUCTIVE_PATTERNS", tc.env)
			vs := (&Config{}).gateDestructiveShell(tc.profile)
			got := hasViolation(vs, "AURA_SHELL_DESTRUCTIVE_PATTERNS", Fatal)
			if got != tc.wantFatal { t.Errorf(...) }
		})
	}
}
```

---

### SEC-01 — `internal/tracesink/sink.go` + `_test.go` (new package) (D-13, service/utility, file-I/O)

**Analog:** `internal/objectstore/identity_store.go` (`encrypt`/`decrypt`/`deriveObjectStoreKey`) — the methods are unexported on `*IdentityStore`, so this is a ~20-line REWRITE, not an import.

**Encrypt/decrypt framing to mirror exactly** (`internal/objectstore/identity_store.go:155-176`, full functions):
```go
// encrypt seals plaintext with a fresh random nonce prepended to the ciphertext.
func (s *IdentityStore) encrypt(plaintext string) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("objectstore identity: nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}
// decrypt opens a nonce-prefixed ciphertext produced by encrypt.
func (s *IdentityStore) decrypt(ciphertext []byte) (string, error) {
	ns := s.aead.NonceSize()
	if len(ciphertext) < ns {
		return "", fmt.Errorf("objectstore identity: ciphertext too short (%d < %d)", len(ciphertext), ns)
	}
	nonce, ct := ciphertext[:ns], ciphertext[ns:]
	plaintext, err := s.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("objectstore identity: decrypt: %w", err)
	}
	return string(plaintext), nil
}
```
**Key derivation to mirror** — see the HKDF excerpt already shown under SEC-09 (`deriveObjectStoreKey`, `identity_store.go:192-208`); use label `"aura-reasoning-trace-sink-v1"` and env `AURA_TRACE_ENCRYPT_KEY` (net-new) instead of `AURA_AUTHULA_SECRET`.

**Fail-closed policy — the exact shape to copy** (`internal/objectstore/identity_store.go:196-197`):
```go
if len(secret) != 64 {
	return nil, fmt.Errorf("objectstore identity: AURA_AUTHULA_SECRET must be 64 hex characters (32 bytes)")
}
```
D-13 requires the SAME shape for `AURA_TRACE_ENCRYPT_KEY`: absent/malformed → refuse to write that trace record (log a warning once, drop the record) — NEVER fall back to plaintext. There must be exactly ONE write path in the new package, unreachable without a valid key (Pitfall 4 in RESEARCH.md).

**Fresh nonce per record is non-negotiable** — GCM fails catastrophically on nonce reuse; an appending file must never reuse one nonce across records (`rand.Read(nonce)` inside every `encrypt` call, exactly as `identity_store.go:157-158` does per-call, not once at sink construction).

---

### SEC-02 — `internal/agui/server_cors.go` (D-14, DELETE) + `server.go` + `config.go`/`config_knobs.go`/`config_validate.go`

**Analog:** itself — the file being deleted is its own best "before" reference; no replacement pattern is needed (the fix is subtraction).

**Whole file to delete** (`internal/agui/server_cors.go`, all 23 lines):
```go
package agui

import "net/http"

func (s *Server) withCORS(next http.Handler) http.Handler {
	if !s.cfg.CORSPermissive {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")   // already missing PATCH/PUT — the drift proof cited in D-14
			h.Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

**Call sites to un-wrap** (`internal/agui/server.go:356-364`, `Mux()`):
```go
	if s.operations == nil {
		return s.withCORS(mux)          // becomes: return mux
	}
	guarded := http.NewServeMux()
	for pattern, meta := range httpMutationRoutes {
		guarded.Handle(pattern, s.idempotencyMutation(mux, meta))
	}
	guarded.Handle("/", mux)
	return s.withCORS(guarded)          // becomes: return guarded
```

**Config surface to remove** — `ServerConfig.CORSPermissive` field (`internal/agui/server.go:41-43`):
```go
type ServerConfig struct {
	CORSPermissive  bool     // <- REMOVE
	BufferCap       int
	...
```
`Config.AGUICORSPermissive` field (`internal/config/config.go` — same name pattern as `AuthulaSecret`, remove the field + its `os.Getenv` wiring), the `AURA_AGUI_CORS_PERMISSIVE` KnobSpec row (`config_knobs.go:77`, shown above), and the now-dead gate (`internal/config/config_validate.go:222-230`, full function):
```go
func (c *Config) gateCORS(p RuntimeProfile) []Violation {
	if !p.Strict() { return nil }
	if c.AGUICORSPermissive {
		return []Violation{{Knob: "AURA_AGUI_CORS_PERMISSIVE", Sev: Fatal, Msg: "permissive CORS is forbidden under " + string(p)}}
	}
	return nil
}
```
Remove this function AND its call in `ValidateProfile` (`config_validate.go:97`, `vs = append(vs, c.gateCORS(p)...)`).

**`cmd/aura/serve.go` construction site** (`serve.go:358-360`):
```go
aguiServer := agui.NewServer(chat.run, chat.conv, agui.ServerConfig{
	CORSPermissive: chat.cfg.AGUICORSPermissive,   // <- REMOVE this line
	BufferCap:      chat.cfg.AGUIBufferCap,
```

**CSRF posture already covers the removal** (`internal/agui/auth.go:14-17`, `internal/agui/auth_cookie.go:123-131`):
```go
// auth.go:14 — "SameSite=Strict is the only CSRF control this phase... Re-evaluate if
// Phase 28/29 introduces a cross-origin write surface." (same-origin-only preserves this.)
func setSessionCookie(w http.ResponseWriter, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		...
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		...
	})
}
```

**Test to replace** (`internal/agui/server_test.go:462-519`, `TestServer_CORSPermissive`): the whole test is deleted (its `on:` sub-cases test a knob that no longer exists); its `off:` sub-case (lines 504-518) becomes the new `TestNoCORSHeaders` — same assertions, no `ServerConfig{CORSPermissive: ...}` construction needed at all:
```go
t.Run("off: no CORS headers, OPTIONS not 204", func(t *testing.T) {
	srv := newTestServerCfg(t, &scriptedRunner{}, store(), ServerConfig{CORSPermissive: false})   // drop the field entirely
	req, _ := http.NewRequest(http.MethodOptions, srv.URL+"/agent/run", nil)
	resp, err := http.DefaultClient.Do(req)
	...
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("restrictive default set ACAO = %q, want none", got)
	}
	if resp.StatusCode == http.StatusNoContent {
		t.Errorf("OPTIONS returned 204 with CORS off; want the mux's method-not-allowed default")
	}
})
```
Also delete/replace `TestGateCORS` (`internal/config/config_validate_test.go:136-152`) since `gateCORS`/`AGUICORSPermissive` disappear.

---

### SEC-03 — `cmd/aura/integrations_console.go` + `integrations_proxy.go` (D-15, CLI/controller, edit in place)

**Analog:** `internal/config/config.go` (`GuardWebBind`) — the in-repo loopback-guard PRECEDENT (a stronger analog than writing from scratch); `integrations_proxy.go`'s own `injectAuth` closure for the token-requirement wiring.

**The gap to fix** (`cmd/aura/integrations_console.go:38-52`, full `mcpConsole`):
```go
func mcpConsole(args []string, out io.Writer) error {
	addr := "127.0.0.1:9099"
	for i := 0; i < len(args); i++ {
		if args[i] == "--addr" && i+1 < len(args) {
			addr = args[i+1]     // raw, unvalidated — the footgun
			i++
		}
	}
	...
	srv := &http.Server{Addr: addr, Handler: newConsoleHandler(), ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()    // no auth wrapper at all
}
```

**The loopback-detection precedent to mirror** (`internal/config/config.go:309-325`, full `GuardWebBind`):
```go
func GuardWebBind(bind string, authConfigured bool, trustProxy bool) error {
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		host = bind // tolerate a bare host with no port
	}
	ip := net.ParseIP(host)
	isLoopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	if isLoopback {
		return nil // loopback always bootable, exactly as before (D-05)
	}
	if authConfigured || trustProxy {
		return nil // unlocked by either credential (D-05)
	}
	return fmt.Errorf("config: AURA_AGUI_BIND=%q is non-loopback but web auth is not configured; ...", bind)
}
```
D-15's shape: `isLoopback(addr)` (same host/IP check) → if non-loopback, require BOTH `--unsafe-non-loopback` flag AND `AURA_INTEGRATIONS_CONSOLE_TOKEN` set, else refuse to bind (return an error instead of calling `ListenAndServe`); log a `slog.Warn` when the unsafe escape is actually used; wire the token check into the proxy's request path (below) rather than `GuardWebBind` itself — this is a NEW, sibling function in `integrations_console.go`, not a call into `internal/config` (the console is a standalone CLI tool, not the daemon).

**Token-injection closure shape to extend for per-request auth** (`cmd/aura/integrations_proxy.go:39-53,46-53`):
```go
type integrationTarget struct {
	baseURL    func() string
	apiPrefix  string
	injectAuth func(h http.Header) // server-side auth injection; nil = none
}
func pimAdminToken() string {
	if v := strings.TrimSpace(os.Getenv("AURA_PIM_MCP_ADMIN_TOKEN")); v != "" {
		return v
	}
	return pimAdminTokenDefault
}
func builtinIntegrations() map[string]integrationTarget {
	return map[string]integrationTarget{
		"calendar": {
			baseURL:   mcpmanager.PIMSidecarBaseURL,
			apiPrefix: "/admin",
			injectAuth: func(h http.Header) {
				h.Set("X-Admin-Token", pimAdminToken())
			},
		},
```
When unsafe-non-loopback mode is active, `newConsoleHandler()` (or a wrapper around it) must require `AURA_INTEGRATIONS_CONSOLE_TOKEN` on every incoming request BEFORE it reaches `newIntegrationsProxy()` — a small middleware in the same style as the `injectAuth` closures above, checked inbound rather than injected outbound.

---

### SEC-06 — `internal/agui/strict_decode.go` (new) (middleware/utility, request-response, D-16/D-16b)

**Analog (the ANTI-PATTERN, do not copy verbatim):** `internal/agui/governance_write_api.go` (`decodeMCPBody`) / `governance_write_skills_api.go` (`decodeSkillsBody`) / `internal/agui/conversations_api.go:162` — all three share the SAME vulnerable single-decode idiom that IS the F-052 bug, not the fix.

**The vulnerable idiom present at 3 sites today** (`internal/agui/governance_write_api.go:190-201`, full `decodeMCPBody` — `governance_write_skills_api.go:216-226`'s `decodeSkillsBody` is byte-identical in shape):
```go
// decodeMCPBody size-caps + decodes a JSON body into dst, writing a clean 400 on a malformed
// body. An empty body is allowed (the verb-only enable/disable use no body; trust/install
// validate downstream in the provider).
func decodeMCPBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}
```
`json.Decoder.Decode` reads only the FIRST JSON value — `{"class":"trusted_local"}{"ignored":true}` decodes successfully with this idiom (the audit's exact repro). It is correct ONLY for the `allowEmpty` half (tolerating `io.EOF` on a truly empty body); it is MISSING the second decode-must-be-EOF step entirely. The third occurrence, `internal/agui/conversations_api.go:160-165`:
```go
r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
var body createConversationBody
if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
	http.Error(w, "invalid request body", http.StatusBadRequest)
	return
}
```

**The correct two-decode helper to write** (no in-repo precedent implements the full pattern — this is the Alex Edwards idiom, net new):
```go
type decodeOpts struct {
	maxBytes   int64
	allowEmpty bool
}

func strictDecodeJSON(w http.ResponseWriter, r *http.Request, dst any, opts decodeOpts) error {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		return fmt.Errorf("strict decode: unsupported content-type %q", ct)
	}
	cap := opts.maxBytes
	if cap <= 0 {
		cap = maxRunBodyBytes // existing package constant, server.go:28 — 1 MiB
	}
	r.Body = http.MaxBytesReader(w, r.Body, cap)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields() // NONE of the 3 existing precedents do this

	err := dec.Decode(dst)
	switch {
	case errors.Is(err, io.EOF) && opts.allowEmpty:
		return nil
	case err != nil:
		return fmt.Errorf("strict decode: %w", err)
	}

	// THE MISSING STEP in every current in-repo precedent (D-16b):
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("strict decode: unexpected trailing data after JSON body")
	}
	return nil
}
```

**Size-cap constant already shared by all 6 target routes** (`internal/agui/server.go:28`):
```go
const maxRunBodyBytes = 1 << 20
```
No route needs a different cap value — the helper can default to this constant.

**Per-route `allowEmpty` (verified at HEAD):**

| Route | Current shape | `allowEmpty` |
|---|---|---|
| `/agent/run` (`server_run_request.go:26`, called from `server.go:394`) | `decodeRunAgentRequest(dec *json.Decoder)` — takes a pre-built decoder, MaxBytesReader applied by the caller at `server.go:393` | `false` |
| approvals-resolve (`approvals_api.go:122-127`) | `r.Body = http.MaxBytesReader(...)` then plain `Decode` | `false` |
| onboarding (`onboarding_api.go:270-304`, `handleOnboardingMutation`/`prepareOnboardingMutation`) | generic wrapper, `MaxBytesReader` at :301, plain `Decode` at :279 | `false` |
| assets (`assets_api.go:80-85`) | `MaxBytesReader` then plain `Decode` | `false` |
| governance MCP (`governance_write_api.go:193-201`) | `decodeMCPBody` — **the D-16b bug** | `true` (doc comment: "verb-only enable/disable use no body") |
| governance skills (`governance_write_skills_api.go:218-226`) | `decodeSkillsBody` — **the D-16b bug** | `true` (doc comment: "delete uses no body") |

**Generic wrapper shape for onboarding** (`internal/agui/onboarding_api.go:270-304`, full functions — useful as a SEPARATE analog for "one route, many call sites" structuring, independent of the decode-strictness fix):
```go
func handleOnboardingMutation[Req any, Resp any](
	s *Server, w http.ResponseWriter, r *http.Request,
	validate func(Req) error,
	call func(context.Context, string, string, Req) (Resp, error),
) {
	var req Req
	token, requester, ok := s.prepareOnboardingMutation(w, r, func() error {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return errors.New("invalid request body")
		}
		return validate(req)
	})
	...
}
func (s *Server) prepareOnboardingMutation(w http.ResponseWriter, r *http.Request, decodeAndValidate func() error) (string, string, bool) {
	...
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	if err := decodeAndValidate(); err != nil { ... }
	...
}
```

**Deliberately deferred (D-16c, do NOT silently sweep in):** `internal/agui/governance_write_scheduler.go:130-135` (`handleSchedulerEdit`) uses a size-cap-only `io.LimitReader` with no trailing-JSON protection at all:
```go
var body schedulerEditBody
dec := json.NewDecoder(io.LimitReader(r.Body, schedulerEditBodyCap))
if err := dec.Decode(&body); err != nil {
	writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	return
}
```
Flag this in the acceptance evidence as consciously out of scope this phase (refactor-on-touch). `internal/agui/connect_api.go:95` (`io.LimitReader(resp.Body, maxRunBodyBytes)`) is a response-body read of an UPSTREAM sidecar reply, not an inbound privileged route — cite only as a size-cap pattern, never add it to the acceptance set.

---

### SEC-05 — GitHub Actions SHA-pinning + SBOM + pin-gate (D-17/D-18/D-19/D-20)

**Analog:** the workflow files' own ALREADY-pinned-by-version tool installs (the style to extend to SHA form) + `scripts/quality_snapshot_gate.sh`/`_test.sh` (the shell-gate + self-test precedent for the new `workflow_pin_gate.sh`).

**Current floating-tag shape to convert** (`.github/workflows/ci.yml:40-58`, representative — same shape repeats 68 times across 4 files):
```yaml
- name: Checkout
  uses: actions/checkout@v7
  with:
    fetch-depth: 0
- name: Set up Go
  uses: actions/setup-go@v7
  with:
    go-version-file: go.mod
    cache: true
```
Target shape (mandatory trailing version comment — Dependabot only updates SHA pins that carry one):
```yaml
- name: Checkout
  uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
```

**Already-pinned-by-VERSION precedent to extend to SHA form** (`.github/workflows/ci.yml:78-86`, the two ANTI-pattern `@latest` tool installs sitting right next to two already-exact-pinned ones):
```yaml
- name: deadcode
  run: |
    go install golang.org/x/tools/cmd/deadcode@latest        # <- must become an exact version
    "$(go env GOPATH)/bin/deadcode" -test $(bash scripts/go_packages.sh)

- name: golangci-lint
  run: |
    go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2   # <- the pattern to mirror
    "$(go env GOPATH)/bin/golangci-lint" run --timeout=5m $(bash scripts/go_packages.sh)
```
Same anti-pattern at `ci.yml:158-161` (`govulncheck@latest`) and `skills.yml:132-134` (`go-mutesting@latest`).

**Two refs that MUST share the identical SHA** (`.github/workflows/codeql.yml:53-58,64-67`):
```yaml
- name: Initialize CodeQL
  uses: github/codeql-action/init@v4
  with:
    languages: ${{ matrix.language }}
    ...
- name: Perform CodeQL Analysis
  uses: github/codeql-action/analyze@v4
  with:
    category: "/language:${{ matrix.language }}"
```
A naive `owner/repo@` regex in the new pin-gate would miss both (3 path segments) — the gate's ref-pattern must treat everything before `@` as one opaque token, not assume exactly one `/`.

**The exact false-positive to NOT flag** (`.github/workflows/release.yml:51-56`, `goreleaser-action` — `version:` is an action INPUT, not a `uses:` ref):
```yaml
- name: Run goreleaser
  uses: goreleaser/goreleaser-action@v7
  with:
    distribution: goreleaser
    version: "~> v2"          # <- an INPUT key, not a `uses:` line — the gate must scope to `uses:` only
    args: release --clean
```

**Shell-gate + self-test precedent to mirror structurally** (`scripts/quality_snapshot_gate.sh:1-11`, header/setup shape):
```bash
#!/usr/bin/env bash
# Enforce docs/aura-quality-snapshot.md freshness for changed quality-owned paths.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

SNAPSHOT="${AURA_QUALITY_SNAPSHOT:-docs/aura-quality-snapshot.md}"
HEAD_REF="${AURA_QUALITY_HEAD:-HEAD}"
CHANGED_FILE="$(mktemp)"
trap 'rm -f "$CHANGED_FILE"' EXIT
```
```bash
# scripts/quality_snapshot_gate_test.sh:1-9,23-41 — tmpdir + fixture + rc-assertion self-test shape
#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

run_expect() {
  local name="$1" want="$2" ...
  set +e
  out="$(... bash scripts/quality_snapshot_gate.sh 2>&1)"
  rc=$?
  set -e
  if [[ "$rc" != "$want" ]]; then
    printf 'FAIL %s: rc=%s want=%s\n%s\n' "$name" "$rc" "$want" "$out" >&2
    exit 1
  fi
}
```
`workflow_pin_gate.sh`/`workflow_pin_gate_test.sh` mirror this exact shape: `set -euo pipefail`, repo-root `cd`, a tmpdir-based self-test with fixture YAML files reproducing (a) the multi-segment `github/codeql-action/init@v4` shape, (b) an indented `run: |` block containing `go install ...@latest`, and (c) the `version: "~> v2"` false-positive — asserting the gate correctly passes/fails each.

**New SBOM step for `release.yml`** (no in-repo analog; per GoReleaser's official `sboms:` docs, additive to `.goreleaser.yaml`):
```yaml
sboms:
  - artifacts: archive   # Go-modules SBOM (binary buildinfo)
  - artifacts: source    # source-tarball SBOM, the ONLY way to capture web/package-lock.json
```
Requires a pinned syft install step in `release.yml` BEFORE the `goreleaser-action` step (`goreleaser-action` does NOT install syft itself):
```yaml
- name: Install syft
  uses: anchore/sbom-action/download-syft@e22c389904149dbc22b58101806040fa8d37a610 # v0.24.0
```

**Dependabot — no change needed** (`.github/dependabot.yml:19-32`, already covers the `github-actions` ecosystem):
```yaml
- package-ecosystem: github-actions
  directory: "/"
  schedule:
    interval: weekly
    day: monday
  ...
  groups:
    actions-minor-patch:
      update-types:
        - minor
        - patch
```

---

### Docs amendments (do BEFORE the corresponding code, per PRD §Q&A revision protocol)

**`.planning/REQUIREMENTS.md`** — current wording to amend (:131,132,139):
```
- [ ] **SEC-01**: Tool output and traces redact secret-like values before persistence; full reasoning-trace mode requires an explicit production warning/fail-fast + retention config + optional encrypted sink. *(F-021)*
- [ ] **SEC-02**: Permissive/wildcard CORS is replaced by explicit origin allowlists, refused when auth is disabled except under an explicit dev profile, sets `Vary: Origin`, and keeps allowed methods in sync with registered routes. *(F-022)*
- [ ] **SEC-09**: The high CodeQL `go/weak-sensitive-data-hashing` finding at `internal/agui/recovery_hash.go` is remediated — sensitive recovery material uses a cryptographically strong, salted KDF/hash rather than a weak/fast hash — and the CodeQL alert resolves to fixed. *(CodeQL-surfaced; not in the F-001..F-052 audit set)*
```
Apply D-10b (SEC-01 scope), D-14b (SEC-02 → same-origin-only), D-08 (SEC-09 → keyed-hash-or-FP) exactly as worded in `40-CONTEXT.md`'s "Amendments required" section (lines 74-78).

**`.planning/ROADMAP.md`** — current Phase 40 success criteria to amend (:734,737):
```
2. Secret-like values are redacted before persistence; permissive CORS is refused when auth is disabled (except dev).
5. The high CodeQL `go/weak-sensitive-data-hashing` finding at `internal/agui/recovery_hash.go` is remediated with a strong salted KDF and the alert resolves (SEC-09).
```
Criterion 2 needs the D-10b/D-14b scope correction (redaction = configured secrets at rest / same-origin not allowlist); criterion 5 needs the D-08 keyed-hash-or-documented-FP wording (a randomly-salted KDF cannot serve a lookup-by-hash PRIMARY KEY).

## Shared Patterns

### HKDF Subkey Derivation (SEC-09 pepper + SEC-01 D-13 sink key)
**Source:** `internal/objectstore/identity_store.go:192-208` (shown in full above under SEC-09).
**Apply to:** `internal/agui/recovery_hash.go` (new `DeriveResetTokenPepper`) and the new `internal/tracesink` package (key derivation). Both derive from a 64-hex-char secret via `crypto/hkdf.Key(sha256.New, raw, nil, <domain-label>, 32)`, fail closed on a malformed/short secret, never reuse the raw secret bytes directly for a second cryptographic purpose.

### AES-256-GCM Fresh-Nonce-Per-Record Framing (SEC-01 D-13)
**Source:** `internal/objectstore/identity_store.go:155-176` (shown in full above under SEC-01 tracesink).
**Apply to:** `internal/tracesink/sink.go` only, this phase. `nonce := make([]byte, aead.NonceSize()); rand.Read(nonce); aead.Seal(nonce, nonce, plaintext, nil)` — nonce-prefixed ciphertext, fresh nonce EVERY call, never once per file/session.

### Canonical Secret-Env Predicate (SEC-01 D-10 inbound, D-11 reasoningtrace fix)
**Source:** `internal/secret/envkey.go` (`IsSecretEnvKey`, `IsSecretEnvVar`, `ContainsCredentialURL`), shown in full above.
**Apply to:** the new `internal/secret/redact_exact.go` (boot-harvested set) AND the fix to `internal/reasoningtrace/reasoningtrace.go`'s `redactString` (replace its inline 4-marker check with a call to this canonical predicate). Never invent a fourth divergent secret-marker list.

### Outbound Pattern Redaction (SEC-01 D-10 outbound half)
**Source:** `internal/redact/string.go` (`String`) + `internal/toolinvocations/store.go:82-91` (`RedactForLedger`, the blessed ledger choke-point), both shown in full above.
**Apply to:** already wired at the ledger (`toolinvocations/store.go:146,150`); fold `redact.String` into `internal/reasoningtrace/reasoningtrace.go`'s `Record` (D-11) as the SAME pattern-based pass. Never apply pattern-based redaction to the INBOUND turn/spill/`.result` surfaces — those stay exact-match only (would corrupt the agent's re-fed working data otherwise).

### Profile-Gated Fatal-Unless-Acked Validation (SEC-01 D-12)
**Source:** `internal/config/config_validate.go:232-247` (`gateDestructiveShell`) + `:249-260` (`gateWebAuth`), both shown in full above.
**Apply to:** the new `gateReasoningTraceFull(p)`. Shape: read a raw env value directly (never import the leaf package it gates), branch on `p.Strict()` or an exact profile match, return `[]Violation{{Knob, Sev: Fatal, Msg}}` on the forbidden combination, `nil` otherwise. Register the new gate in `ValidateProfile`'s aggregation list (`config_validate.go:88-105`) — it must never first-fail; every unmet requirement surfaces in one pass.

### Loopback-or-Explicit-Escalation Bind Guard (SEC-03)
**Source:** `internal/config/config.go:309-325` (`GuardWebBind`), shown in full above.
**Apply to:** the new bind-guard in `cmd/aura/integrations_console.go`. Shape: `net.SplitHostPort` + `net.ParseIP(...).IsLoopback()` (or `host == "localhost"`) to detect loopback; loopback always passes; non-loopback requires an explicit unlock (here: `--unsafe-non-loopback` AND a console token, vs. `GuardWebBind`'s `authConfigured || trustProxy`); a clear, actionable error message naming the exact env/flag to set.

### Size-Capped, Empty-Tolerant JSON Decode (SEC-06 — the shape to KEEP, extended with the missing trailing-JSON step)
**Source:** `internal/agui/governance_write_api.go:190-201` / `governance_write_skills_api.go:216-226` (shown in full above) — correct for the `MaxBytesReader` + `allowEmpty` halves, VULNERABLE for trailing JSON.
**Apply to:** all 6 SEC-06 acceptance-set routes via the new `internal/agui/strict_decode.go` helper. Keep the `http.MaxBytesReader(w, r.Body, maxRunBodyBytes)` + first-`Decode`-may-`io.EOF` shape; ADD `dec.DisallowUnknownFields()` before the first decode and a second `dec.Decode(&struct{}{})` that MUST return `io.EOF` after it.

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `.goreleaser.yaml` `sboms:` block | config | batch | No SBOM block exists anywhere in this repo yet (`grep sboms .goreleaser.yaml` → 0 hits) — D-17 is purely additive; the pattern comes from GoReleaser's own official docs (`https://goreleaser.com/customization/sbom/`), cited in RESEARCH.md, not from an in-repo precedent. |
| `internal/eval/injection_cot_eval.go` scenario PAYLOADS specifically (not the harness shape, which has a strong analog) | service/dataset | event-driven | The deterministic-suite corpus (`injection_suite_test.go`) is hand-written per D-02; this LLM tier's payload TEXT is explicitly sourced from an EXTERNAL corpus (garak `promptinject`/`latentinjection` or promptfoo redteam plugins) per D-04 — there is no in-repo analog for adversarial payload text itself, only for the harness structure around it. |

## Metadata

**Analog search scope:** `internal/gateway/`, `internal/eval/`, `internal/agui/`, `internal/conversations/`, `internal/agent/tools/`, `internal/reasoningtrace/`, `internal/config/`, `internal/secret/`, `internal/redact/`, `internal/toolinvocations/`, `internal/objectstore/`, `internal/scoring/`, `internal/agent/mcptools/`, `cmd/aura/`, `.github/workflows/`, `.goreleaser.yaml`, `.github/dependabot.yml`, `scripts/`, `.planning/REQUIREMENTS.md`, `.planning/ROADMAP.md`.
**Files scanned (Read/Grep):** 45+ distinct files at HEAD, all cross-checked against `40-CONTEXT.md`/`40-RESEARCH.md`'s own independent verification pass (2026-07-22) — zero drift found beyond the already-documented `internal/gateway/scoring.go` → `internal/scoring/scoring.go` path mislabel (confirmed again this session: `internal/gateway/` has no `scoring.go` file).
**Pattern extraction date:** 2026-07-22
