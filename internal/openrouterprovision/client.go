// client.go is the OpenRouter Provisioning-API client's four verbs:
// MintKey, GetKey, PatchKey and RevokeKey.
package openrouterprovision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// MintKey mints one identity's OpenRouter key via POST /api/v1/keys
// (02-OPENROUTER-API.md, retrieved 2026-09-08). Requires a MANAGEMENT
// credential — per that document's credential table, a management key can
// mint and revoke keys account-wide but cannot call the completion
// endpoints, and an inference key cannot call this endpoint at all.
//
// The raw key in the result is returned by the provider in THIS response
// and never again: MintKey hands it to the caller and this package never
// logs it, never wraps it into an error string, and never retains it past
// the call.
func MintKey(ctx context.Context, client *http.Client, baseURL, apiKey string, req MintRequest) (MintResult, error) {
	if !req.LimitReset.valid() {
		return MintResult{}, fmt.Errorf("%w: %q", ErrInvalidLimitReset, req.LimitReset)
	}
	if strings.TrimSpace(req.IdentityID) == "" {
		return MintResult{}, fmt.Errorf("openrouterprovision: mint key: empty identity id")
	}

	wireReq := mintRequestWire{
		Name:       req.Name,
		Limit:      req.Limit,
		LimitReset: req.LimitReset,
		External:   mintExternalWire{User: req.IdentityID},
	}
	payload, err := json.Marshal(wireReq)
	if err != nil {
		return MintResult{}, fmt.Errorf("openrouterprovision: mint key: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/keys", bytes.NewReader(payload))
	if err != nil {
		return MintResult{}, fmt.Errorf("openrouterprovision: mint key: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return MintResult{}, fmt.Errorf("openrouterprovision: mint key: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return MintResult{}, fmt.Errorf("openrouterprovision: mint key: read response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		return MintResult{}, classify(resp.StatusCode, respBody)
	}

	var wireResp mintResponseWire
	if err := json.Unmarshal(respBody, &wireResp); err != nil {
		return MintResult{}, fmt.Errorf("openrouterprovision: mint key: decode response: %w", err)
	}
	return MintResult{Key: wireResp.Key, Record: keyRecordFromWire(wireResp.Data)}, nil
}

// GetKey reads one key's record via GET /api/v1/keys/{hash}. Requires a
// MANAGEMENT credential (same table as MintKey) — this is the admin-roster
// read, distinct from GET /api/v1/key (singular), which
// internal/llm/spend.go already calls with the INFERENCE key for a
// different purpose (that key's own spend, not the roster).
func GetKey(ctx context.Context, client *http.Client, baseURL, apiKey, hash string) (KeyRecord, error) {
	if strings.TrimSpace(hash) == "" {
		return KeyRecord{}, fmt.Errorf("openrouterprovision: get key: empty hash")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/keys/"+hash, nil)
	if err != nil {
		return KeyRecord{}, fmt.Errorf("openrouterprovision: get key: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(httpReq)
	if err != nil {
		return KeyRecord{}, fmt.Errorf("openrouterprovision: get key: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return KeyRecord{}, fmt.Errorf("openrouterprovision: get key: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return KeyRecord{}, classify(resp.StatusCode, respBody)
	}

	var wireResp struct {
		Data keyDataWire `json:"data"`
	}
	if err := json.Unmarshal(respBody, &wireResp); err != nil {
		return KeyRecord{}, fmt.Errorf("openrouterprovision: get key: decode response: %w", err)
	}
	return keyRecordFromWire(wireResp.Data), nil
}

// PatchKey changes only the fields set in patch — "every field optional;
// send only what changes" (02-OPENROUTER-API.md, PATCH
// /api/v1/keys/{hash}).
//
// Measured live 2026-09-08: a downward limit denies inference in about 5
// seconds (M-05); an upward limit on an exhausted key took about 25
// seconds before inference recovered (403 at t+18s, 200 at t+25s — M-06).
// PatchKey does not wait for either: internal/llm/openai_compat/client.go:47
// deliberately disables the SDK's own automatic retry, and a client that
// hides a 25-second propagation behind a blocking wait turns a stated
// latency into an unexplained hang. The cockpit surfaces the latency to
// the human (UI-SPEC §Credit panel item 4); this function does not smooth
// it over.
func PatchKey(ctx context.Context, client *http.Client, baseURL, apiKey, hash string, patch KeyPatch) (KeyRecord, error) {
	if strings.TrimSpace(hash) == "" {
		return KeyRecord{}, fmt.Errorf("openrouterprovision: patch key: empty hash")
	}
	if patch.LimitReset != nil && !patch.LimitReset.valid() {
		return KeyRecord{}, fmt.Errorf("%w: %q", ErrInvalidLimitReset, *patch.LimitReset)
	}

	// KeyPatch and patchRequestWire share identical fields in identical
	// order — a straight conversion (staticcheck S1016) rather than a
	// field-by-field literal.
	wireReq := patchRequestWire(patch)
	payload, err := json.Marshal(wireReq)
	if err != nil {
		return KeyRecord{}, fmt.Errorf("openrouterprovision: patch key: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, strings.TrimRight(baseURL, "/")+"/keys/"+hash, bytes.NewReader(payload))
	if err != nil {
		return KeyRecord{}, fmt.Errorf("openrouterprovision: patch key: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return KeyRecord{}, fmt.Errorf("openrouterprovision: patch key: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return KeyRecord{}, fmt.Errorf("openrouterprovision: patch key: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return KeyRecord{}, classify(resp.StatusCode, respBody)
	}

	var wireResp struct {
		Data keyDataWire `json:"data"`
	}
	if err := json.Unmarshal(respBody, &wireResp); err != nil {
		return KeyRecord{}, fmt.Errorf("openrouterprovision: patch key: decode response: %w", err)
	}
	return keyRecordFromWire(wireResp.Data), nil
}

// RevokeKey is the CRED-08 verb: DELETE then a verifying GET, inside one
// function so the verification cannot be skipped by a caller — the
// requirement is "the revocation is verified rather than assumed", and a
// verification a caller can forget is one that a caller will forget.
//
// Measured live 2026-09-08 (M-10): DELETE kills inference within about 5
// seconds and the following GET /keys/{hash} then returns 404. A DELETE
// that itself returns 404 (the key is already gone) still counts as
// success provided the GET also 404s — the deprovision saga re-runs after
// an interruption and must converge, matching
// internal/agui/deprovision.go's Delete/Deny by-id 404=success contract
// for every other plane.
func RevokeKey(ctx context.Context, client *http.Client, baseURL, apiKey, hash string) error {
	if strings.TrimSpace(hash) == "" {
		return fmt.Errorf("openrouterprovision: revoke key: empty hash")
	}
	url := strings.TrimRight(baseURL, "/") + "/keys/" + hash

	delReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("openrouterprovision: revoke key: build delete request: %w", err)
	}
	delReq.Header.Set("Authorization", "Bearer "+apiKey)

	delResp, err := client.Do(delReq)
	if err != nil {
		return fmt.Errorf("openrouterprovision: revoke key: %w", err)
	}
	delBody, err := io.ReadAll(delResp.Body)
	_ = delResp.Body.Close()
	if err != nil {
		return fmt.Errorf("openrouterprovision: revoke key: read delete response: %w", err)
	}
	if delResp.StatusCode != http.StatusOK && delResp.StatusCode != http.StatusNotFound {
		return classify(delResp.StatusCode, delBody)
	}
	if delResp.StatusCode == http.StatusOK {
		var wireResp deleteResponseWire
		if err := json.Unmarshal(delBody, &wireResp); err != nil {
			return fmt.Errorf("openrouterprovision: revoke key: decode delete response: %w", err)
		}
		if !wireResp.Deleted {
			return fmt.Errorf("openrouterprovision: revoke key: provider did not confirm deletion")
		}
	}

	// Verify: a revoke that cannot prove the key is gone is a failure, not
	// a success (CRED-08) — this is the whole point of the pair.
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("openrouterprovision: revoke key: build verify request: %w", err)
	}
	getReq.Header.Set("Authorization", "Bearer "+apiKey)

	getResp, err := client.Do(getReq)
	if err != nil {
		return fmt.Errorf("openrouterprovision: revoke key: verify: %w", err)
	}
	_, err = io.ReadAll(getResp.Body)
	_ = getResp.Body.Close()
	if err != nil {
		return fmt.Errorf("openrouterprovision: revoke key: read verify response: %w", err)
	}
	if getResp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("openrouterprovision: revoke key: verification failed: GET /keys/%s returned %d, want 404 — the key is still readable", hash, getResp.StatusCode)
	}
	return nil
}
