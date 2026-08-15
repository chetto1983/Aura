package garageadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/idroot"
)

// bucketPrefix namespaces every per-identity bucket as aura-<identity>.
const bucketPrefix = "aura-"

// maxAdminBody caps the admin API response we read into memory (defensive; admin
// responses are small JSON objects).
const maxAdminBody = 1 << 20

// ErrEmptyConfig is returned by New when the endpoint or token is empty — the
// admin client fails closed rather than dialing an unauthenticated admin API.
var ErrEmptyConfig = errors.New("garageadmin: endpoint and token are required")

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient overrides the default *http.Client (used by tests to inject an
// httptest server transport / timeout).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.hc = hc
		}
	}
}

// Client talks to the Garage Admin API v2 with a bearer admin token over the
// internal compose network (never host-reachable — Pitfall 3).
type Client struct {
	endpoint string
	token    string
	hc       *http.Client
}

// New builds a Client. It fails closed on an empty endpoint or token so a
// mis-provisioned deployment never dials an unauthenticated admin API.
func New(endpoint, token string, opts ...Option) (*Client, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	token = strings.TrimSpace(token)
	if endpoint == "" || token == "" {
		return nil, ErrEmptyConfig
	}
	c := &Client{endpoint: endpoint, token: token, hc: &http.Client{Timeout: 30 * time.Second}}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// BucketForIdentity maps an identity to its bucket name aura-<identity> after the
// shared profile traversal/charset guard rejects any injection/traversal — so a
// hostile identity string can never be spliced into an admin API call or resolve
// to another identity's bucket.
func BucketForIdentity(identity string) (string, error) {
	if err := idroot.ValidateIdentity(identity); err != nil {
		return "", fmt.Errorf("garageadmin: %w", err)
	}
	return bucketPrefix + identity, nil
}

// post issues an authenticated POST to path with an optional JSON body and query,
// returning the status code and the (capped) raw response body.
func (c *Client) post(ctx context.Context, path string, query url.Values, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("garageadmin: encode %s: %w", path, err)
		}
		reader = bytes.NewReader(b)
	}
	target := c.endpoint + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("garageadmin: build %s: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("garageadmin: call %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAdminBody))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("garageadmin: read %s: %w", path, err)
	}
	return resp.StatusCode, raw, nil
}

// statusErr builds an error from an unexpected status, including a bounded snippet
// of the body for diagnostics (admin errors carry a small JSON code object).
func statusErr(op string, status int, raw []byte) error {
	snippet := strings.TrimSpace(string(raw))
	if len(snippet) > 256 {
		snippet = snippet[:256]
	}
	return fmt.Errorf("garageadmin: %s returned status %d: %s", op, status, snippet)
}

func is2xx(status int) bool { return status >= 200 && status < 300 }

// CreateBucket creates the bucket with the given global alias and returns its
// internal id. It is idempotent: if the alias already exists (Garage answers
// non-2xx), the existing bucket is resolved by alias and its id returned — so the
// provisioning saga can re-run a create leg. A genuine failure (auth/other) makes
// the by-alias lookup fail too, and the original create error is surfaced.
func (c *Client) CreateBucket(ctx context.Context, globalAlias string) (string, error) {
	status, raw, err := c.post(ctx, "/v2/CreateBucket", nil, createBucketRequest{GlobalAlias: globalAlias})
	if err != nil {
		return "", err
	}
	if is2xx(status) {
		var info bucketInfo
		if err := json.Unmarshal(raw, &info); err != nil {
			return "", fmt.Errorf("garageadmin: decode CreateBucket: %w", err)
		}
		if info.ID != "" {
			return info.ID, nil
		}
	}
	// Non-2xx (already-exists) or 2xx-without-id: resolve the existing bucket by
	// alias. Success here means the bucket already existed → idempotent success.
	if id, lookupErr := c.BucketIDByAlias(ctx, globalAlias); lookupErr == nil && id != "" {
		return id, nil
	}
	if is2xx(status) {
		return "", fmt.Errorf("garageadmin: CreateBucket returned no bucket id")
	}
	return "", statusErr("CreateBucket", status, raw)
}

// BucketIDByAlias resolves a bucket's internal id from its global alias via
// GET /v2/GetBucketInfo?globalAlias=<alias>. Exported because a caller that
// needs the id of an EXISTING bucket should ask for it, not call CreateBucket
// and lean on its idempotence -- that reads as "mint" at every call site.
func (c *Client) BucketIDByAlias(ctx context.Context, alias string) (string, error) {
	target := c.endpoint + "/v2/GetBucketInfo?" + url.Values{"globalAlias": {alias}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", fmt.Errorf("garageadmin: build GetBucketInfo: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("garageadmin: call GetBucketInfo: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAdminBody))
	if err != nil {
		return "", fmt.Errorf("garageadmin: read GetBucketInfo: %w", err)
	}
	if !is2xx(resp.StatusCode) {
		return "", statusErr("GetBucketInfo", resp.StatusCode, raw)
	}
	var info bucketInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return "", fmt.Errorf("garageadmin: decode GetBucketInfo: %w", err)
	}
	return info.ID, nil
}

// CreateKey creates a scoped access key and returns its access + secret key. The
// secret is shown ONCE by Garage and is unrecoverable afterwards, so this is NOT
// name-idempotent (Garage key names are non-unique labels): the caller persists
// the returned secret immediately (Task 3, encrypted at rest) and skips CreateKey
// on saga re-run by consulting that stored row — re-creating would mint a second
// key and orphan the first.
func (c *Client) CreateKey(ctx context.Context, name string) (accessKeyID, secretAccessKey string, err error) {
	status, raw, err := c.post(ctx, "/v2/CreateKey", nil, createKeyRequest{Name: name})
	if err != nil {
		return "", "", err
	}
	if !is2xx(status) {
		return "", "", statusErr("CreateKey", status, raw)
	}
	var info keyInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return "", "", fmt.Errorf("garageadmin: decode CreateKey: %w", err)
	}
	if info.AccessKeyID == "" || info.SecretAccessKey == "" {
		return "", "", fmt.Errorf("garageadmin: CreateKey returned empty credentials")
	}
	return info.AccessKeyID, info.SecretAccessKey, nil
}

// AllowBucketKey grants perms to accessKeyID on bucketID (per-bucket, F-007).
func (c *Client) AllowBucketKey(ctx context.Context, bucketID, accessKeyID string, perms Permissions) error {
	status, raw, err := c.post(ctx, "/v2/AllowBucketKey", nil, bucketKeyRequest{
		BucketID: bucketID, AccessKeyID: accessKeyID, Permissions: perms,
	})
	if err != nil {
		return err
	}
	if !is2xx(status) {
		return statusErr("AllowBucketKey", status, raw)
	}
	return nil
}

// DenyBucketKey revokes perms from accessKeyID on bucketID. Idempotent: a missing
// bucket or key (404) is treated as already-denied.
func (c *Client) DenyBucketKey(ctx context.Context, bucketID, accessKeyID string, perms Permissions) error {
	status, raw, err := c.post(ctx, "/v2/DenyBucketKey", nil, bucketKeyRequest{
		BucketID: bucketID, AccessKeyID: accessKeyID, Permissions: perms,
	})
	if err != nil {
		return err
	}
	if is2xx(status) || status == http.StatusNotFound {
		return nil
	}
	return statusErr("DenyBucketKey", status, raw)
}

// DeleteBucket deletes the bucket by id. Idempotent (404-missing = success) so the
// de-provisioning saga can re-run.
func (c *Client) DeleteBucket(ctx context.Context, bucketID string) error {
	status, raw, err := c.post(ctx, "/v2/DeleteBucket", url.Values{"id": {bucketID}}, nil)
	if err != nil {
		return err
	}
	if is2xx(status) || status == http.StatusNotFound {
		return nil
	}
	return statusErr("DeleteBucket", status, raw)
}

// DeleteKey deletes the key by id. Idempotent (404-missing = success).
func (c *Client) DeleteKey(ctx context.Context, accessKeyID string) error {
	status, raw, err := c.post(ctx, "/v2/DeleteKey", url.Values{"id": {accessKeyID}}, nil)
	if err != nil {
		return err
	}
	if is2xx(status) || status == http.StatusNotFound {
		return nil
	}
	return statusErr("DeleteKey", status, raw)
}
