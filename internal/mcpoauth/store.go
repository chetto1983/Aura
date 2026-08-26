// Package mcpoauth persists the OAuth grant a remote MCP server issues, one row per
// (identity, server), with every credential encrypted at rest.
//
// It exists because a remote MCP server's token identifies a PERSON, not a deployment.
// Aura's managed catalog is a single shared servers.json, so a token parked there would be
// readable by every identity on the box; the token therefore lives in Postgres under the
// two RLS layers migration 0100 installs, and the ciphertext is unreadable even to a
// caller who reaches the row.
//
// What this package does NOT do: OAuth. Discovery, dynamic client registration, PKCE,
// issuer validation, the code exchange and the refresh all live in the MCP SDK's
// auth.AuthorizationCodeHandler, which is already a dependency. This is the storage half
// the SDK asks for through its InitialTokenSource and NewTokenSource seams — the same
// division LibreChat and Hermes both settled on.
package mcpoauth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
)

// keyDerivationInfo domain-separates this store's wrapping key from every other key
// derived from the same AURA_AUTHULA_SECRET. Sharing an info string with
// internal/objectstore would mean one leaked key opens both stores, which is the whole
// reason HKDF takes an info parameter.
const keyDerivationInfo = "aura-mcp-oauth-identity-key-v1"

// ErrNoGrant reports that this identity has not authorized this server. It is a normal
// answer, not a failure: it is what makes the SDK run the authorization flow instead of
// mounting the server unauthenticated.
var ErrNoGrant = errors.New("mcpoauth: no grant for this identity and server")

// ErrAmbiguousOwner reports that more than one identity has authorized the same server, so
// a single process-wide mount cannot be attributed to either.
var ErrAmbiguousOwner = errors.New("mcpoauth: more than one identity has authorized this server")

// Grant is one decrypted OAuth grant, in memory only.
//
// ClientInfo carries the dynamic-client-registration result when the authorization server
// minted one. It is encrypted like the tokens because it is not metadata: DCR returns a
// client_secret, so the registration result is itself a credential — the detail LibreChat
// encrypts as a third ciphertext and the one an implementation is most likely to miss.
type Grant struct {
	ServerName   string
	ResourceURL  string
	AccessToken  string
	RefreshToken string
	ClientInfo   []byte
	TokenType    string
	Scopes       []string
	// ExpiresAt is ABSOLUTE. The OAuth token shape carries a relative expires_in, which
	// has no wall-clock reference once the process restarts; storing the instant is what
	// lets a restart reconstruct the remaining TTL instead of believing a dead token is
	// still valid until the first 401.
	ExpiresAt time.Time
}

// Expired reports whether the grant's access token is past its expiry, with a leeway so a
// token that dies mid-flight is refreshed before the request rather than after the 401.
// A zero ExpiresAt means the server issued no expiry and the token is taken at its word.
func (g Grant) Expired(now time.Time, leeway time.Duration) bool {
	if g.ExpiresAt.IsZero() {
		return false
	}
	return !now.Add(leeway).Before(g.ExpiresAt)
}

// Store reads and writes grants for the identity carried on the request context.
type Store struct {
	pool *pgxpool.Pool
	aead cipher.AEAD
}

// NewStore builds the store. authulaSecretHex is the 64-hex-char AURA_AUTHULA_SECRET, the
// same secret and the same trust boundary internal/objectstore uses. It fails closed on a
// malformed secret so a mis-provisioned deployment never runs with a weak wrapping key.
func NewStore(pool *pgxpool.Pool, authulaSecretHex string) (*Store, error) {
	if pool == nil {
		return nil, errors.New("mcpoauth: nil pool")
	}
	key, err := deriveKey(authulaSecretHex)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: gcm: %w", err)
	}
	return &Store{pool: pool, aead: aead}, nil
}

// Load returns the grant this identity holds for serverName, or ErrNoGrant.
//
// It does NOT check expiry: an expired access token is still worth returning, because its
// refresh token is what avoids sending the operator back to a browser. Deciding what to do
// with an expired grant belongs to the token source, not to storage.
func (s *Store) Load(ctx context.Context, serverName string) (Grant, error) {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return Grant{}, err
	}
	id, err := parseUUID(identity)
	if err != nil {
		return Grant{}, err
	}
	var row sqlc.AuraIdentityMcpOauth
	err = db.WithIdentityTx(ctx, s.pool, identity, func(q *sqlc.Queries) error {
		var qerr error
		row, qerr = q.GetIdentityMCPOAuth(ctx, sqlc.GetIdentityMCPOAuthParams{
			IdentityID: id,
			ServerName: serverName,
		})
		return qerr
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Grant{}, fmt.Errorf("%w: %q", ErrNoGrant, serverName)
	}
	if err != nil {
		return Grant{}, fmt.Errorf("mcpoauth: load %q: %w", serverName, err)
	}
	return s.decodeRow(row)
}

// Save writes the grant, replacing any earlier one for the same (identity, server). A
// refresh rewrites the same row rather than accumulating history: an old access token is
// not evidence worth keeping, it is a credential worth destroying.
func (s *Store) Save(ctx context.Context, g Grant) error {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return err
	}
	id, err := parseUUID(identity)
	if err != nil {
		return err
	}
	if strings.TrimSpace(g.ServerName) == "" {
		return errors.New("mcpoauth: save needs a server name")
	}
	if strings.TrimSpace(g.AccessToken) == "" {
		// A grant with no access token is not a grant. Storing one would make Load
		// succeed and hand the transport an empty Authorization header, which reads as
		// an auth failure from the server rather than as the missing grant it is.
		return errors.New("mcpoauth: save needs an access token")
	}
	access, err := s.seal([]byte(g.AccessToken))
	if err != nil {
		return err
	}
	refresh, err := s.sealOptional([]byte(g.RefreshToken))
	if err != nil {
		return err
	}
	clientInfo, err := s.sealOptional(g.ClientInfo)
	if err != nil {
		return err
	}
	tokenType := strings.TrimSpace(g.TokenType)
	if tokenType == "" {
		tokenType = "Bearer"
	}
	scopes := g.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	params := sqlc.UpsertIdentityMCPOAuthParams{
		IdentityID:      id,
		ServerName:      g.ServerName,
		ResourceUrl:     g.ResourceURL,
		AccessTokenEnc:  access,
		RefreshTokenEnc: refresh,
		ClientInfoEnc:   clientInfo,
		TokenType:       tokenType,
		Scopes:          scopes,
		ExpiresAt:       timestamptz(g.ExpiresAt),
	}
	err = db.WithIdentityTx(ctx, s.pool, identity, func(q *sqlc.Queries) error {
		return q.UpsertIdentityMCPOAuth(ctx, params)
	})
	if err != nil {
		return fmt.Errorf("mcpoauth: save %q: %w", g.ServerName, err)
	}
	return nil
}

// Delete revokes the local grant and reports whether a row actually went. The bool is the
// point: without it a caller cannot tell a real revocation from a no-op on a row RLS
// filtered away, and would report success for a token that is still live.
func (s *Store) Delete(ctx context.Context, serverName string) (bool, error) {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return false, err
	}
	id, err := parseUUID(identity)
	if err != nil {
		return false, err
	}
	var affected int64
	err = db.WithIdentityTx(ctx, s.pool, identity, func(q *sqlc.Queries) error {
		var qerr error
		affected, qerr = q.DeleteIdentityMCPOAuth(ctx, sqlc.DeleteIdentityMCPOAuthParams{
			IdentityID: id,
			ServerName: serverName,
		})
		return qerr
	})
	if err != nil {
		return false, fmt.Errorf("mcpoauth: delete %q: %w", serverName, err)
	}
	return affected > 0, nil
}

// Authorization is one entry in the list of servers an identity has authorized. It carries
// no ciphertext on purpose: rendering a name and an expiry must not pull three credentials
// into memory.
type Authorization struct {
	ServerName  string
	ResourceURL string
	ExpiresAt   time.Time
	UpdatedAt   time.Time
}

// List returns the servers this identity has authorized, ordered by name.
func (s *Store) List(ctx context.Context) ([]Authorization, error) {
	identity, err := requireIdentity(ctx)
	if err != nil {
		return nil, err
	}
	id, err := parseUUID(identity)
	if err != nil {
		return nil, err
	}
	var rows []sqlc.ListIdentityMCPOAuthServersRow
	err = db.WithIdentityTx(ctx, s.pool, identity, func(q *sqlc.Queries) error {
		var qerr error
		rows, qerr = q.ListIdentityMCPOAuthServers(ctx, id)
		return qerr
	})
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: list: %w", err)
	}
	out := make([]Authorization, 0, len(rows))
	for _, r := range rows {
		out = append(out, Authorization{
			ServerName:  r.ServerName,
			ResourceURL: r.ResourceUrl,
			ExpiresAt:   r.ExpiresAt.Time,
			UpdatedAt:   r.UpdatedAt.Time,
		})
	}
	return out, nil
}

func (s *Store) decodeRow(row sqlc.AuraIdentityMcpOauth) (Grant, error) {
	access, err := s.open(row.AccessTokenEnc)
	if err != nil {
		return Grant{}, err
	}
	refresh, err := s.openOptional(row.RefreshTokenEnc)
	if err != nil {
		return Grant{}, err
	}
	clientInfo, err := s.openOptional(row.ClientInfoEnc)
	if err != nil {
		return Grant{}, err
	}
	return Grant{
		ServerName:   row.ServerName,
		ResourceURL:  row.ResourceUrl,
		AccessToken:  string(access),
		RefreshToken: string(refresh),
		ClientInfo:   clientInfo,
		TokenType:    row.TokenType,
		Scopes:       row.Scopes,
		ExpiresAt:    row.ExpiresAt.Time,
	}, nil
}

// seal prepends a fresh random nonce to the ciphertext, matching
// internal/objectstore.IdentityStore so both stores read the same on disk.
func (s *Store) seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("mcpoauth: nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// sealOptional returns nil for an absent value rather than the ciphertext of an empty
// string, so a NULL column keeps meaning "the server issued none".
func (s *Store) sealOptional(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, nil
	}
	return s.seal(plaintext)
}

func (s *Store) open(ciphertext []byte) ([]byte, error) {
	ns := s.aead.NonceSize()
	if len(ciphertext) < ns {
		return nil, fmt.Errorf("mcpoauth: ciphertext too short (%d < %d)", len(ciphertext), ns)
	}
	plaintext, err := s.aead.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
	if err != nil {
		// Deliberately does not echo the ciphertext: a decrypt failure means the wrapping
		// key changed or the row was tampered with, and neither is diagnosed by dumping
		// bytes into a log.
		return nil, fmt.Errorf("mcpoauth: decrypt: %w", err)
	}
	return plaintext, nil
}

func (s *Store) openOptional(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, nil
	}
	return s.open(ciphertext)
}

// requireIdentity fails closed on a context with no principal. There is no `local`
// fallback here, unlike internal/objectstore: that store keeps one for the CLI's shared
// bucket, but a token belongs to a person, so "no principal" must never resolve to
// somebody's grant.
func requireIdentity(ctx context.Context) (string, error) {
	id := strings.TrimSpace(identityctx.IdentityID(ctx))
	if id == "" {
		return "", errors.New("mcpoauth: no identity on context")
	}
	return id, nil
}

func parseUUID(identity string) (pgtype.UUID, error) {
	var out pgtype.UUID
	if err := out.Scan(identity); err != nil {
		return pgtype.UUID{}, fmt.Errorf("mcpoauth: identity %q is not a uuid: %w", identity, err)
	}
	return out, nil
}

func timestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

func deriveKey(authulaSecretHex string) ([]byte, error) {
	return deriveKeyWithInfo(authulaSecretHex, keyDerivationInfo)
}

// deriveKeyWithInfo takes the info string as a parameter so a test can derive a key under
// ANOTHER store's info and assert the two differ. Domain separation asserted in a comment
// is domain separation nobody checks.
func deriveKeyWithInfo(authulaSecretHex, info string) ([]byte, error) {
	secret := strings.TrimSpace(authulaSecretHex)
	if len(secret) != 64 {
		return nil, errors.New("mcpoauth: AURA_AUTHULA_SECRET must be 64 hex characters (32 bytes)")
	}
	raw, err := hex.DecodeString(secret)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: AURA_AUTHULA_SECRET must be valid hex: %w", err)
	}
	key, err := hkdf.Key(sha256.New, raw, nil, info, 32)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: derive key: %w", err)
	}
	return key, nil
}

// OwnerOf reports which of candidates holds a grant for serverName.
//
// It exists because process boot has no identity and the grants are per-identity. LibreChat
// never needs this: everything there happens inside a request, so getOAuthTokens() always
// has a userId to load with (MCPConnectionFactory.discoverToolsInternal). Aura mounts at
// boot as well, and a server the human already authorized must come back after a restart
// rather than demand a second consent — so the identity is resolved from the grant instead
// of from a request.
//
// Each candidate is asked with its own identity bound, which is what keeps this inside RLS
// rather than around it: migration 0100's RESTRICTIVE floor means a caller with no
// app.current_identity sees nothing, and that floor stays intact here.
//
// A server two identities have authorized returns ErrAmbiguousOwner. One process-wide tool
// registry cannot serve both without handing one person's tools the other person's token,
// and guessing which would be the worse answer. Aura's OWN sidecars are the documented
// exception and use OwnersOf instead — see the comment there for why several owners are
// normal for those and only for those.
func (s *Store) OwnerOf(ctx context.Context, serverName string, candidates []string) (string, error) {
	owners, err := s.OwnersOf(ctx, serverName, candidates)
	if err != nil {
		return "", err
	}
	if len(owners) > 1 {
		return "", fmt.Errorf("%w: %q", ErrAmbiguousOwner, serverName)
	}
	return owners[0], nil
}

// OwnersOf returns EVERY candidate holding a grant for serverName, in the order given,
// or ErrNoGrant when none does.
//
// It is OwnerOf without the uniqueness rule, and it exists for the one server class where
// several owners are normal rather than a conflict: a sidecar Aura ships itself. Every
// identity is meant to hold its own grant for those (the subject is the tenancy
// boundary), while the process-wide boot mount only needs SOME authorized subject to
// discover the tool schema with — the per-identity session pool then opens each caller's
// own session, and IdentityBindingMiddleware refuses a call from anyone else. Making
// OwnerOf refuse that case would leave Aura's own memory unmounted for everybody the
// moment a second person was enrolled.
//
// For a third-party server the ambiguity refusal stands: OwnerOf is still what that path
// calls, and one process-wide registry cannot serve two people's remote tokens.
func (s *Store) OwnersOf(ctx context.Context, serverName string, candidates []string) ([]string, error) {
	found := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		scoped := identityctx.WithIdentityID(ctx, candidate)
		if _, err := s.Load(scoped, serverName); err != nil {
			if errors.Is(err, ErrNoGrant) {
				continue
			}
			return nil, err
		}
		found = append(found, candidate)
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrNoGrant, serverName)
	}
	return found, nil
}
