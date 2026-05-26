package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
	toolregistry "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/llm"
)

// ToolExecutor is the direct test surface for Aura's LLM-visible registry.
// Production wires *registry.Registry; tests can use a fake.
type ToolExecutor interface {
	Names() []string
	Execute(ctx context.Context, name string, args map[string]any) (string, error)
}

// ToolProbeRecorder is the persistence boundary for direct /tools/call probes.
// It records metadata-only evidence: tool name, arg keys/hash, outcome, timing,
// and a redacted error preview. Raw argument values and tool output are never
// stored.
type ToolProbeRecorder interface {
	RecordToolProbe(ctx context.Context, record ToolProbeRecord) error
}

// ToolProbeRecord is one direct API probe to persist into runs + tool_attempts.
type ToolProbeRecord struct {
	RunID          string
	ToolName       string
	Arguments      map[string]any
	UserID         string
	ConversationID string
	StartedAt      time.Time
	Elapsed        time.Duration
	Class          string
	Reason         string
	Err            error
}

// SQLiteToolProbeRecorder records direct API tool probes in the same durable
// tables consumed by the agent loop observability surfaces.
type SQLiteToolProbeRecorder struct {
	db       *sql.DB
	attempts *attempts.SQLiteRepo
}

// NewSQLiteToolProbeRecorder wraps the shared Aura DB pool for /tools/call.
func NewSQLiteToolProbeRecorder(db *sql.DB) *SQLiteToolProbeRecorder {
	return &SQLiteToolProbeRecorder{db: db, attempts: attempts.NewSQLiteRepo(db)}
}

func (r *SQLiteToolProbeRecorder) RecordToolProbe(ctx context.Context, record ToolProbeRecord) error {
	if r == nil || r.db == nil || r.attempts == nil {
		return attempts.ErrRepoUnavailable
	}
	runID := strings.TrimSpace(record.RunID)
	if runID == "" {
		runID = newToolProbeRunID()
	}
	startedAt := record.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	elapsed := record.Elapsed
	if elapsed < 0 {
		elapsed = 0
	}
	endedAt := startedAt.Add(elapsed)
	userID := strings.TrimSpace(record.UserID)
	if userID == "" {
		userID = "api-tool"
	}
	conversationID := strings.TrimSpace(record.ConversationID)
	if conversationID == "" {
		conversationID = "api-tool"
	}
	metaJSON, _ := json.Marshal(map[string]any{
		"surface":   "api/tools/call",
		"tool_name": record.ToolName,
	})
	lastError := redactDirectToolError(record.Err, record.Arguments)
	if _, err := r.db.ExecContext(ctx, `INSERT OR IGNORE INTO runs
		(id, thread_id, principal_id, channel, status, started_at, updated_at, completed_at, last_error, metadata_json)
		VALUES (?, ?, ?, 'api', 'completed', ?, ?, ?, ?, ?)`,
		runID,
		conversationID,
		userID,
		startedAt.UTC().Format(time.RFC3339Nano),
		endedAt.UTC().Format(time.RFC3339Nano),
		endedAt.UTC().Format(time.RFC3339Nano),
		lastError,
		string(metaJSON),
	); err != nil {
		return fmt.Errorf("api tool probe: insert run: %w", err)
	}

	argsHash, argKeys := toolregistry.ObservationArgsHash(record.Arguments)
	obs := toolregistry.ToolObservation{
		RunID:         runID,
		ToolName:      record.ToolName,
		ToolKind:      toolregistry.ToolKindOf(record.ToolName),
		AttemptN:      1,
		Outcome:       toolregistry.BucketOf(record.Class),
		Class:         record.Class,
		Reason:        record.Reason,
		ArgsHash:      argsHash,
		ArgKeys:       argKeys,
		ErrorRedacted: lastError,
		StartedAt:     startedAt,
		ElapsedMS:     elapsed.Milliseconds(),
	}
	if err := r.attempts.Record(ctx, obs); err != nil {
		return fmt.Errorf("api tool probe: record attempt: %w", err)
	}
	return nil
}

type toolCallRequest struct {
	Name           string         `json:"name"`
	Args           map[string]any `json:"args,omitempty"`
	UserID         string         `json:"user_id,omitempty"`
	ConversationID string         `json:"conversation_id,omitempty"`
}

type toolCallResponse struct {
	Name            string `json:"name"`
	Output          string `json:"output"`
	OutputBytes     int    `json:"output_bytes"`
	RunID           string `json:"run_id,omitempty"`
	AttemptRecorded bool   `json:"attempt_recorded,omitempty"`
}

func handleToolRegistryList(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Tools == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "tool registry unavailable")
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, map[string]any{"tools": deps.Tools.Names()})
	}
}

func handleToolRegistryCall(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Tools == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "tool registry unavailable")
			return
		}
		var req toolCallRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			writeError(w, deps.Logger, http.StatusBadRequest, "name is required")
			return
		}
		if req.Args == nil {
			req.Args = map[string]any{}
		}
		startedAt := time.Now()
		runID := ""
		if deps.ToolProbeRecorder != nil {
			runID = newToolProbeRunID()
		}
		callCtx, callIdentity, err := toolCallContext(r, deps, req)
		if err != nil {
			recordToolProbe(deps, r.Context(), ToolProbeRecord{
				RunID:          runID,
				ToolName:       req.Name,
				Arguments:      req.Args,
				UserID:         strings.TrimSpace(req.UserID),
				ConversationID: strings.TrimSpace(req.ConversationID),
				StartedAt:      startedAt,
				Elapsed:        time.Since(startedAt),
				Class:          "permission",
				Reason:         "auth_context",
				Err:            err,
			})
			writeError(w, deps.Logger, http.StatusForbidden, err.Error())
			return
		}
		out, err := deps.Tools.Execute(callCtx, req.Name, req.Args)
		if err != nil {
			var awaitErr *toolregistry.ErrAwaitingUserInput
			if errors.As(err, &awaitErr) {
				out := marshalToolCallPayload(map[string]any{
					"awaiting_user_input": true,
					"question":            awaitErr.Question,
					"options":             awaitErr.Options,
					"kind":                awaitErr.Kind,
				})
				recorded := recordToolProbe(deps, r.Context(), ToolProbeRecord{
					RunID:          runID,
					ToolName:       req.Name,
					Arguments:      req.Args,
					UserID:         callIdentity.UserID,
					ConversationID: callIdentity.ConversationID,
					StartedAt:      startedAt,
					Elapsed:        time.Since(startedAt),
					Class:          "blocked",
					Reason:         "awaiting_user_input",
					Err:            errors.New("awaiting user input"),
				})
				writeToolCallOutput(w, deps, req.Name, out, toolCallOutputMeta{
					RunID:           runID,
					AttemptRecorded: recorded,
				})
				return
			}
			classified := classifyToolCallError(err)
			recordToolProbe(deps, r.Context(), ToolProbeRecord{
				RunID:          runID,
				ToolName:       req.Name,
				Arguments:      req.Args,
				UserID:         callIdentity.UserID,
				ConversationID: callIdentity.ConversationID,
				StartedAt:      startedAt,
				Elapsed:        time.Since(startedAt),
				Class:          classified.class,
				Reason:         classified.reason,
				Err:            err,
			})
			writeError(w, deps.Logger, classified.status, err.Error())
			return
		}
		recorded := recordToolProbe(deps, r.Context(), ToolProbeRecord{
			RunID:          runID,
			ToolName:       req.Name,
			Arguments:      req.Args,
			UserID:         callIdentity.UserID,
			ConversationID: callIdentity.ConversationID,
			StartedAt:      startedAt,
			Elapsed:        time.Since(startedAt),
		})
		writeToolCallOutput(w, deps, req.Name, out, toolCallOutputMeta{
			RunID:           runID,
			AttemptRecorded: recorded,
		})
	}
}

type toolCallOutputMeta struct {
	RunID           string
	AttemptRecorded bool
}

func writeToolCallOutput(w http.ResponseWriter, deps Deps, name, out string, meta toolCallOutputMeta) {
	writeJSON(w, deps.Logger, http.StatusOK, toolCallResponse{
		Name:            name,
		Output:          out,
		OutputBytes:     len(out),
		RunID:           meta.RunID,
		AttemptRecorded: meta.AttemptRecorded,
	})
}

func marshalToolCallPayload(payload map[string]any) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		return `{"awaiting_user_input":true}`
	}
	return string(raw)
}

type toolCallIdentity struct {
	UserID         string
	ConversationID string
}

func toolCallContext(r *http.Request, deps Deps, req toolCallRequest) (context.Context, toolCallIdentity, error) {
	ctx := r.Context()
	userID := strings.TrimSpace(req.UserID)
	if authUserID := userIDFromRequest(r); authUserID != "" {
		if userID != "" && userID != authUserID {
			return nil, toolCallIdentity{}, errors.New("authenticated user_id override is forbidden")
		}
		userID = authUserID
		ctx = identity.WithActorID(ctx, actorIDFromRequest(r))
		ctx = identity.WithAuthority(ctx, deps.Auth)
	}
	if userID == "" {
		userID = "api-tool"
	}
	conversationID := strings.TrimSpace(req.ConversationID)
	if conversationID == "" {
		conversationID = "api-tool"
	}
	ctx = toolregistry.WithUserID(ctx, userID)
	ctx = toolregistry.WithConversationID(ctx, conversationID)
	ctx = toolregistry.WithAllowedToolNames(ctx, deps.Tools.Names())
	return ctx, toolCallIdentity{UserID: userID, ConversationID: conversationID}, nil
}

type toolCallErrorClassification struct {
	status int
	class  string
	reason string
}

func classifyToolCallError(err error) toolCallErrorClassification {
	if err == nil {
		return toolCallErrorClassification{status: http.StatusOK}
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case errors.Is(err, context.Canceled):
		return toolCallErrorClassification{status: http.StatusRequestTimeout, class: "cancelled", reason: "context_cancelled"}
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(lower, "timeout"), strings.Contains(lower, "deadline exceeded"):
		return toolCallErrorClassification{status: http.StatusGatewayTimeout, class: "timeout", reason: "timeout"}
	case errors.Is(err, identity.ErrUnauthorized), strings.Contains(lower, "not authorized"), strings.Contains(lower, "unauthorized"), strings.Contains(lower, "forbidden"), strings.Contains(lower, "capability_denied"):
		return toolCallErrorClassification{status: http.StatusForbidden, class: "permission", reason: "authorization_denied"}
	case errors.Is(err, llm.ErrSchemaValidation):
		return toolCallErrorClassification{status: http.StatusBadRequest, class: "validation", reason: "schema_validation"}
	case errors.Is(err, ErrSkillNotFound), errors.Is(err, os.ErrNotExist), msg == "tool not found", strings.Contains(lower, " not found"), strings.Contains(lower, "no such file"), strings.Contains(lower, "does not exist"):
		return toolCallErrorClassification{status: http.StatusNotFound, class: "not_found", reason: "not_found"}
	case strings.Contains(lower, "rate limit"), strings.Contains(lower, "too many requests"):
		return toolCallErrorClassification{status: http.StatusTooManyRequests, class: "rate_limited", reason: "rate_limited"}
	case strings.Contains(lower, " is required"), strings.Contains(lower, "invalid "), strings.Contains(lower, "unknown action"), strings.Contains(lower, "unsupported "), strings.Contains(lower, "must be"), strings.Contains(lower, "missing "):
		return toolCallErrorClassification{status: http.StatusBadRequest, class: "validation", reason: "invalid_request"}
	case strings.Contains(lower, "i/o timeout"), strings.Contains(lower, "connection refused"), strings.Contains(lower, "network is unreachable"):
		return toolCallErrorClassification{status: http.StatusBadGateway, class: "io", reason: "io"}
	default:
		return toolCallErrorClassification{status: http.StatusInternalServerError, class: "error", reason: "tool_error"}
	}
}

func recordToolProbe(deps Deps, ctx context.Context, record ToolProbeRecord) bool {
	if deps.ToolProbeRecorder == nil {
		return false
	}
	if err := deps.ToolProbeRecorder.RecordToolProbe(ctx, record); err != nil {
		if deps.Logger != nil {
			deps.Logger.Error("api_tool_probe_record_failed", "err", err, "tool_name", record.ToolName)
		}
		return false
	}
	return true
}

func newToolProbeRunID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "run_toolprobe_0000000000000000"
	}
	return "run_toolprobe_" + hex.EncodeToString(buf[:])
}

func redactDirectToolError(err error, args map[string]any) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	msg = directToolURLCredentialRe.ReplaceAllString(msg, "$1<redacted>")
	msg = directToolBase64BlobRe.ReplaceAllString(msg, "<base64>")
	for _, value := range directToolStringArgs(args) {
		if len(value) >= 3 {
			msg = strings.ReplaceAll(msg, value, "<arg>")
		}
	}
	if len(msg) > 500 {
		msg = msg[:500] + "...(truncated)"
	}
	return msg
}

func directToolStringArgs(args map[string]any) []string {
	if len(args) == 0 {
		return nil
	}
	values := make([]string, 0, len(args))
	var walk func(any)
	walk = func(v any) {
		switch typed := v.(type) {
		case string:
			if s := strings.TrimSpace(typed); s != "" {
				values = append(values, s)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	for _, value := range args {
		walk(value)
	}
	return values
}

var (
	directToolURLCredentialRe = regexp.MustCompile(`([?&](?i:token|api[_-]?key|secret|auth|bearer)=)[^&\s"]+`)
	directToolBase64BlobRe    = regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)
)
