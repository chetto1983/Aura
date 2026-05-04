package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/sandbox"
	"github.com/aura/aura/internal/source"
)

// ExecuteCodeTool lets the LLM run Python code in Aura's isolated runtime.
type ExecuteCodeTool struct {
	manager     *sandbox.Manager
	sender      DocumentSender
	sourceStore *source.Store
}

// NewExecuteCodeTool creates the execute_code tool. Returns nil if manager
// is nil (sandbox not available).
func NewExecuteCodeTool(manager *sandbox.Manager) *ExecuteCodeTool {
	return NewExecuteCodeToolWithStore(manager, nil, nil)
}

// NewExecuteCodeToolWithSender creates execute_code with optional artifact
// delivery. The sender is used only when sandbox code emits artifacts.
func NewExecuteCodeToolWithSender(manager *sandbox.Manager, sender DocumentSender) *ExecuteCodeTool {
	return NewExecuteCodeToolWithStore(manager, sender, nil)
}

// NewExecuteCodeToolWithStore creates execute_code with optional artifact
// delivery and source persistence. The store is used only when sandbox code
// emits artifacts.
func NewExecuteCodeToolWithStore(manager *sandbox.Manager, sender DocumentSender, sourceStore *source.Store) *ExecuteCodeTool {
	if manager == nil {
		return nil
	}
	return &ExecuteCodeTool{manager: manager, sender: sender, sourceStore: sourceStore}
}

func (t *ExecuteCodeTool) Name() string { return "execute_code" }

func (t *ExecuteCodeTool) Description() string {
	return "Execute Python code in an isolated WASM sandbox. " +
		"Use this for calculations, data processing, simulations, or any task that requires running code. " +
		"The sandbox is ephemeral; no state persists between executions. " +
		"Use create_xlsx/create_docx/create_pdf for simple documents; use this for computed artifacts, plots, custom data exports, or workflows that genuinely need code. " +
		"To return files, write them under /tmp/aura_out; Aura collects plain files from that directory, persists them as sandbox_artifact sources, and delivers them to Telegram when possible. " +
		"Packages are limited to Aura's bundled runtime profile. Set allow_network=true only when HTTP access is explicitly needed. " +
		"Timeout is configurable up to the server limit (default 120s)."
}

func (t *ExecuteCodeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"code": map[string]any{
				"type":        "string",
				"description": "Python code to execute in the sandbox",
			},
			"allow_network": map[string]any{
				"type":        "boolean",
				"description": "Allow network access from the sandbox. Default false.",
			},
		},
		"required": []string{"code"},
	}
}

func (t *ExecuteCodeTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	code, ok := args["code"].(string)
	if !ok || code == "" {
		return "", fmt.Errorf("code is required and must be a string")
	}

	allowNetwork := false
	if v, ok := args["allow_network"].(bool); ok {
		allowNetwork = v
	}

	result, err := t.manager.Execute(ctx, code, allowNetwork)
	if err != nil {
		return "", fmt.Errorf("sandbox execution failed: %w", err)
	}

	if !result.OK {
		return "", fmt.Errorf("execution failed (exit=%d): %s", result.ExitCode, result.Stderr)
	}

	out := fmt.Sprintf("exit_code: %d\nelapsed_ms: %d\n\n%s", result.ExitCode, result.ElapsedMs, result.Stdout)
	if result.Stderr != "" {
		out += fmt.Sprintf("\n--- stderr ---\n%s", result.Stderr)
	}
	if len(result.Artifacts) > 0 {
		persisted, err := t.persistArtifacts(ctx, result.Artifacts)
		if err != nil {
			return "", err
		}
		delivered, err := t.deliverArtifacts(ctx, result.Artifacts)
		if err != nil {
			return "", err
		}
		out += "\n\nartifacts:"
		for i, artifact := range result.Artifacts {
			out += fmt.Sprintf("\n- %s (%d bytes, %s, delivered=%t, persisted=%t",
				artifact.Name, artifact.SizeBytes, artifact.MimeType, delivered[i], persisted[i].ok)
			if persisted[i].sourceID != "" {
				out += fmt.Sprintf(", source_id=%s", persisted[i].sourceID)
			}
			if persisted[i].duplicate {
				out += ", duplicate=true"
			}
			out += ")"
		}
	}
	return out, nil
}

type persistedArtifact struct {
	ok        bool
	sourceID  string
	duplicate bool
}

func (t *ExecuteCodeTool) persistArtifacts(ctx context.Context, artifacts []sandbox.Artifact) ([]persistedArtifact, error) {
	persisted := make([]persistedArtifact, len(artifacts))
	if t.sourceStore == nil {
		return persisted, nil
	}
	for i, artifact := range artifacts {
		name := strings.TrimSpace(artifact.Name)
		if name == "" {
			name = fmt.Sprintf("artifact-%d.bin", i+1)
		}
		mime := strings.TrimSpace(artifact.MimeType)
		if mime == "" {
			mime = "application/octet-stream"
		}
		src, dup, err := t.sourceStore.Put(ctx, source.PutInput{
			Kind:     source.KindSandboxArtifact,
			Filename: name,
			MimeType: mime,
			Bytes:    artifact.Bytes,
		})
		if err != nil {
			return persisted, fmt.Errorf("execute_code: persist artifact %s: %w", name, err)
		}
		if src.Status != source.StatusIngested {
			src, err = t.sourceStore.Update(src.ID, func(rec *source.Source) error {
				rec.Status = source.StatusIngested
				rec.Error = ""
				return nil
			})
			if err != nil {
				return persisted, fmt.Errorf("execute_code: mark artifact %s ingested: %w", name, err)
			}
		}
		persisted[i] = persistedArtifact{ok: true, sourceID: src.ID, duplicate: dup}
	}
	return persisted, nil
}

func (t *ExecuteCodeTool) deliverArtifacts(ctx context.Context, artifacts []sandbox.Artifact) ([]bool, error) {
	delivered := make([]bool, len(artifacts))
	if t.sender == nil || UserIDFromContext(ctx) == "" {
		return delivered, nil
	}
	userID := UserIDFromContext(ctx)
	for i, artifact := range artifacts {
		name := strings.TrimSpace(artifact.Name)
		if name == "" {
			name = fmt.Sprintf("artifact-%d.bin", i+1)
		}
		caption := "Aura sandbox artifact: " + name
		if err := t.sender.SendDocumentToUser(userID, name, artifact.Bytes, caption); err != nil {
			return delivered, fmt.Errorf("execute_code: artifact %s delivery failed: %w", name, err)
		}
		delivered[i] = true
	}
	return delivered, nil
}
