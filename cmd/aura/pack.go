package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/chetto1983/aura/internal/packs"
)

const packUsage = `usage: aura pack {list <owner/repo> | show <owner/repo/plugin>} [--json]

  list  every plugin in a repository, one line each
  show  one plugin in full: skills, connectors, commands

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
	if action != "list" && action != "show" {
		fmt.Fprintln(os.Stderr, packUsage)
		os.Exit(1)
	}
	if err := packCommand(context.Background(), action, ref, asJSON, os.Stdout); err != nil {
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
