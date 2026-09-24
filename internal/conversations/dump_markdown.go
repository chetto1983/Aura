package conversations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/secret"
	"github.com/google/uuid"
)

// DumpAsset is one asset of the conversation's thread as the raw export lists it.
type DumpAsset struct {
	ID         string
	FileName   string
	MIMEType   string
	SizeBytes  int64
	SourceKind string
	Status     string
}

// Markdown renders the owner's raw export (prd.md §7): a metadata header, one section per
// persisted turn in seq order, then compaction summaries and the thread's assets.
// Configured secret values are masked over the finished document, so no field can
// bypass the mask.
func (d Dump) Markdown(conv Conversation, assets []DumpAsset, exportedAt time.Time) []byte {
	var b strings.Builder
	writeDumpHeader(&b, conv, exportedAt)
	callNames := make(map[string]string)
	for _, t := range d.Turns {
		writeDumpTurn(&b, t, callNames)
	}
	writeDumpCompactions(&b, d.Compactions)
	writeDumpAssets(&b, assets)
	return []byte(secret.RedactConfigured(b.String()))
}

func writeDumpHeader(b *strings.Builder, conv Conversation, exportedAt time.Time) {
	fmt.Fprintf(b, "# %s\n\n", conv.DisplayTitle())
	fmt.Fprintf(b, "- conversation: `%s`\n", conv.ID)
	fmt.Fprintf(b, "- model: `%s`\n", conv.Model)
	fmt.Fprintf(b, "- status: %s\n", conv.Status)
	fmt.Fprintf(b, "- created: %s\n", conv.CreatedAt)
	fmt.Fprintf(b, "- exported: %s\n", exportedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(b, "- tokens: input %d · output %d · cached %d · cost $%.4f\n",
		conv.TotalInputTokens, conv.TotalOutputTokens, conv.TotalCachedTokens, conv.TotalCostUSD)
}

// writeDumpTurn records each call's name in callNames so a later tool turn can say which
// call it answers — or that it answers none, which is how an orphan shows up.
func writeDumpTurn(b *strings.Builder, t DumpTurn, callNames map[string]string) {
	fmt.Fprintf(b, "\n## #%d · %s · %s", t.Seq, t.Role, t.CreatedAt.UTC().Format(time.RFC3339))
	if t.Role == llm.RoleTool {
		b.WriteString(" · " + toolResultLabel(t.ToolCallID, callNames))
	}
	writeBranchPointers(b, t)
	if t.InputTokens+t.OutputTokens+t.CachedTokens+t.ContextTokens > 0 {
		fmt.Fprintf(b, " · tokens in %d · out %d · cached %d · context %d",
			t.InputTokens, t.OutputTokens, t.CachedTokens, t.ContextTokens)
	}
	b.WriteByte('\n')

	if t.DeliveryKey != "" {
		fmt.Fprintf(b, "\ndelivery key: `%s`\n", t.DeliveryKey)
	}
	if len(t.AttachmentIDs) > 0 {
		fmt.Fprintf(b, "\nattachments: `%s`\n", strings.Join(t.AttachmentIDs, "`, `"))
	}
	if t.Reasoning != "" {
		b.WriteString("\n**reasoning**")
		if t.ReasoningDurationMS > 0 {
			fmt.Fprintf(b, " (%.1f s)", float64(t.ReasoningDurationMS)/1000)
		}
		b.WriteString("\n\n")
		writeFenced(b, "", t.Reasoning)
	}
	writeDumpToolCalls(b, t.ToolCalls, callNames)
	if strings.TrimSpace(t.Content) == "" {
		b.WriteString("\n_(empty content)_\n")
		return
	}
	b.WriteByte('\n')
	writeFenced(b, "", t.Content)
}

// writeBranchPointers names a turn's branch and parent only where they depart from the
// linear chain, so an unforked conversation reads without noise. ParentSeq 0 is NULL.
func writeBranchPointers(b *strings.Builder, t DumpTurn) {
	if t.BranchID != uuid.Nil.String() {
		fmt.Fprintf(b, " · branch `%s`", t.BranchID)
	}
	switch t.ParentSeq {
	case t.Seq - 1:
	case 0:
		b.WriteString(" · no parent")
	default:
		fmt.Fprintf(b, " · parent #%d", t.ParentSeq)
	}
}

func toolResultLabel(callID string, callNames map[string]string) string {
	if callID == "" {
		return "result without a call id"
	}
	if name, ok := callNames[callID]; ok {
		return fmt.Sprintf("result of `%s` · id `%s`", name, callID)
	}
	return fmt.Sprintf("result of unknown call `%s`", callID)
}

func writeDumpToolCalls(b *strings.Builder, raw []byte, callNames map[string]string) {
	if len(raw) == 0 {
		return
	}
	calls, err := decodeToolCalls(raw)
	if err != nil {
		b.WriteString("\n**tool calls** (undecodable)\n\n")
		writeFenced(b, "", string(raw))
		return
	}
	for _, c := range calls {
		callNames[c.ID] = c.Function.Name
		fmt.Fprintf(b, "\n**tool call** `%s` · id `%s`\n\n", c.Function.Name, c.ID)
		writeToolArguments(b, c.Function.Arguments)
	}
}

// writeToolArguments pretty-prints valid JSON and keeps anything else verbatim: a model
// that emitted malformed arguments is exactly what the dump must show.
func writeToolArguments(b *strings.Builder, args string) {
	if strings.TrimSpace(args) == "" {
		b.WriteString("_(no arguments)_\n")
		return
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, []byte(args), "", "  ") == nil {
		writeFenced(b, "json", pretty.String())
		return
	}
	writeFenced(b, "", args)
}

func writeDumpCompactions(b *strings.Builder, compactions []DumpCompaction) {
	if len(compactions) == 0 {
		return
	}
	b.WriteString("\n## Compactions\n")
	for _, c := range compactions {
		fmt.Fprintf(b, "\n### branch `%s` · through #%d · %d source turns · model `%s` · updated %s\n\n",
			c.BranchID, c.CoversThroughSeq, c.SourceTurns, c.Model, c.UpdatedAt.UTC().Format(time.RFC3339))
		writeFenced(b, "", c.Summary)
	}
}

func writeDumpAssets(b *strings.Builder, assets []DumpAsset) {
	if len(assets) == 0 {
		return
	}
	b.WriteString("\n## Assets\n\n")
	for _, a := range assets {
		fmt.Fprintf(b, "- `%s` · %s · %d bytes · source %s · status %s · id `%s`\n",
			a.FileName, a.MIMEType, a.SizeBytes, a.SourceKind, a.Status, a.ID)
	}
}

// writeFenced wraps text in a fence STRICTLY longer than its longest backtick run: an
// equal-length run inside a tool result would otherwise close the block early and turn
// the rest of the turn into document structure.
func writeFenced(b *strings.Builder, lang, text string) {
	fence := strings.Repeat("`", max(longestBacktickRun(text)+1, 3))
	b.WriteString(fence + lang + "\n" + text)
	if !strings.HasSuffix(text, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString(fence + "\n")
}

func longestBacktickRun(text string) int {
	longest, current := 0, 0
	for _, r := range text {
		if r != '`' {
			current = 0
			continue
		}
		current++
		longest = max(longest, current)
	}
	return longest
}
