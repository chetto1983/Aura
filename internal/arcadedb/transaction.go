package arcadedb

import (
	"context"
	"fmt"
	"net/http"
)

// sessionHeader is ArcadeDB's own header name for scoping a request to an
// open transaction: /api/v1/begin echoes it back on the 204 response, and
// any /api/v1/query or /api/v1/command carrying it back sees that
// transaction's own uncommitted writes (read-your-own-writes), invisible to
// every other session until commit. Verified live against this project's
// ArcadeDB server before writing attachFactSourceOnce's transactional path
// below: a query WITH this header sees an uncommitted write from the
// session that owns it; the identical query WITHOUT it does not, and a
// rolled-back session's write never becomes visible either way.
const sessionHeader = "arcadedb-session-id"

// beginTx opens an explicit transaction and returns its session id. Every
// commandInTx/queryInTx call meant to participate in it must pass the same
// id, and the transaction must end with exactly one commitTx or rollbackTx.
func (c *Client) beginTx(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.beginURL, nil)
	if err != nil {
		return "", fmt.Errorf("arcadedb: build begin transaction request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("arcadedb: begin transaction: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		return "", decodeServerError(resp)
	}
	session := resp.Header.Get(sessionHeader)
	if session == "" {
		return "", fmt.Errorf("arcadedb: begin transaction: server returned no %s header", sessionHeader)
	}
	return session, nil
}

// endTx posts to endpoint (commitURL or rollbackURL) for sessionID.
//
// A commit's OWN conflict signal on the whole transaction -- ArcadeDB's real
// concurrency guarantee, unlike a bare auto-committed UPDATE -- arrives here
// as a *ServerError from decodeServerError, classified by
// isTransientWriteConflict exactly like every other retryable write
// conflict this package already handles. Measured live (8 concurrent
// BEGIN+read+append+COMMIT cycles racing the same fact row): every commit
// that returned 204 has its write durably reflected in the final row, every
// commit that returned a conflict does not, and the two counts always
// match -- no commit ever silently disagreed with the persisted state, the
// failure mode this transactional rewrite exists to close (see
// attachFactSourceOnce's doc comment for what the non-transactional
// version's blind spot looked like).
func (c *Client) endTx(ctx context.Context, endpoint, sessionID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("arcadedb: build transaction-end request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set(sessionHeader, sessionID)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("arcadedb: end transaction: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		return decodeServerError(resp)
	}
	return nil
}

func (c *Client) commitTx(ctx context.Context, sessionID string) error {
	return c.endTx(ctx, c.commitURL, sessionID)
}

// rollbackTx is best-effort cleanup for a transaction this call is
// abandoning (a permanent error mid-transaction, or a caller that already
// knows commitTx will not be reached). Its own failure is not this
// package's problem to propagate: ArcadeDB reclaims an abandoned session on
// its own, and the caller already has the real error that triggered the
// rollback.
func (c *Client) rollbackTx(ctx context.Context, sessionID string) {
	_ = c.endTx(ctx, c.rollbackURL, sessionID)
}

// commandInTx and queryInTx are Command/Query scoped to an open transaction
// session (see sessionHeader's doc comment for the isolation this buys).
func (c *Client) commandInTx(ctx context.Context, sessionID, statement string, params map[string]any) ([]map[string]any, error) {
	return c.executeSession(ctx, c.commandURL, sessionID, "sql", statement, params)
}

func (c *Client) queryInTx(ctx context.Context, sessionID, statement string, params map[string]any) ([]map[string]any, error) {
	return c.executeSession(ctx, c.queryURL, sessionID, "sql", statement, params)
}
