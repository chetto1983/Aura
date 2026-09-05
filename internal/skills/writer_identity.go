package skills

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// writer_identity.go answers "whose skill is this write" (amendment #214). A Writer built at
// the composition root is deployment-global; For derives the one that writes AS an identity,
// into that identity's roots, recording a catalog row it can later share.
//
// It is a DERIVED Writer rather than an identity argument on twenty methods, for the reason
// mcpregistry.Store.Tx is: the scope is fixed for the whole call sequence, and threading it
// through each method would let a caller mix two scopes in one operation — write into A's
// root and audit as B — which is precisely the confusion this slice exists to remove.

// For returns the Writer that writes as identity: same pool, same validation config, roots
// resolved through Layout.
//
// Three cases collapse to "unchanged", and that is deliberate — an unscoped caller and a
// deployment with per-identity skills switched off must behave exactly as they did before
// #214: an empty identity, a Layout with no Identities base, and an identity that resolves to
// no own root all return the receiver untouched.
func (w *Writer) For(identity string) (*Writer, error) {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return w, nil
	}
	roots, err := w.layout.For(identity)
	if err != nil {
		return nil, fmt.Errorf("skills writer for %q: %w", identity, err)
	}
	if roots.Identity == "" {
		return w, nil
	}
	scoped := *w
	scoped.activeDir = roots.Identity
	scoped.exportDir = roots.Export
	scoped.archiveDir = filepath.Join(roots.Identity, StageArchived)
	scoped.owner = catalogOwner(identity)
	return &scoped, nil
}

// Owner reports the identity this Writer writes as, empty on the deployment-global one.
func (w *Writer) Owner() string { return w.owner }

// ActiveDir is the root this Writer's skills land in. The cockpit quotes it back in the
// install response, so an operator is told where the tree actually went rather than where the
// deployment's own skills live.
func (w *Writer) ActiveDir() string { return w.activeDir }

// catalogOwner keeps only an identity that can BE an owner in Postgres. aura.skill_catalog
// keys on aura.identities.id, a uuid; the legacy label-shaped ids ("local", "cli") name no
// row and would fail the RLS cast at write time. A label-shaped identity therefore gets its
// own directories and no catalog row — the filesystem boundary still holds, only sharing is
// unavailable, which is the truth about an identity the identity table does not know.
func catalogOwner(identity string) string {
	if _, err := uuid.Parse(identity); err != nil {
		return ""
	}
	return identity
}

// catalogOp is what one write does to the owner's catalog row. It runs inside the SAME
// transaction as the audit row, so a skill cannot become visible with no row and a row
// cannot outlive a write that failed. A nil op is the global Writer's answer: no owner, no
// catalog.
type catalogOp func(context.Context, *sqlc.Queries) error

// catalogUpsertOp records (or refreshes) the owner's catalog row for a skill.
func (w *Writer) catalogUpsertOp(name, description string, always bool, hash string) catalogOp {
	if w.owner == "" {
		return nil
	}
	return func(ctx context.Context, q *sqlc.Queries) error {
		_, err := UpsertCatalogTx(ctx, q, CatalogUpsert{
			OwnerID:     w.owner,
			Name:        name,
			Description: description,
			AlwaysApply: always,
			ContentHash: hash,
		})
		return err
	}
}

// catalogDeleteOp removes the owner's catalog row, and with it — via the 0118 trigger —
// every grant standing on it. Only delete does this: archiving changes a skill's lifecycle,
// not who owns it, so an archived skill keeps its row and its shares.
func (w *Writer) catalogDeleteOp(name string) catalogOp {
	if w.owner == "" {
		return nil
	}
	return func(ctx context.Context, q *sqlc.Queries) error {
		return DeleteCatalogTx(ctx, q, w.owner, name)
	}
}

// commitLedger writes the audit row and the catalog change as ONE transaction.
//
// The identity-scoped path uses db.WithIdentityTx because aura.skill_catalog is behind RLS:
// without app.current_identity the insert is refused, which is the intended failure rather
// than a case to work around. The global path keeps the plain db.WithTx it always had, so a
// deployment with no per-identity skills runs exactly the statements it ran before.
func (w *Writer) commitLedger(ctx context.Context, audit AuditInsert, cat catalogOp) error {
	run := func(q *sqlc.Queries) error {
		if err := InsertAuditTx(ctx, q, audit); err != nil {
			return err
		}
		if cat == nil {
			return nil
		}
		return cat(ctx, q)
	}
	if w.owner == "" {
		return db.WithTx(ctx, w.pool, run)
	}
	return db.WithIdentityTx(ctx, w.pool, w.owner, run)
}
