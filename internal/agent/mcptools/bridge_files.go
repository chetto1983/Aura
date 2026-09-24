package mcptools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chetto1983/aura/internal/mcp"
)

// FileSink writes the files a tool result carried where the agent can open them.
// tools.MCPFileSink is the implementation; it lives beside document_open, so this
// package never imports the sandbox.
type FileSink interface {
	Materialize(ctx context.Context, server string, parts []mcp.FilePart) []mcp.FileOutcome
}

// filesNote tells the model what to do with the files; the turn deletes whatever it
// leaves behind.
const filesNote = "Delete each file (rm) once you are done with it. Whatever is left is deleted when this turn ends; copy a file elsewhere in /workspace to keep it."

// noFileSink is every file's reason on a mount with no sink: a host with no
// workspace, such as the pool-free CLI paths.
const noFileSink = "this host has no workspace for MCP files"

// filesFooter materializes parts and renders what became of them as the JSON block the
// model reads after the result's text. Only paths, sizes and hashes appear: the bytes
// never reach the model, the idempotency ledger or the transcript.
func (s *MountedServer) filesFooter(ctx context.Context, parts []mcp.FilePart) string {
	footer := struct {
		Files []mcp.FileOutcome `json:"files"`
		Note  string            `json:"note,omitempty"`
	}{Files: s.materialize(ctx, parts)}
	for _, outcome := range footer.Files {
		if outcome.Path != "" {
			footer.Note = filesNote
			break
		}
	}
	raw, err := json.Marshal(footer)
	if err != nil {
		return fmt.Sprintf("the files this result carried could not be reported: %v", err)
	}
	return string(raw)
}

func (s *MountedServer) materialize(ctx context.Context, parts []mcp.FilePart) []mcp.FileOutcome {
	if s.files != nil {
		return s.files.Materialize(ctx, s.name, parts)
	}
	outcomes := make([]mcp.FileOutcome, len(parts))
	for i, part := range parts {
		outcomes[i] = part.NotMaterialized(noFileSink)
	}
	return outcomes
}
