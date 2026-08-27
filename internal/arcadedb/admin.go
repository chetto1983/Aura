package arcadedb

// Server/database/user lifecycle -- create/drop a database, create/drop a
// user, version and credential probes. Split out of client.go (statement
// execution: Query/Command/Read/Write/Script) to keep that file under
// CLAUDE.md's 600 LOC file cap; this is one coherent concern (SERVER-level
// admin, not per-database statements) the same way transaction.go is.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// CreateDatabase provisions one database through the SERVER endpoint, which is
// where database lifecycle lives — the per-database command endpoint cannot make
// one that does not exist yet. It needs a credential with server rights, so this
// is the one call the memory sidecar makes as the admin user rather than as the
// tenant.
func (c *Client) CreateDatabase(ctx context.Context, name string) ([]map[string]any, error) {
	return c.serverCommand(ctx, "create database "+name)
}

// DropDatabase removes one database and everything in it. It is what
// "purge this identity's memory" means when memory is one database per identity:
// exact, and with nothing left to sweep.
func (c *Client) DropDatabase(ctx context.Context, name string) ([]map[string]any, error) {
	return c.serverCommand(ctx, "drop database "+name)
}

// serverCommand posts to the server endpoint, whose payload is {"command": …} —
// not the {"language", "command", "params"} shape every database statement uses.
func (c *Client) serverCommand(ctx context.Context, command string) ([]map[string]any, error) {
	body, err := json.Marshal(map[string]any{"command": command})
	if err != nil {
		return nil, fmt.Errorf("arcadedb: encode server command: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.serverURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("arcadedb: build server request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.authHeader)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: server request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, decodeServerError(resp)
	}
	return nil, nil
}

// CreateUser provisions a server user scoped to the named databases. The
// databases map is the ONLY way to set that scope: ArcadeDB has no command to
// widen an existing user's access (`update user` is not a server command, and
// `ALTER USER` only changes a password), so a user is created with the databases
// it will ever reach.
func (c *Client) CreateUser(ctx context.Context, name, password string, databases map[string][]string) error {
	payload, err := json.Marshal(map[string]any{
		"name": name, "password": password, "databases": databases,
	})
	if err != nil {
		return fmt.Errorf("arcadedb: encode user: %w", err)
	}
	_, err = c.serverCommand(ctx, "create user "+string(payload))
	return err
}

// DropUser removes a server user. Paired with DropDatabase it is the whole of
// "forget this person": the data and the only credential that could read it.
func (c *Client) DropUser(ctx context.Context, name string) error {
	_, err := c.serverCommand(ctx, "drop user "+name)
	return err
}

// authedGet issues an authenticated GET. Every /api/v1 endpoint is auth-guarded,
// so the credential travels on the reads too. The caller owns the response body
// and its own status handling: for the two probes below a non-200 is the ANSWER,
// not a failure, and a shared helper that folded them together would erase the
// difference between "refused" and "unreachable".
func (c *Client) authedGet(ctx context.Context, endpoint string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: build request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: get %s: %w", endpoint, err)
	}
	return resp, nil
}

// DatabaseExists asks the server whether it still holds the named database.
//
// GET /api/v1/exists/{database} -> 200 {"result": true|false} (studio api.html,
// "Check Database Exists"). This is the POSTCONDITION of a purge: DropDatabase
// returns no rows, so the only proof that the data is gone is the server saying
// the container is gone. It is a strictly stronger statement than counting the
// nodes a delete was supposed to reach — there is nothing left to count.
func (c *Client) DatabaseExists(ctx context.Context, name string) (bool, error) {
	if strings.TrimSpace(name) == "" {
		return false, fmt.Errorf("arcadedb: database name must be non-empty")
	}
	resp, err := c.authedGet(ctx, c.baseURL+"/api/v1/exists/"+url.PathEscape(name))
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false, decodeServerError(resp)
	}
	var decoded struct {
		Result *bool `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return false, fmt.Errorf("arcadedb: decode exists response: %w", err)
	}
	if decoded.Result == nil {
		// A body with no `result` is not "it is gone" — it is an answer we cannot
		// read, and an erasure proof must not accept one.
		return false, fmt.Errorf("arcadedb: exists response for %q carries no result", name)
	}
	return *decoded.Result, nil
}

// CredentialAccepted reports whether the server still accepts THIS client's
// credential.
//
// ArcadeDB publishes no list-users endpoint, so a negative bind is the only way
// to prove `drop user` took. GET /api/v1/ready is the probe ArcadeDB's own
// user-management tests use for exactly this: GetReadyHandler answers 204 when
// the credential is accepted, while the auth layer answers 401 or 403 when it is
// refused.
//
// The (bool, error) split is load-bearing: false means REFUSED, an error means
// the answer is unknown. Collapsing them would read a server that is merely down
// as a successful purge.
func (c *Client) CredentialAccepted(ctx context.Context) (bool, error) {
	resp, err := c.authedGet(ctx, c.baseURL+"/api/v1/ready")
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return true, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return false, nil
	default:
		return false, decodeServerError(resp)
	}
}

// minSecureVersion is the first ArcadeDB release that authorizes a database
// created at runtime.
//
// CVE-2026-44221 (CVSS 9.0, fixed in 26.4.2) was two defects: an uninitialized
// fileAccessMap treated as permissive, and — the one that matters here —
// ArcadeDBServer.createDatabase() omitting the security factory, "disabling
// record-level authorization for newly created databases". Aura's memory creates
// a database per identity AT RUNTIME and rests its entire isolation on the server
// refusing a credential scoped elsewhere. On an affected version it would not
// refuse, and nothing would say so.
//
// So the pin in compose.yaml is not enough: a downgrade must be a refusal, not a
// silent loss of isolation.
var minSecureVersion = [3]int{26, 4, 2}

// ServerVersion reports the running server's version string.
func (c *Client) ServerVersion(ctx context.Context) (string, error) {
	resp, err := c.authedGet(ctx, c.serverURL+"?mode=basic")
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", decodeServerError(resp)
	}
	var decoded struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("arcadedb: decode server version: %w", err)
	}
	return decoded.Version, nil
}

// VerifySecureVersion refuses a server old enough to have CVE-2026-44221.
func (c *Client) VerifySecureVersion(ctx context.Context) error {
	raw, err := c.ServerVersion(ctx)
	if err != nil {
		return err
	}
	version, err := parseVersion(raw)
	if err != nil {
		return fmt.Errorf("arcadedb: unreadable server version %q: %w", raw, err)
	}
	if version.Less(minSecureVersion) {
		return fmt.Errorf(
			"arcadedb %s is affected by CVE-2026-44221: a database created at runtime is "+
				"left unauthorized, which is exactly what per-identity memory relies on. "+
				"Upgrade to %d.%d.%d or later",
			raw, minSecureVersion[0], minSecureVersion[1], minSecureVersion[2])
	}
	return nil
}

// version is major/minor/patch. ArcadeDB reports "26.7.3 (build …)" and tags a
// hotfix as "26.7.3-hotfix", so both the build suffix and a pre-release suffix
// have to fall away before comparison.
type version [3]int

func (v version) Less(other [3]int) bool {
	for i := range v {
		if v[i] != other[i] {
			return v[i] < other[i]
		}
	}
	return false
}

func parseVersion(raw string) (version, error) {
	field := strings.TrimSpace(raw)
	if i := strings.IndexAny(field, " ("); i >= 0 {
		field = field[:i]
	}
	if i := strings.IndexAny(field, "-+"); i >= 0 {
		field = field[:i]
	}
	parts := strings.Split(field, ".")
	if len(parts) < 3 {
		return version{}, fmt.Errorf("want major.minor.patch, got %q", field)
	}
	var out version
	for i := range out {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return version{}, fmt.Errorf("component %d of %q: %w", i, field, err)
		}
		out[i] = n
	}
	return out, nil
}
