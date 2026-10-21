package documents

import (
	"encoding/json"
	"path"
)

func previewRecipe(kind, dir, source string) (string, map[string][]byte) {
	files := map[string][]byte{}
	if kind == "photo" {
		requests := []any{
			map[string]any{"id": 1, "method": "doc.open", "params": map[string]any{"path": source}},
			map[string]any{"id": 2, "method": "doc.render", "params": map[string]any{"maxSide": previewEdge, "path": path.Join(dir, "preview.png")}},
		}
		files["photo.jsonl"] = previewJSONLines(requests)
		return "photocraft-cli serve --automation-read-root " + dir + " --automation-write-root " + dir + " < " + path.Join(dir, "photo.jsonl"), files
	}
	pdf := source
	conversion := ""
	if kind != "pdf" {
		pdf = path.Join(dir, "stage.pdf")
		switch kind {
		case "word":
			conversion = "wordcraft-cli convert " + source + " " + pdf
		case "grid":
			files["grid.jsonl"] = previewJSONLines([]any{
				map[string]any{"command": "sheet.activate", "params": map[string]any{"sheet": 0}},
				map[string]any{"command": "file.exportPdf", "params": map[string]any{"path": pdf, "sheets": "active", "fitToPage": true}},
			})
			conversion = "gridcraft-cli run --in " + source + " --script " + path.Join(dir, "grid.jsonl") + " --quiet"
		case "deck":
			conversion = "deckcraft-cli run --in " + source + " --export " + pdf + ` --export-params '{"slides":[0],"includeHidden":true}'`
		}
		conversion += " && "
	}
	steps := []any{
		map[string]any{"tool": "doc_open", "args": map[string]any{"path": pdf}},
		map[string]any{"tool": "render_preview", "args": map[string]any{"page": 1, "max_side": previewEdge}, "out": path.Join(dir, "preview.png")},
	}
	files["pdf.json"], _ = json.Marshal(steps)
	return conversion + "pdfcraft-cli run --script " + path.Join(dir, "pdf.json") + " --root " + dir, files
}

func previewJSONLines(values []any) []byte {
	var body []byte
	for _, value := range values {
		line, _ := json.Marshal(value)
		body = append(body, line...)
		body = append(body, '\n')
	}
	return body
}
