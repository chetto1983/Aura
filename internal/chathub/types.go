// Package chathub is the channel-neutral entry point for every conversation
// that drives an agent turn in Aura. Telegram, the web dashboard, /api/chat,
// scheduled heartbeat tasks and cron jobs all produce the same InboundMessage
// and consume the same OutboundEvent stream. The Agent Loop (built on
// internal/agentruntime) sees a uniform input and emits structured events;
// the adapters translate those events into channel-specific outputs
// (Telegram progressive edits, SSE frames, buffered JSON for /api/chat,
// silent task updates for heartbeat/cron).
//
// Wave 3.0 Slice 0 — extraction of the runtime from internal/telegram.
// See docs/wave-3-chathub-slice0.md for scope, decisions and the migration
// path. Slice 0 ships ONLY the contracts + Hub plumbing; subsequent slices
// (streaming, attachments, questions, threads, React UI) layer on top
// without touching the agent-side core.
//
// Design rules locked in by PRD §20 (anti-overengineering):
//   - max 5 types in this file
//   - no factory pattern, no plugin system, no abstract base classes
//   - adapters live under chathub/adapters/, not as generic interfaces
//   - channel-specific metadata travels via ChannelData (map[string]any) on
//     InboundMessage; the Agent Loop MUST NOT read this field. Adapters that
//     need state across inbound+outbound use Run.Metadata.
package chathub

import "time"

// Channel identifies the surface a message arrived from or is being
// delivered to. Kept as a string (not iota) so test fixtures and SQLite
// rows store the human-readable value.
type Channel string

const (
	ChannelTelegram  Channel = "telegram"
	ChannelWeb       Channel = "web"
	ChannelHeartbeat Channel = "heartbeat"
	ChannelCron      Channel = "cron"
)

// DeliveryMode tells the outbound adapter how aggressively to surface the
// run's output to the user. PRD §2.1 + §11.4. Heartbeat / cron jobs run
// in silent mode (no notification, no Telegram edit) but still complete
// the agent turn and write to memory.
type DeliveryMode string

const (
	DeliveryModeStreaming    DeliveryMode = "streaming"    // SSE / progressive
	DeliveryModeDeferred     DeliveryMode = "deferred"     // buffered, send when done
	DeliveryModeSilent       DeliveryMode = "silent"       // memory/wiki only
	DeliveryModeNotification DeliveryMode = "notification" // single push
)

// RunStatus is the state machine documented in PRD §11.5.
type RunStatus string

const (
	RunStatusQueued          RunStatus = "queued"
	RunStatusRunning         RunStatus = "running"
	RunStatusWaitingForUser  RunStatus = "waiting_for_user"
	RunStatusCancelling      RunStatus = "cancelling"
	RunStatusCancelled       RunStatus = "cancelled"
	RunStatusCompleted       RunStatus = "completed"
	RunStatusFailed          RunStatus = "failed"
)

// EventType enumerates every event the Agent Loop can emit. Subset of
// PRD §11.2 streaming events. Slice 0 ships the type constants; Slice 4
// implements the streaming wire format on top. Adapters can ignore types
// they don't care about (a buffered web outbound only needs Done/Error;
// SSE needs Delta + ToolStart + ToolEnd).
type EventType string

const (
	EventRunStarted        EventType = "run_started"
	EventMessageCreated    EventType = "message_created"
	EventMessageDelta      EventType = "message_delta"
	EventToolStart         EventType = "tool_start"
	EventToolDelta         EventType = "tool_delta"
	EventToolEnd           EventType = "tool_end"
	EventAttachmentStatus  EventType = "attachment_status"
	EventQuestionRequested EventType = "question_requested"
	EventMessageDone       EventType = "message_done"
	EventUsage             EventType = "usage"
	EventDone              EventType = "done"
	EventError             EventType = "error"
	EventCancelled         EventType = "cancelled"
)

// AttachmentRef is the chathub view of an uploaded file. Adapters resolve
// channel-specific upload payloads (Telegram document, web multipart) into
// this stable shape before InboundMessage reaches the Agent Loop. The
// agent never sees the upload bytes — it sees a source_id and decides how
// to read it via the source tool.
type AttachmentRef struct {
	ClientID string // adapter-supplied stable id for retry/correlation
	SourceID string // src_<sha16> once the source store has persisted it
	Filename string
	MimeType string
	Size     int64
	Status   string // queued | uploading | stored | extracting | extract_complete | ingested | failed | cancelled
}

// QuestionAnswer is the inbound payload when the user is replying to a
// previously-emitted question_requested event. PRD §9.2 Question state
// machine. Slice 0 ships the shape; full question UX lands in Slice 6.
type QuestionAnswer struct {
	QuestionID        string
	SelectedOptionIDs []string
	FreeText          string
	AnsweredMessageID string
}

// InboundMessage is the channel-neutral envelope produced by every
// InboundAdapter. The Agent Loop is contract-bound to read ONLY the fields
// declared here — channel-specific metadata travels in ChannelData and is
// adapter-private.
type InboundMessage struct {
	ID          string
	Channel     Channel
	PrincipalID string // channel-neutral identity; resolved by SessionResolver
	ThreadID    string // empty for single-turn channels (heartbeat first run)
	Text        string
	Attachments []AttachmentRef
	Question    *QuestionAnswer // set when answering a question_requested event
	Locale      string
	TimeZone    string
	Mode        DeliveryMode
	CreatedAt   time.Time

	// ChannelData carries adapter-private state. Examples: telegram tele.Bot
	// pointer + chat_id needed by the outbound adapter to send the reply;
	// web request_id needed by the SSE adapter to flush frames. The Agent
	// Loop MUST NOT read this map — assertions on its absence in
	// agentruntime tests guard the invariant.
	ChannelData map[string]any
}

// OutboundEvent is the channel-neutral envelope every Agent-Loop emit
// produces. Each adapter consumes the subset of types it can render: a
// Telegram outbound batches deltas into progressive edits, a buffered
// /api/chat outbound only acts on MessageDone, an SSE outbound flushes
// every type as a typed frame.
//
// Seq is monotonic within a RunID and lets adapters detect duplicates
// during replay (PRD §11.2 "ordine è per run_id, seq").
type OutboundEvent struct {
	ID        string
	RunID     string
	ThreadID  string
	Channel   Channel
	Type      EventType
	Seq       int64
	MessageID string         // empty until MessageCreated
	Content   string         // delta payload for streaming events
	Payload   map[string]any // typed per Type (e.g. ToolStart has tool name + args keys)
	CreatedAt time.Time
}

// Run is the in-memory record of a single agent turn. Persisted by the
// adapter that owns the channel (telegram_outbound writes back to the
// conversations table; web_outbound to chat_runs once Slice 3 ships).
// Slice 0 keeps Run purely in-memory; the persistence layer arrives with
// Slice 3.
type Run struct {
	ID          string
	ThreadID    string
	PrincipalID string
	Channel     Channel
	Status      RunStatus
	Model       string
	StartedAt   time.Time
	CompletedAt *time.Time
	LastError   string
	Metadata    map[string]any // adapter-private state across inbound+outbound
}
