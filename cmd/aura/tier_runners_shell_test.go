package main

import (
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
)

// The text layer under tier_runners_resolve_test.go: shell words and variables, and the
// slice of Makefile syntax the walk needs. It reads text, not control flow.

type makeRule struct {
	prereqs []string
	recipe  []string
}

type makefileRules struct {
	vars  map[string]string
	rules map[string]*makeRule
}

var (
	makeVarLine  = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*(::=|:=|\?=|\+=|=)\s*(.*)$`)
	makeRuleLine = regexp.MustCompile(`^([^\s:=#][^:=]*?)\s*:(.*)$`)
	makeRef      = regexp.MustCompile(`\$\$|\$[({]([A-Za-z_][A-Za-z0-9_]*)[)}]`)
	shellVar     = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:?-([^}]*))?\}|\$([A-Za-z_][A-Za-z0-9_]*)`)
	shellAssign  = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
	heredocStart = regexp.MustCompile(`(?:^|[^<])<<-?\s*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)
)

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
		if prior, ok := mk.vars[m[1]]; ok && m[2] == "+=" {
			mk.vars[m[1]] = prior + " " + m[3]
		} else {
			mk.vars[m[1]] = m[3]
		}
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
