// identity subcommand dispatcher for `aura identity {list|get|grant|revoke}`
// (CORE-03 Req#6). Hand-rolled switch tree mirroring runDB (cmd/aura/db.go),
// NOT cobra.
//
// DEVIATION (04-RESEARCH OPEN QUESTION 1 / Assumption A2): CONTEXT D-A3-05 says
// "cobra command group", but go.mod has no spf13/cobra and the codebase uses
// nested switch dispatchers (runDB). CLAUDE.md mandates following existing
// patterns; SPEC never requires cobra. Implemented as a switch subcommand tree.
// This is a HOW deviation from CONTEXT wording, not a SPEC requirement change.
//
// Each branch loads config, opens the aura_app pool, constructs identity.Store,
// runs one operation, prints a human-readable line, exits non-zero on error.
// grant/revoke of '*' surface the Store's system-managed error to stderr with a
// non-zero exit (the wildcard is seeded, never CLI-managed).
package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identity"
)

const identityUsage = "usage: aura identity {list|get <name>|grant <name> <cap>|revoke <name> <cap>}"

func runIdentity(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, identityUsage)
		os.Exit(1)
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config load:", err)
		os.Exit(1)
	}
	ctx := context.Background()

	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer pool.Close()
	store := identity.New(pool)

	switch args[0] {
	case "list":
		identityList(ctx, store)
	case "get":
		identityGet(ctx, store, args[1:])
	case "grant":
		identityGrant(ctx, store, args[1:])
	case "revoke":
		identityRevoke(ctx, store, args[1:])
	default:
		fmt.Fprintln(os.Stderr, identityUsage)
		os.Exit(1)
	}
}

func identityList(ctx context.Context, store *identity.Store) {
	ids, err := store.ListIdentities(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(ids) == 0 {
		fmt.Println("ok: no identities")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tKIND\tID")
	for _, id := range ids {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", id.Name, id.Kind, id.ID)
	}
	_ = w.Flush()
}

func identityGet(ctx context.Context, store *identity.Store, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, identityUsage)
		os.Exit(1)
	}
	id, err := store.GetIdentityByName(ctx, args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "NAME\t%s\n", id.Name)
	_, _ = fmt.Fprintf(w, "KIND\t%s\n", id.Kind)
	_, _ = fmt.Fprintf(w, "ID\t%s\n", id.ID)
	_ = w.Flush()
}

func identityGrant(ctx context.Context, store *identity.Store, args []string) {
	name, capability := identityNameCap(args)
	id, err := store.GetIdentityByName(ctx, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := store.GrantCapability(ctx, id.ID, capability); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("ok: granted %q to %s\n", capability, name)
}

func identityRevoke(ctx context.Context, store *identity.Store, args []string) {
	name, capability := identityNameCap(args)
	id, err := store.GetIdentityByName(ctx, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := store.RevokeCapability(ctx, id.ID, capability); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("ok: revoked %q from %s\n", capability, name)
}

// identityNameCap extracts the <name> <cap> pair shared by grant/revoke, exiting
// with the usage line when either is missing.
func identityNameCap(args []string) (name, capability string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, identityUsage)
		os.Exit(1)
	}
	return args[0], args[1]
}
