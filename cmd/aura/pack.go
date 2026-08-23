package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/packs"
)

const packUsage = `usage: aura pack {list <owner/repo> | show <owner/repo/plugin>} [--json]
       aura pack install <owner/repo/plugin>
       aura pack trust <owner/repo/plugin> --class <class> --reason "why"

  list     every plugin in a repository, one line each
  show     one plugin in full: skills, connectors, commands
  install  install the whole pack: skills + connectors, connectors BLOCKED
  trust    the single approval a pack takes — sweeps every connector it installed

A pack is a knowledge-work plugin read as ONE unit (amendment #126). The skills
catalogue indexes its skills individually and drops the grouping; this reads the
repository the catalogue names and puts it back.`

// packResolve is the seam. It defaults to a real shallow clone; tests replace it
// so the whole command is exercisable without a network or a git binary.
var packResolve = func(ctx context.Context, ref packs.Ref) ([]packs.Pack, error) {
	return (&packs.Resolver{}).Resolve(ctx, ref)
}

func runPack(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, packUsage)
		os.Exit(1)
	}
	action, ref := args[0], args[1]
	asJSON := false
	for _, a := range args[2:] {
		if a == "--json" {
			asJSON = true
		}
	}
	// install and trust WRITE, so they take the pool the other two never need.
	switch action {
	case "list", "show":
		if err := packCommand(context.Background(), action, ref, asJSON, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "install", "trust":
		runPackWrite(action, args)
	default:
		fmt.Fprintln(os.Stderr, packUsage)
		os.Exit(1)
	}
}

// runPackWrite is the writing half. It is split out because install and trust
// need a pool and an audit actor, and the read verbs deliberately need neither.
func runPackWrite(action string, args []string) {
	ctx, pool, closePool, err := packWriteDeps()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer closePool()

	switch action {
	case "install":
		err = packInstall(ctx, pool, args[1], os.Stdout)
	case "trust":
		// The SAME parser `aura mcp trust` uses: one flag grammar, one required-
		// reason rule, one known-class default. A pack approving many servers at
		// once must not have a laxer front door than approving one.
		ref, class, reason, perr := parseMCPTrustArgs(args[1:])
		if perr != nil {
			fmt.Fprintln(os.Stderr, perr)
			os.Exit(1)
		}
		err = packTrust(ctx, pool, ref, class, reason, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// packCommand is the whole command minus process concerns, so a test drives it
// with a writer and reads the bytes rather than asserting on an exit code.
func packCommand(ctx context.Context, action, refArg string, asJSON bool, out io.Writer) error {
	ref, err := packs.ParseRef(refArg)
	if err != nil {
		return err
	}
	if action == "show" && ref.Directory == "" {
		// A repository holding several plugins has no single pack to show, and
		// silently showing the first would be a guess presented as an answer.
		return fmt.Errorf("show needs a plugin: %s/<plugin> (use `aura pack list %s` to see them)", ref.Source, ref.Source)
	}
	found, err := packResolve(ctx, ref)
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(found)
	}
	if action == "list" {
		packs.WriteList(out, found)
		return nil
	}
	packs.WriteDetail(out, found[0])
	return nil
}

// packWriteDeps opens the audited pool the writing verbs need, mirroring
// runMCPDispatch: a pool failure outside server-production is a warning and the
// write proceeds UNAUDITED, because refusing to install a pack on a laptop with
// no database would be the wrong trade — but it is said out loud, every time.
func packWriteDeps() (context.Context, *pgxpool.Pool, func(), error) {
	ctx := cliInvocationContext
	if ctx == nil {
		ctx = context.Background()
	}
	cfg := config.LoadDB()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		if cfg.Profile == config.ProfileServerProduction {
			return nil, nil, func() {}, err
		}
		fmt.Fprintf(os.Stderr, "warning: pack: could not open an audited database pool (%v); proceeding unaudited\n", err)
		return ctx, nil, func() {}, nil
	}
	return ctx, pool, pool.Close, nil
}
