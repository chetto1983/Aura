package sandbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrSessionNotFound is returned by SessionControl.Terminate when no active
// session matches the requested conversation id. The operator CLI maps it to a
// distinct exit so a typo is not confused with a docker failure.
var ErrSessionNotFound = errors.New("no active sandbox session for that conversation")

// SessionControl is the operator-facing control plane the `aura sandbox sessions`
// CLI drives (list/terminate/prune). It is a one-shot, reaper-free counterpart to
// SessionManager: it reads/mutates the sandbox_sessions registry over the sqlc
// querier and drives container stop/rm via the SAME LookPath-gated, fixed-argv
// dockerClient (D-05 — NEVER the socket). It owns no goroutine, so the CLI builds
// it, runs one verb, and exits.
type SessionControl struct {
	docker dockerClient
	store  sessionStore
}

// NewSessionControl builds the CLI control plane over the production docker CLI and
// the sqlc querier (*sqlc.Queries satisfies sessionStore).
func NewSessionControl(store sessionStore) *SessionControl {
	return newSessionControl(NewDockerCLI(), store)
}

// newSessionControl is the injectable constructor (the unit tier passes a fake
// docker + store so List/Terminate/Prune run with no daemon and no Postgres).
func newSessionControl(docker dockerClient, store sessionStore) *SessionControl {
	return &SessionControl{docker: docker, store: store}
}

// SessionRow is the rendered projection of one registry row for the CLI list view.
type SessionRow struct {
	ID             string
	ConversationID string
	ContainerID    string
	StartedAt      string
	LastUsedAt     string
	Status         string
}

// List returns the active sessions (newest-touched last, matching ListActive's
// ORDER BY) projected to display strings.
func (c *SessionControl) List(ctx context.Context) ([]SessionRow, error) {
	rows, err := c.store.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active sessions: %w", err)
	}
	out := make([]SessionRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, projectRow(r))
	}
	return out, nil
}

// Terminate stops + removes the active session container for convID and marks the
// registry row terminated. A docker stop/rm failure still proceeds to MarkTerminated
// (the boot recovery stray-sweep reconciles a leftover container next start — A7),
// so the registry never lies about a session the operator asked to kill. Returns
// ErrSessionNotFound when no active row matches.
func (c *SessionControl) Terminate(ctx context.Context, convID string) error {
	rows, err := c.store.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("terminate %s: list active: %w", convID, err)
	}
	for _, r := range rows {
		if r.ConversationID.String() != convID {
			continue
		}
		_ = c.docker.stop(ctx, r.ContainerID)
		_ = c.docker.remove(ctx, r.ContainerID)
		if mErr := c.store.MarkTerminated(ctx, r.ID); mErr != nil {
			return fmt.Errorf("terminate %s: mark terminated: %w", convID, mErr)
		}
		return nil
	}
	return fmt.Errorf("terminate %s: %w", convID, ErrSessionNotFound)
}

// Prune terminates every active session: it stops + removes each container, marks
// each row terminated, then runs the stray-container sweep so a crashed prior
// process's orphans are also docker-rm'd. It returns the number of registry rows
// pruned. Per-container/per-row failures are non-fatal (reconciled next boot); only
// a structural ListActive failure aborts.
func (c *SessionControl) Prune(ctx context.Context) (int, error) {
	rows, err := c.store.ListActive(ctx)
	if err != nil {
		return 0, fmt.Errorf("prune: list active: %w", err)
	}
	pruned := 0
	for _, r := range rows {
		_ = c.docker.stop(ctx, r.ContainerID)
		_ = c.docker.remove(ctx, r.ContainerID)
		if mErr := c.store.MarkTerminated(ctx, r.ID); mErr == nil {
			pruned++
		}
	}
	if stray, sErr := c.docker.listStray(ctx); sErr == nil {
		for _, id := range stray {
			_ = c.docker.remove(ctx, id)
		}
	}
	return pruned, nil
}

func projectRow(r sqlc.AuraSandboxSessions) SessionRow {
	return SessionRow{
		ID:             r.ID.String(),
		ConversationID: r.ConversationID.String(),
		ContainerID:    r.ContainerID,
		StartedAt:      fmtTimestamp(r.StartedAt),
		LastUsedAt:     fmtTimestamp(r.LastUsedAt),
		Status:         r.Status,
	}
}

// fmtTimestamp renders a pgtype.Timestamptz as RFC3339 (UTC), or "-" when NULL.
func fmtTimestamp(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return "-"
	}
	return ts.Time.UTC().Format(time.RFC3339)
}
