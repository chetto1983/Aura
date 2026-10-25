package approvaltext

import (
	"encoding/json"
	"strings"
)

// maxPluginLineBytes bounds one line of a plugin card, which the backend caps at 3500 bytes
// in all (internal/plugins card.go).
const maxPluginLineBytes = 4096

// pluginLine is one line of a plugin approval card after its header: the param it derives
// into, the English prefix the card writes, and the Italian one Telegram renders.
type pluginLine struct {
	param, prefix, italian string
}

type pluginCard struct {
	key, header, italianHeader string
	lines                      []pluginLine
}

// pluginCards mirror plugins.Review.Question and plugins.RemovalQuestion line by line. A
// question that is not exactly one of them — a forged line, a card the backend cut short —
// derives nothing and renders verbatim.
var pluginCards = []pluginCard{
	{
		key: PluginInstallKey, header: "Approve plugin install?", italianHeader: "Approva l'installazione del plugin?",
		lines: []pluginLine{
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
		lines: []pluginLine{
			{"plugin", "plugin: ", "plugin: "},
			{"source", "source: ", "origine: "},
			{"skills", "skills ", "competenze "}, //nolint:misspell // "competenze" is the Italian plural of "competenza", not English "competence".
			{"connectors", "connectors ", "connettori "},
			{"data", "plugin data: ", "dati del plugin: "},
		},
	},
}

func pluginCardFor(key string) (pluginCard, bool) {
	for _, card := range pluginCards {
		if card.key == key {
			return card, true
		}
	}
	return pluginCard{}, false
}

func derivePlugin(question string, fields map[string]json.RawMessage) (Presentation, bool) {
	digest := RootString(fields, "approval_sha256")
	if digest == "" {
		return Presentation{}, false
	}
	got := strings.Split(question, "\n")
	for _, card := range pluginCards {
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

func pluginLimits(key string) map[string]int {
	card, ok := pluginCardFor(key)
	if !ok {
		return nil
	}
	limits := map[string]int{"digest": maxShortParamBytes}
	for _, line := range card.lines {
		limits[line.param] = maxPluginLineBytes
	}
	return limits
}

func pluginRequired(key string) []string {
	card, _ := pluginCardFor(key)
	required := []string{"digest"}
	for _, line := range card.lines {
		required = append(required, line.param)
	}
	return required
}

func renderPluginItalian(p Presentation) string {
	card, _ := pluginCardFor(p.Key)
	lines := []string{card.italianHeader}
	for _, line := range card.lines {
		lines = append(lines, line.italian+italianPluginValue(line.param, p.Params[line.param]))
	}
	return strings.Join(append(lines, "sha256: "+p.Params["digest"]), "\n")
}

// italianPluginValue translates the card's whole-value words: an empty list, the mcp state
// and what happens to the plugin's data. Words inside a list stay as the card wrote them,
// beside the plugin's own quoted strings.
func italianPluginValue(param, value string) string {
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
	default:
		if count, ok := strings.CutSuffix(value, "): none"); ok {
			return count + "): nessuno"
		}
	}
	return value
}
