package agui

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/chetto1983/aura/internal/secret"
	"github.com/chetto1983/aura/internal/skills"
	"github.com/google/uuid"
)

// governance_api.go is the Phase-28 thin REST adapter over the Wave-0 governance seams
// (GOV-01/02/03). Six read-only GET routes project the MCP registry, the skills
// lifecycle + audit ledger, and the scheduler tasks + run history onto flat JSON. There
// is NO business logic here: each handler nil-checks its provider (503 when unwired),
// makes ONE provider call, and projects to JSON — mirroring graph_api.go. The static MCP
// list always renders; the live per-server probe is a SEPARATE per-row request so a hung
// server isolates to its own row (28-RESEARCH Hard Problem 3).
//
// The routes are registered on the agui Server.Mux under the /api/ carve-out; the
// PARENT-mux mount behind RequireAuth is cmd/aura/serve_webui.go's job (the whole-origin
// gate is inherited, no second auth check here — never a bare /api/). Every wire error
// passes through sanitizeErr (no DSN/token/host leak); env VALUES are NEVER serialized —
// only IsSecretEnvKey-flagged env KEY names appear as redacted chips (T-28-02-01).

// defaultProbeTimeout bounds a single live MCP probe (28-RESEARCH Hard Problem 3:
// 3s — long enough for a healthy stdio spawn + tools/list, short enough that a hung
// server's row resolves to a timed-out result within the UX patience window). A hung or
// dead server fails ONLY its own row because each probe is its own request under this
// per-request deadline. Tests override s.probeTimeout to keep the deadline-honoring path
// fast; a zero value falls back here.
const defaultProbeTimeout = 3 * time.Second

// defaultRunHistoryLimit / defaultRunHistoryOffset are the GOV-03 run-history pagination
// defaults (28-RESEARCH §REST Endpoint Shapes: limit 25, offset 0) applied when the query
// params are absent or unparseable. The store clamps both to a safe int32 range.
const (
	defaultRunHistoryLimit  = 25
	defaultRunHistoryOffset = 0
)

// mcpEnvChip is one MCP env entry rendered for the board: the KEY name plus a redacted
// flag when the key looks sensitive (secret.IsSecretEnvKey). The VALUE is NEVER carried —
// an env value can hold a token/DSN, so it never crosses to the wire (T-28-02-01).
type mcpEnvChip struct {
	Key      string `json:"key"`
	Redacted bool   `json:"redacted"`
}

// mcpServerRow is one GOV-01 MCP-governance row: the static status snapshot fields plus
// the configured source/profile/risk-policy fields and redacted env-KEY chips. lastError is
// RedactSecrets-cleaned before it reaches here.
type mcpServerRow struct {
	Name             string       `json:"name"`
	Source           string       `json:"source"`
	Trust            string       `json:"trust"`
	RiskPolicy       string       `json:"riskPolicy"`
	Runtime          string       `json:"runtime"`
	StartupState     string       `json:"startupState"`
	AuthStatus       string       `json:"authStatus"`
	Profiles         []string     `json:"profiles"`
	NetworkAllowlist []string     `json:"networkAllowlist"`
	EnvKeys          []mcpEnvChip `json:"envKeys"`
	LastError        string       `json:"lastError,omitempty"`
}

// skillRow is one GOV-02 skills-governance row. It is the projection shared by the active
// tab (from skills.Skill) and the pending/archived tabs (from skills.StageSkill). A field
// with no source on a given stage renders as "" (A2: never fabricated) and the omitempty
// keys drop. CRITICALLY there is no run/activate/action field — a pending row is
// non-runnable by construction (T-28-02-04 / GOV-02 prohibition #1).
type skillRow struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Language    string `json:"language,omitempty"`
	ContentHash string `json:"contentHash,omitempty"`
}

// skillAuditRow is the safe wire projection for one audit ledger row. It deliberately
// omits PausedStateToken and IdentityID: those are authorization/resume internals that do
// not belong in the read-only board response. The parent mux gates this route with
// governance.read, and this DTO is the second line of defense if more fields land on the
// domain row later.
type skillAuditRow struct {
	ID                string                `json:"ID"`
	CreatedAt         time.Time             `json:"CreatedAt"`
	ActorID           string                `json:"ActorID"`
	SkillName         string                `json:"SkillName"`
	Action            skills.AuditAction    `json:"Action"`
	ContentHash       string                `json:"ContentHash"`
	ApprovalSource    skills.ApprovalSource `json:"ApprovalSource"`
	GateRecommended   bool                  `json:"GateRecommended"`
	GateTaken         bool                  `json:"GateTaken"`
	BlocklistOverride bool                  `json:"BlocklistOverride"`
}

// schedulerTaskRow is the safe wire projection for a scheduled task. It omits Payload,
// IdentityID, and OriginConversationID, which can carry private prompt/context material
// or cross-identity linkage not needed by the board.
type schedulerTaskRow struct {
	ID           string            `json:"ID"`
	Kind         cron.TaskKind     `json:"Kind"`
	ScheduleKind cron.ScheduleKind `json:"ScheduleKind"`
	CronExpr     string            `json:"CronExpr"`
	EveryMinutes int               `json:"EveryMinutes"`
	RunAt        time.Time         `json:"RunAt"`
	TZ           string            `json:"TZ"`
	StepBudget   int               `json:"StepBudget"`
	Status       string            `json:"Status"`
	NextRunAt    time.Time         `json:"NextRunAt"`
	NotifyRoute  string            `json:"NotifyRoute"`
	CreatedAt    time.Time         `json:"CreatedAt"`
	UpdatedAt    time.Time         `json:"UpdatedAt"`
}

// schedulerRunRow is the safe run-history projection. It omits PausedStateToken while
// preserving LastHeartbeatAt for the read-only operator board; operator-visible text
// fields are sanitized before serialization.
type schedulerRunRow struct {
	ID                string    `json:"ID"`
	TaskID            string    `json:"TaskID"`
	Status            string    `json:"Status"`
	StepBudget        int       `json:"StepBudget"`
	StartedAt         time.Time `json:"StartedAt"`
	LastHeartbeatAt   time.Time `json:"LastHeartbeatAt"`
	CompletedWithHash string    `json:"CompletedWithHash"`
	Summary           string    `json:"Summary"`
	LastError         string    `json:"LastError"`
	MissedSince       time.Time `json:"MissedSince"`
	CompletedAt       time.Time `json:"CompletedAt"`
}

// registerGovernanceRoutes mounts the six read-only governance routes on the supplied mux
// using Go 1.22 method-pattern routing. They are SPECIFIC method+path siblings under the
// /api/ carve-out — never a bare /api/ (which would shadow /api/integrations/). The
// parent-mux mount behind RequireAuth lives in cmd/aura/serve_webui.go.
func (s *Server) registerGovernanceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/governance/mcp", s.handleMCPList)
	mux.HandleFunc("GET /api/governance/mcp/{name}/probe", s.handleMCPProbe)
	mux.HandleFunc("GET /api/governance/skills", s.handleSkillsList)
	mux.HandleFunc("GET /api/governance/skills/audit", s.handleSkillsAudit)
	mux.HandleFunc("GET /api/governance/scheduler", s.handleSchedulerList)
	mux.HandleFunc("GET /api/governance/scheduler/{id}/runs", s.handleSchedulerRuns)
}

// handleMCPList serves GET /api/governance/mcp (GOV-01): the static, fast, always-rendering
// MCP server list. Rows come from manager.SnapshotStatus (deterministic by-name order); for
// each server the env entries are projected to redacted KEY chips (value never sent) and the
// lastError is RedactSecrets-cleaned. Zero servers → 200 {servers: []} (the board still
// renders). An unwired provider → 503.
func (s *Server) handleMCPList(w http.ResponseWriter, _ *http.Request) {
	if s.governance.MCP == nil {
		http.Error(w, "mcp board not configured", http.StatusServiceUnavailable)
		return
	}
	doc := s.governance.MCP.Servers()
	statuses := mcpmanager.SnapshotStatus(doc)
	rows := make([]mcpServerRow, 0, len(statuses))
	for _, st := range statuses {
		server := doc.MCPServers[st.Name]
		rows = append(rows, mcpServerRow{
			Name:         st.Name,
			Source:       server.Source,
			Trust:        st.Trust,
			RiskPolicy:   st.Trust,
			Runtime:      st.Runtime,
			StartupState: st.StartupState,
			AuthStatus:   st.AuthStatus,
			Profiles:     mcpProfiles(doc, st.Name),
			// Pitfall 4 / T-32-02-AL: the []string{} literal (non-nil) is REQUIRED so an
			// empty allowlist marshals "networkAllowlist":[] not null. Do NOT switch to a
			// nil-based copy (e.g. append(server.Runtime.Network[:0:0], ...) on a nil input
			// stays nil). Pinned by TestGovernanceMCPEmptyAllowlistIsArrayNotNull.
			NetworkAllowlist: append([]string{}, server.Runtime.Network...),
			EnvKeys:          envChips(server.Env),
			LastError:        mcp.RedactSecrets(st.LastError),
		})
	}
	writeJSON(w, map[string]any{"servers": rows})
}

func mcpProfiles(doc mcp.ManagedConfig, server string) []string {
	profiles := make([]string, 0, len(doc.Profiles))
	for profile, cfg := range doc.Profiles {
		for _, name := range cfg.Servers {
			if name == server {
				profiles = append(profiles, profile)
				break
			}
		}
	}
	sort.Strings(profiles)
	return profiles
}

// envChips projects a server's `KEY=VALUE` env slice onto redacted KEY chips. The VALUE is
// dropped entirely — only the KEY name (split at the first '=') and an IsSecretEnvKey flag
// reach the wire, so a token/DSN value can never leak (T-28-02-01). A keyless entry is
// skipped.
func envChips(env []string) []mcpEnvChip {
	chips := make([]mcpEnvChip, 0, len(env))
	for _, entry := range env {
		key := entry
		if i := strings.IndexByte(entry, '='); i >= 0 {
			key = entry[:i]
		}
		if key == "" {
			continue
		}
		chips = append(chips, mcpEnvChip{Key: key, Redacted: secret.IsSecretEnvKey(key)})
	}
	return chips
}

// handleMCPProbe serves GET /api/governance/mcp/{name}/probe (GOV-01): a bounded, per-row
// LIVE doctor + tool-count probe. The {name} is looked up in the LOADED config (404 if
// absent — Prohibition #5: configured-servers-only, never a body-supplied URL/command); the
// probe then runs under context.WithTimeout(r.Context(), 3s) so a hung/dead server resolves
// to a sanitized error result for ITS row only (200, isolated) and never stalls siblings.
func (s *Server) handleMCPProbe(w http.ResponseWriter, r *http.Request) {
	if s.governance.MCP == nil {
		http.Error(w, "mcp board not configured", http.StatusServiceUnavailable)
		return
	}
	name := r.PathValue("name")
	doc := s.governance.MCP.Servers()
	server, ok := doc.MCPServers[name]
	if !ok {
		// Configured-servers-only: an unknown name never dials anything (T-28-02-02).
		http.Error(w, "mcp server not found", http.StatusNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.probeDeadline())
	defer cancel()
	res := s.governance.MCP.Probe(ctx, name, server)
	// The probe already redacts its own Detail/Err; sanitizeErr is the belt-and-suspenders
	// over the wire string. A timeout/dial failure is OK=false (200, isolated), not a 5xx.
	res.Detail = SanitizeString(res.Detail)
	res.Err = SanitizeString(res.Err)
	writeJSON(w, res)
}

// probeDeadline resolves the per-probe timeout, falling back to defaultProbeTimeout when
// the test override is unset (zero). Kept off the constructor so production uses 3s and
// tests can shrink it to exercise the deadline-honoring path quickly.
func (s *Server) probeDeadline() time.Duration {
	if s.probeTimeout > 0 {
		return s.probeTimeout
	}
	return defaultProbeTimeout
}

// handleSkillsList serves GET /api/governance/skills?stage=active|pending|archived (GOV-02):
// the active tab reads the Loader snapshot; pending/archived read the per-stage metadata
// reader (never a mounted body — GOV-02 prohibition #1). Pending/archived rows carry NO
// action/run field by construction (the projection has none). An unknown stage → 400; an
// empty stage → 200 {skills: []}. An unwired provider → 503; a backend error → sanitized 502.
func (s *Server) handleSkillsList(w http.ResponseWriter, r *http.Request) {
	if s.governance.Skills == nil {
		http.Error(w, "skills board not configured", http.StatusServiceUnavailable)
		return
	}
	stage := r.URL.Query().Get("stage")
	if stage == "" {
		stage = stageActive
	}
	switch stage {
	case stageActive:
		writeJSON(w, map[string]any{"skills": activeSkillRows(s.governance.Skills.ActiveSkills())})
	case skills.StagePending, skills.StageArchived:
		staged, err := s.governance.Skills.StageSkills(stage)
		if err != nil {
			writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
			return
		}
		writeJSON(w, map[string]any{"skills": stageSkillRows(staged)})
	default:
		http.Error(w, "unknown skills stage", http.StatusBadRequest)
	}
}

// stageActive names the active lifecycle tab (the Loader snapshot), the default stage when
// ?stage is absent. The pending/archived stage names live in the skills package
// (skills.StagePending/StageArchived) so the reader and the board agree on one vocabulary.
const stageActive = "active"

// activeSkillRows projects the loaded active skills onto the board row shape — the body is
// NEVER carried (the manifest-visible metadata only). No action field: read-only board.
func activeSkillRows(loaded []skills.Skill) []skillRow {
	rows := make([]skillRow, 0, len(loaded))
	for _, sk := range loaded {
		rows = append(rows, skillRow{
			Name:        sk.Name,
			Description: sk.Description,
			Type:        sk.Type,
			Language:    sk.Language,
		})
	}
	return rows
}

// stageSkillRows projects pending/archived StageSkills onto the board row shape. StageSkill
// already excludes the body (GOV-02 prohibition #1); the row adds no action field, so a
// pending row is non-runnable by construction (T-28-02-04).
func stageSkillRows(staged []skills.StageSkill) []skillRow {
	rows := make([]skillRow, 0, len(staged))
	for _, sk := range staged {
		rows = append(rows, skillRow{
			Name:        sk.Name,
			Description: sk.Description,
			Type:        sk.Type,
			Language:    sk.Language,
			ContentHash: sk.ContentHash,
		})
	}
	return rows
}

// handleSkillsAudit serves GET /api/governance/skills/audit (GOV-02): the append-only
// ledger rows newest-first via AuditStore.List. ?limit (store default 100 when absent) and
// ?since (RFC3339, optional lower bound) narrow the page. An unwired provider → 503; a
// backend error → sanitized 502; an empty ledger → 200 {rows: []}.
func (s *Server) handleSkillsAudit(w http.ResponseWriter, r *http.Request) {
	if s.governance.Skills == nil {
		http.Error(w, "skills board not configured", http.StatusServiceUnavailable)
		return
	}
	filter := skills.AuditFilter{}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n := parseLimit(raw); n > 0 {
			filter.Limit = n
		}
	}
	if raw := r.URL.Query().Get("since"); raw != "" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			filter.Since = t
		}
	}
	rows, err := s.governance.Skills.AuditLog(r.Context(), filter)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
		return
	}
	if rows == nil {
		rows = []skills.AuditRow{}
	}
	writeJSON(w, map[string]any{"rows": skillAuditRows(rows)})
}

// handleSchedulerList serves GET /api/governance/scheduler (GOV-03): the active tasks
// ordered by next fire via ListActiveTasks. Read-only — mutates nothing. An unwired
// provider → 503; a backend error → sanitized 502; no tasks → 200 {tasks: []}.
func (s *Server) handleSchedulerList(w http.ResponseWriter, r *http.Request) {
	if s.governance.Scheduler == nil {
		http.Error(w, "scheduler board not configured", http.StatusServiceUnavailable)
		return
	}
	tasks, err := s.governance.Scheduler.ListActiveTasks(r.Context())
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
		return
	}
	if tasks == nil {
		tasks = []cron.Task{}
	}
	writeJSON(w, map[string]any{"tasks": schedulerTaskRows(tasks)})
}

// handleSchedulerRuns serves GET /api/governance/scheduler/{id}/runs (GOV-03): the
// paginated, newest-first run history for one task. The {id} is uuid-guarded to a clean 404
// BEFORE any store call (a non-UUID id can never identify a task — R3), then ?limit (default
// 25) / ?offset (default 0) drive ListRunsForTask. Read-only — mutates nothing. An unwired
// provider → 503; a backend error → sanitized 502; a valid id with no runs → 200 {runs: []}.
func (s *Server) handleSchedulerRuns(w http.ResponseWriter, r *http.Request) {
	if s.governance.Scheduler == nil {
		http.Error(w, "scheduler board not configured", http.StatusServiceUnavailable)
		return
	}
	id, ok := parseTaskID(w, r)
	if !ok {
		return
	}
	limit := defaultRunHistoryLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n := parseLimit(raw); n > 0 {
			limit = n
		}
	}
	offset := defaultRunHistoryOffset
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if n := parseOffset(raw); n > 0 {
			offset = n
		}
	}
	runs, err := s.governance.Scheduler.ListRunsForTask(r.Context(), id, limit, offset)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
		return
	}
	if runs == nil {
		runs = []cron.Run{}
	}
	writeJSON(w, map[string]any{"runs": schedulerRunRows(runs)})
}

func skillAuditRows(rows []skills.AuditRow) []skillAuditRow {
	out := make([]skillAuditRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, skillAuditRow{
			ID:                row.ID,
			CreatedAt:         row.CreatedAt,
			ActorID:           row.ActorID,
			SkillName:         row.SkillName,
			Action:            row.Action,
			ContentHash:       row.ContentHash,
			ApprovalSource:    row.ApprovalSource,
			GateRecommended:   row.GateRecommended,
			GateTaken:         row.GateTaken,
			BlocklistOverride: row.BlocklistOverride,
		})
	}
	return out
}

func schedulerTaskRows(tasks []cron.Task) []schedulerTaskRow {
	out := make([]schedulerTaskRow, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, schedulerTaskRow{
			ID:           task.ID,
			Kind:         task.Kind,
			ScheduleKind: task.ScheduleKind,
			CronExpr:     task.CronExpr,
			EveryMinutes: task.EveryMinutes,
			RunAt:        task.RunAt,
			TZ:           task.TZ,
			StepBudget:   task.StepBudget,
			Status:       task.Status,
			NextRunAt:    task.NextRunAt,
			NotifyRoute:  task.NotifyRoute,
			CreatedAt:    task.CreatedAt,
			UpdatedAt:    task.UpdatedAt,
		})
	}
	return out
}

func schedulerRunRows(runs []cron.Run) []schedulerRunRow {
	out := make([]schedulerRunRow, 0, len(runs))
	for _, run := range runs {
		out = append(out, schedulerRunRow{
			ID:                run.ID,
			TaskID:            run.TaskID,
			Status:            run.Status,
			StepBudget:        run.StepBudget,
			StartedAt:         run.StartedAt,
			LastHeartbeatAt:   run.LastHeartbeatAt,
			CompletedWithHash: run.CompletedWithHash,
			Summary:           SanitizeString(run.Summary),
			LastError:         SanitizeString(run.LastError),
			MissedSince:       run.MissedSince,
			CompletedAt:       run.CompletedAt,
		})
	}
	return out
}

// parseTaskID resolves the {id} path param to a clean 404 BEFORE any store call: a
// non-UUID id can never identify an existing scheduler task, so it writes the 404 (R3)
// rather than leaking the store's parse error as a 500. Mirrors parseConvID
// (conversations_api.go) for the scheduler subtree.
func parseTaskID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "scheduler task not found", http.StatusNotFound)
		return "", false
	}
	return id, true
}

// parseOffset reads the ?offset= query value, falling back to 0 when it is absent,
// non-numeric, or negative. The store clamps both limit and offset to a safe int32 range.
func parseOffset(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
