package display

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

const walkTruncatedNotice = "[walk truncated: hit the node/time budget — results are partial, not exhaustive; narrow `path` or `glob`]"

var searchMatchLine = regexp.MustCompile(`^(.+):([1-9][0-9]*): (.*)$`)
var searchContextLine = regexp.MustCompile(`^(.+)-([1-9][0-9]*)- (.*)$`)
var searchCountLine = regexp.MustCompile(`^(.+): ([1-9][0-9]*)$`)
var searchAmbiguousMatch = regexp.MustCompile(`:[1-9][0-9]*: `)
var searchAmbiguousContext = regexp.MustCompile(`-[1-9][0-9]*- `)
var searchAmbiguousCount = regexp.MustCompile(`: [1-9][0-9]*`)

func decodeSearchFilesPreview(in PreviewInput) (Table, bool) {
	if len(in.Arguments) > maxFileDisplayBytes || len(in.ResultPreview) > maxFileDisplayBytes {
		return Table{}, false
	}
	var args struct {
		Pattern    string `json:"pattern"`
		Target     string `json:"target"`
		OutputMode string `json:"output_mode"`
	}
	if json.Unmarshal([]byte(in.Arguments), &args) != nil || strings.TrimSpace(args.Pattern) == "" {
		return Table{}, false
	}
	if args.Target == "" {
		args.Target = "content"
	}
	if args.Target != "content" && args.Target != "files" {
		return Table{}, false
	}
	if args.OutputMode == "" {
		args.OutputMode = "content"
	}
	if args.OutputMode != "content" && args.OutputMode != "files_only" && args.OutputMode != "count" {
		return Table{}, false
	}
	mode := args.OutputMode
	if args.Target == "files" {
		mode = "files_only"
	}
	result := in.ResultPreview
	var notice string
	if trimmed, ok := strings.CutSuffix(result, "\n"+walkTruncatedNotice); ok {
		result = trimmed
		notice = walkTruncatedNotice
	}
	table := Table{Notice: notice, Rows: make([][]string, 0)}
	switch mode {
	case "content":
		table.Columns = []string{"File", "Line", "Kind", "Text"}
	case "files_only":
		table.Columns = []string{"File"}
	case "count":
		table.Columns = []string{"File", "Count"}
	}
	if result == "[no matches]" {
		return table, true
	}
	if result == "" {
		return Table{}, false
	}
	lines := strings.Split(result, "\n")
	if len(lines) > 500 {
		return Table{}, false
	}
	for _, line := range lines {
		if line == "" || len(line) > 4096 {
			return Table{}, false
		}
		var row []string
		switch mode {
		case "files_only":
			if !validSearchPath(line) {
				return Table{}, false
			}
			row = []string{line}
		case "count":
			m := searchCountLine.FindStringSubmatch(line)
			if m == nil || !validSearchPath(m[1]) || searchAmbiguousCount.MatchString(m[1]) {
				return Table{}, false
			}
			if _, err := strconv.ParseUint(m[2], 10, 32); err != nil {
				return Table{}, false
			}
			row = []string{m[1], m[2]}
		case "content":
			m, kind := searchMatchLine.FindStringSubmatch(line), "match"
			if m == nil {
				m, kind = searchContextLine.FindStringSubmatch(line), "context"
			}
			if m == nil || !validSearchPath(m[1]) || searchAmbiguousMatch.MatchString(m[1]) || searchAmbiguousContext.MatchString(m[1]) {
				return Table{}, false
			}
			if _, err := strconv.ParseUint(m[2], 10, 32); err != nil {
				return Table{}, false
			}
			row = []string{m[1], m[2], kind, m[3]}
		}
		table.Rows = append(table.Rows, row)
	}
	return table, true
}

func validSearchPath(path string) bool {
	return strings.TrimSpace(path) != "" && len(path) <= 2048 && !strings.ContainsAny(path, "\r\n") && path != walkTruncatedNotice
}
