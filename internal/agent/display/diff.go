package display

import (
	"encoding/json"
	"path"
	"regexp"
	"strconv"
	"strings"
)

const (
	maxFileDisplayBytes = 64 << 10
	maxDiffLines        = 1000
)

var unifiedHunk = regexp.MustCompile(`^@@ -(\d+),(\d+) \+(\d+),(\d+) @@$`)

// Diff is a validated single-file unified patch result.
type Diff struct {
	Filename  string     `json:"filename"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Lines     []DiffLine `json:"lines"`
}

// DiffLine is one context, added, or removed line in a patch result.
type DiffLine struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

func validDisplayPath(given, actual string) bool {
	if strings.TrimSpace(given) == "" || len(given) > 1024 || strings.ContainsAny(given, "\r\n") || strings.TrimSpace(actual) == "" || len(actual) > 1024 {
		return false
	}
	if path.Clean(given) == path.Clean(actual) {
		return true
	}
	return !path.IsAbs(given) && path.Clean(path.Join("/workspace", given)) == path.Clean(actual)
}

func decodePatchDiff(in PreviewInput) (Diff, bool) {
	if len(in.Arguments) > maxFileDisplayBytes || len(in.ResultPreview) > maxFileDisplayBytes {
		return Diff{}, false
	}
	var args struct {
		Path      string `json:"path"`
		Mode      string `json:"mode"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
		Patch     string `json:"patch"`
	}
	if json.Unmarshal([]byte(in.Arguments), &args) != nil {
		return Diff{}, false
	}
	switch args.Mode {
	case "", "replace":
		if args.OldString == "" || args.OldString == args.NewString {
			return Diff{}, false
		}
	case "patch":
		if strings.TrimSpace(args.Patch) == "" {
			return Diff{}, false
		}
	default:
		return Diff{}, false
	}
	rows := strings.Split(strings.TrimSuffix(in.ResultPreview, "\n"), "\n")
	if len(rows) < 4 || len(rows) > maxDiffLines+3 || !strings.HasPrefix(rows[0], "--- a/") || !strings.HasPrefix(rows[1], "+++ b/") {
		return Diff{}, false
	}
	oldPath := strings.TrimPrefix(rows[0], "--- a/")
	newPath := strings.TrimPrefix(rows[1], "+++ b/")
	if oldPath != newPath || !validDisplayPath(args.Path, oldPath) {
		return Diff{}, false
	}
	m := unifiedHunk.FindStringSubmatch(rows[2])
	if m == nil {
		return Diff{}, false
	}
	oldExpected, errOld := strconv.Atoi(m[2])
	newExpected, errNew := strconv.Atoi(m[4])
	if errOld != nil || errNew != nil || oldExpected > maxDiffLines || newExpected > maxDiffLines {
		return Diff{}, false
	}
	result := Diff{Filename: args.Path, Lines: make([]DiffLine, 0, len(rows)-3)}
	oldSeen, newSeen := 0, 0
	for _, row := range rows[3:] {
		if row == "" {
			return Diff{}, false
		}
		switch row[0] {
		case ' ':
			result.Lines = append(result.Lines, DiffLine{Kind: "context", Text: row[1:]})
			oldSeen++
			newSeen++
		case '-':
			result.Lines = append(result.Lines, DiffLine{Kind: "removed", Text: row[1:]})
			result.Deletions++
			oldSeen++
		case '+':
			result.Lines = append(result.Lines, DiffLine{Kind: "added", Text: row[1:]})
			result.Additions++
			newSeen++
		default:
			return Diff{}, false
		}
		if oldSeen > oldExpected || newSeen > newExpected {
			return Diff{}, false
		}
	}
	if oldSeen != oldExpected || newSeen != newExpected || result.Additions+result.Deletions == 0 {
		return Diff{}, false
	}
	return result, true
}
