// Package mcpenv prepares the durable environment a stdio MCP server launches from
// (amendment #211).
//
// It exists because installing a stdio server never installed anything: `aura mcp add` and
// the cockpit's Custom mode wrote a launch declaration, so a `uvx <pkg>` row re-resolved its
// package at EVERY mount — including the boot mount, which is exactly when egress may be
// missing and when the warm cache cannot help (compose mounts a named volume over
// /root/.cache/uv, and a named volume is seeded once and never refreshed).
//
// Prepare turns that declaration into a path. It materialises the server's environment ONCE
// under Root and rewrites the launch to absolute paths inside it, so every later mount runs a
// binary rather than a resolver. It does not verify the result: the caller does that with
// mcp.ProbeServer, which already dials, handshakes and counts tools under a deadline.
//
// Only the two ecosystems that actually ship stdio MCP servers are prepared. Anything else —
// a plain binary, an absolute path, an interpreter the operator manages — passes through
// unprepared, and is still verified by the caller.
package mcpenv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/chetto1983/aura/internal/idroot"
)

// ErrEntrypoint reports that an install finished but produced no single executable to launch.
// It is a refusal, not a guess: storing a command nobody could resolve is what amendment #211
// exists to stop.
var ErrEntrypoint = errors.New("mcpenv: install produced no unambiguous entrypoint")

// CommandRunner runs one preparation command and returns its combined output. It mirrors the
// seam skills.Installer uses, so preparation is unit-testable without uv, npm or a network.
type CommandRunner func(ctx context.Context, dir, name string, args ...string) (string, error)

// Launch is a stdio server's command line, before or after preparation.
type Launch struct {
	Command string
	Args    []string
}

// Report says what preparation did, for the operator-facing install output. Prepared is false
// for a passthrough — that is a normal outcome, not a failure.
type Report struct {
	Prepared   bool
	Ecosystem  string // "python" | "node" | "" when passed through
	Dir        string
	Package    string
	Entrypoint string
}

// Preparer materialises environments under Root. A zero Run uses the real exec runner the
// composition root supplies; tests inject their own.
type Preparer struct {
	Root string
	Run  CommandRunner
}

// Prepare resolves in into the launch a mount should actually run. name is the server's
// registry name and becomes a directory, so it goes through the same traversal guard every
// other operator-keyed root uses (idroot) rather than a second one written here.
func (p *Preparer) Prepare(ctx context.Context, name string, in Launch) (Launch, Report, error) {
	if p == nil || strings.TrimSpace(p.Root) == "" || p.Run == nil {
		return in, Report{}, nil
	}
	req, ok := classify(in)
	if !ok {
		return in, Report{}, nil
	}
	// A resolver accepts more than a distribution name: a local path, a VCS URL, a wheel. The
	// metadata lookup that finds the entrypoint cannot, and guessing one from a path is how a
	// wrong binary gets stored — so this refuses BEFORE installing anything and says what to
	// do instead. Measured 2026-09-05: a local-path spec otherwise failed with a bare
	// "exit status 1" from the query, naming neither the cause nor the remedy. A declaration
	// that names its own entrypoint (`uvx --from <spec> <cmd>`) needs no lookup and is exempt.
	if req.entrypoint == "" && !plainDistribution(req.pkg) {
		return Launch{}, Report{}, fmt.Errorf(
			"%w: %q is a path or URL, not a package name — name the executable it installs, as `%s`",
			ErrEntrypoint, req.pkg, explicitSpecForm(req.ecosystem, req.pkg))
	}
	dir, err := idroot.RootIdentityDir(p.Root, name)
	if err != nil {
		return Launch{}, Report{}, fmt.Errorf("mcpenv: server name %q cannot name a directory: %w", name, err)
	}
	// Rebuild from scratch so a reinstall cannot inherit a half-written tree from a failed one.
	if err := os.RemoveAll(dir); err != nil {
		return Launch{}, Report{}, fmt.Errorf("mcpenv: clear %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Launch{}, Report{}, fmt.Errorf("mcpenv: create %s: %w", dir, err)
	}

	binDir, declared, err := p.install(ctx, dir, req)
	if err != nil {
		return Launch{}, Report{}, err
	}
	entry, err := resolveEntrypoint(binDir, req.pkg, declared)
	if err != nil {
		return Launch{}, Report{}, err
	}
	return Launch{Command: entry, Args: req.rest},
		Report{Prepared: true, Ecosystem: req.ecosystem, Dir: dir, Package: req.pkg, Entrypoint: entry},
		nil
}

// install materialises req under dir and reports the bin directory plus the console scripts to
// choose from. An explicit entrypoint IS the choice — the operator named it, so nothing is
// discovered and nothing is guessed; resolveEntrypoint still requires it to exist on disk.
func (p *Preparer) install(ctx context.Context, dir string, req request) (string, []string, error) {
	if req.ecosystem == "node" {
		binDir, err := p.installNode(ctx, dir, req.pkg)
		if err != nil || req.entrypoint != "" {
			return binDir, []string{req.entrypoint}, err
		}
		declared, err := p.nodeDeclared(dir, req.pkg)
		return binDir, declared, err
	}
	binDir, python, err := p.installPython(ctx, dir, req.pkg)
	if err != nil || req.entrypoint != "" {
		return binDir, []string{req.entrypoint}, err
	}
	declared, err := p.pythonDeclared(ctx, dir, python, req.pkg)
	return binDir, declared, err
}

func (p *Preparer) installPython(ctx context.Context, dir, pkg string) (binDir, python string, err error) {
	venv := filepath.Join(dir, "venv")
	if _, err := p.Run(ctx, dir, "uv", "venv", venv); err != nil {
		return "", "", fmt.Errorf("mcpenv: uv venv: %w", err)
	}
	python = filepath.Join(venv, "bin", "python")
	if _, err := p.Run(ctx, dir, "uv", "pip", "install", "--python", python, pkg); err != nil {
		return "", "", fmt.Errorf("mcpenv: uv pip install %s: %w", pkg, err)
	}
	return filepath.Join(venv, "bin"), python, nil
}

// entrypointQuery asks an installed distribution which console scripts IT declares. Listing
// the venv's bin/ instead does not work and the difference is not subtle: installing one
// small server put ~20 executables there, all but one belonging to its dependencies
// (httpx, uvicorn, typer, jsonschema, ...). Measured 2026-09-05 against mcp-server-time,
// which returns exactly ["mcp-server-time"] here.
const entrypointQuery = `import importlib.metadata as m,sys
d=m.distribution(sys.argv[1])
print("\n".join(e.name for e in d.entry_points if e.group=="console_scripts"))`

func (p *Preparer) pythonDeclared(ctx context.Context, dir, python, pkg string) ([]string, error) {
	dist := distributionName(pkg)
	out, err := p.Run(ctx, dir, python, "-c", entrypointQuery, dist)
	if err != nil {
		return nil, fmt.Errorf("mcpenv: %s installed, but its metadata could not be read as distribution %q: %w", pkg, dist, err)
	}
	return splitLines(out), nil
}

func (p *Preparer) installNode(ctx context.Context, dir, pkg string) (string, error) {
	if _, err := p.Run(ctx, dir, "npm", "install", "--prefix", dir, pkg); err != nil {
		return "", fmt.Errorf("mcpenv: npm install %s: %w", pkg, err)
	}
	return filepath.Join(dir, "node_modules", ".bin"), nil
}

// nodeDeclared reads the installed package's own "bin" field. Same reason as the python side:
// node_modules/.bin carries every dependency's binaries too, so the directory is not the answer.
func (p *Preparer) nodeDeclared(dir, pkg string) ([]string, error) {
	manifest := filepath.Join(dir, "node_modules", filepath.FromSlash(distributionName(pkg)), "package.json")
	raw, err := os.ReadFile(manifest) // #nosec G304 -- path built from the env root and the operator's package name
	if err != nil {
		return nil, fmt.Errorf("mcpenv: read %s: %w", manifest, err)
	}
	var meta nodeManifest
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("mcpenv: parse %s: %w", manifest, err)
	}
	return nodeBinNames(meta), nil
}

type nodeManifest struct {
	Name string          `json:"name"`
	Bin  json.RawMessage `json:"bin"`
}

// nodeBinNames reads npm's two "bin" shapes: a bare string (one binary, named after the
// package) or an object of name -> path.
func nodeBinNames(meta nodeManifest) []string {
	if len(meta.Bin) == 0 {
		return nil
	}
	var single string
	if err := json.Unmarshal(meta.Bin, &single); err == nil {
		return []string{path.Base(meta.Name)}
	}
	var many map[string]string
	if err := json.Unmarshal(meta.Bin, &many); err != nil {
		return nil
	}
	names := make([]string, 0, len(many))
	for n := range many {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// distributionName strips what a resolver accepts but a metadata lookup does not: an extras
// marker, a version constraint, an npm @version suffix. A scoped npm name keeps its leading
// @, so only a LATER @ separates the version.
func distributionName(pkg string) string {
	name := strings.TrimSpace(pkg)
	if i := strings.IndexAny(name, "[=<>~!;"); i >= 0 {
		name = name[:i]
	}
	if at := strings.LastIndex(name, "@"); at > 0 {
		name = name[:at]
	}
	return strings.TrimSpace(name)
}

// plainDistribution reports whether pkg is a name a metadata lookup can resolve, rather than
// a path, a URL or an archive a resolver would also accept.
func plainDistribution(pkg string) bool {
	name := distributionName(pkg)
	if name == "" || strings.ContainsAny(name, "/\\") && !strings.HasPrefix(name, "@") {
		return false
	}
	if strings.Contains(name, "://") || strings.HasPrefix(name, ".") {
		return false
	}
	// A scoped npm package is @scope/name — one slash, after the leading @.
	if strings.HasPrefix(name, "@") {
		return strings.Count(name, "/") == 1
	}
	return true
}

func splitLines(out string) []string {
	var names []string
	for l := range strings.SplitSeq(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			names = append(names, l)
		}
	}
	return names
}

// request is what a launch declaration asked for: the thing to install, the executable to run
// from it when the declaration names one, and the arguments that belong to the SERVER rather
// than to the resolver.
type request struct {
	pkg        string
	entrypoint string // "" means "discover it from the install"
	ecosystem  string
	rest       []string
}

// fromFlag is each resolver's "install this spec, then run that executable" flag — uvx spells
// it --from, npx spells it --package. Both take a value, and both make the entrypoint explicit.
var fromFlag = map[string]map[string]bool{
	"python": {"--from": true},
	"node":   {"--package": true, "-p": true},
}

// explicitSpecForm writes the remedy in the resolver's OWN grammar. Composing it from a flag
// name and a hardcoded `--from` produced `npx --package --from <spec> <exe>` — a command that
// does not exist, told to every npm operator who pasted a path (audit A3, 2026-09-05).
func explicitSpecForm(ecosystem, pkg string) string {
	if ecosystem == "node" {
		return "npx --package " + pkg + " <executable>"
	}
	return "uvx --from " + pkg + " <executable>"
}

// resolverBooleans are the value-less flags a launch declaration commonly carries. Every OTHER
// flag is one whose grammar this package does not know, and a flag that takes a value would
// otherwise have its VALUE read as the package name — `uvx --with foo pkg` installing "foo" is
// a silent wrong install, which is worse than not preparing at all. So an unknown flag ends
// preparation and the declaration is launched as written.
var resolverBooleans = map[string]bool{
	"-y": true, "--yes": true, "-q": true, "--quiet": true, "--offline": true,
	"--no-cache": true, "--native-tls": true, "--isolated": true, "--refresh": true,
}

// classify reads the declared launch. `uvx pkg --flag` means "--flag" is the server's and the
// package name is not; `uvx --from <spec> <cmd>` names both the spec to install and the
// executable to run from it.
func classify(in Launch) (request, bool) {
	var req request
	switch strings.ToLower(strings.TrimSuffix(filepath.Base(strings.TrimSpace(in.Command)), ".exe")) {
	case "uvx":
		req.ecosystem = "python"
	case "npx":
		req.ecosystem = "node"
	default:
		return request{}, false
	}
	takesValue := fromFlag[req.ecosystem]
	for i := 0; i < len(in.Args); i++ {
		arg := in.Args[i]
		switch {
		case takesValue[arg]:
			if i+1 >= len(in.Args) {
				return request{}, false
			}
			req.pkg, i = in.Args[i+1], i+1
		case strings.HasPrefix(arg, "-"):
			flag, _, inline := strings.Cut(arg, "=")
			if inline && takesValue[flag] {
				req.pkg = arg[len(flag)+1:]
				continue
			}
			if !resolverBooleans[arg] {
				return request{}, false // a grammar this package does not know: do not guess
			}
		case req.pkg == "":
			req.pkg = arg
			req.rest = append([]string(nil), in.Args[i+1:]...)
			return req, true
		default:
			// --from named the spec, so this positional names the executable to run from it.
			req.entrypoint = arg
			req.rest = append([]string(nil), in.Args[i+1:]...)
			return req, true
		}
	}
	// A resolver invoked with no package names nothing installable; leave it alone.
	return request{}, false
}

// resolveEntrypoint picks among the console scripts the installed package DECLARES — never
// among the executables sitting in the bin directory, which belong to the dependency tree as
// much as to the server. One declared script is the answer; several are disambiguated only by
// an exact package-name match, because choosing "the closest" is how a wrong binary gets
// stored. The chosen name is then required to exist on disk: metadata that promises a script
// the install did not write is a broken install, not an entrypoint.
func resolveEntrypoint(binDir, pkg string, declared []string) (string, error) {
	sort.Strings(declared)
	var chosen string
	switch {
	case len(declared) == 0:
		return "", fmt.Errorf("%w: %s declares no console script", ErrEntrypoint, pkg)
	case len(declared) == 1:
		chosen = declared[0]
	default:
		// An npm scope never appears in a bin name, so the name to match is the unscoped one.
		want := path.Base(distributionName(pkg))
		for _, n := range declared {
			if n == want {
				chosen = n
				break
			}
		}
		if chosen == "" {
			return "", fmt.Errorf("%w: %s declares %d console scripts (%s) and none is named %q — declare the entrypoint directly",
				ErrEntrypoint, pkg, len(declared), strings.Join(declared, ", "), want)
		}
	}
	// The chosen name came out of the installed package's own metadata, so it is as
	// package-controlled as the code the install just ran. Measured 2026-09-05 (audit A2): a
	// `"bin": {"../../../outside/evil": "./a.js"}` manifest yielded a stored command OUTSIDE
	// the environment root, and that path outlives the environment it was supposed to live in.
	// An entrypoint is one file in one directory; anything else is not a name, it is a route.
	if chosen != filepath.Base(chosen) || chosen == "." || chosen == ".." {
		return "", fmt.Errorf("%w: %s declares %q, which is a path rather than an executable name", ErrEntrypoint, pkg, chosen)
	}
	full := filepath.Join(binDir, chosen)
	if _, err := os.Stat(full); err != nil {
		return "", fmt.Errorf("%w: %s declares %q but the install left no such executable in %s", ErrEntrypoint, pkg, chosen, binDir)
	}
	return full, nil
}
