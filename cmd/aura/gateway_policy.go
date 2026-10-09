package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/chetto1983/aura/internal/approvalpolicies"
)

// `aura gateway policy {list|set|clear}` — the operator's surface over the per-identity
// ask/deny policies (prd.md §5, 2026-10-09). Unlike the cockpit route, which is own-identity,
// the CLI resolves any identity by name: a policy only narrows what an agent may do, so an
// admin setting one on another identity is the safe direction, and set_by records who.

// policyCLIStore is the narrow seam the command needs; *approvalpolicies.Store satisfies it.
type policyCLIStore interface {
	Set(ctx context.Context, identityID, tool, action string, policy approvalpolicies.Policy, setBy string) error
	List(ctx context.Context, identityID string) ([]approvalpolicies.Row, error)
	Clear(ctx context.Context, identityID, tool, action string) (bool, error)
}

// policyCLIActor is the attribution a CLI write records: the CLI runs as the root operator
// on the host, not as a signed-in principal.
const policyCLIActor = "cli"

var errGatewayPolicyUsage = errors.New(gatewayUsage)

// gatewayPolicyCommand runs one `aura gateway policy` verb. resolve maps an identity NAME to
// its uuid. It returns an error instead of exiting so the verbs are testable without a
// database; runGateway prints it and exits 1.
func gatewayPolicyCommand(
	ctx context.Context, resolve func(string) (string, error), store policyCLIStore, args []string, out io.Writer,
) error {
	if len(args) < 2 {
		return errGatewayPolicyUsage
	}
	verb, name, rest := args[0], args[1], args[2:]
	identityID, err := resolve(name)
	if err != nil {
		return err
	}
	switch verb {
	case "list":
		if len(rest) != 0 {
			return errGatewayPolicyUsage
		}
		return gatewayPolicyList(ctx, store, identityID, out)
	case "set":
		if len(rest) < 2 || len(rest) > 3 {
			return errGatewayPolicyUsage
		}
		tool, action, word := rest[0], "", rest[len(rest)-1]
		if len(rest) == 3 {
			action = rest[1]
		}
		policy, err := approvalpolicies.ParsePolicy(word)
		if err != nil {
			return err
		}
		if err := store.Set(ctx, identityID, tool, action, policy, policyCLIActor); err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "ok: %q is now %s for %s\n", approvalpoliciesSubject(tool, action), policy, name)
		return err
	case "clear":
		if len(rest) < 1 || len(rest) > 2 {
			return errGatewayPolicyUsage
		}
		action := ""
		if len(rest) == 2 {
			action = rest[1]
		}
		cleared, err := store.Clear(ctx, identityID, rest[0], action)
		if err != nil {
			return err
		}
		subject := approvalpoliciesSubject(rest[0], action)
		if !cleared {
			// Not an error, but never printed as success: an operator who mistyped the verb
			// would otherwise believe a policy they meant to lift is gone.
			_, err = fmt.Fprintf(out, "no policy for %q — nothing cleared\n", subject)
			return err
		}
		_, err = fmt.Fprintf(out, "ok: cleared the policy for %q\n", subject)
		return err
	}
	return errGatewayPolicyUsage
}

func gatewayPolicyList(ctx context.Context, store policyCLIStore, identityID string, out io.Writer) error {
	rows, err := store.List(ctx, identityID)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		_, err = fmt.Fprintln(out, "no tool policies — every tool follows its tier and the standing grants")
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "TOOL\tACTION\tPOLICY\tSET AT\tBY")
	for _, r := range rows {
		action, by := r.Action, r.SetBy
		if action == "" {
			action = "-"
		}
		if by == "" {
			by = "-"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Tool, action, r.Policy, r.SetAt.Format("2006-01-02 15:04"), by)
	}
	return w.Flush()
}

func approvalpoliciesSubject(tool, action string) string {
	return approvalpolicies.Row{Tool: tool, Action: action}.Subject()
}
