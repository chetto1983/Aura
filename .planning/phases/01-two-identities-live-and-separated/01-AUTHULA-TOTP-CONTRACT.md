# Authula TOTP Enrollment Wire Contract

**Measured:** 2026-09-07, from the installed module under `$(go env GOMODCACHE)`. No Authula
server was started; every claim below cites a source file read this session, not a doc site or
a probe against a running instance (CLAUDE.md READ THE DOCUMENTATION FIRST).

## Commands run and their actual output

```
$ go list -m github.com/Authula/authula
github.com/Authula/authula v1.43.0

$ go doc github.com/Authula/authula/plugins/totp
package totp // import "github.com/Authula/authula/plugins/totp"

func BuildUseCases(p *TOTPPlugin) *usecases.UseCases
func MigrationSet(provider string) migrations.MigrationSet
func Routes(p *TOTPPlugin) []models.Route
type API struct{ ... }
    func BuildAPI(plugin *TOTPPlugin) *API
type TOTPHookID string
    const HookIDTOTPIntercept TOTPHookID = "totp.intercept"
type TOTPPlugin struct{ ... }
    func New(config types.TOTPPluginConfig) *TOTPPlugin
```

`go doc -all` added no exported symbol the summary did not already name — every unexported
detail below (the use case bodies, the crypto) was confirmed by reading the source tree under
`$(go env GOMODCACHE)/github.com/!authula/authula@v1.43.0/plugins/totp/` directly, per the task's
instruction to read the installed source rather than stop at the doc summary.

```
$ grep -rn "totp" internal/webauth/
internal/webauth/authula.go:42:	totpplugin "github.com/Authula/authula/plugins/totp"
internal/webauth/authula.go:43:	totptypes "github.com/Authula/authula/plugins/totp/types"
internal/webauth/authula.go:207:		totpplugin.New(totptypes.TOTPPluginConfig{
internal/webauth/authula.go:208:			Enabled:         true,
internal/webauth/authula.go:209:			BackupCodeCount: 10,
internal/webauth/authula.go:210-212:		SecureCookie/SameSite set, Digits/PeriodSeconds NOT set (plugin defaults apply)
```

Aura wires the plugin with `Enabled: true` and does not override `Digits` or `PeriodSeconds`,
so the plugin's own defaults (`plugin.go:102-103`: `Digits: 6, PeriodSeconds: 30`) govern —
standard RFC 6238 6-digit/30-second codes, what any off-the-shelf TOTP library expects.

## 1. Module version the answer is true of

`github.com/Authula/authula v1.43.0` — this is the version `go list -m` reports and the version
under which `$(go env GOMODCACHE)/github.com/!authula/authula@v1.43.0/` was read. It matches
`internal/webauth/authula.go`'s package-doc comment, which also names v1.42.0 in prose — the
`go.mod`-pinned, `go list`-reported version (v1.43.0) is the one this document is true of, not
the comment's.

## 2. The enrollment entry point

Two exported routes, both built by `totp.Routes(p *TOTPPlugin) []models.Route`
(`plugins/totp/routes.go:38-88`), both requiring an authenticated user actor
(`middleware.RequireActor(models.ActorUser)` — the caller already holds a valid session, this is
NOT the pending-cookie stage):

- **`POST /totp/enable`** — `handlers.EnableHandler`, backed by
  `usecases.EnableUseCase.Enable(ctx, userID, appName)` (`plugins/totp/usecases/enable_usecase.go:50`).
  This is the entry point: it generates a fresh secret, persists it, and returns the material an
  enrolling client needs.
- **`GET /totp/get-uri`** — `handlers.GetTOTPURIHandler`, backed by
  `usecases.GetTOTPURIUseCase.GetTOTPURI` — a read of the already-provisioned URI, not a second
  enrollment path. Not exercised further here; `/totp/enable` is the one that matters for a
  freshly provisioned identity with no TOTP state yet.

Aura's own wiring point is `internal/webauth/authula.go:207` (`totpplugin.New(...)`) — the plugin
is enabled today with default routes; nothing in that file's read range narrows or renames them.

## 3. What the enrollment response carries

`POST /totp/enable`'s handler (`plugins/totp/handlers/enable_handler.go:20-40`) returns
`types.EnableResponse{TotpURI, BackupCodes}` (`plugins/totp/types/types.go:86-88`) built from
`EnableUseCase.Enable`'s `*types.EnableResult`.

The decisive field is `TotpURI`, built by
`services.TOTPService.BuildURI(secret, issuer, email)` (`plugins/totp/services/totp_service.go:66-74`):

```go
func (s *TOTPService) BuildURI(secret, issuer, email string) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(email)
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	v.Set("digits", fmt.Sprintf("%d", s.Digits))
	v.Set("period", fmt.Sprintf("%d", s.PeriodSeconds))
	return fmt.Sprintf("otpauth://totp/%s?%s", label, v.Encode())
}
```

`secret` here is the **plaintext** base32 seed — `TOTPService.GenerateSecret()`
(`totp_service.go:29-35`) makes 20 random bytes and `base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString`s
them, before it is ever encrypted for storage (`enable_usecase.go:67-71`: the plaintext `secret`
is what `BuildURI` receives; only a *separate copy*, `encryptedSecret`, goes to
`TOTPRepo.Create`). The `otpauth://` URI is therefore not just a QR-rendering convenience — it is
the plaintext seed itself, carried as an ordinary query parameter (`secret=...`), plus `issuer`,
`digits=6`, `period=30`. Any RFC 6238 client — the URI's `secret` param decoded from base32, fed
through the standard HOTP/TOTP construction — can compute a valid code from this response with
no image decoding step. `BackupCodeService.Generate()` also returns plaintext one-time backup
codes in the same response, a second (non-TOTP) headless path this document does not need.

**Answer: it hands back a shared secret, not only a QR image.** The QR is a rendering of the
same `otpauth://` URI a headless client can parse directly.

## 4. The verify step that completes enrollment

`POST /totp/verify` — `handlers.VerifyTOTPHandler`, backed by
`usecases.VerifyTOTPUseCase.Verify(ctx, pendingToken, code, trustDevice, ip, userAgent)`
(`plugins/totp/usecases/verify_totp_usecase.go:56-104`). It requires the `totp_pending` cookie
`/totp/enable` sets when `SkipVerificationOnEnable` is false (Aura's wiring does not set this
flag, so the plugin default — verification required — applies), plus a JSON body carrying `code`.

`Verify` decrypts the **same secret** `/totp/enable` persisted
(`uc.TokenService.Decrypt(record.Secret)`, line 71) and checks the caller's `code` against it
with `TOTPService.ValidateCode(secret, code, time.Now().UTC())` (line 74) — the identical
RFC 6238 construction (`totp_service.go:47-58`, `±1` time-step window) that a client would use to
compute a code from the plaintext `secret` handed back in step 3's response. **Yes: a code
computed from (3) is accepted by (4)** — they are proven to be the same secret by the source,
not by an assumption that they match.

## 5. Verdict

**Enrollment IS headlessly automatable.** `POST /totp/enable` (authenticated session) returns a
plaintext base32 TOTP secret inside a standard `otpauth://` URI; a caller that decodes that
`secret` and computes an RFC 6238 code (6 digits, 30-second period — the plugin defaults Aura's
wiring does not override) can complete enrollment via `POST /totp/verify` with no human and no
QR-image decoding step.

## 6. What this measurement does NOT show

- **It is a source read, not an executed round trip.** No Authula server was started; `/totp/enable`
  was never actually called and its JSON body was never actually observed over the wire — this
  is the code that would produce that body, read directly, not a captured response.
- **It is true of one pinned version, `v1.43.0`.** A future `go.mod` bump could change
  `EnableResponse`'s shape, the URI format, or the default `Digits`/`PeriodSeconds` without this
  document being re-read.
- **It says nothing about whether Aura's own middleware exposes the route the plugin defines.**
  `totpplugin.New(...)` is wired in `internal/webauth/authula.go:207`, but whether Aura's HTTP
  mux actually mounts `/totp/enable` and `/totp/verify` reachably, under what base path, and
  whether any Aura-side handler wraps or rejects the call before it reaches the plugin, is
  unmeasured here — that is `internal/agui`'s / the AG-UI gateway's routing, not this plugin.
- **It says nothing about the first-login trigger.** `EnforceFirstLogin`
  (`cmd/aura/serve_onboarding.go:87-104`) only sets two Authula user-metadata markers
  (`aura_must_change_password`, `aura_totp_enrollment_required`); its own comment states the
  login-time code that reads those markers and forces the enrollment redirect "is wired at the
  cutover (plan 12)" — grepped this session, zero other references exist
  (`grep -rn "aura_totp_enrollment_required" --include=*.go .` returns only the three lines that
  define and set it). This document measures the plugin's enrollment wire contract in isolation;
  it does not claim a freshly provisioned identity is today actually routed into that enrollment
  flow at first login — that routing is unbuilt.
- **`SkipVerificationOnEnable`'s effective value here was read from the plugin's `ApplyDefaults()`,
  not from a live `Config()` dump.** Aura's wiring (`authula.go:207-213`) does not set the field,
  so the default is inferred to apply — consistent with the source but not independently
  confirmed against a running instance.

## Sources cited

- `internal/agui/onboarding_provision.go:200-210` — `EnforceFirstLogin` call site, what it marks
- `internal/webauth/authula.go:1-60,190-213` — Aura's plugin wiring, `totpplugin.New(...)`
- `cmd/aura/serve_onboarding.go:73-104` — `authulaCoreAdapter.EnforceFirstLogin`, the metadata
  markers, and the comment naming the unwired login-time enforcement
- `scripts/agui_smoke.sh:255-314` — the existing verify-only flow (`totp_redirect` on login,
  `POST /totp/verify` with an operator-supplied `AURA_E2E_AUTHULA_TOTP_CODE`); confirms this
  script never calls `/totp/enable` and therefore never exercises what this document measures
- `$(go env GOMODCACHE)/github.com/!authula/authula@v1.43.0/plugins/totp/routes.go:11-88`
- `.../plugins/totp/handlers/enable_handler.go:20-40`
- `.../plugins/totp/handlers/get_totp_uri_handler.go`
- `.../plugins/totp/handlers/verify_totp_handler.go`
- `.../plugins/totp/usecases/enable_usecase.go:50-134`
- `.../plugins/totp/usecases/verify_totp_usecase.go:56-104`
- `.../plugins/totp/services/totp_service.go:29-74`
- `.../plugins/totp/types/types.go:86-119`
- `.../plugins/totp/plugin.go:36,100-103` — `ApplyDefaults`, `Digits: 6, PeriodSeconds: 30`
- `.../plugins/totp/constants/*.go` — `CookieTOTPPending = "totp_pending"`, error sentinels
