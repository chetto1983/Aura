package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/tokenjuice"
)

const (
	toolCompactionBodyLimit      = 4 << 20 // 4 MiB: large enough for real tool output, bounded for the API.
	maxToolCompactionInlineChars = 20_000
	maxToolCompactionCommand     = 8_192
	maxToolCompactionArgv        = 64
	maxToolCompactionArg         = 2_048
)

type toolCompactionRequest struct {
	Mode string `json:"mode,omitempty"`

	ToolName string   `json:"tool_name"`
	Argv     []string `json:"argv,omitempty"`
	Command  string   `json:"command,omitempty"`
	Stdout   string   `json:"stdout,omitempty"`
	Stderr   string   `json:"stderr,omitempty"`
	Content  string   `json:"content,omitempty"`
	ExitCode *int     `json:"exit_code,omitempty"`

	MaxInlineChars int     `json:"max_inline_chars,omitempty"`
	ForceRuleID    string  `json:"force_rule_id,omitempty"`
	Raw            bool    `json:"raw,omitempty"`
	MinInputBytes  int     `json:"min_input_bytes,omitempty"`
	MinRatio       float64 `json:"min_ratio,omitempty"`
}

type toolCompactionResponse struct {
	Mode       string              `json:"mode"`
	InlineText string              `json:"inline_text"`
	Applied    bool                `json:"applied"`
	RuleID     string              `json:"rule_id"`
	Stats      toolCompactionStats `json:"stats"`
	Warnings   []string            `json:"warnings,omitempty"`
}

type toolCompactionStats struct {
	ToolName       string  `json:"tool_name"`
	OriginalBytes  int     `json:"original_bytes"`
	CompactedBytes int     `json:"compacted_bytes"`
	OriginalChars  int     `json:"original_chars"`
	CompactedChars int     `json:"compacted_chars"`
	SavedBytes     int     `json:"saved_bytes"`
	SavedChars     int     `json:"saved_chars"`
	Family         string  `json:"family"`
	Confidence     float64 `json:"confidence"`
	Ratio          float64 `json:"ratio"`
}

func handleToolCompact(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req toolCompactionRequest
		if err := decodeToolCompactionBody(w, r, &req); err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid JSON body")
			return
		}
		resp, err := compactToolRequest(req)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, resp)
	}
}

func decodeToolCompactionBody(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, toolCompactionBodyLimit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("body must contain one JSON object")
	}
	return nil
}

func compactToolRequest(req toolCompactionRequest) (toolCompactionResponse, error) {
	mode, err := validateToolCompactionRequest(req)
	if err != nil {
		return toolCompactionResponse{}, err
	}

	switch mode {
	case "tokenjuice":
		return compactWithTokenJuice(req), nil
	case "tool_result":
		return compactToolResultPreview(req), nil
	default:
		return toolCompactionResponse{}, fmt.Errorf("unsupported mode %q", mode)
	}
}

func validateToolCompactionRequest(req toolCompactionRequest) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "tokenjuice"
	}
	if mode != "tokenjuice" && mode != "tool_result" {
		return "", fmt.Errorf("unsupported mode %q", req.Mode)
	}
	if strings.TrimSpace(req.ToolName) == "" {
		return "", errors.New("tool_name is required")
	}
	if len(req.Argv) > maxToolCompactionArgv {
		return "", fmt.Errorf("argv has %d entries, max %d", len(req.Argv), maxToolCompactionArgv)
	}
	for i, arg := range req.Argv {
		if len(arg) > maxToolCompactionArg {
			return "", fmt.Errorf("argv[%d] is too long", i)
		}
	}
	if len(req.Command) > maxToolCompactionCommand {
		return "", fmt.Errorf("command is too long")
	}
	if req.MaxInlineChars < 0 || req.MaxInlineChars > maxToolCompactionInlineChars {
		return "", fmt.Errorf("max_inline_chars must be between 0 and %d", maxToolCompactionInlineChars)
	}
	if req.MinInputBytes < 0 || req.MinInputBytes > toolCompactionBodyLimit {
		return "", fmt.Errorf("min_input_bytes must be between 0 and %d", toolCompactionBodyLimit)
	}
	if req.MinRatio < 0 || req.MinRatio > 1 {
		return "", errors.New("min_ratio must be between 0 and 1")
	}
	return mode, nil
}

func compactWithTokenJuice(req toolCompactionRequest) toolCompactionResponse {
	stdout := req.Stdout
	stderr := req.Stderr
	if stdout == "" && stderr == "" && req.Content != "" {
		stdout = req.Content
	}
	result := tokenjuice.Compact(tokenjuice.Input{
		ToolName: req.ToolName,
		Argv:     req.Argv,
		Command:  req.Command,
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: req.ExitCode,
	}, tokenjuice.Options{
		MaxInlineChars: req.MaxInlineChars,
		ForceRuleID:    strings.TrimSpace(req.ForceRuleID),
		Raw:            req.Raw,
		MinInputBytes:  req.MinInputBytes,
		MinRatio:       req.MinRatio,
	})

	return toolCompactionResponse{
		Mode:       "tokenjuice",
		InlineText: result.InlineText,
		Applied:    result.Applied,
		RuleID:     result.RuleID,
		Stats:      statsFromTokenJuice(result.Stats),
	}
}

func compactToolResultPreview(req toolCompactionRequest) toolCompactionResponse {
	input := req.Content
	if input == "" {
		input = req.Stdout
		if req.Stderr != "" {
			if input != "" {
				input += "\n"
			}
			input += req.Stderr
		}
	}
	maxChars := req.MaxInlineChars
	if maxChars <= 0 {
		maxChars = 1200
	}
	output := conversation.CompactToolResultContent(req.ToolName, input, maxChars)
	return toolCompactionResponse{
		Mode:       "tool_result",
		InlineText: output,
		Applied:    output != input,
		RuleID:     "conversation/tool-result-preview",
		Stats:      statsFromStrings(req.ToolName, input, output, "tool-result", 1),
	}
}

func statsFromTokenJuice(stats tokenjuice.Stats) toolCompactionStats {
	return toolCompactionStats{
		ToolName:       stats.ToolName,
		OriginalBytes:  stats.OriginalBytes,
		CompactedBytes: stats.CompactedBytes,
		OriginalChars:  stats.OriginalChars,
		CompactedChars: stats.CompactedChars,
		SavedBytes:     stats.OriginalBytes - stats.CompactedBytes,
		SavedChars:     stats.OriginalChars - stats.CompactedChars,
		Family:         stats.Family,
		Confidence:     stats.Confidence,
		Ratio:          stats.Ratio(),
	}
}

func statsFromStrings(toolName, input, output, family string, confidence float64) toolCompactionStats {
	originalBytes := len([]byte(input))
	compactedBytes := len([]byte(output))
	originalChars := utf8.RuneCountInString(input)
	compactedChars := utf8.RuneCountInString(output)
	ratio := 1.0
	if originalBytes > 0 {
		ratio = float64(compactedBytes) / float64(originalBytes)
	}
	return toolCompactionStats{
		ToolName:       toolName,
		OriginalBytes:  originalBytes,
		CompactedBytes: compactedBytes,
		OriginalChars:  originalChars,
		CompactedChars: compactedChars,
		SavedBytes:     originalBytes - compactedBytes,
		SavedChars:     originalChars - compactedChars,
		Family:         family,
		Confidence:     confidence,
		Ratio:          ratio,
	}
}
