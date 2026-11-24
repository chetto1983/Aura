package pimsplit

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/agent/mcptools"
)

// calendarTools is the PIM's 29 tool names, which are also the actions its multiplexed tool took.
var calendarTools = mcptools.CalendarToolNames()

// subjectTable holds one gateway table's statements. Grants and policies are keyed alike, identity
// + tool + action; a grant has no kind column, so its kind reads as the literal 'grant'.
type subjectTable struct {
	label string
	// purge deletes every row named after a PIM tool ($1, text[]).
	purge string
	// read locks the rows on one multiplexed tool ($1), each identity's tool-wide row first.
	read string
	// copy writes the row ($1 identity, $2 tool, $3 action) under each name of $4 (text[]) with no
	// action, and never over a row already there.
	copy string
	// remove deletes the row ($1 identity, $2 tool, $3 action).
	remove string
}

var grants = subjectTable{
	label: "gateway_approval_grants",
	purge: `DELETE FROM aura.gateway_approval_grants WHERE tool = ANY($1::text[])
RETURNING identity_id::text, tool, action, 'grant'`,
	read: `SELECT identity_id::text, tool, action, 'grant' FROM aura.gateway_approval_grants
WHERE tool = $1 ORDER BY identity_id, action FOR UPDATE`,
	copy: `INSERT INTO aura.gateway_approval_grants (identity_id, tool, action, granted_at, granted_by)
SELECT g.identity_id, t.name, '', g.granted_at, g.granted_by
FROM aura.gateway_approval_grants g, unnest($4::text[]) AS t(name)
WHERE g.identity_id = $1::uuid AND g.tool = $2 AND g.action = $3
ON CONFLICT (identity_id, tool, action) DO NOTHING`,
	remove: `DELETE FROM aura.gateway_approval_grants WHERE identity_id = $1::uuid AND tool = $2 AND action = $3`,
}

var policies = subjectTable{
	label: "gateway_tool_policies",
	purge: `DELETE FROM aura.gateway_tool_policies WHERE tool = ANY($1::text[])
RETURNING identity_id::text, tool, action, policy`,
	read: `SELECT identity_id::text, tool, action, policy FROM aura.gateway_tool_policies
WHERE tool = $1 ORDER BY identity_id, action FOR UPDATE`,
	copy: `INSERT INTO aura.gateway_tool_policies (identity_id, tool, action, policy, set_at, set_by)
SELECT p.identity_id, t.name, '', p.policy, p.set_at, p.set_by
FROM aura.gateway_tool_policies p, unnest($4::text[]) AS t(name)
WHERE p.identity_id = $1::uuid AND p.tool = $2 AND p.action = $3
ON CONFLICT (identity_id, tool, action) DO NOTHING`,
	remove: `DELETE FROM aura.gateway_tool_policies WHERE identity_id = $1::uuid AND tool = $2 AND action = $3`,
}

type subject struct{ identity, tool, action, kind string }

type move int

const (
	moveRewrite move = iota
	moveFanOut
	moveDrop
)

// decide is what becomes of one row on the multiplexed tool. Only a deny can be widened to every
// tool: it can only restrict, and dropping it would let through mail the operator had refused. A
// tool-wide grant or ask fanned out would be a permission nobody set on the new tools.
func decide(action, kind string) (move, string) {
	switch {
	case action == "" && kind == "deny":
		return moveFanOut, ""
	case action == "":
		return moveDrop, "a tool-wide " + kind + " is not fanned out to the PIM's tools"
	case slices.Contains(calendarTools, action):
		return moveRewrite, ""
	default:
		return moveDrop, "unknown action: the PIM has no tool by that name"
	}
}

// subjects settles one table. The rows already named after a PIM tool go first, so a rewritten row
// can meet only a fanned-out deny, which read's order writes first and copy never overwrites.
func (u upgrader) subjects(ctx context.Context, table subjectTable) error {
	var named []string
	for _, ns := range u.namespaces {
		for _, tool := range calendarTools {
			named = append(named, mcptools.NamespacedName(ns, tool))
		}
	}
	purged, err := u.collect(ctx, table.purge, named)
	if err != nil {
		return fmt.Errorf("%s: delete the rows named after a PIM tool: %w", table.label, err)
	}
	for _, s := range purged {
		u.drop(table, s, "named after a PIM tool before it existed, it never applied")
	}
	for _, ns := range u.namespaces {
		stored, err := u.collect(ctx, table.read, mcptools.NamespacedName(ns, calendarServer))
		if err != nil {
			return fmt.Errorf("%s: read the multiplexed rows: %w", table.label, err)
		}
		for _, s := range stored {
			if err := u.settle(ctx, table, ns, s); err != nil {
				return fmt.Errorf("%s: %w", table.label, err)
			}
		}
	}
	return nil
}

func (u upgrader) settle(ctx context.Context, table subjectTable, ns string, s subject) error {
	verdict, reason := decide(s.action, s.kind)
	var targets []string
	switch verdict {
	case moveRewrite:
		targets = []string{mcptools.NamespacedName(ns, s.action)}
		u.report.Rewritten++
	case moveFanOut:
		for _, tool := range calendarTools {
			targets = append(targets, mcptools.NamespacedName(ns, tool))
		}
		u.report.FannedOutDenies++
	default:
		u.drop(table, s, reason)
	}
	if len(targets) > 0 {
		if _, err := u.tx.Exec(ctx, table.copy, s.identity, s.tool, s.action, targets); err != nil {
			return fmt.Errorf("copy a row onto the PIM's tools: %w", err)
		}
	}
	if _, err := u.tx.Exec(ctx, table.remove, s.identity, s.tool, s.action); err != nil {
		return fmt.Errorf("delete a multiplexed row: %w", err)
	}
	return nil
}

func (u upgrader) drop(table subjectTable, s subject, reason string) {
	u.report.Dropped++
	u.log.Info("pim tool split: dropped a stored approval", "table", table.label, "identity", s.identity,
		"tool", s.tool, "action", s.action, "kind", s.kind, "reason", reason)
}

func (u upgrader) collect(ctx context.Context, sql string, arg any) ([]subject, error) {
	rows, err := u.tx.Query(ctx, sql, arg)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (subject, error) {
		var s subject
		return s, row.Scan(&s.identity, &s.tool, &s.action, &s.kind)
	})
}
