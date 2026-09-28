package display

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// These three built-ins own their result text, but only their list modes have a
// stable row grammar. Parse that grammar strictly; actions and ambiguous text
// keep the escaped raw result instead of gaining table controls.
func normalizeNativeList(in PreviewInput) (Payload, bool) {
	if len(in.ResultPreview) > maxMemoryPreviewBytes {
		return Payload{}, false
	}
	var args struct {
		Action string `json:"action"`
	}
	if json.Unmarshal([]byte(in.Arguments), &args) != nil || args.Action != "list" {
		return Payload{}, false
	}
	var title string
	var columns []string
	var rows [][]string
	var omitted int
	var ok bool
	switch in.ToolName {
	case "task":
		title, columns = "native_tasks", []string{"Task", "Kind", "Schedule", "Next", "State"}
		rows, ok = parseTaskList(in.ResultPreview)
	case "skill":
		title, columns = "native_skills", []string{"Skill", "Description"}
		rows, omitted, ok = parseSkillList(in.ResultPreview)
	case "plugin_pack":
		title, columns = "native_packs", []string{"Pack", "Version", "Skills", "Connectors", "Commands", "Source"}
		rows, ok = parsePackList(in.ResultPreview)
	default:
		return Payload{}, false
	}
	if !ok {
		return Payload{}, false
	}
	visible := min(len(rows), maxMemoryRows)
	return Payload{Type: KindTable, ToolCallID: in.ToolCallID, Title: title, Table: &Table{
		Columns: columns, Rows: rows[:visible], OmittedRows: omitted + len(rows) - visible,
	}}, true
}

func parseTaskList(preview string) ([][]string, bool) {
	if preview == "no scheduled tasks" {
		return [][]string{}, true
	}
	lines := strings.Split(preview, "\n")
	if len(lines) < 2 || !strings.HasSuffix(lines[0], " task(s):") {
		return nil, false
	}
	count, err := strconv.Atoi(strings.TrimSuffix(lines[0], " task(s):"))
	if err != nil || count <= 0 || count != len(lines)-1 {
		return nil, false
	}
	rows := make([][]string, 0, count)
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, "  ") {
			return nil, false
		}
		fields := strings.Fields(line)
		if len(fields) < 4 || len(fields[0]) > 128 || !strings.HasPrefix(fields[1], "kind=") || !strings.HasPrefix(fields[3], "next=") {
			return nil, false
		}
		kind := strings.TrimPrefix(fields[1], "kind=")
		if kind != "reminder" && kind != "agent_job" && kind != "backup_postgres" {
			return nil, false
		}
		if fields[2] != "at" && fields[2] != "every" && fields[2] != "cron" {
			return nil, false
		}
		next := strings.TrimPrefix(fields[3], "next=")
		if next != "—" {
			if _, err := time.Parse(time.RFC3339, next); err != nil {
				return nil, false
			}
		}
		state := "active"
		statusText, _, _ := strings.Cut(line, "  payload=")
		statusText, _, _ = strings.Cut(statusText, "  notify=")
		if strings.Contains(statusText, " [awaiting approval]") {
			state = "awaiting approval"
		} else if strings.Contains(statusText, " [unschedulable]") {
			state = "unschedulable"
		}
		rows = append(rows, boundedNativeRow(fields[0], kind, fields[2], next, state))
	}
	return rows, true
}

func parseSkillList(preview string) ([][]string, int, bool) {
	const tail = "\n\nNOTE: this listed INSTALLED skills only."
	body, _, found := strings.Cut(preview, tail)
	if !found {
		return nil, 0, false
	}
	body = strings.TrimSpace(body)
	if body == "No skills available." {
		return [][]string{}, 0, true
	}
	rows := make([][]string, 0)
	omitted := 0
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasSuffix(line, " more — search with skill action=list {query}") {
			if omitted != 0 {
				return nil, 0, false
			}
			var err error
			omitted, err = strconv.Atoi(strings.TrimSuffix(line, " more — search with skill action=list {query}"))
			if err != nil || omitted <= 0 {
				return nil, 0, false
			}
			continue
		}
		if !strings.HasPrefix(line, "- ") || omitted > 0 {
			return nil, 0, false
		}
		name, description, found := strings.Cut(strings.TrimPrefix(line, "- "), ": ")
		if !found || len(name) == 0 || len(name) > 64 || description == "" {
			return nil, 0, false
		}
		for _, c := range name {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return nil, 0, false
			}
		}
		rows = append(rows, boundedNativeRow(name, description))
	}
	return rows, omitted, len(rows) > 0
}

func parsePackList(preview string) ([][]string, bool) {
	lines := strings.Split(strings.TrimSpace(preview), "\n")
	if len(lines) == 0 || !strings.HasSuffix(lines[0], " pack(s)") {
		return nil, false
	}
	count, err := strconv.Atoi(strings.TrimSuffix(lines[0], " pack(s)"))
	if err != nil || count < 0 || count != len(lines)-1 {
		return nil, false
	}
	rows := make([][]string, 0, count)
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, "  ") {
			return nil, false
		}
		fields := strings.Fields(line)
		if len(fields) != 9 || fields[3] != "skills" || fields[5] != "connectors" || fields[7] != "commands" {
			return nil, false
		}
		for _, cell := range []string{fields[2], fields[4], fields[6]} {
			if value, err := strconv.Atoi(cell); err != nil || value < 0 {
				return nil, false
			}
		}
		if len(fields[0]) > 64 || len(fields[8]) > 256 || !strings.Contains(fields[8], "/") {
			return nil, false
		}
		rows = append(rows, boundedNativeRow(fields[0], fields[1], fields[2], fields[4], fields[6], fields[8]))
	}
	return rows, true
}

func boundedNativeRow(cells ...string) []string {
	row := make([]string, 0, len(cells))
	for _, cell := range cells {
		row = append(row, memoryCell(cell))
	}
	return row
}
