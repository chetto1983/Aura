package source

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
)

const goExtractorVersion = "go_extract_v1"

func ExtractGo(ctx context.Context, in ExtractInput) (ExtractResult, error) {
	select {
	case <-ctx.Done():
		return ExtractResult{}, ctx.Err()
	default:
	}
	if in.Source == nil {
		return ExtractResult{}, fmt.Errorf("source: nil source")
	}
	switch in.Source.Kind {
	case KindText, KindMarkdown:
		return textExtract(in), nil
	case KindJSON:
		return jsonExtract(in)
	case KindCSV:
		return csvExtract(in)
	default:
		return ExtractResult{}, fmt.Errorf("source: no Go extractor for kind %s", in.Source.Kind)
	}
}

func textExtract(in ExtractInput) ExtractResult {
	body := strings.TrimSpace(string(in.Bytes))
	return ExtractResult{
		Markdown: body + "\n",
		Metadata: ExtractionMeta{
			ExtractorName:    "go_text",
			ExtractorVersion: goExtractorVersion,
			TextBytes:        len([]byte(body)),
		},
	}
}

func jsonExtract(in ExtractInput) (ExtractResult, error) {
	var v any
	if err := json.Unmarshal(in.Bytes, &v); err != nil {
		return ExtractResult{}, fmt.Errorf("source: parse json: %w", err)
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ExtractResult{}, fmt.Errorf("source: render json: %w", err)
	}
	md := "```json\n" + string(pretty) + "\n```\n"
	return ExtractResult{
		Markdown: md,
		Metadata: ExtractionMeta{
			ExtractorName:    "go_json",
			ExtractorVersion: goExtractorVersion,
			TextBytes:        len([]byte(md)),
		},
	}, nil
}

func csvExtract(in ExtractInput) (ExtractResult, error) {
	r := csv.NewReader(bytes.NewReader(in.Bytes))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return ExtractResult{}, fmt.Errorf("source: parse csv: %w", err)
	}
	if len(rows) == 0 {
		return ExtractResult{}, fmt.Errorf("source: empty csv")
	}
	md := renderMarkdownTable(rows, 200)
	return ExtractResult{
		Markdown: md,
		Metadata: ExtractionMeta{
			ExtractorName:    "go_csv",
			ExtractorVersion: goExtractorVersion,
			TextBytes:        len([]byte(md)),
			RowCount:         len(rows),
		},
	}, nil
}

func renderMarkdownTable(rows [][]string, maxRows int) string {
	if len(rows) == 0 {
		return ""
	}
	cols := len(rows[0])
	var sb strings.Builder
	writeRow := func(row []string) {
		sb.WriteString("|")
		for i := 0; i < cols; i++ {
			cell := ""
			if i < len(row) {
				cell = strings.ReplaceAll(strings.TrimSpace(row[i]), "|", "\\|")
			}
			sb.WriteString(" " + cell + " |")
		}
		sb.WriteString("\n")
	}
	writeRow(rows[0])
	sb.WriteString("|")
	for i := 0; i < cols; i++ {
		sb.WriteString(" --- |")
	}
	sb.WriteString("\n")
	limit := len(rows)
	if limit > maxRows {
		limit = maxRows
	}
	for _, row := range rows[1:limit] {
		writeRow(row)
	}
	if len(rows) > maxRows {
		sb.WriteString("\n_Extraction truncated after 200 rows._\n")
	}
	return sb.String()
}
