package display

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

const extractedPrefix = "[extracted from a document format — this is rendered text, not the raw file bytes]\n"
const writeReceiptSuffix = "\nverified:true — the on-disk content hash was confirmed; do not re-read the file to check the write landed"

var readPageHint = regexp.MustCompile(`^\[Truncated: showing lines (\d+)-(\d+) of (\d+)\. Use offset=(\d+) to continue reading\.\]$`)
var readBudgetHint = regexp.MustCompile(`^\[Output truncated at the 100000-char read budget after (\d+) line\(s\) \(showing lines (\d+)-(\d+) of (\d+)\)\. Use offset=(\d+) to continue\.\]$`)
var writeReceipt = regexp.MustCompile(`^wrote (\d+) bytes to (.+)$`)

func decodeReadFilePreview(in PreviewInput) (CodeInput, bool) {
	if len(in.Arguments) > maxFileDisplayBytes || len(in.ResultPreview) > maxFileDisplayBytes {
		return CodeInput{}, false
	}
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
	}
	if json.Unmarshal([]byte(in.Arguments), &args) != nil || !validDisplayPath(args.Path, args.Path) {
		return CodeInput{}, false
	}
	if args.Offset < 1 {
		args.Offset = 1
	}
	result := in.ResultPreview
	out := CodeInput{Filename: args.Path, FirstLine: args.Offset}
	if strings.HasPrefix(result, extractedPrefix) {
		out.Extracted = true
		result = strings.TrimPrefix(result, extractedPrefix)
	}
	if result == "[empty file]" {
		out.Body = ""
		return out, true
	}
	if cut := strings.Index(result, "\n\n["); cut >= 0 {
		out.Notice = result[cut+2:]
		result = result[:cut]
	}
	if result == "" {
		return CodeInput{}, false
	}
	lines := strings.Split(result, "\n")
	if len(lines) > maxDiffLines {
		return CodeInput{}, false
	}
	content := make([]string, 0, len(lines))
	for i, line := range lines {
		prefix := strconv.Itoa(args.Offset+i) + "|"
		if !strings.HasPrefix(line, prefix) || len(line) > 2048 {
			return CodeInput{}, false
		}
		content = append(content, strings.TrimPrefix(line, prefix))
	}
	if out.Notice != "" && !validReadNotice(out.Notice, args.Offset, len(lines)) {
		return CodeInput{}, false
	}
	out.Body = strings.Join(content, "\n")
	return out, true
}

func validReadNotice(notice string, offset, lines int) bool {
	if m := readPageHint.FindStringSubmatch(notice); m != nil {
		start, _ := strconv.Atoi(m[1])
		end, _ := strconv.Atoi(m[2])
		total, _ := strconv.Atoi(m[3])
		next, _ := strconv.Atoi(m[4])
		return start == offset && end == offset+lines-1 && total > end && next == end+1
	}
	if m := readBudgetHint.FindStringSubmatch(notice); m != nil {
		kept, _ := strconv.Atoi(m[1])
		start, _ := strconv.Atoi(m[2])
		end, _ := strconv.Atoi(m[3])
		total, _ := strconv.Atoi(m[4])
		next, _ := strconv.Atoi(m[5])
		return kept == lines && start == offset && end == offset+lines-1 && total >= end && next == end+1
	}
	return false
}

func decodeWriteFilePreview(in PreviewInput) (CodeInput, bool) {
	if len(in.Arguments) > maxFileDisplayBytes || len(in.ResultPreview) > maxFileDisplayBytes {
		return CodeInput{}, false
	}
	var args struct {
		Path    string  `json:"path"`
		Content *string `json:"content"`
	}
	if json.Unmarshal([]byte(in.Arguments), &args) != nil || args.Content == nil || !validDisplayPath(args.Path, args.Path) {
		return CodeInput{}, false
	}
	header, ok := strings.CutSuffix(in.ResultPreview, writeReceiptSuffix)
	if !ok {
		return CodeInput{}, false
	}
	m := writeReceipt.FindStringSubmatch(header)
	if m == nil || !validDisplayPath(args.Path, m[2]) {
		return CodeInput{}, false
	}
	bytes, err := strconv.Atoi(m[1])
	if err != nil || bytes != len(*args.Content) {
		return CodeInput{}, false
	}
	return CodeInput{Body: *args.Content, Filename: args.Path, FirstLine: 1}, true
}
