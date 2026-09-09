package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identitykey"
	"github.com/chetto1983/aura/internal/openrouterprovision"
)

// fakeCreditSpendReader answers PeriodSpend from an in-memory map keyed on identity id.
type fakeCreditSpendReader struct {
	spend map[string]float64
	err   error
}

func (f *fakeCreditSpendReader) PeriodSpend(_ context.Context, identityID, _ string) (float64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.spend[identityID], nil
}

// fakeCreditKeyStore answers Load/Save from a single in-memory record, scoped by
// whatever identity the caller put on ctx (identityctx.WithIdentityID) -- mirrors
// *identitykey.Store's own ctx-scoping contract.
type fakeCreditKeyStore struct {
	rec       identitykey.Record
	hasKey    bool
	loadErr   error
	saveErr   error
	saveCalls int
	savedRec  identitykey.Record
}

func (f *fakeCreditKeyStore) Load(context.Context) (identitykey.Record, error) {
	if f.loadErr != nil {
		return identitykey.Record{}, f.loadErr
	}
	if !f.hasKey {
		return identitykey.Record{}, identitykey.ErrNoKey
	}
	return f.rec, nil
}

func (f *fakeCreditKeyStore) Save(_ context.Context, r identitykey.Record) error {
	f.saveCalls++
	f.savedRec = r
	if f.saveErr != nil {
		return f.saveErr
	}
	f.rec = r
	f.hasKey = true
	return nil
}

// fakeCreditProvider records PatchCap calls and answers a configurable error.
type fakeCreditProvider struct {
	err       error
	calls     int
	lastHash  string
	lastPatch openrouterprovision.KeyPatch
}

func (f *fakeCreditProvider) PatchCap(_ context.Context, hash string, patch openrouterprovision.KeyPatch) (openrouterprovision.KeyRecord, error) {
	f.calls++
	f.lastHash = hash
	f.lastPatch = patch
	if f.err != nil {
		return openrouterprovision.KeyRecord{}, f.err
	}
	return openrouterprovision.KeyRecord{Hash: hash}, nil
}

// fakeCreditInvalidator records Invalidate calls.
type fakeCreditInvalidator struct {
	calls []string
}

func (f *fakeCreditInvalidator) Invalidate(identityID string) {
	f.calls = append(f.calls, identityID)
}

func newTestCreditServer(spend *fakeCreditSpendReader, keys *fakeCreditKeyStore, provider *fakeCreditProvider, invalidate *fakeCreditInvalidator, backendBills bool) *Server {
	s := &Server{}
	var inv creditCacheInvalidator
	if invalidate != nil {
		inv = invalidate
	}
	var prov creditProvider
	if provider != nil {
		prov = provider
	}
	var ks creditKeyStore
	if keys != nil {
		ks = keys
	}
	var sp creditSpendReader
	if spend != nil {
		sp = spend
	}
	s.SetCreditAPI(sp, ks, prov, inv, backendBills)
	return s
}

func creditRequest(method, path, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	r.SetPathValue("id", testLocalID)
	return r
}

// TestAdminGetCredit proves the GET response carries cap/spend/remaining and nothing
// about the key -- asserted on the marshalled bytes.
func TestAdminGetCredit(t *testing.T) {
	keys := &fakeCreditKeyStore{hasKey: true, rec: identitykey.Record{
		Key: "sk-or-v1-should-never-appear", Hash: "hash-abc", Label: "sk-or-v1-caa...61c",
		LimitUSD: 5.00, LimitReset: "monthly",
	}}
	spend := &fakeCreditSpendReader{spend: map[string]float64{testLocalID: 1.50}}
	s := newTestCreditServer(spend, keys, &fakeCreditProvider{}, &fakeCreditInvalidator{}, true)

	rec := httptest.NewRecorder()
	s.handleGetCredit(rec, creditRequest(http.MethodGet, "/api/admin/identities/"+testLocalID+"/credit", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"cap"`, `"spend"`, `"remaining"`} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing field %s: %s", want, body)
		}
	}
	for _, forbidden := range []string{"key", "hash", "ciphertext", "label", "sk-or-v1"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("response leaks %q: %s", forbidden, body)
		}
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["cap"] != 5.0 {
		t.Errorf("cap = %v, want 5.0", out["cap"])
	}
	if out["spend"] != 1.5 {
		t.Errorf("spend = %v, want 1.5", out["spend"])
	}
	if out["remaining"] != 3.5 {
		t.Errorf("remaining = %v, want 3.5", out["remaining"])
	}
}

// TestAdminGetCreditLocalBackendExempt proves CRED-09: on a non-billing backend the
// response says exempt rather than a fabricated zero cap.
func TestAdminGetCreditLocalBackendExempt(t *testing.T) {
	s := newTestCreditServer(&fakeCreditSpendReader{}, &fakeCreditKeyStore{}, &fakeCreditProvider{}, &fakeCreditInvalidator{}, false)
	rec := httptest.NewRecorder()
	s.handleGetCredit(rec, creditRequest(http.MethodGet, "/api/admin/identities/"+testLocalID+"/credit", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["exempt"] != true {
		t.Errorf("exempt = %v, want true", out["exempt"])
	}
	if _, ok := out["cap"]; ok {
		t.Errorf("exempt response must not carry cap, got %v", out)
	}
}

// TestAdminGetCreditSpendEqualsCapReadsAsFull proves the CRED-06 boundary: spend
// exactly equal to cap reports 100% and remaining zero; one cent below reports under
// 100%.
func TestAdminGetCreditSpendEqualsCapReadsAsFull(t *testing.T) {
	keys := &fakeCreditKeyStore{hasKey: true, rec: identitykey.Record{Hash: "h", LimitUSD: 5.00, LimitReset: "monthly"}}
	spend := &fakeCreditSpendReader{spend: map[string]float64{testLocalID: 5.00}}
	s := newTestCreditServer(spend, keys, &fakeCreditProvider{}, &fakeCreditInvalidator{}, true)

	rec := httptest.NewRecorder()
	s.handleGetCredit(rec, creditRequest(http.MethodGet, "/api/admin/identities/"+testLocalID+"/credit", ""))
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["percent_used"] != float64(100) {
		t.Errorf("percent_used = %v, want 100 when spend == cap", out["percent_used"])
	}
	if out["remaining"] != 0.0 {
		t.Errorf("remaining = %v, want 0 when spend == cap", out["remaining"])
	}

	// One cent below the cap reads under 100%.
	spend.spend[testLocalID] = 4.99
	rec2 := httptest.NewRecorder()
	s.handleGetCredit(rec2, creditRequest(http.MethodGet, "/api/admin/identities/"+testLocalID+"/credit", ""))
	var out2 map[string]any
	if err := json.Unmarshal(rec2.Body.Bytes(), &out2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	pct, _ := out2["percent_used"].(float64)
	if pct >= 100 {
		t.Errorf("percent_used = %v, want < 100 one cent below the cap", out2["percent_used"])
	}
}

// TestAdminSetCredit proves a POST with cap and interval writes the store, PATCHes
// the provider once with both, and invalidates the resolver cache exactly once.
func TestAdminSetCredit(t *testing.T) {
	keys := &fakeCreditKeyStore{hasKey: true, rec: identitykey.Record{Key: "sk-x", Hash: "hash-1", Label: "l", LimitUSD: 0, LimitReset: "monthly"}}
	provider := &fakeCreditProvider{}
	invalidator := &fakeCreditInvalidator{}
	s := newTestCreditServer(&fakeCreditSpendReader{}, keys, provider, invalidator, true)

	body := `{"cap":"5.00","reset_interval":"weekly"}`
	rec := httptest.NewRecorder()
	s.handleSetCredit(rec, creditRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/credit", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if keys.saveCalls != 1 {
		t.Fatalf("Save calls = %d, want 1", keys.saveCalls)
	}
	if keys.savedRec.LimitUSD != 5.00 || keys.savedRec.LimitReset != "weekly" {
		t.Errorf("saved record = %+v, want LimitUSD=5.00 LimitReset=weekly", keys.savedRec)
	}
	if keys.savedRec.Key != "sk-x" || keys.savedRec.Hash != "hash-1" {
		t.Errorf("saved record must preserve the existing Key/Hash, got %+v", keys.savedRec)
	}
	if provider.calls != 1 {
		t.Fatalf("PatchCap calls = %d, want 1", provider.calls)
	}
	if provider.lastPatch.Limit == nil || provider.lastPatch.LimitReset == nil {
		t.Fatalf("PatchCap patch = %+v, want both Limit and LimitReset set", provider.lastPatch)
	}
	if len(invalidator.calls) != 1 || invalidator.calls[0] != testLocalID {
		t.Fatalf("Invalidate calls = %v, want exactly one call for %s", invalidator.calls, testLocalID)
	}
}

// TestAdminSetCreditCapOnly proves changing the cap alone leaves the reset interval
// unchanged in both the store and the outgoing PATCH.
func TestAdminSetCreditCapOnly(t *testing.T) {
	keys := &fakeCreditKeyStore{hasKey: true, rec: identitykey.Record{Hash: "h", LimitUSD: 1.00, LimitReset: "daily"}}
	provider := &fakeCreditProvider{}
	s := newTestCreditServer(&fakeCreditSpendReader{}, keys, provider, &fakeCreditInvalidator{}, true)

	rec := httptest.NewRecorder()
	s.handleSetCredit(rec, creditRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/credit", `{"cap":"9.00"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if keys.savedRec.LimitReset != "daily" {
		t.Errorf("reset_interval changed to %q, want unchanged daily", keys.savedRec.LimitReset)
	}
	if keys.savedRec.LimitUSD != 9.00 {
		t.Errorf("cap = %v, want 9.00", keys.savedRec.LimitUSD)
	}
	if provider.lastPatch.LimitReset != nil {
		t.Errorf("PatchCap sent LimitReset=%v, want nil (unchanged field omitted)", *provider.lastPatch.LimitReset)
	}
}

// TestAdminSetCreditIntervalOnly is CapOnly's mirror: changing the interval alone
// leaves the cap unchanged.
func TestAdminSetCreditIntervalOnly(t *testing.T) {
	keys := &fakeCreditKeyStore{hasKey: true, rec: identitykey.Record{Hash: "h", LimitUSD: 3.00, LimitReset: "daily"}}
	provider := &fakeCreditProvider{}
	s := newTestCreditServer(&fakeCreditSpendReader{}, keys, provider, &fakeCreditInvalidator{}, true)

	rec := httptest.NewRecorder()
	s.handleSetCredit(rec, creditRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/credit", `{"reset_interval":"monthly"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if keys.savedRec.LimitUSD != 3.00 {
		t.Errorf("cap changed to %v, want unchanged 3.00", keys.savedRec.LimitUSD)
	}
	if keys.savedRec.LimitReset != "monthly" {
		t.Errorf("reset_interval = %q, want monthly", keys.savedRec.LimitReset)
	}
	if provider.lastPatch.Limit != nil {
		t.Errorf("PatchCap sent Limit=%v, want nil (unchanged field omitted)", *provider.lastPatch.Limit)
	}
}

// TestAdminSetCreditEmptyBodyRefused proves a body with neither field is refused with
// a 400 naming what was missing; no store write and no PATCH occur.
func TestAdminSetCreditEmptyBodyRefused(t *testing.T) {
	keys := &fakeCreditKeyStore{hasKey: true, rec: identitykey.Record{Hash: "h", LimitUSD: 1, LimitReset: "monthly"}}
	provider := &fakeCreditProvider{}
	s := newTestCreditServer(&fakeCreditSpendReader{}, keys, provider, &fakeCreditInvalidator{}, true)

	rec := httptest.NewRecorder()
	s.handleSetCredit(rec, creditRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/credit", `{}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if keys.saveCalls != 0 {
		t.Errorf("Save calls = %d, want 0", keys.saveCalls)
	}
	if provider.calls != 0 {
		t.Errorf("PatchCap calls = %d, want 0", provider.calls)
	}
}

// TestAdminSetCreditNegativeCapRefused proves a negative cap is refused before any
// write, while zero is accepted (a separate case, exercised by TestAdminSetCredit's
// own default-zero starting record).
func TestAdminSetCreditNegativeCapRefused(t *testing.T) {
	keys := &fakeCreditKeyStore{hasKey: true, rec: identitykey.Record{Hash: "h", LimitUSD: 1, LimitReset: "monthly"}}
	provider := &fakeCreditProvider{}
	s := newTestCreditServer(&fakeCreditSpendReader{}, keys, provider, &fakeCreditInvalidator{}, true)

	rec := httptest.NewRecorder()
	s.handleSetCredit(rec, creditRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/credit", `{"cap":"-1.00"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if keys.saveCalls != 0 || provider.calls != 0 {
		t.Errorf("negative cap must write nothing: saveCalls=%d patchCalls=%d", keys.saveCalls, provider.calls)
	}
}

// TestAdminSetCreditPrecisionRule proves a cap of 5.126 is stored and PATCHed as 5.13
// -- half-up to two decimals -- and the response reports the applied value.
func TestAdminSetCreditPrecisionRule(t *testing.T) {
	keys := &fakeCreditKeyStore{hasKey: true, rec: identitykey.Record{Hash: "h", LimitUSD: 0, LimitReset: "monthly"}}
	provider := &fakeCreditProvider{}
	s := newTestCreditServer(&fakeCreditSpendReader{}, keys, provider, &fakeCreditInvalidator{}, true)

	rec := httptest.NewRecorder()
	s.handleSetCredit(rec, creditRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/credit", `{"cap":"5.126"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if keys.savedRec.LimitUSD != 5.13 {
		t.Errorf("stored cap = %v, want 5.13", keys.savedRec.LimitUSD)
	}
	if provider.lastPatch.Limit == nil || provider.lastPatch.Limit.String() != "5.13" {
		t.Errorf("PATCHed cap = %v, want 5.13", provider.lastPatch.Limit)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["cap"] != 5.13 {
		t.Errorf("response cap = %v, want 5.13 (the applied value, not the input 5.126)", out["cap"])
	}
}

// TestAdminSetCreditProviderFailureAfterStoreWrite pins what happens when the second
// half fails: the response names which half landed, asserted on content.
func TestAdminSetCreditProviderFailureAfterStoreWrite(t *testing.T) {
	keys := &fakeCreditKeyStore{hasKey: true, rec: identitykey.Record{Hash: "h", LimitUSD: 0, LimitReset: "monthly"}}
	provider := &fakeCreditProvider{err: errors.New("provider unreachable")}
	invalidator := &fakeCreditInvalidator{}
	s := newTestCreditServer(&fakeCreditSpendReader{}, keys, provider, invalidator, true)

	rec := httptest.NewRecorder()
	s.handleSetCredit(rec, creditRequest(http.MethodPost, "/api/admin/identities/"+testLocalID+"/credit", `{"cap":"5.00"}`))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", rec.Code, rec.Body.String())
	}
	if keys.saveCalls != 1 {
		t.Fatalf("Save calls = %d, want 1 (the store write must have happened before the provider call)", keys.saveCalls)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"store_applied":true`) || !strings.Contains(body, `"provider_applied":false`) {
		t.Fatalf("response must name which half landed, got %s", body)
	}
	if len(invalidator.calls) != 0 {
		t.Errorf("Invalidate must NOT be called when the provider update failed, got %v", invalidator.calls)
	}
}

// TestAdminCreditRoutesAreIdempotencyRegistered is a map lookup: the mutating credit
// route is present in httpMutationRoutes so a route added without registration fails
// here rather than in production.
func TestAdminCreditRoutesAreIdempotencyRegistered(t *testing.T) {
	meta, ok := httpMutationRoutes["POST /api/admin/identities/{id}/credit"]
	if !ok {
		t.Fatal("POST /api/admin/identities/{id}/credit is absent from httpMutationRoutes")
	}
	if meta.Normalize == "" {
		t.Errorf("credit route metadata incomplete: %+v", meta)
	}
}
