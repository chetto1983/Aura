package approvaltext

import (
	"encoding/json"
	"slices"
	"strings"
)

// maxCardLineBytes bounds one line of a line card, which the plugin backend caps at 3500 bytes
// in all (internal/plugins card.go) and the task cancel card well under that.
const maxCardLineBytes = 4096

// cardLine is one line of a line card after its header: the param it derives into, the English
// prefix the card writes, and the Italian one Telegram renders.
type cardLine struct {
	param, prefix, italian string
}

// lineCard is an approval question written as a header, one `prefix: value` line per param and
// a closing `sha256: <digest>` line.
type lineCard struct {
	key, header, italianHeader string
	lines                      []cardLine
}

// pluginCards mirror plugins.Review.Question and plugins.RemovalQuestion line by line. A
// question that is not exactly one of them — a forged line, a card the backend cut short —
// derives nothing and renders verbatim.
var pluginCards = []lineCard{
	{
		key: PluginInstallKey, header: "Approve plugin install?", italianHeader: "Approva l'installazione del plugin?",
		lines: []cardLine{
			{"plugin", "plugin: ", "plugin: "},
			{"source", "source: ", "origine: "},
			{"skills", "skills ", "competenze "}, //nolint:misspell // "competenze" is the Italian plural of "competenza", not English "competence".
			{"connectors", "connectors ", "connettori "},
			{"skipped", "skipped ", "saltati "},
			{"mcp", "mcp: ", "mcp: "},
			{"notes", "notes ", "note "},
		},
	},
	{
		key: PluginRemoveKey, header: "Approve plugin removal?", italianHeader: "Approva la rimozione del plugin?",
		lines: []cardLine{
			{"plugin", "plugin: ", "plugin: "},
			{"source", "source: ", "origine: "},
			{"skills", "skills ", "competenze "}, //nolint:misspell // "competenze" is the Italian plural of "competenza", not English "competence".
			{"connectors", "connectors ", "connettori "},
			{"data", "plugin data: ", "dati del plugin: "},
		},
	},
}

// taskCancelCards mirror the task tool's cancel approval (tools.taskCancelQuestion).
var taskCancelCards = []lineCard{{
	key:           TaskCancelKey,
	header:        "Approve cancelling a task not scheduled in this chat?",
	italianHeader: "Approva l'annullamento di un'attività non pianificata in questa chat?",
	lines: []cardLine{
		{"task", "task: ", "attività: "},
		{"kind", "kind: ", "tipo: "},
		{"schedule", "schedule: ", "pianificazione: "},
		{"next", "next run: ", "prossima esecuzione: "},
		{"payload", "payload: ", "contenuto: "},
	},
}}

func cardFor(key string) (lineCard, bool) {
	for _, card := range slices.Concat(pluginCards, taskCancelCards) {
		if card.key == key {
			return card, true
		}
	}
	return lineCard{}, false
}

func deriveCard(question string, fields map[string]json.RawMessage, cards []lineCard) (Presentation, bool) {
	digest := RootString(fields, "approval_sha256")
	if digest == "" {
		return Presentation{}, false
	}
	got := strings.Split(question, "\n")
	for _, card := range cards {
		if len(got) != len(card.lines)+2 || got[0] != card.header || got[len(got)-1] != "sha256: "+digest {
			continue
		}
		params := map[string]string{"digest": digest}
		for i, line := range card.lines {
			value, ok := strings.CutPrefix(got[i+1], line.prefix)
			if !ok {
				return Presentation{}, false
			}
			params[line.param] = value
		}
		p := Presentation{Key: card.key, Params: params}
		return p, valid(p)
	}
	return Presentation{}, false
}

func cardLimits(key string) map[string]int {
	card, ok := cardFor(key)
	if !ok {
		return nil
	}
	limits := map[string]int{"digest": maxShortParamBytes}
	for _, line := range card.lines {
		limits[line.param] = maxCardLineBytes
	}
	return limits
}

func cardRequired(key string) []string {
	card, _ := cardFor(key)
	required := []string{"digest"}
	for _, line := range card.lines {
		required = append(required, line.param)
	}
	return required
}

func renderCardItalian(p Presentation) string {
	card, _ := cardFor(p.Key)
	lines := []string{card.italianHeader}
	for _, line := range card.lines {
		lines = append(lines, line.italian+italianCardValue(line.param, p.Params[line.param]))
	}
	return strings.Join(append(lines, "sha256: "+p.Params["digest"]), "\n")
}

// italianCardValue translates a card's whole-value words: an empty list, the mcp state, what
// happens to the plugin's data and a task with no next run. Words inside a list stay as the
// card wrote them, beside the plugin's own quoted strings.
func italianCardValue(param, value string) string {
	switch param {
	case "mcp":
		if value == "enabled" {
			return "attivo"
		}
		if reason, ok := strings.CutPrefix(value, "disabled — "); ok {
			return "disattivato — " + reason
		}
	case "data":
		switch value {
		case "kept":
			return "conservati"
		case "deleted in your box":
			return "eliminati nel tuo box"
		}
	case "next":
		if value == "none" {
			return "nessuna"
		}
	default:
		if count, ok := strings.CutSuffix(value, "): none"); ok {
			return count + "): nessuno"
		}
	}
	return value
}
