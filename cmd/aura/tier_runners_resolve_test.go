package main

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
)

// tier_runners_resolve_test.go answers one question for tier_runners_test.go: which build
// tags does a workflow step actually hand to `go test`? It reads three layers and no more:
//
//  1. the step's own run text, with the workflow, job and step env;
//  2. every make target the step names, through prerequisites and $(MAKE), with the
//     Makefile's variables expanded;
//  3. every scripts/*.sh the step or a recipe launches. A script is a leaf: what it
//     launches in turn is not followed.
//
// That is as deep as CI goes today: musr_e2e reaches it as
// ci.yml -> make musr-e2e -> scripts/musr_e2e.sh. It reads text, not control flow, so a
// step's `if:`, the workflow trigger, a script's early `exit 0` and a `-run` filter are
// all invisible to it.

type workflowSpec struct {
	Env  map[string]any `yaml:"env"`
	Jobs map[string]struct {
		Env   map[string]any `yaml:"env"`
		Steps []struct {
			Run string         `yaml:"run"`
			Env map[string]any `yaml:"env"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

type makeRule struct {
	prereqs []string
	recipe  []string
}

type makefileRules struct {
	vars  map[string]string
	rules map[string]*makeRule
}

type tierWalk struct {
	root    string
	make    makefileRules
	runners map[string][]string
}

var (
	makeVarLine   = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*(?:::=|:=|\?=|\+=|=)\s*(.*)$`)
	makeRuleLine  = regexp.MustCompile(`^([^\s:=#][^:=]*?)\s*:(.*)$`)
	makeRef       = regexp.MustCompile(`\$\$|\$[({]([A-Za-z_][A-Za-z0-9_]*)[)}]`)
	shellVar      = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:?-([^}]*))?\}|\$([A-Za-z_][A-Za-z0-9_]*)`)
	shellAssign   = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
	heredocStart  = regexp.MustCompile(`(?:^|[^<])<<-?\s*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)
	scriptPath    = regexp.MustCompile(`^(?:\./)?(scripts/[A-Za-z0-9_./-]+\.sh)$`)
	launchers     = []string{"bash", "sh", "source", ".", "exec", "time", "!", "if", "then", "do", "else"}
	declareWords  = []string{"export", "local", "readonly", "declare"}
	errNoMakeRule = errors.New("no such target in the Makefile")
)

// workflowTierRunners maps each tag some workflow step passes to `go test` to the steps
// that pass it, as "<workflow> <job> -> make <target> -> scripts/<x>.sh".
func workflowTierRunners(root string) (map[string][]string, error) {
	mk, err := readMakefile(filepath.Join(root, "Makefile"))
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, pattern := range []string{"*.yml", "*.yaml"} {
		matches, err := filepath.Glob(filepath.Join(root, ".github", "workflows", pattern))
		if err != nil {
			return nil, err
		}
		paths = append(paths, matches...)
	}
	w := &tierWalk{root: root, make: mk, runners: map[string][]string{}}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var wf workflowSpec
		if err := yaml.Unmarshal(data, &wf); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		for _, jobID := range slices.Sorted(maps.Keys(wf.Jobs)) {
			job := wf.Jobs[jobID]
			for _, step := range job.Steps {
				if step.Run == "" {
					continue
				}
				env := stringEnv(wf.Env, job.Env, step.Env)
				if err := w.body(step.Run, env, filepath.Base(path)+" "+jobID, true); err != nil {
					return nil, err
				}
			}
		}
	}
	for tag, via := range w.runners {
		slices.Sort(via)
		w.runners[tag] = slices.Compact(via)
	}
	return w.runners, nil
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
	w.record(goFlagsTags(vars["GOFLAGS"]), via)
	for _, line := range shellLines(text) {
		for _, words := range shellCommands(line) {
			rest, cmdVars := w.assignments(words, vars, via)
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
func (w *tierWalk) assignments(words []string, vars map[string]string, via string) ([]string, map[string]string) {
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
		if m[1] == "GOFLAGS" {
			w.record(goFlagsTags(cmdVars[m[1]]), via)
		}
	}
	if declared || n == len(words) {
		maps.Copy(vars, cmdVars)
		return nil, vars
	}
	return words[n:], cmdVars
}

func (w *tierWalk) command(words []string, vars map[string]string, via string, follow bool) error {
	for i := range words {
		words[i] = expandShell(words[i], vars)
	}
	for i, word := range words {
		launched := i == 0 || slices.Contains(launchers, words[i-1])
		switch {
		case word == "go" && i+1 < len(words) && words[i+1] == "test":
			w.record(goTestTags(words[i+2:]), via)
		case !follow || !launched:
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
		return fmt.Errorf("%s: %w", via, errNoMakeRule)
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

func (w *tierWalk) record(tags []string, via string) {
	for _, tag := range tags {
		w.runners[tag] = append(w.runners[tag], via)
	}
}

// goTestTags reads the tags a `go test` argument list sets. A run limited to `-run '^$'`
// only compiles, so it runs no tier; a value still holding a `$` ("$1", ${{ matrix.x }})
// could not be resolved and names no tag.
func goTestTags(args []string) []string {
	var tags []string
	compileOnly := false
	for i := 0; i < len(args) && args[i] != "-args"; i++ {
		if !strings.HasPrefix(args[i], "-") {
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		if name != "tags" && name != "run" {
			continue
		}
		if !hasValue && i+1 < len(args) {
			i++
			value = args[i]
		}
		if name == "run" {
			compileOnly = value == "^$"
			continue
		}
		for _, tag := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' }) {
			if !strings.Contains(tag, "$") {
				tags = append(tags, tag)
			}
		}
	}
	if compileOnly {
		return nil
	}
	return tags
}

// goFlagsTags reads -tags out of a GOFLAGS value, which applies to every go command the
// step runs, a test driver's bare `go test` included (skills.yml's go-mutesting step).
func goFlagsTags(goflags string) []string {
	var tags []string
	for flag := range strings.FieldsSeq(goflags) {
		if strings.HasPrefix(strings.TrimLeft(flag, "-"), "tags=") {
			tags = append(tags, goTestTags([]string{flag})...)
		}
	}
	return tags
}

func expandShell(s string, vars map[string]string) string {
	return shellVar.ReplaceAllStringFunc(s, func(ref string) string {
		m := shellVar.FindStringSubmatch(ref)
		name := m[1] + m[4]
		value, set := vars[name]
		if m[2] == "" {
			if !set {
				return ref
			}
			return value
		}
		if !set || (strings.HasPrefix(m[2], ":") && value == "") {
			return expandShell(m[3], vars)
		}
		return value
	})
}

// shellLines joins continuation lines and drops comments and heredoc bodies, which are
// another language's text (scripts embed Python that way).
func shellLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\\\n", " ")
	var lines []string
	heredoc := ""
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if heredoc != "" {
			if trimmed == heredoc {
				heredoc = ""
			}
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if m := heredocStart.FindStringSubmatch(line); m != nil {
			heredoc = m[1]
		}
		lines = append(lines, line)
	}
	return lines
}

// shellCommands splits a line into commands at unquoted ; & | ( ), each as words with
// quotes removed, and stops at an unquoted comment. `$(...)` therefore becomes its own
// command, which is what it is.
func shellCommands(line string) [][]string {
	var commands [][]string
	var words []string
	var word strings.Builder
	inWord := false
	endWord := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	endCommand := func() {
		endWord()
		if len(words) > 0 {
			commands = append(commands, words)
			words = nil
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\'' || c == '"':
			end := strings.IndexByte(line[i+1:], c)
			if end < 0 {
				end = len(line) - i - 1
			}
			word.WriteString(line[i+1 : i+1+end])
			inWord = true
			i += end + 1
		case c == '\\' && i+1 < len(line):
			word.WriteByte(line[i+1])
			inWord = true
			i++
		case c == '#' && !inWord:
			endCommand()
			return commands
		case c == ' ' || c == '\t':
			endWord()
		case strings.IndexByte(";&|()", c) >= 0:
			endCommand()
		default:
			word.WriteByte(c)
			inWord = true
		}
	}
	endCommand()
	return commands
}

// readMakefile keeps what the walk needs: variable definitions and, per target, its
// prerequisites and recipe. A missing Makefile has no targets.
func readMakefile(path string) (makefileRules, error) {
	mk := makefileRules{vars: map[string]string{}, rules: map[string]*makeRule{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return mk, nil
	}
	if err != nil {
		return mk, err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\\\n", " ")
	var current []*makeRule
	inDefine := false
	for line := range strings.SplitSeq(text, "\n") {
		switch {
		case inDefine:
			inDefine = strings.TrimSpace(line) != "endef"
		case strings.HasPrefix(line, "\t"):
			for _, rule := range current {
				rule.recipe = append(rule.recipe, strings.TrimLeft(line[1:], "@-+ \t"))
			}
		case strings.TrimSpace(line) == "":
		case strings.HasPrefix(line, "define "):
			inDefine, current = true, nil
		default:
			current = mk.parseLine(line)
		}
	}
	return mk, nil
}

// parseLine records a variable or a rule line and returns the rules a following recipe
// belongs to. `target: NAME = value` is a target-specific variable, not a rule.
func (mk makefileRules) parseLine(line string) []*makeRule {
	if m := makeVarLine.FindStringSubmatch(line); m != nil {
		mk.vars[m[1]] = m[2]
		return nil
	}
	m := makeRuleLine.FindStringSubmatch(line)
	if m == nil || strings.Contains(m[2], "=") {
		return nil
	}
	prereqs, _, _ := strings.Cut(m[2], "#")
	var rules []*makeRule
	for target := range strings.FieldsSeq(m[1]) {
		if mk.rules[target] == nil {
			mk.rules[target] = &makeRule{}
		}
		mk.rules[target].prereqs = append(mk.rules[target].prereqs, strings.Fields(prereqs)...)
		rules = append(rules, mk.rules[target])
	}
	return rules
}

// expand resolves $(NAME) and ${NAME} from the Makefile, $(MAKE) to make, and $$ to the
// shell's $. A name refers to itself at most once (GO_PACKAGES does, through $(eval)).
func (mk makefileRules) expand(s string, expanding ...string) string {
	return makeRef.ReplaceAllStringFunc(s, func(ref string) string {
		if ref == "$$" {
			return "$"
		}
		name := ref[2 : len(ref)-1]
		if name == "MAKE" {
			return "make"
		}
		value, ok := mk.vars[name]
		if !ok || slices.Contains(expanding, name) {
			return ref
		}
		return mk.expand(value, append(expanding, name)...)
	})
}
