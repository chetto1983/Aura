// Package pimsplit carries the operators' stored approvals, policies and email drafts across the
// PIM tool split (docs/superpowers/specs/2026-11-23-pim-tool-split-design.md §5.5): the PIM
// advertised one multiplexed `calendar` tool with an `action` argument, and now advertises one tool
// per operation, which is what the gateway and the drafts are keyed on.
//
// The new names go through mcptools.NamespacedName, which truncates and hashes a long name, so the
// rewrite is Go and not SQL. `aura db migrate` runs it once, as the migrate role, which owns the
// tables and so is not narrowed by their row security.
package pimsplit

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/jackc/pgx/v5"
)

// upgradeName is this upgrade's row in aura.data_upgrades (migration 0158).
const upgradeName = "pim_tool_split"

const (
	calendarSource = "recipe:calendar"
	// calendarServer is the catalog name the appliance mounts the PIM under with no registry row
	// (manager.RuntimeSet), and the multiplexed tool's own name.
	calendarServer = "calendar"
)

// Report counts the old-shape rows the upgrade settled; each stored row lands in one count.
type Report struct {
	// Rewritten is per-action grants and policies moved onto their tool. A rewritten ask that meets
	// a fanned-out deny on the same tool is counted here and loses to it.
	Rewritten int
	// FannedOutDenies is tool-wide deny policies copied onto each PIM tool.
	FannedOutDenies int
	// Dropped is grants and policies deleted: tool-wide grants and asks, unknown actions, and rows
	// already named after a PIM tool before the split.
	Dropped int
	// DraftsRewritten is pending and approved email drafts moved onto send_email.
	DraftsRewritten int
	// DraftsExpired is pending and approved email drafts closed: those naming no sending account,
	// and those whose server is no PIM mount or whose arguments the new send refuses.
	DraftsExpired int
}

// Upgrade rewrites every old-shape row in one transaction, the first time it runs on a database,
// and records that it ran in the same transaction. Later runs find the record and change nothing:
// a row written after the upgrade can have exactly the shape of one it deletes (migration 0158).
func Upgrade(ctx context.Context, conn *pgx.Conn, log *slog.Logger) (Report, error) {
	var report Report
	applied := false
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO aura.data_upgrades (name) VALUES ($1) ON CONFLICT (name) DO NOTHING`, upgradeName)
		if err != nil {
			return fmt.Errorf("record the pim tool split: %w", err)
		}
		if tag.RowsAffected() == 0 {
			applied = true
			return nil
		}
		namespaces, err := pimNamespaces(ctx, tx)
		if err != nil {
			return err
		}
		u := upgrader{tx: tx, log: log, namespaces: namespaces, report: &report}
		for _, table := range []subjectTable{grants, policies} {
			if err := u.subjects(ctx, table); err != nil {
				return err
			}
		}
		return u.drafts(ctx)
	})
	if err != nil {
		return Report{}, fmt.Errorf("pim tool split: %w", err)
	}
	log.Info("pim tool split upgrade", "already_applied", applied,
		"rewritten", report.Rewritten, "fanned_out_denies", report.FannedOutDenies, "dropped", report.Dropped,
		"drafts_rewritten", report.DraftsRewritten, "drafts_expired", report.DraftsExpired)
	return report, nil
}

type upgrader struct {
	tx         pgx.Tx
	log        *slog.Logger
	namespaces []string
	report     *Report
}

type server struct{ name, source string }

func pimNamespaces(ctx context.Context, tx pgx.Tx) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT name, source FROM aura.mcp_server`)
	if err != nil {
		return nil, fmt.Errorf("read the mcp servers: %w", err)
	}
	servers, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (server, error) {
		var s server
		return s, row.Scan(&s.name, &s.source)
	})
	if err != nil {
		return nil, fmt.Errorf("read the mcp servers: %w", err)
	}
	return namespacesOf(servers), nil
}

// namespacesOf is every server the PIM is mounted as: each registry row of its recipe, and the
// catalog name, which the appliance mounts with no row unless an operator's server took it.
func namespacesOf(servers []server) []string {
	var out []string
	defaultMount := true
	for _, s := range servers {
		if s.source == calendarSource {
			out = append(out, s.name)
		} else if s.name == calendarServer {
			defaultMount = false
		}
	}
	if defaultMount && !slices.Contains(out, calendarServer) {
		out = append(out, calendarServer)
	}
	slices.Sort(out)
	return out
}
