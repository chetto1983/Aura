package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// cliDispatchers names, for each command group, the function whose switch dispatches its
// verbs; "" is the top level. A group dispatched by an if-chain instead of a switch (gateway's
// grants/policy split, pack's list/show) has no entry: its sub-groups are listed here, and pack
// is a leaf.
var cliDispatchers = map[string]string{
	"":               "main",
	"agent":          "runAgent",
	"chat":           "runChat",
	"config":         "runConfig",
	"db":             "runDB",
	"docs":           "runDocsCommand",
	"gateway grants": "runGateway",
	"gateway policy": "gatewayPolicyCommand",
	"identity":       "runIdentity",
	"mcp":            "runMCPCommand",
	"mcp profile":    "mcpProfile",
	"memory":         "memoryVerbToTool",
	"objectstore":    "runObjectStore",
	"paused-states":  "runPausedStates",
	"retention":      "runRetentionCommand",
	"skills":         "runSkills",
	"skills snippet": "skillsSnippet",
	"task":           "runTask",
	"web":            "runWeb",
	"web tool":       "runWebTool",
}

// cliExemptions are the verbs no smoke row or contract runs, each with why. The inventory test
// fails on a dispatched verb missing from the rows and from here, and on an entry here that no
// longer names a dispatched verb.
var cliExemptions = map[string]string{
	"serve":                     "the daemon; serve_smoke_test.go boots it",
	"shell":                     "an interactive REPL that needs a model",
	"swarm-demo":                "drives a model",
	"toolpipe":                  "hidden latency harness reading NDJSON tool calls",
	"cache-audit":               "hidden KV-prefix gate; ci.yml's cache-invariant job runs it",
	"pack":                      "fetches a plugin repository from GitHub",
	"db reset":                  "destroys the database the rows share; db_reset_idempotency_integration_test.go runs it",
	"docs mcp":                  "a stdio MCP server that blocks on stdin",
	"docs ingest":               "needs an onboarded identity's object store",
	"docs open":                 "needs a document docs ingest stored",
	"identity create":           "reads the password and answer from a hidden prompt",
	"identity recover-operator": "reads the new password from a hidden prompt",
	"mcp add":                   "registers an operator server; mcp_add_test.go runs its core",
	"mcp login":                 "an interactive OAuth browser flow",
	"mcp logs":                  "reads a running server's log",
	"mcp tools":                 "opens a live session; mcp_status_test.go opens sessions against a recording fixture",
	"objectstore restore":       "needs a backup to restore",
	"retention apply":           "needs the token a plan prints; retention_test.go applies through runRetentionCommand",
	"skills share":              "needs a second identity, which identity create cannot make unattended",
	"skills unshare":            "needs a share from skills share",
	"skills snippet exec":       "runs code in the identity's sandbox box",
	"task approve":              "needs a task pending approval, which only a model's schedule makes",
	"web tool web_search":       "needs SearXNG",
	"web tool web_fetch":        "fetches from the network",
}

// cliVerbKey names the dispatched verb argv reaches: its longest group prefix and the verb that
// follows, or the bare command when it is no group.
func cliVerbKey(argv []string) string {
	key := ""
	for i := range argv {
		prefix := strings.Join(argv[:i+1], " ")
		if _, ok := cliDispatchers[prefix]; !ok && !cliGroupPrefix(prefix) {
			return strings.TrimSpace(key + " " + argv[i])
		}
		key = prefix
	}
	return key
}

// cliGroupPrefix is true for a word that only opens groups, like "gateway".
func cliGroupPrefix(prefix string) bool {
	for group := range cliDispatchers {
		if strings.HasPrefix(group, prefix+" ") {
			return true
		}
	}
	return false
}

// dispatchedCLIVerbs reads every leaf verb from the dispatchers' source.
func dispatchedCLIVerbs(t *testing.T) []string {
	t.Helper()
	labels := caseLabelsByFunc(t, ".")
	var leaves []string
	for group, fn := range cliDispatchers {
		verbs, ok := labels[fn]
		if !ok || len(verbs) == 0 {
			t.Fatalf("dispatcher %s (group %q) is gone or has no case labels: update cliDispatchers", fn, group)
		}
		for _, verb := range verbs {
			if strings.HasPrefix(verb, "-") {
				continue // --version, -v: aliases of a verb, not verbs
			}
			key := strings.TrimSpace(group + " " + verb)
			if _, isGroup := cliDispatchers[key]; isGroup || cliGroupPrefix(key) {
				continue
			}
			leaves = append(leaves, key)
		}
	}
	slices.Sort(leaves)
	return slices.Compact(leaves)
}

func caseLabelsByFunc(t *testing.T, dir string) map[string][]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	out := map[string][]string{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				clause, ok := n.(*ast.CaseClause)
				if !ok {
					return true
				}
				for _, expr := range clause.List {
					if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if verb, err := strconv.Unquote(lit.Value); err == nil {
							out[fn.Name.Name] = append(out[fn.Name.Name], verb)
						}
					}
				}
				return true
			})
		}
	}
	return out
}

// inventoryGaps compares the dispatched verbs with what runs them. Each gap is one line.
func inventoryGaps(dispatched []string, covered map[string]string, exempt map[string]string) []string {
	var gaps []string
	for _, verb := range dispatched {
		if covered[verb] == "" && exempt[verb] == "" {
			gaps = append(gaps, "dispatched with no smoke row, contract or exemption: "+verb)
		}
		if covered[verb] != "" && exempt[verb] != "" {
			gaps = append(gaps, "exempt yet covered by "+covered[verb]+": "+verb)
		}
	}
	for verb, by := range covered {
		if !slices.Contains(dispatched, verb) {
			gaps = append(gaps, by+" runs a verb nothing dispatches: "+verb)
		}
	}
	for verb := range exempt {
		if !slices.Contains(dispatched, verb) {
			gaps = append(gaps, "stale exemption: "+verb)
		}
	}
	slices.Sort(gaps)
	return gaps
}

// TestCLIVerbInventory fails on a verb the CLI dispatches that nothing runs (C1 of
// docs/superpowers/verification/2026-10-31-cli-audit.md: aura memory remember failed for two
// months because no test ran it).
func TestCLIVerbInventory(t *testing.T) {
	t.Parallel()
	covered := map[string]string{}
	for _, row := range cliSmokeRows {
		covered[cliVerbKey(row.argv)] = "smoke row " + row.name
	}
	for _, argv := range memoryVerbCalls {
		covered[cliVerbKey(append([]string{"memory"}, argv...))] = "the memory contract"
	}
	for _, gap := range inventoryGaps(dispatchedCLIVerbs(t), covered, cliExemptions) {
		t.Error(gap)
	}
	for verb, reason := range cliExemptions {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("exemption %q gives no reason", verb)
		}
	}
}

func TestCLIVerbInventoryFailsClosed(t *testing.T) {
	t.Parallel()
	dispatched := []string{"task list", "task cancel"}
	if gaps := inventoryGaps(dispatched, map[string]string{"task list": "row"}, nil); len(gaps) != 1 ||
		!strings.Contains(gaps[0], "task cancel") {
		t.Errorf("a dispatched verb with nothing running it gave %v", gaps)
	}
	if gaps := inventoryGaps(dispatched, map[string]string{"task list": "row", "task cancel": "row"},
		map[string]string{"task purge": "gone"}); len(gaps) != 1 || !strings.Contains(gaps[0], "stale exemption: task purge") {
		t.Errorf("a stale exemption gave %v", gaps)
	}
	if gaps := inventoryGaps(dispatched, map[string]string{"task list": "row", "task cancel": "row", "task gone": "row x"},
		nil); len(gaps) != 1 || !strings.Contains(gaps[0], "task gone") {
		t.Errorf("a row for a verb nothing dispatches gave %v", gaps)
	}
	if gaps := inventoryGaps(dispatched, map[string]string{"task list": "row", "task cancel": "row"},
		map[string]string{"task list": "why"}); len(gaps) != 1 || !strings.Contains(gaps[0], "exempt yet covered") {
		t.Errorf("an exempt verb that a row runs gave %v", gaps)
	}
}

func TestCLIVerbKeyFollowsTheGroups(t *testing.T) {
	t.Parallel()
	for argv, want := range map[string]string{
		"task list --json":                 "task list",
		"gateway grants list operator":     "gateway grants list",
		"gateway policy set op shell deny": "gateway policy set",
		"skills snippet save x":            "skills snippet save",
		"mcp profile list":                 "mcp profile list",
		"web tool web_search q":            "web tool web_search",
		"version":                          "version",
		"pack list chetto1983/aura":        "pack",
		"memory search mario":              "memory search",
	} {
		if got := cliVerbKey(strings.Fields(argv)); got != want {
			t.Errorf("cliVerbKey(%q) = %q, want %q", argv, got, want)
		}
	}
}

// The dispatchers' source must be where the test reads it, or the inventory reads nothing.
func TestCLIInventoryReadsThisPackage(t *testing.T) {
	t.Parallel()
	if _, err := os.Stat("main.go"); err != nil {
		t.Fatalf("the inventory parses the package's own source: %v", err)
	}
	if verbs := dispatchedCLIVerbs(t); !slices.Contains(verbs, "task list") || !slices.Contains(verbs, "memory remember") {
		t.Fatalf("dispatched verbs = %v, want task list and memory remember among them", verbs)
	}
}
