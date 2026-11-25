package main

import (
	"regexp"
	"strings"
)

// The Python layer under tier_runners_resolve_test.go: the `go test` argv lists a driver
// spells as list literals.

const unresolvedPython = "$unresolved"

var pythonGoTestList = regexp.MustCompile(`^\[\s*["']go["']\s*,\s*["']test["']\s*,`)

// pythonGoTestLists reads every Python list literal that starts "go", "test" — the argv a
// driver hands to subprocess. A list inside a # comment or inside any string literal (a
// docstring included) is not one. In a list it accepts exactly: elements that are single-
// or double-quoted one-line string literals, optionally r- or R-prefixed (escapes \\, \'
// and \" in a plain string), separated by commas, across any number of lines, with an
// optional trailing comma and # comments between elements. Any other element — a name, an
// f-string, a call, a concatenation, a triple-quoted string, another escape — becomes
// unresolvedPython. It reads text, not control flow: a list under `if False:` counts.
func pythonGoTestLists(src string) [][]string {
	var lists [][]string
	for i := 0; i < len(src); {
		switch c := src[i]; {
		case c == '#':
			i = skipPythonComment(src, i)
		case c == '"' || c == '\'':
			i = skipPythonString(src, i)
		case c == '[' && pythonGoTestList.MatchString(src[i:]):
			var list []string
			list, i = pythonListElements(src, i+1)
			lists = append(lists, list)
		default:
			i++
		}
	}
	return lists
}

// pythonListElements reads the elements from i to the list's closing bracket and returns
// them with the index after it.
func pythonListElements(src string, i int) ([]string, int) {
	var elements []string
	i = skipPythonSpace(src, i)
	for i < len(src) && src[i] != ']' {
		value, end, ok := pythonString(src, i)
		next := skipPythonSpace(src, end)
		if !ok || next >= len(src) || (src[next] != ',' && src[next] != ']') {
			value, next = unresolvedPython, skipPythonElement(src, i)
		}
		elements = append(elements, value)
		if next < len(src) && src[next] == ',' {
			next++
		}
		i = skipPythonSpace(src, next)
	}
	return elements, min(i+1, len(src))
}

func skipPythonSpace(src string, i int) int {
	for i < len(src) {
		switch {
		case strings.IndexByte(" \t\r\n", src[i]) >= 0:
			i++
		case src[i] == '#':
			i = skipPythonComment(src, i)
		default:
			return i
		}
	}
	return i
}

func skipPythonComment(src string, i int) int {
	if end := strings.IndexByte(src[i:], '\n'); end >= 0 {
		return i + end
	}
	return len(src)
}

// skipPythonString returns the index after the string literal whose opening quote is at
// i, triple-quoted or not. A backslash always keeps the next character inside the string,
// raw strings included; an unterminated one-line string ends at the newline.
func skipPythonString(src string, i int) int {
	quote := src[i]
	if triple := strings.Repeat(string(quote), 3); strings.HasPrefix(src[i:], triple) {
		for j := i + 3; j < len(src); j++ {
			if src[j] == '\\' {
				j++
			} else if strings.HasPrefix(src[j:], triple) {
				return j + 3
			}
		}
		return len(src)
	}
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case quote:
			return j + 1
		case '\n':
			return j
		}
	}
	return len(src)
}

// pythonString reads the string literal at i and returns its value and the index after it.
func pythonString(src string, i int) (string, int, bool) {
	raw := i < len(src) && (src[i] == 'r' || src[i] == 'R')
	if raw {
		i++
	}
	if i >= len(src) || (src[i] != '"' && src[i] != '\'') {
		return "", i, false
	}
	quote := src[i]
	if strings.HasPrefix(src[i:], strings.Repeat(string(quote), 3)) {
		return "", i, false
	}
	var value strings.Builder
	for j := i + 1; j < len(src); j++ {
		switch c := src[j]; {
		case c == quote:
			return value.String(), j + 1, true
		case c == '\n':
			return "", i, false
		case c == '\\' && j+1 < len(src):
			if !raw && strings.IndexByte(`\'"`, src[j+1]) < 0 {
				return "", i, false
			}
			if raw {
				value.WriteByte(c)
			}
			value.WriteByte(src[j+1])
			j++
		default:
			value.WriteByte(c)
		}
	}
	return "", i, false
}

// skipPythonElement returns the index of the comma or bracket that ends the element at i,
// stepping over nested brackets and string literals.
func skipPythonElement(src string, i int) int {
	depth := 0
	for i < len(src) {
		switch c := src[i]; {
		case c == '"' || c == '\'':
			i = skipPythonString(src, i)
			continue
		case strings.IndexByte("([{", c) >= 0:
			depth++
		case (c == ',' || c == ']') && depth == 0:
			return i
		case strings.IndexByte(")]}", c) >= 0:
			depth--
		}
		i++
	}
	return i
}
