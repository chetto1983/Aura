package agentdef

import (
	"embed"
	"strings"
	"text/template"

	registry "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/llm"
)

//go:embed announce.md
var announceFS embed.FS

var announceTemplate = template.Must(template.ParseFS(announceFS, "announce.md"))

type AnnounceData struct {
	Archetype     string
	DisplayName   string
	ModelOverride string
}

func RenderAnnounce(data AnnounceData) string {
	data.Archetype = strings.TrimSpace(data.Archetype)
	data.DisplayName = strings.TrimSpace(data.DisplayName)
	data.ModelOverride = strings.TrimSpace(data.ModelOverride)
	var b strings.Builder
	if err := announceTemplate.Execute(&b, data); err != nil {
		return ""
	}
	return strings.TrimSpace(b.String())
}

func AnnouncementForToolCall(delegates []registry.Tool, call llm.ToolCall) string {
	for _, tool := range delegates {
		delegate, ok := tool.(*delegateTool)
		if !ok || delegate.Name() != call.Name {
			continue
		}
		return RenderAnnounce(AnnounceData{
			Archetype:     delegate.target.ID,
			DisplayName:   delegate.target.DisplayName,
			ModelOverride: stringArg(call.Arguments, "model"),
		})
	}
	return ""
}

func AnnouncementsForCalls(delegates []registry.Tool, calls []llm.ToolCall) []string {
	out := make([]string, 0, len(calls))
	for _, call := range calls {
		if text := AnnouncementForToolCall(delegates, call); text != "" {
			out = append(out, text)
		}
	}
	return out
}
