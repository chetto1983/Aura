// profile subcommand dispatcher for `aura profile {show|add-fact}`.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/profile"
)

const profileUsage = "usage: aura profile {show [--identity <id>] [--json]|add-fact [--identity <id>] <fact>}"

func runProfile(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, profileUsage)
		os.Exit(exitUsage)
	}
	switch args[0] {
	case "show":
		profileShow(args[1:])
	case "add-fact":
		profileAddFact(args[1:])
	default:
		fmt.Fprintln(os.Stderr, profileUsage)
		os.Exit(exitUsage)
	}
}

func profileShow(args []string) {
	fs, identity, jsonOut := profileFlagSet("profile show")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, profileUsage)
		os.Exit(exitUsage)
	}
	store := profileStore()
	loaded, err := store.ReadProfile(*identity)
	if err != nil {
		profileExit("show", *identity, err)
	}
	if *jsonOut {
		if err := json.NewEncoder(os.Stdout).Encode(loaded); err != nil {
			fmt.Fprintln(os.Stderr, "profile show:", err)
			os.Exit(1)
		}
		return
	}
	fmt.Print(profile.FormatSectionTree(loaded.AgentMD))
}

func profileAddFact(args []string) {
	fs, identity, _ := profileFlagSet("profile add-fact")
	if err := fs.Parse(args); err != nil || fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, profileUsage)
		os.Exit(exitUsage)
	}
	fact := strings.Join(fs.Args(), " ")
	store := profileStore()
	added, err := store.AddFact(*identity, fact)
	if err != nil {
		profileExit("add-fact", *identity, err)
	}
	if added {
		fmt.Printf("ok: added fact to profile %q\n", *identity)
		return
	}
	fmt.Printf("ok: fact already present in profile %q\n", *identity)
}

func profileFlagSet(name string) (*flag.FlagSet, *string, *bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	identity := fs.String("identity", "local", "profile identity")
	jsonOut := fs.Bool("json", false, "render JSON")
	return fs, identity, jsonOut
}

func profileStore() *profile.Store {
	cfg := config.LoadDB()
	return profile.NewStore(cfg.ProfileDir)
}

func profileExit(action, identity string, err error) {
	switch {
	case errors.Is(err, profile.ErrProfileNotFound):
		fmt.Fprintf(os.Stderr, "profile %s: identity %q has no Agent.md profile yet\n", action, identity)
	case errors.Is(err, profile.ErrInvalidIdentity):
		fmt.Fprintf(os.Stderr, "profile %s: invalid identity %q\n", action, identity)
	default:
		fmt.Fprintf(os.Stderr, "profile %s: %v\n", action, err)
	}
	os.Exit(1)
}
