package garageadmin

import (
	"context"
	"errors"
	"net/http"
)

// bucketPrefix namespaces every per-identity bucket as aura-<identity>.
const bucketPrefix = "aura-"

// ErrEmptyConfig is returned by New when the endpoint or token is empty — the
// admin client fails closed rather than dialing an unauthenticated admin API.
var ErrEmptyConfig = errors.New("garageadmin: endpoint and token are required")

// errNotImplemented is the RED-phase stub sentinel; the GREEN implementation
// replaces every method body and removes this.
var errNotImplemented = errors.New("garageadmin: not implemented")

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

// Client talks to the Garage Admin API v2 with a bearer admin token.
type Client struct {
	endpoint string
	token    string
	hc       *http.Client
}

// New builds a Client. It fails closed on an empty endpoint or token.
func New(endpoint, token string, opts ...Option) (*Client, error) {
	_ = opts
	return nil, errNotImplemented
}

// BucketForIdentity maps an identity to its bucket name aura-<identity>.
func BucketForIdentity(identity string) (string, error) {
	_ = identity
	return "", errNotImplemented
}

// CreateBucket creates the bucket with the given global alias and returns its
// internal id. It is idempotent: an already-existing alias resolves to that
// bucket's id.
func (c *Client) CreateBucket(ctx context.Context, globalAlias string) (string, error) {
	_, _ = ctx, globalAlias
	return "", errNotImplemented
}

// CreateKey creates a scoped access key and returns its access + secret key.
func (c *Client) CreateKey(ctx context.Context, name string) (accessKeyID, secretAccessKey string, err error) {
	_, _ = ctx, name
	return "", "", errNotImplemented
}

// AllowBucketKey grants perms to accessKeyID on bucketID.
func (c *Client) AllowBucketKey(ctx context.Context, bucketID, accessKeyID string, perms Permissions) error {
	_, _, _, _ = ctx, bucketID, accessKeyID, perms
	return errNotImplemented
}

// DenyBucketKey revokes perms from accessKeyID on bucketID. Idempotent.
func (c *Client) DenyBucketKey(ctx context.Context, bucketID, accessKeyID string, perms Permissions) error {
	_, _, _, _ = ctx, bucketID, accessKeyID, perms
	return errNotImplemented
}

// DeleteBucket deletes the bucket by id. Idempotent (404-missing = success).
func (c *Client) DeleteBucket(ctx context.Context, bucketID string) error {
	_, _ = ctx, bucketID
	return errNotImplemented
}

// DeleteKey deletes the key by id. Idempotent (404-missing = success).
func (c *Client) DeleteKey(ctx context.Context, accessKeyID string) error {
	_, _ = ctx, accessKeyID
	return errNotImplemented
}
