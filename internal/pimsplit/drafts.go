package pimsplit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

// The outcome codes of an email draft the upgrade closes instead of rewriting.
const (
	// accountRequired: the draft names no sending account, and the new send_email refuses it
	// (operator, 2026-11-23); the operator must choose the account again.
	accountRequired = "account_required"
	// unmappable: its server is no longer a PIM mount, or the new send refuses its arguments.
	unmappable = "tool_split_unmappable"
)

type draftRow struct {
	id                  string
	registered          string
	original, effective []byte
}

// draftPlan is a draft's rewrite, or the outcome code that closes it when expire is set.
type draftPlan struct {
	expire                  string
	registered              string
	original, effective     json.RawMessage
	originalFP, effectiveFP string
}

// drafts settles the email drafts still waiting in the multiplexed shape. A draft being dispatched
// is left alone, since whatever its send did stays the truth, and a resolved one is history.
func (u upgrader) drafts(ctx context.Context) error {
	legacy := messagedrafts.LegacyEmailTarget()
	rows, err := u.tx.Query(ctx, `SELECT id::text, registered_tool_name, original_args, effective_args
FROM aura.message_drafts
WHERE recipe = $1 AND tool_name = $2 AND action = $3 AND status IN ('pending', 'approved')
ORDER BY id FOR UPDATE`, legacy.Recipe, legacy.Tool, legacy.Action)
	if err != nil {
		return fmt.Errorf("read the waiting email drafts: %w", err)
	}
	waiting, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (draftRow, error) {
		var d draftRow
		return d, row.Scan(&d.id, &d.registered, &d.original, &d.effective)
	})
	if err != nil {
		return fmt.Errorf("read the waiting email drafts: %w", err)
	}
	for _, d := range waiting {
		plan := planDraft(u.namespaces, d)
		if plan.expire != "" {
			err = u.expire(ctx, d.id, plan.expire)
		} else {
			err = u.rewrite(ctx, d.id, plan)
		}
		if err != nil {
			return fmt.Errorf("email draft %s: %w", d.id, err)
		}
	}
	return nil
}

func (u upgrader) expire(ctx context.Context, id, outcome string) error {
	if _, err := u.tx.Exec(ctx, `UPDATE aura.message_drafts
SET status = 'expired', outcome_code = $2, resolved_at = now(), updated_at = now()
WHERE id = $1::uuid AND status IN ('pending', 'approved')`, id, outcome); err != nil {
		return err
	}
	u.report.DraftsExpired++
	u.log.Info("pim tool split: expired an email draft", "draft", id, "outcome", outcome)
	return nil
}

func (u upgrader) rewrite(ctx context.Context, id string, plan draftPlan) error {
	var effective, effectiveFP any
	if plan.effective != nil {
		effective, effectiveFP = string(plan.effective), plan.effectiveFP
	}
	target := messagedrafts.EmailTarget()
	if _, err := u.tx.Exec(ctx, `UPDATE aura.message_drafts
SET tool_name = $2, action = $3, registered_tool_name = $4, original_args = $5, original_fingerprint = $6,
    effective_args = $7, effective_fingerprint = $8, updated_at = now()
WHERE id = $1::uuid AND status IN ('pending', 'approved')`,
		id, target.Tool, target.Action, plan.registered, string(plan.original), plan.originalFP,
		effective, effectiveFP); err != nil {
		return err
	}
	u.report.DraftsRewritten++
	return nil
}

// planDraft moves a draft onto its mount's send_email tool, with the arguments and fingerprints
// messagedrafts.Create would give the same call there. The account is read from what will be sent:
// the reviewed arguments when there are some.
func planDraft(namespaces []string, row draftRow) draftPlan {
	ns, ok := namespaceOf(namespaces, row.registered)
	if !ok {
		return draftPlan{expire: unmappable}
	}
	plan := draftPlan{registered: mcptools.NamespacedName(ns, "send_email")}
	var err error
	if plan.original, err = sendEmailArgs(row.original); err != nil {
		return draftPlan{expire: unmappable}
	}
	sent := plan.original
	if row.effective != nil {
		if plan.effective, err = sendEmailArgs(row.effective); err != nil {
			return draftPlan{expire: unmappable}
		}
		sent = plan.effective
	}
	if !namesAccount(sent) {
		return draftPlan{expire: accountRequired}
	}
	target := messagedrafts.EmailTarget()
	if plan.originalFP, err = messagedrafts.Fingerprint(target, plan.original); err != nil {
		return draftPlan{expire: unmappable}
	}
	if plan.effective != nil {
		if plan.effectiveFP, err = messagedrafts.Fingerprint(target, plan.effective); err != nil {
			return draftPlan{expire: unmappable}
		}
	}
	return plan
}

// namespaceOf finds the mount a draft was made on by the name the bridge gave its calendar tool,
// never by a suffix: a long namespace is truncated and hashed.
func namespaceOf(namespaces []string, registered string) (string, bool) {
	for _, ns := range namespaces {
		if mcptools.NamespacedName(ns, calendarServer) == registered {
			return ns, true
		}
	}
	return "", false
}

// sendEmailArgs drops the multiplexed "action" key and canonicalises the rest as Create does:
// Fingerprint hashes the bytes it is given, and jsonb handed them back in its own key order.
func sendEmailArgs(stored []byte) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(stored, &fields); err != nil {
		return nil, err
	}
	delete(fields, "action")
	stripped, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return messagedrafts.MergeApprovedArgs(messagedrafts.EmailTarget(), stripped, nil)
}

func namesAccount(args json.RawMessage) bool {
	var named struct {
		AccountID string `json:"accountId"`
	}
	return json.Unmarshal(args, &named) == nil && strings.TrimSpace(named.AccountID) != ""
}
