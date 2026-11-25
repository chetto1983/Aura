package main

import (
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
)

// tier_runners_resolve_test.go answers one question for tier_runners_test.go: which
// `go test` invocations does CI actually make, with which tags, packages and -run? It
// reads three layers and no more:
//
//  1. the step's own run text, with the workflow, job and step env;
//  2. every make target the step names, through prerequisites and $(MAKE), with the
//     Makefile's variables expanded;
//  3. every scripts/*.sh the step or a recipe launches, and every scripts/*.py it runs as
//     `python3 scripts/x.py`, whose literal ["go", "test", ...] lists are read. A script is
//     a leaf: what it launches or imports in turn is not followed.
//
// That is as deep as CI goes today: musr_e2e reaches it as
// ci.yml -> make musr-e2e -> scripts/musr_e2e.sh. It reads text, not control flow: a
// script's early `exit 0` is invisible, which is why manual_test_tiers.txt can strip a
// route's credit. A step that may not run on a push (an `if:`, continue-on-error, a
// workflow without a push trigger) is recorded with that condition.

type workflowStep struct {
	Run             string         `yaml:"run"`
	Env             map[string]any `yaml:"env"`
	If              any            `yaml:"if"`
	ContinueOnError any            `yaml:"continue-on-error"`
}

type workflowSpec struct {
	On   any            `yaml:"on"`
	Env  map[string]any `yaml:"env"`
	Jobs map[string]struct {
		Env             map[string]any `yaml:"env"`
		If              any            `yaml:"if"`
		ContinueOnError any            `yaml:"continue-on-error"`
		Steps           []workflowStep `yaml:"steps"`
	} `yaml:"jobs"`
}

// goTestRun is one `go test` a workflow step makes. packages are repo-relative patterns
// ("internal/db", "internal/db/...", "..."); an argument that could not be resolved is
// left out, so it covers nothing.
type goTestRun struct {
	via       string
	condition string
	tags      []string
	race      bool
	packages  []string
	run       string
}

type tierWalk struct {
	root      string
	module    string
	make      makefileRules
	condition string
	runs      []goTestRun
}

var (
	scriptPath       = regexp.MustCompile(`^(?:\./)?(scripts/[A-Za-z0-9_./-]+\.sh)$`)
	pythonDriverPath = regexp.MustCompile(`^(?:\./)?(scripts/[A-Za-z0-9_./-]+\.py)$`)
	unresolvedVar    = regexp.MustCompile(`\$[A-Za-z0-9_{(]`)
	redirect         = regexp.MustCompile(`^[0-9]*[<>]+`)
	moduleLine       = regexp.MustCompile(`(?m)^module\s+(\S+)`)
	launchers        = []string{"bash", "sh", "source", ".", "exec", "time", "!", "if", "then", "do", "else"}
	declareWords     = []string{"export", "local", "readonly", "declare"}
	alwaysRuns       = []string{"true", "always()", "success()", "!cancelled()"}
)

func workflowTierRunners(root string) ([]goTestRun, error) {
	mk, err := readMakefile(filepath.Join(root, "Makefile"))
	if err != nil {
		return nil, err
	}
	w := &tierWalk{root: root, make: mk}
	if gomod, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil {
		if m := moduleLine.FindSubmatch(gomod); m != nil {
			w.module = string(m[1])
		}
	}
	var paths []string
	for _, pattern := range []string{"*.yml", "*.yaml"} {
		matches, err := filepath.Glob(filepath.Join(root, ".github", "workflows", pattern))
		if err != nil {
			return nil, err
		}
		paths = append(paths, matches...)
	}
	for _, file := range paths {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var wf workflowSpec
		if err := yaml.Unmarshal(data, &wf); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		for _, jobID := range slices.Sorted(maps.Keys(wf.Jobs)) {
			job := wf.Jobs[jobID]
			for _, step := range job.Steps {
				if step.Run == "" {
					continue
				}
				w.condition = joinConditions(pushCondition(wf.On),
					runCondition(job.If, job.ContinueOnError), runCondition(step.If, step.ContinueOnError))
				env := stringEnv(wf.Env, job.Env, step.Env)
				if err := w.body(step.Run, env, filepath.Base(file)+" "+jobID, true); err != nil {
					return nil, err
				}
			}
		}
	}
	return w.runs, nil
}

// pushCondition says why a workflow does not run on every push to a branch.
func pushCondition(on any) string {
	switch v := on.(type) {
	case string:
		if v == "push" {
			return ""
		}
	case []any:
		if slices.Contains(v, any("push")) {
			return ""
		}
	case map[string]any:
		push, ok := v["push"]
		if filter, isMap := push.(map[string]any); isMap && filter["branches"] == nil && filter["tags"] != nil {
			return "workflow pushes run on tags only"
		}
		if ok {
			return ""
		}
	}
	return "workflow has no push trigger"
}

// runCondition says why a job or step may not run, or not fail, on a push. always(),
// success() and !cancelled() still run on every push that gets that far.
func runCondition(ifValue, continueOnError any) string {
	if continueOnError != nil && fmt.Sprint(continueOnError) != "false" {
		return fmt.Sprintf("continue-on-error: %v", continueOnError)
	}
	if ifValue == nil {
		return ""
	}
	expr := strings.TrimSpace(fmt.Sprint(ifValue))
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(expr, "${{"), "}}"))
	if slices.Contains(alwaysRuns, inner) {
		return ""
	}
	return "if: " + expr
}

func joinConditions(conditions ...string) string {
	return strings.Join(slices.DeleteFunc(conditions, func(c string) bool { return c == "" }), "; ")
}

func stringEnv(layers ...map[string]any) map[string]string {
	env := map[string]string{}
	for _, layer := range layers {
		for name, value := range layer {
			env[name] = fmt.Sprint(value)
		}
	}
	return env
}

// body walks shell text as a sequence of commands, keeping the variables it assigns so a
// later `go test -tags "${TAGS}"` resolves. follow is false inside a script: scripts are
// leaves.
func (w *tierWalk) body(text string, env map[string]string, via string, follow bool) error {
	vars := maps.Clone(env)
	for _, line := range shellLines(text) {
		for _, words := range shellCommands(line) {
			rest, cmdVars := assignments(words, vars)
			if err := w.command(rest, cmdVars, via, follow); err != nil {
				return err
			}
		}
	}
	return nil
}

// assignments consumes a command's leading NAME=value words. With nothing after them, or
// behind export/local/readonly/declare, they set the body's variables; in front of a
// command they set that command's environment only.
func assignments(words []string, vars map[string]string) ([]string, map[string]string) {
	declared := len(words) > 0 && slices.Contains(declareWords, words[0])
	if declared {
		words = words[1:]
	}
	cmdVars := maps.Clone(vars)
	n := 0
	for ; n < len(words); n++ {
		m := shellAssign.FindStringSubmatch(words[n])
		if m == nil {
			break
		}
		cmdVars[m[1]] = expandShell(m[2], cmdVars)
	}
	if declared || n == len(words) {
		maps.Copy(vars, cmdVars)
		return nil, vars
	}
	return words[n:], cmdVars
}

// command looks for go test, make and scripts/*.sh only where a command can start: the
// first word, or after a launcher (bash, if, !, ...). `echo go test -tags x` runs nothing.
func (w *tierWalk) command(words []string, vars map[string]string, via string, follow bool) error {
	for i := range words {
		words[i] = expandShell(words[i], vars)
	}
	for i, word := range words {
		if i > 0 && !slices.Contains(launchers, words[i-1]) {
			continue
		}
		switch {
		case word == "go" && i+1 < len(words) && words[i+1] == "test":
			w.record(words[i+2:], vars["GOFLAGS"], via)
			return nil
		case !follow:
		case (word == "python3" || word == "python") && i+1 < len(words) && pythonDriverPath.MatchString(words[i+1]):
			return w.pythonDriver(pythonDriverPath.FindStringSubmatch(words[i+1])[1], words[i+2:], vars, via)
		case word == "make":
			for _, target := range words[i+1:] {
				if strings.HasPrefix(target, "-") || strings.Contains(target, "=") {
					continue
				}
				if err := w.makeTarget(target, vars, via+" -> make "+target); err != nil {
					return err
				}
			}
			return nil
		case scriptPath.MatchString(word):
			if err := w.script(scriptPath.FindStringSubmatch(word)[1], vars, via); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *tierWalk) makeTarget(target string, env map[string]string, via string) error {
	rule, ok := w.make.rules[target]
	if !ok {
		return fmt.Errorf("%s: no such target in the Makefile", via)
	}
	for _, prereq := range rule.prereqs {
		if err := w.makeTarget(prereq, env, via+" -> make "+prereq); err != nil {
			return err
		}
	}
	return w.body(w.make.expand(strings.Join(rule.recipe, "\n")), env, via, true)
}

func (w *tierWalk) script(rel string, env map[string]string, via string) error {
	data, err := os.ReadFile(filepath.Join(w.root, filepath.FromSlash(rel)))
	if err != nil {
		return fmt.Errorf("%s: %w", via, err)
	}
	return w.body(string(data), env, via+" -> "+rel, false)
}

// pythonDriver records the `go test` argv lists a Python driver spells as literals (see
// pythonGoTestLists). Like a shell script, a driver is a leaf: what it imports is not read.
// Which lists a driver runs is its own control flow, so they count only when it is launched
// with `--tier all` (agent_memory_eval.py's switch for every suite); any other or no
// --tier credits nothing. A list with any element that is not a literal credits nothing
// either: `go test` takes flags after the packages too, so no position is safe.
func (w *tierWalk) pythonDriver(rel string, args []string, env map[string]string, via string) error {
	data, err := os.ReadFile(filepath.Join(w.root, filepath.FromSlash(rel)))
	if err != nil {
		return fmt.Errorf("%s: %w", via, err)
	}
	if pythonTier(args) != "all" {
		return nil
	}
	for _, argv := range pythonGoTestLists(string(data)) {
		if !slices.Contains(argv, unresolvedPython) {
			w.record(argv[2:], env["GOFLAGS"], via+" -> "+rel)
		}
	}
	return nil
}

func pythonTier(args []string) string {
	for i, arg := range args {
		if tier, ok := strings.CutPrefix(arg, "--tier="); ok {
			return tier
		}
		if arg == "--tier" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func (w *tierWalk) record(args []string, goflags, via string) {
	run := w.goTest(args, goflags)
	run.via, run.condition = via, w.condition
	w.runs = append(w.runs, run)
}

// goTest reads a `go test` argument list. GOFLAGS applies first and the command line
// overrides it, as `go help environment` says: a -tags on the command line replaces the
// GOFLAGS one. An argument that could not be resolved and is not the value of -tags, -run
// or -skip may be any flag — `$EXTRA` could hold -run=^X$ — so the run then covers nothing.
func (w *tierWalk) goTest(args []string, goflags string) goTestRun {
	args = withoutRedirects(args)
	env := readGoTestArgs(strings.Fields(goflags))
	cmd := readGoTestArgs(args)
	run := goTestRun{tags: cmd.tags, race: cmd.race || env.race, run: cmd.run}
	if !cmd.tagsSet && env.tagsSet {
		run.tags = env.tags
	}
	if cmd.opaque {
		return run
	}
	for i := 0; i < len(args) && args[i] != "-args"; i++ {
		if pkg, ok := packagePattern(args[i], w.module); ok {
			run.packages = append(run.packages, pkg)
		}
	}
	return run
}

// withoutRedirects drops shell redirections (`> file`, `2>`, `<in`), which are no
// arguments of go test.
func withoutRedirects(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		switch redirect.FindString(args[i]) {
		case "":
			out = append(out, args[i])
		case args[i]:
			i++
		}
	}
	return out
}

type goTestArgs struct {
	tags          []string
	run           string
	tagsSet, race bool
	opaque        bool
}

// readGoTestArgs reads -tags, -race and -run, and whether an unresolved argument stands
// where a flag could. A tag value still holding a `$` ("$1", ${{ matrix.x }}) could not be
// resolved and names no tag.
func readGoTestArgs(args []string) goTestArgs {
	var out goTestArgs
	for i := 0; i < len(args) && args[i] != "-args"; i++ {
		if !strings.HasPrefix(args[i], "-") {
			out.opaque = out.opaque || strings.Contains(args[i], "$")
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		if name == "race" {
			out.race = true
		}
		if name != "tags" && name != "run" && name != "skip" {
			continue
		}
		if !hasValue && i+1 < len(args) {
			i++
			value = args[i]
		}
		switch name {
		case "run":
			out.run = value
		case "tags":
			out.tagsSet, out.tags = true, nil
			for _, tag := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' }) {
				if !strings.Contains(tag, "$") {
					out.tags = append(out.tags, tag)
				}
			}
		}
	}
	return out
}

// packagePattern turns a package argument into a repo-relative pattern. Only ./paths and
// the module's import paths are packages; anything else (a redirect target, a flag value)
// is not, and an argument holding an unresolved variable covers nothing.
func packagePattern(arg, module string) (string, bool) {
	if strings.Contains(arg, "$") {
		return "", false
	}
	if module != "" && (arg == module || strings.HasPrefix(arg, module+"/")) {
		arg = "./" + strings.TrimPrefix(strings.TrimPrefix(arg, module), "/")
	} else if arg != "." && !strings.HasPrefix(arg, "./") {
		return "", false
	}
	pattern := path.Clean(arg)
	if pattern == ".." || strings.HasPrefix(pattern, "../") {
		return "", false
	}
	return pattern, true
}

// covers reports whether the run's packages include the package in dir.
func (r goTestRun) covers(dir string) bool {
	for _, pattern := range r.packages {
		base, all := strings.CutSuffix(pattern, "/...")
		if pattern == "..." {
			base, all = ".", true
		}
		if dir == base || (all && (base == "." || strings.HasPrefix(dir, base+"/"))) {
			return true
		}
	}
	return false
}

// runs reports whether -run selects the top-level test name. Like `go test`, only the
// pattern's first slash-separated element applies to it; an unresolved variable selects
// nothing.
func (r goTestRun) runs(test string) bool {
	if r.run == "" {
		return true
	}
	if unresolvedVar.MatchString(r.run) {
		return false
	}
	re, err := regexp.Compile(topLevelPattern(r.run))
	return err == nil && re.MatchString(test)
}

func topLevelPattern(pattern string) string {
	depth := 0
	for i, c := range pattern {
		switch c {
		case '[', '(':
			depth++
		case ']', ')':
			depth--
		case '/':
			if depth == 0 {
				return pattern[:i]
			}
		}
	}
	return pattern
}
