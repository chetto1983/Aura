// `aura gateway grants {list|revoke}` — the operator's surface over the durable "always
// approve" rows the approval prompt creates (PRD amendment #127). Hand-rolled switch tree
// mirroring runIdentity/runDB, not cobra: go.mod has no spf13/cobra and this codebase
// dispatches subcommands with nested switches.
//
// It is a CLI and not a cockpit panel, and that is a stated debt, not an oversight: the
// operator who GRANTS from the browser revokes from the terminal. What makes it liveable in
// the meantime is that a grant is never silent — every call a standing grant lets through
// records approval_scope in its reservation, so the audit view shows which grant did it.
package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/chetto1983/aura/internal/approvalgrants"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identity"
)

const gatewayUsage = "usage: aura gateway grants {list <identity>|revoke <identity> <tool> [action]}\n" +
	"  <identity> = the identity NAME (`aura identity list`)\n" +
	"  [action]   = the verb of an action-multiplexed tool; omit it for a plain tool"

func runGateway(args []string) {
	if len(args) < 2 || args[0] != "grants" {
		fmt.Fprintln(os.Stderr, gatewayUsage)
		os.Exit(1)
	}
	// A DB-only domain — the LLM-free config load so this does not require an LLM key.
	cfg := config.LoadDB()
	ctx := context.Background()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer pool.Close()

	switch args[1] {
	case "list":
		gatewayGrantsList(ctx, identity.New(pool), approvalgrants.New(pool), args[2:])
	case "revoke":
		gatewayGrantsRevoke(ctx, identity.New(pool), approvalgrants.New(pool), args[2:])
	default:
		fmt.Fprintln(os.Stderr, gatewayUsage)
		os.Exit(1)
	}
}

// resolveGrantIdentity maps the operator-typed identity NAME to its uuid. Names are what an
// operator has (`aura identity list` prints them); the uuid is what the table is keyed on.
func resolveGrantIdentity(ctx context.Context, ids *identity.Store, args []string, want int) (string, []string) {
	if len(args) < want {
		fmt.Fprintln(os.Stderr, gatewayUsage)
		os.Exit(1)
	}
	idn, err := ids.GetIdentityByName(ctx, args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return idn.ID, args[1:]
}

func gatewayGrantsList(ctx context.Context, ids *identity.Store, store *approvalgrants.Store, args []string) {
	identityID, _ := resolveGrantIdentity(ctx, ids, args, 1)
	grants, err := store.List(ctx, identityID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(grants) == 0 {
		fmt.Println("no standing approval grants — every destructive call is asked for")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "TOOL\tACTION\tGRANTED AT\tBY")
	for _, g := range grants {
		action := g.Action
		if action == "" {
			action = "-"
		}
		by := g.GrantedBy
		if by == "" {
			by = "-"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", g.Tool, action, g.GrantedAt.Format("2006-01-02 15:04"), by)
	}
	_ = w.Flush()
}

func gatewayGrantsRevoke(ctx context.Context, ids *identity.Store, store *approvalgrants.Store, args []string) {
	identityID, rest := resolveGrantIdentity(ctx, ids, args, 2)
	action := ""
	if len(rest) > 1 {
		action = rest[1]
	}
	removed, err := store.Revoke(ctx, identityID, rest[0], action)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	subject := rest[0]
	if action != "" {
		subject += " " + action
	}
	if !removed {
		// Not an error — but never printed as success: an operator who mistyped the verb
		// would otherwise walk away believing they closed a gate that is still open.
		fmt.Printf("no standing grant for %q — nothing revoked\n", subject)
		return
	}
	fmt.Printf("ok: revoked the standing approval for %q\n", subject)
}
