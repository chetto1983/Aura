// Command deadexport reports the exported functions and methods of the main module that no
// production code references: the dead code deadcode cannot see. deadcode's RTA keeps
// every exported method of a type that reaches an interface alive, since reflection might
// call it (go/callgraph/rta), so an exported method nobody calls on a type stored in an
// interface reads as live there. This tool counts type-checked references from non-test
// code instead, and spares a method only when an interface in the program could reach it.
//
// Usage, from the module root (scripts/deadcode_production_gate.sh runs it):
//
//	deadexport [-tags a,b] [packages]
//
// It prints one "<symbol>\t<file:line>" line per finding, in deadcode's symbol spelling,
// and exits 0 whether or not it found anything; the gate decides what a finding means. A
// package that fails to load or type-check exits 2: a partial program would make every
// function whose callers did not load read as dead.
package main

import (
	"flag"
	"fmt"
	"os"

	"golang.org/x/tools/go/packages"
)

func main() {
	tags := flag.String("tags", "", "comma-separated build tags")
	flag.Parse()
	patterns := flag.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "deadexport:", err)
		os.Exit(2)
	}
	findings, err := run(dir, *tags, patterns)
	if err != nil {
		fmt.Fprintln(os.Stderr, "deadexport:", err)
		os.Exit(2)
	}
	for _, f := range findings {
		fmt.Printf("%s\t%s\n", f.Symbol, f.Pos)
	}
}

func run(dir, tags string, patterns []string) ([]finding, error) {
	cfg := &packages.Config{Dir: dir, Mode: packages.LoadAllSyntax | packages.NeedModule, Tests: false}
	if tags != "" {
		cfg.BuildFlags = []string{"-tags=" + tags}
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		return nil, fmt.Errorf("%d package error(s); refusing to report on a partial program", n)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages matched %v", patterns)
	}
	return analyze(pkgs, dir), nil
}
