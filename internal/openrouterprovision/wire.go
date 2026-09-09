// Package openrouterprovision is the OpenRouter Provisioning-API client:
// mint a per-identity key at an explicit cap, read one back, change its cap
// and reset interval, and revoke it with a verified follow-up read. Every
// exported function takes its own *http.Client, base URL and API key — no
// package-level default client, no package-level base URL, no init() — the
// same convention internal/llm/spend.go:53's FetchSpend and
// internal/llm/pricing_source.go's FetchModelPrice already use, and the
// reason this whole package's tests run against httptest with no daemon and
// no network (RESEARCH Q6).
//
// Field names throughout this package are transcribed from
// .planning/phases/02-two-roles-and-a-budget/02-OPENROUTER-API.md, itself
// transcribed from https://openrouter.ai/openapi.json on 2026-09-08 — never
// from recall. Measured behaviours (the 403 on an exhausted cap, the
// omitempty trap on a zero limit, the DELETE+GET revoke pair) are cited at
// the function or type that encodes them.
package openrouterprovision

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// USDCap is a spending cap carried as an exact integer number of cents, end
// to end. A float64 field can drift after arithmetic and encoding/json's
// default float formatting is not a contract; this type is decimal-safe
// instead, per CLAUDE.md's "money is decimal, not float-shaped-like-money."
// The rounding rule is stated once, here: an admin-entered value is rounded
// HALF-UP to two decimals at NewUSDCapFromString — after that point the cap
// is an exact passthrough, never re-rounded.
type USDCap int64

// NewUSDCapFromString parses an admin-entered amount (e.g. the cockpit's cap
// input) and rounds it HALF-UP to the nearest cent. A negative amount is
// refused — cutting credit is a PATCH that disables the key, not a negative
// limit.
func NewUSDCapFromString(s string) (USDCap, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("openrouterprovision: empty cap")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("openrouterprovision: parse cap %q: %w", s, err)
	}
	if f < 0 {
		return 0, fmt.Errorf("openrouterprovision: cap %q must not be negative", s)
	}
	return USDCap(int64(math.Floor(f*100 + 0.5))), nil
}

// String renders the cap as a fixed two-decimal USD amount — "0.00",
// "0.01", "5.25" — never a float artefact.
func (c USDCap) String() string {
	cents := int64(c)
	whole := cents / 100
	frac := cents % 100
	if frac < 0 {
		frac = -frac
	}
	return fmt.Sprintf("%d.%02d", whole, frac)
}

// MarshalJSON emits the cap as a bare JSON number token (not a quoted
// string) at a fixed two decimals, so a zero cap marshals literally as
// "0.00" — containing the "limit":0 prefix TestMintAtZeroCap asserts on —
// and 0.01 marshals as exactly "0.01", never a float artefact like
// "0.009999999776".
func (c USDCap) MarshalJSON() ([]byte, error) {
	return []byte(c.String()), nil
}

// UnmarshalJSON accepts the provider's own decimal or integer number token
// and rounds HALF-UP to the nearest cent — the same rule
// NewUSDCapFromString applies at the admin input boundary. A JSON null
// decodes to zero cents; callers that must distinguish "the provider said
// null" from "the provider said zero" use a *USDCap field instead (see
// keyDataWire.LimitRemaining below) rather than relying on this method's
// null handling.
func (c *USDCap) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "" || s == "null" {
		*c = 0
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("openrouterprovision: decode cap %q: %w", s, err)
	}
	*c = USDCap(int64(math.Floor(f*100 + 0.5)))
	return nil
}

// LimitReset is OpenRouter's reset-interval enum — exactly three documented
// values (02-OPENROUTER-API.md, POST/PATCH /api/v1/keys). An admin chooses
// one per identity (D-10); an unrecognised value is refused locally before
// a request goes out, rather than as a provider 400.
type LimitReset string

// The three reset intervals OpenRouter documents for limit_reset. A null
// value (never resets) is represented by a nil *LimitReset, not a fourth
// constant here.
const (
	LimitResetDaily   LimitReset = "daily"
	LimitResetWeekly  LimitReset = "weekly"
	LimitResetMonthly LimitReset = "monthly"
)

// valid reports whether r is one of the three documented reset intervals.
func (r LimitReset) valid() bool {
	switch r {
	case LimitResetDaily, LimitResetWeekly, LimitResetMonthly:
		return true
	default:
		return false
	}
}

// MintRequest is the caller-facing input to MintKey.
type MintRequest struct {
	// IdentityID becomes external.user on the wire — the field
	// 02-OPENROUTER-API.md identifies as where an Aura identity belongs, and
	// the field that makes OpenRouter's analytics read as identity rows
	// without a join.
	IdentityID string
	// Name surfaces as api_key_id in OpenRouter's analytics dimensions.
	Name string
	// Limit is the spending cap. Zero is the product's default state (D-09)
	// and is a real, meaningful value here — never treat it as "unset".
	Limit USDCap
	// LimitReset must be one of LimitResetDaily/Weekly/Monthly; MintKey
	// refuses any other value before sending the request.
	LimitReset LimitReset
}

// mintRequestWire is the POST /api/v1/keys body.
type mintRequestWire struct {
	Name string `json:"name"`
	// Limit deliberately carries NO omitempty. A zero cap is the product's
	// default state (D-09); omitempty drops a zero-valued field (USDCap's
	// underlying kind is int64, and encoding/json's isEmptyValue treats a
	// zero integer as empty), which would mint an UNCAPPED key against an
	// account whose pool the operator pays for — the exact inverse of
	// CRED-02. TestMintAtZeroCap asserts the marshalled bytes for this
	// reason: a decoded struct cannot tell an omitted field from a zero one.
	Limit      USDCap           `json:"limit"`
	LimitReset LimitReset       `json:"limit_reset"`
	External   mintExternalWire `json:"external"`
}

type mintExternalWire struct {
	User string `json:"user"`
}

// mintResponseWire is the 201 response. Key is the raw credential — it
// appears in THIS response only; there is no endpoint that returns it
// again.
type mintResponseWire struct {
	Key  string      `json:"key"`
	Data keyDataWire `json:"data"`
}

// keyDataWire is the `data` object POST/GET/PATCH /api/v1/keys{,/{hash}}
// all share, transcribed from 02-OPENROUTER-API.md (retrieved 2026-09-08
// from openapi.json, 1,933,855 bytes / 81 paths). Fields this package does
// not read (byok_usage*, created_at, ...) are still declared so the decoder
// never fails on a field the live response carries but this package was
// not told to expect.
type keyDataWire struct {
	Hash      string  `json:"hash"`
	Label     string  `json:"label"`
	Name      string  `json:"name"`
	Disabled  bool    `json:"disabled"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	ExpiresAt *string `json:"expires_at"`
	// Limit is null on the wire when a key is uncapped; nil here carries
	// that meaning.
	Limit *USDCap `json:"limit"`
	// LimitRemaining: nil = the provider did not say (absence), a non-nil
	// pointer to 0 = a real zero balance (data). TestGetKeyDecodesZeroAndNullDistinctly
	// asserts these are not collapsed into each other — the same discipline
	// internal/llm/spend_test.go:51-54 states for zero daily spend.
	LimitRemaining     *USDCap     `json:"limit_remaining"`
	LimitReset         *LimitReset `json:"limit_reset"`
	Usage              float64     `json:"usage"`
	UsageDaily         float64     `json:"usage_daily"`
	UsageWeekly        float64     `json:"usage_weekly"`
	UsageMonthly       float64     `json:"usage_monthly"`
	ByokUsage          float64     `json:"byok_usage"`
	ByokUsageDaily     float64     `json:"byok_usage_daily"`
	ByokUsageWeekly    float64     `json:"byok_usage_weekly"`
	ByokUsageMonthly   float64     `json:"byok_usage_monthly"`
	ExternalUser       *string     `json:"external_user"`
	IncludeBYOKInLimit bool        `json:"include_byok_in_limit"`
	CreatorUserID      *string     `json:"creator_user_id"`
	WorkspaceID        string      `json:"workspace_id"`
}

// KeyRecord is the decoded, caller-facing projection of keyDataWire.
type KeyRecord struct {
	Hash           string
	Label          string
	Name           string
	Disabled       bool
	Limit          *USDCap // nil = uncapped
	LimitRemaining *USDCap // nil = absent, non-nil zero = a real zero balance
	LimitReset     *LimitReset
	ExternalUser   string
	Usage          float64
	UsageDaily     float64
	UsageWeekly    float64
	UsageMonthly   float64
}

func keyRecordFromWire(w keyDataWire) KeyRecord {
	rec := KeyRecord{
		Hash:           w.Hash,
		Label:          w.Label,
		Name:           w.Name,
		Disabled:       w.Disabled,
		Limit:          w.Limit,
		LimitRemaining: w.LimitRemaining,
		LimitReset:     w.LimitReset,
		Usage:          w.Usage,
		UsageDaily:     w.UsageDaily,
		UsageWeekly:    w.UsageWeekly,
		UsageMonthly:   w.UsageMonthly,
	}
	if w.ExternalUser != nil {
		rec.ExternalUser = *w.ExternalUser
	}
	return rec
}

// MintResult is MintKey's return value.
type MintResult struct {
	// Key is the raw OpenRouter API key. The provider returns it in this
	// ONE response and never again (02-OPENROUTER-API.md, POST
	// /api/v1/keys) — this package never logs it, never wraps it into an
	// error string, and never retains it beyond this call. Do not
	// "helpfully" add a debug log here; there is no endpoint that returns
	// it a second time.
	Key    string
	Record KeyRecord
}

// KeyPatch carries only the fields to change. A nil field means "leave
// unchanged"; a non-nil pointer — even one pointing at a zero value — means
// "set to exactly this", so patching a cap to zero (the admin action that
// cuts an identity off, M-05) is a real, sent zero rather than an omission.
type KeyPatch struct {
	Limit              *USDCap
	LimitReset         *LimitReset
	Disabled           *bool
	Name               *string
	IncludeBYOKInLimit *bool
}

// patchRequestWire is the PATCH /api/v1/keys/{hash} body — "every field
// optional; send only what changes" (02-OPENROUTER-API.md). omitempty here
// is SAFE, unlike mintRequestWire.Limit's: encoding/json's isEmptyValue
// only checks IsNil() for a pointer kind, never the pointee's value, so a
// nil *USDCap is correctly omitted ("not set") while a non-nil pointer to
// zero is still sent in full ("set to exactly zero") — the omitempty trap
// that bites a plain USDCap field cannot happen on a pointer field.
type patchRequestWire struct {
	Limit              *USDCap     `json:"limit,omitempty"`
	LimitReset         *LimitReset `json:"limit_reset,omitempty"`
	Disabled           *bool       `json:"disabled,omitempty"`
	Name               *string     `json:"name,omitempty"`
	IncludeBYOKInLimit *bool       `json:"include_byok_in_limit,omitempty"`
}

// deleteResponseWire is the DELETE /api/v1/keys/{hash} body.
type deleteResponseWire struct {
	Deleted bool `json:"deleted"`
}
