package mcpenv

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRun records the commands preparation issues and materialises the files a real uv/npm
// would leave behind, so the resolver is exercised against a real directory without either
// tool or a network.
type fakeRun struct {
	calls    [][]string
	produce  map[string][]string // dir relative to the env dir -> executables to create
	declared []string            // what the installed package says its console scripts are
	files    map[string]string   // extra files to write (node package.json)
	fail     string              // substring of a command to fail on
}

func (f *fakeRun) run(_ context.Context, dir, name string, args ...string) (string, error) {
	line := append([]string{name}, args...)
	f.calls = append(f.calls, line)
	joined := strings.Join(line, " ")
	if f.fail != "" && strings.Contains(joined, f.fail) {
		return "boom", errors.New("command failed: " + joined)
	}
	for rel, bins := range f.produce {
		binDir := filepath.Join(dir, rel)
		if err := os.MkdirAll(binDir, 0o700); err != nil {
			return "", err
		}
		for _, b := range bins {
			if err := os.WriteFile(filepath.Join(binDir, b), []byte("#!/bin/sh\n"), 0o700); err != nil {
				return "", err
			}
		}
	}
	for rel, body := range f.files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			return "", err
		}
	}
	// The entrypoint query: answer as importlib.metadata would, one name per line.
	if strings.Contains(joined, "importlib.metadata") {
		return strings.Join(f.declared, "\n") + "\n", nil
	}
	return "", nil
}

func newPreparer(t *testing.T, f *fakeRun) *Preparer {
	t.Helper()
	return &Preparer{Root: t.TempDir(), Run: f.run}
}

// Measured 2026-09-05 against the real package: `uv pip install` leaves ~20 executables in
// venv/bin, all but one belonging to the dependency tree. Listing the directory would have to
// choose among them; asking the distribution what IT declares does not.
func TestPreparePythonIgnoresDependencyScripts(t *testing.T) {
	f := &fakeRun{
		produce: map[string][]string{"venv/bin": {
			"python", "python3", "pip", "calculator-mcp-server",
			"httpx", "uvicorn", "typer", "jsonschema", "isympy", "f2py", "pygmentize",
		}},
		declared: []string{"calculator-mcp-server"},
	}
	p := newPreparer(t, f)

	out, rep, err := p.Prepare(context.Background(), "calculator",
		Launch{Command: "uvx", Args: []string{"calculator-mcp-server", "--stdio"}})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !rep.Prepared || rep.Ecosystem != "python" || rep.Package != "calculator-mcp-server" {
		t.Fatalf("report = %#v", rep)
	}
	if filepath.Base(out.Command) != "calculator-mcp-server" || !filepath.IsAbs(out.Command) {
		t.Fatalf("command = %q, want an absolute path to the installed script", out.Command)
	}
	// The package name is the resolver's argument; everything after it is the server's.
	if len(out.Args) != 1 || out.Args[0] != "--stdio" {
		t.Fatalf("args = %#v, want [--stdio]", out.Args)
	}
	if len(f.calls) != 3 || f.calls[0][0] != "uv" || f.calls[1][1] != "pip" ||
		!strings.Contains(strings.Join(f.calls[2], " "), "importlib.metadata") {
		t.Fatalf("calls = %#v, want uv venv, uv pip install, then the entrypoint query", f.calls)
	}
}

func TestPrepareNodeSkipsResolverFlags(t *testing.T) {
	f := &fakeRun{
		produce: map[string][]string{"node_modules/.bin": {"some-mcp", "acorn", "semver"}},
		files:   map[string]string{"node_modules/some-mcp/package.json": `{"name":"some-mcp","bin":"./cli.js"}`},
	}
	p := newPreparer(t, f)

	out, rep, err := p.Prepare(context.Background(), "some",
		Launch{Command: "npx", Args: []string{"-y", "some-mcp", "--port", "0"}})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if rep.Ecosystem != "node" || rep.Package != "some-mcp" {
		t.Fatalf("report = %#v", rep)
	}
	if filepath.Base(out.Command) != "some-mcp" {
		t.Fatalf("command = %q", out.Command)
	}
	if strings.Join(out.Args, " ") != "--port 0" {
		t.Fatalf("args = %#v, want the server's own flags only", out.Args)
	}
}

func TestPreparePassesThroughWhatItCannotPrepare(t *testing.T) {
	for _, in := range []Launch{
		{Command: "/usr/local/bin/my-server", Args: []string{"--stdio"}},
		{Command: "node", Args: []string{"server.js"}},
		{Command: "uvx"}, // a resolver naming no package installs nothing
		{Command: "npx", Args: []string{"-y"}},
	} {
		f := &fakeRun{}
		p := newPreparer(t, f)
		out, rep, err := p.Prepare(context.Background(), "svc", in)
		if err != nil {
			t.Fatalf("Prepare(%#v): %v", in, err)
		}
		if rep.Prepared {
			t.Fatalf("Prepare(%#v) reported prepared, want passthrough", in)
		}
		if out.Command != in.Command {
			t.Fatalf("Prepare(%#v) rewrote the command to %q", in, out.Command)
		}
		if len(f.calls) != 0 {
			t.Fatalf("Prepare(%#v) ran %#v, want nothing", in, f.calls)
		}
	}
}

func TestPrepareRefusesAnAmbiguousEntrypoint(t *testing.T) {
	f := &fakeRun{
		produce:  map[string][]string{"venv/bin": {"python", "pip", "alpha", "beta"}},
		declared: []string{"alpha", "beta"},
	}
	p := newPreparer(t, f)
	_, _, err := p.Prepare(context.Background(), "svc", Launch{Command: "uvx", Args: []string{"pkg"}})
	if !errors.Is(err, ErrEntrypoint) {
		t.Fatalf("err = %v, want ErrEntrypoint", err)
	}
	if !strings.Contains(err.Error(), "alpha, beta") {
		t.Fatalf("err %q should name what the package declares so the operator can pick", err)
	}
}

func TestPrepareDisambiguatesByExactPackageName(t *testing.T) {
	f := &fakeRun{
		produce:  map[string][]string{"venv/bin": {"python", "pip", "pkg", "pkg-helper"}},
		declared: []string{"pkg", "pkg-helper"},
	}
	p := newPreparer(t, f)
	out, _, err := p.Prepare(context.Background(), "svc", Launch{Command: "uvx", Args: []string{"pkg"}})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if filepath.Base(out.Command) != "pkg" {
		t.Fatalf("command = %q, want the script named exactly like the package", out.Command)
	}
}

func TestPrepareRefusesWhenNothingWasInstalled(t *testing.T) {
	f := &fakeRun{produce: map[string][]string{"venv/bin": {"python", "pip"}}} // declares nothing
	p := newPreparer(t, f)
	_, _, err := p.Prepare(context.Background(), "svc", Launch{Command: "uvx", Args: []string{"pkg"}})
	if !errors.Is(err, ErrEntrypoint) {
		t.Fatalf("err = %v, want ErrEntrypoint", err)
	}
}

func TestPrepareSurfacesAFailedInstall(t *testing.T) {
	f := &fakeRun{fail: "pip install"}
	p := newPreparer(t, f)
	_, _, err := p.Prepare(context.Background(), "svc", Launch{Command: "uvx", Args: []string{"pkg"}})
	if err == nil || !strings.Contains(err.Error(), "uv pip install pkg") {
		t.Fatalf("err = %v, want the failing install named", err)
	}
}

func TestPrepareRejectsANameThatEscapesTheRoot(t *testing.T) {
	f := &fakeRun{}
	p := newPreparer(t, f)
	for _, name := range []string{"../evil", "a/b", "", ".hidden"} {
		if _, _, err := p.Prepare(context.Background(), name, Launch{Command: "uvx", Args: []string{"pkg"}}); err == nil {
			t.Fatalf("Prepare(name=%q) = nil error, want a refusal", name)
		}
	}
}

// A reinstall must not inherit a tree a previous failed one left behind: the stale binary
// would resolve, and the operator would be running the old package believing it is the new.
func TestPrepareClearsAPreviousEnvironment(t *testing.T) {
	f := &fakeRun{
		produce:  map[string][]string{"venv/bin": {"python", "pip", "fresh"}},
		declared: []string{"fresh"},
	}
	p := newPreparer(t, f)
	stale := filepath.Join(p.Root, "svc", "venv", "bin")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stale, "stale"), []byte("old"), 0o700); err != nil {
		t.Fatalf("seed: %v", err)
	}
	out, _, err := p.Prepare(context.Background(), "svc", Launch{Command: "uvx", Args: []string{"pkg"}})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if filepath.Base(out.Command) != "fresh" {
		t.Fatalf("command = %q, want the freshly installed script", out.Command)
	}
	if _, err := os.Stat(filepath.Join(stale, "stale")); !os.IsNotExist(err) {
		t.Fatalf("stale executable survived the reinstall")
	}
}

func TestZeroPreparerPassesEverythingThrough(t *testing.T) {
	var p *Preparer
	in := Launch{Command: "uvx", Args: []string{"pkg"}}
	out, rep, err := p.Prepare(context.Background(), "svc", in)
	if err != nil || rep.Prepared || out.Command != in.Command {
		t.Fatalf("nil preparer: out=%#v rep=%#v err=%v", out, rep, err)
	}
	unconfigured := &Preparer{}
	if out, rep, err = unconfigured.Prepare(context.Background(), "svc", in); err != nil || rep.Prepared {
		t.Fatalf("unconfigured preparer: out=%#v rep=%#v err=%v", out, rep, err)
	}
}

// Metadata that promises a script the install did not write is a broken install. Storing the
// path anyway would defer the failure to the first mount, which is the failure mode #211 exists
// to remove.
func TestPrepareRefusesADeclaredScriptThatIsNotOnDisk(t *testing.T) {
	f := &fakeRun{
		produce:  map[string][]string{"venv/bin": {"python", "pip"}},
		declared: []string{"ghost"},
	}
	p := newPreparer(t, f)
	_, _, err := p.Prepare(context.Background(), "svc", Launch{Command: "uvx", Args: []string{"pkg"}})
	if !errors.Is(err, ErrEntrypoint) || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("err = %v, want ErrEntrypoint naming the missing script", err)
	}
}

// A node package's "bin" object names several binaries; the package name disambiguates.
func TestPrepareNodePicksTheNamedBinFromAnObject(t *testing.T) {
	f := &fakeRun{
		produce: map[string][]string{"node_modules/.bin": {"some-mcp", "some-mcp-dev"}},
		files: map[string]string{
			"node_modules/some-mcp/package.json": `{"name":"some-mcp","bin":{"some-mcp":"./cli.js","some-mcp-dev":"./dev.js"}}`,
		},
	}
	p := newPreparer(t, f)
	out, _, err := p.Prepare(context.Background(), "svc", Launch{Command: "npx", Args: []string{"some-mcp"}})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if filepath.Base(out.Command) != "some-mcp" {
		t.Fatalf("command = %q, want the bin named like the package", out.Command)
	}
}

// A resolver accepts what a metadata lookup does not: extras, version constraints, an npm
// @version. A scoped npm name keeps its leading @.
func TestDistributionNameStripsWhatOnlyAResolverUnderstands(t *testing.T) {
	for in, want := range map[string]string{
		"calculator-mcp-server": "calculator-mcp-server",
		"pkg[cli]":              "pkg",
		"pkg==1.2.3":            "pkg",
		"pkg>=1,<2":             "pkg",
		"some-mcp@1.2.3":        "some-mcp",
		"@scope/some-mcp@1.2.3": "@scope/some-mcp",
		"@scope/some-mcp":       "@scope/some-mcp",
		"  spaced-pkg == 2 ":    "spaced-pkg",
	} {
		if got := distributionName(in); got != want {
			t.Errorf("distributionName(%q) = %q, want %q", in, got, want)
		}
	}
}

// A resolver takes paths, URLs and archives; the metadata lookup that finds the entrypoint
// does not. Refusing before the install, with the remedy in the message, is the difference
// between an operator who knows what to do and one reading "exit status 1".
func TestPrepareRefusesAPathOrURLBeforeInstalling(t *testing.T) {
	for _, pkg := range []string{
		"/home/user/some-checkout", "./local-pkg", "git+https://example.test/x.git",
		"https://example.test/pkg.whl",
	} {
		f := &fakeRun{}
		p := newPreparer(t, f)
		_, _, err := p.Prepare(context.Background(), "svc", Launch{Command: "uvx", Args: []string{pkg}})
		if !errors.Is(err, ErrEntrypoint) {
			t.Fatalf("Prepare(%q) = %v, want ErrEntrypoint", pkg, err)
		}
		if !strings.Contains(err.Error(), "uvx --from "+pkg+" <executable>") {
			t.Fatalf("err %q should tell the operator the exact command to run instead", err)
		}
		if len(f.calls) != 0 {
			t.Fatalf("Prepare(%q) ran %#v before refusing", pkg, f.calls)
		}
	}
	// A scoped npm name is not a path and must survive.
	f := &fakeRun{
		produce: map[string][]string{"node_modules/.bin": {"srv"}},
		files:   map[string]string{"node_modules/@scope/srv/package.json": `{"name":"@scope/srv","bin":"./x.js"}`},
	}
	p := newPreparer(t, f)
	if _, _, err := p.Prepare(context.Background(), "svc", Launch{Command: "npx", Args: []string{"@scope/srv"}}); err != nil {
		t.Fatalf("scoped npm package refused: %v", err)
	}
}

// `uvx --from <spec> <cmd>` is the documented way to run a server whose executable is not
// named after its distribution — and the only way to install one from git, which no metadata
// lookup could resolve. The spec is installed and the named executable is the entrypoint: the
// operator declared it, so nothing is discovered and nothing is guessed.
func TestPrepareInstallsAnExplicitSpecAndRunsTheNamedExecutable(t *testing.T) {
	f := &fakeRun{produce: map[string][]string{"venv/bin": {"python", "pip", "calc"}}}
	p := newPreparer(t, f)
	out, rep, err := p.Prepare(context.Background(), "calc", Launch{
		Command: "uvx",
		Args:    []string{"--from", "git+https://example.test/calc.git", "calc", "--stdio"},
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if rep.Package != "git+https://example.test/calc.git" || filepath.Base(out.Command) != "calc" {
		t.Fatalf("report = %#v, command = %q", rep, out.Command)
	}
	if strings.Join(out.Args, " ") != "--stdio" {
		t.Fatalf("args = %#v, want the server's own flags only", out.Args)
	}
	// No metadata query: the entrypoint was declared, so there is nothing to ask.
	for _, c := range f.calls {
		if strings.Contains(strings.Join(c, " "), "importlib.metadata") {
			t.Fatalf("queried metadata for a declared entrypoint: %#v", f.calls)
		}
	}
}

func TestPrepareReadsTheNpmFormOfAnExplicitSpec(t *testing.T) {
	for _, args := range [][]string{
		{"-y", "--package", "@scope/srv", "srv-bin"},
		{"--package=@scope/srv", "srv-bin"},
		{"-p", "@scope/srv", "srv-bin"},
	} {
		f := &fakeRun{produce: map[string][]string{"node_modules/.bin": {"srv-bin"}}}
		p := newPreparer(t, f)
		out, rep, err := p.Prepare(context.Background(), "srv", Launch{Command: "npx", Args: args})
		if err != nil {
			t.Fatalf("Prepare(%v): %v", args, err)
		}
		if rep.Package != "@scope/srv" || filepath.Base(out.Command) != "srv-bin" {
			t.Fatalf("Prepare(%v): report = %#v, command = %q", args, rep, out.Command)
		}
	}
}

// An explicit entrypoint is still required to exist: a typo in the declaration must fail the
// install, not be stored and fail at the first mount.
func TestPrepareRefusesAnExplicitEntrypointThatWasNotInstalled(t *testing.T) {
	f := &fakeRun{produce: map[string][]string{"venv/bin": {"python", "pip"}}}
	p := newPreparer(t, f)
	_, _, err := p.Prepare(context.Background(), "svc",
		Launch{Command: "uvx", Args: []string{"--from", "pkg", "typo"}})
	if !errors.Is(err, ErrEntrypoint) || !strings.Contains(err.Error(), "typo") {
		t.Fatalf("err = %v, want ErrEntrypoint naming the missing executable", err)
	}
}

// A flag this package does not know may take a VALUE, and reading that value as the package
// name is a silent wrong install — `uvx --with httpx pkg` would install httpx. Passing the
// declaration through unprepared is the honest outcome: nothing is guessed, and the caller
// still verifies the launch with a real handshake.
func TestPrepareLeavesAGrammarItDoesNotKnowAlone(t *testing.T) {
	for _, in := range []Launch{
		{Command: "uvx", Args: []string{"--with", "httpx", "pkg"}},
		{Command: "uvx", Args: []string{"--python", "3.12", "pkg"}},
		{Command: "npx", Args: []string{"--userconfig", "/etc/npmrc", "pkg"}},
		{Command: "uvx", Args: []string{"--from"}}, // a value flag with no value
	} {
		f := &fakeRun{}
		p := newPreparer(t, f)
		out, rep, err := p.Prepare(context.Background(), "svc", in)
		if err != nil || rep.Prepared || out.Command != in.Command {
			t.Fatalf("Prepare(%#v): out=%#v rep=%#v err=%v, want an untouched passthrough", in, out, rep, err)
		}
		if len(f.calls) != 0 {
			t.Fatalf("Prepare(%#v) ran %#v, want nothing", in, f.calls)
		}
	}
}

// An npm scope never appears in a bin name, so a scoped package with several bins is
// disambiguated by its unscoped name — matching the scoped one would refuse every such package.
func TestPrepareNodeMatchesAScopedPackageByItsUnscopedName(t *testing.T) {
	f := &fakeRun{
		produce: map[string][]string{"node_modules/.bin": {"srv", "srv-dev"}},
		files: map[string]string{
			"node_modules/@scope/srv/package.json": `{"name":"@scope/srv","bin":{"srv":"./cli.js","srv-dev":"./dev.js"}}`,
		},
	}
	p := newPreparer(t, f)
	out, _, err := p.Prepare(context.Background(), "svc", Launch{Command: "npx", Args: []string{"@scope/srv"}})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if filepath.Base(out.Command) != "srv" {
		t.Fatalf("command = %q, want the bin named like the unscoped package", out.Command)
	}
}

// Audit A3: the remedy is composed per resolver. Asserting only that "--from" appears passed
// for `npx --package --from <spec> <exe>`, a command that does not exist — so this asserts the
// whole sentence, one per ecosystem.
func TestRefusalNamesEachResolversOwnGrammar(t *testing.T) {
	for _, c := range []struct{ command, want string }{
		{"uvx", "uvx --from ./local <executable>"},
		{"npx", "npx --package ./local <executable>"},
	} {
		f := &fakeRun{}
		p := newPreparer(t, f)
		_, _, err := p.Prepare(context.Background(), "svc", Launch{Command: c.command, Args: []string{"./local"}})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: err = %v, want it to name %q", c.command, err, c.want)
		}
		if strings.Contains(err.Error(), "--package --from") {
			t.Fatalf("%s: err %q names a flag combination no resolver accepts", c.command, err)
		}
	}
}

// Audit A2: the entrypoint name comes from the installed package's own metadata, so it is
// package-controlled. A name carrying a path escapes the environment root, and the absolute
// path that lands in the registry outlives the environment it was supposed to live in.
func TestPrepareRefusesAnEntrypointNameThatIsAPath(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "evil"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, in := range []Launch{
		{Command: "npx", Args: []string{"srv"}},
		{Command: "uvx", Args: []string{"srv"}},
	} {
		f := &fakeRun{
			produce:  map[string][]string{"node_modules/.bin": {"x"}, "venv/bin": {"python"}},
			declared: []string{"../../../outside/evil"},
			files: map[string]string{
				"node_modules/srv/package.json": `{"name":"srv","bin":{"../../../outside/evil":"./a.js"}}`,
			},
		}
		p := &Preparer{Root: root, Run: f.run}
		out, _, err := p.Prepare(context.Background(), "svc", in)
		if !errors.Is(err, ErrEntrypoint) {
			t.Fatalf("%s: err = %v, want ErrEntrypoint", in.Command, err)
		}
		if out.Command != "" {
			t.Fatalf("%s: stored %q, a command outside the environment root", in.Command, out.Command)
		}
	}
}
