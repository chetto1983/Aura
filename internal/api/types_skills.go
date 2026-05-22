package api

import "time"

// Task is the response shape for /tasks endpoints.
type Task struct {
	Name                 string     `json:"name"`
	Kind                 string     `json:"kind"`
	Payload              string     `json:"payload,omitempty"`
	RecipientID          string     `json:"recipient_id,omitempty"`
	ScheduleKind         string     `json:"schedule_kind"`
	ScheduleAt           *time.Time `json:"schedule_at,omitempty"`
	ScheduleDaily        string     `json:"schedule_daily,omitempty"`
	ScheduleWeekdays     string     `json:"schedule_weekdays,omitempty"`
	ScheduleEveryMinutes int        `json:"schedule_every_minutes,omitempty"`
	NextRunAt            time.Time  `json:"next_run_at"`
	LastRunAt            *time.Time `json:"last_run_at,omitempty"`
	LastError            string     `json:"last_error,omitempty"`
	LastOutput           string     `json:"last_output,omitempty"`
	LastMetricsJSON      string     `json:"last_metrics_json,omitempty"`
	WakeSignature        string     `json:"wake_signature,omitempty"`
	Status               string     `json:"status"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// SkillSummary is one row of GET /skills.
type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SkillDetail is the response of GET /skills/{name}. Truncated is true when
// Content was clipped at maxSkillBodyChars.
type SkillDetail struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Content     string `json:"content"`
	Truncated   bool   `json:"truncated,omitempty"`
}

// SkillCatalogItem is one row from GET /skills/catalog (proxies skills.sh).
type SkillCatalogItem struct {
	Source         string `json:"source"`
	SkillID        string `json:"skill_id,omitempty"`
	Name           string `json:"name"`
	Installs       int    `json:"installs"`
	InstallCommand string `json:"install_command,omitempty"`
}

// SkillInstallResponse is the body of POST /skills/install.
type SkillInstallResponse struct {
	OK     bool   `json:"ok"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// SkillDeleteResponse is the body of POST /skills/{name}/delete.
type SkillDeleteResponse struct {
	OK   bool   `json:"ok"`
	Name string `json:"name"`
}

// ConversationTurn is one row of GET /conversations.
type ConversationTurn struct {
	ID             int64  `json:"id"`
	ChatID         int64  `json:"chat_id"`
	UserID         int64  `json:"user_id"`
	TurnIndex      int64  `json:"turn_index"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	ToolCalls      string `json:"tool_calls,omitempty"`
	ToolCallID     string `json:"tool_call_id,omitempty"`
	LLMCalls       int    `json:"llm_calls,omitempty"`
	ToolCallsCount int    `json:"tool_calls_count,omitempty"`
	ElapsedMS      int64  `json:"elapsed_ms,omitempty"`
	TokensIn       int    `json:"tokens_in,omitempty"`
	TokensOut      int    `json:"tokens_out,omitempty"`
	CreatedAt      string `json:"created_at"`
}

// ConversationDetail is the response of GET /conversations/{id}. ToolCalls
// is the raw JSON string from the DB so the frontend can parse/expand it.
type ConversationDetail struct {
	ID             int64  `json:"id"`
	ChatID         int64  `json:"chat_id"`
	UserID         int64  `json:"user_id"`
	TurnIndex      int64  `json:"turn_index"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	ToolCalls      string `json:"tool_calls,omitempty"`
	ToolCallID     string `json:"tool_call_id,omitempty"`
	LLMCalls       int    `json:"llm_calls,omitempty"`
	ToolCallsCount int    `json:"tool_calls_count,omitempty"`
	ElapsedMS      int64  `json:"elapsed_ms,omitempty"`
	TokensIn       int    `json:"tokens_in,omitempty"`
	TokensOut      int    `json:"tokens_out,omitempty"`
	CreatedAt      string `json:"created_at"`
}

// ProposedUpdate is one row of GET /summaries (mirrors proposed_updates table).
type ProposedUpdate struct {
	ID             int64           `json:"id"`
	ChatID         int64           `json:"chat_id"`
	Fact           string          `json:"fact"`
	Action         string          `json:"action"`
	TargetSlug     string          `json:"target_slug,omitempty"`
	Similarity     float64         `json:"similarity"`
	SourceTurnIDs  []int64         `json:"source_turn_ids"`
	Category       string          `json:"category,omitempty"`
	RelatedSlugs   []string        `json:"related_slugs"`
	Provenance     Provenance      `json:"provenance,omitempty"`
	SkillLifecycle *SkillLifecycle `json:"skill_lifecycle,omitempty"`
	Status         string          `json:"status"`
	Kind           string          `json:"kind,omitempty"`
	SignatureHash  string          `json:"signature_hash,omitempty"`
	CreatedAt      string          `json:"created_at"`
}

// SummaryBatchRequest is the body of POST /summaries/batch.
type SummaryBatchRequest struct {
	IDs []int64 `json:"ids"`
}

// SummaryBatchFailure is one failed entry in a batch response.
type SummaryBatchFailure struct {
	ID    int64  `json:"id"`
	Error string `json:"error"`
}

// SummaryBatchResponse is the body of POST /summaries/batch/approve|reject.
type SummaryBatchResponse struct {
	Updated []ProposedUpdate      `json:"updated"`
	Failed  []SummaryBatchFailure `json:"failed"`
}

// EvidenceRef is one source reference in a Provenance record.
type EvidenceRef struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	Page    int    `json:"page,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

// Provenance tracks the origin of a proposed wiki update.
type Provenance struct {
	OriginTool   string         `json:"origin_tool,omitempty"`
	OriginReason string         `json:"origin_reason,omitempty"`
	ProposalKind string         `json:"proposal_kind,omitempty"`
	Evidence     []EvidenceRef  `json:"evidence,omitempty"`
	Skill        *SkillProposal `json:"skill,omitempty"`
	AgentJobID   string         `json:"agent_job_id,omitempty"`
	SwarmRunID   string         `json:"swarm_run_id,omitempty"`
	SwarmTaskID  string         `json:"swarm_task_id,omitempty"`
}

// SkillProposal is the skill-specific payload embedded in a Provenance record.
type SkillProposal struct {
	Action       string   `json:"action"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	SmokePrompt  string   `json:"smoke_prompt,omitempty"`
	Content      string   `json:"content,omitempty"`
	Reason       string   `json:"reason,omitempty"`
}

// SkillLifecycle documents the review contract for skill proposals.
type SkillLifecycle struct {
	Mode          string `json:"mode"`
	ReviewStatus  string `json:"review_status"`
	InstallStatus string `json:"install_status"`
	SmokeStatus   string `json:"smoke_status"`
	NextStep      string `json:"next_step"`
}
