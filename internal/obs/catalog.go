package obs

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// InstrumentID is the stable code identity of one catalog-owned instrument.
type InstrumentID string

// InstrumentKind is the catalog's finite instrument-shape vocabulary.
type InstrumentKind string

// AttributeKey is one of the six bounded metric dimensions Aura permits.
type AttributeKey string

// KindCounter and its siblings are the supported instrument shapes.
const (
	KindCounter   InstrumentKind = "counter"
	KindHistogram InstrumentKind = "histogram"
	KindGauge     InstrumentKind = "gauge"
	KindUpDown    InstrumentKind = "updowncounter"
)

// AttributeOperation and its siblings are the only permitted metric dimensions.
const (
	AttributeOperation  AttributeKey = "operation"
	AttributeToolClass  AttributeKey = "tool_class"
	AttributeTransport  AttributeKey = "transport"
	AttributeOutcome    AttributeKey = "outcome"
	AttributeErrorClass AttributeKey = "error_class"
	AttributeState      AttributeKey = "state"
)

// ValueOther and the ToolClass values form the bounded tool-class vocabulary.
const (
	ValueOther = "other"

	ToolClassBuiltin     = "builtin"
	ToolClassFilesystem  = "filesystem"
	ToolClassShell       = "shell"
	ToolClassWeb         = "web"
	ToolClassInteraction = "interaction"
	ToolClassMCP         = "mcp"
	ToolClassScheduler   = "scheduler"
	ToolClassData        = "data"
)

// AgentBudgetStepsID and the remaining IDs assign one owner to every Aura metric.
const (
	AgentBudgetStepsID           InstrumentID = "agent_budget_steps"
	BoundaryInFlightID           InstrumentID = "boundary_in_flight"
	AgentToolDispatchID          InstrumentID = "agent_tool_dispatch"
	AgentLLMStreamOpenID         InstrumentID = "agent_llm_stream_open"
	AgentLLMStreamRetryID        InstrumentID = "agent_llm_stream_retry"
	AgentToolDurationID          InstrumentID = "agent_tool_duration"
	AgentTurnsID                 InstrumentID = "agent_turns"
	AgentLLMDurationID           InstrumentID = "agent_llm_duration"
	AgentLLMErrorsID             InstrumentID = "agent_llm_errors"
	AgentToolErrorsID            InstrumentID = "agent_tool_errors"
	AgentHooksID                 InstrumentID = "agent_hooks"
	AgentPromptTokensID          InstrumentID = "agent_prompt_tokens"
	AgentCompletionTokensID      InstrumentID = "agent_completion_tokens"
	AgentCacheReadTokensID       InstrumentID = "agent_cached_tokens" //nolint:gosec // Public metric identifier, not a credential.
	AgentCostUSDID               InstrumentID = "agent_cost_usd"
	AgentSpanExportFailuresID    InstrumentID = "agent_span_export_failures"
	AgentSpanIDEntropyFailuresID InstrumentID = "agent_span_id_entropy_failures"
	AgentPrefixDriftID           InstrumentID = "agent_prefix_drift"
	AgentLLMCallsID              InstrumentID = "agent_llm_calls"
	AgentPauseTransitionsID      InstrumentID = "agent_pause_transitions"
	RunnerResumeCallsID          InstrumentID = "runner_resume_calls"
	RunnerResumeDurationID       InstrumentID = "runner_resume_duration"
	MCPCallsID                   InstrumentID = "mcp_calls"
	MCPDurationID                InstrumentID = "mcp_duration"
	DBCallsID                    InstrumentID = "db_calls"
	DBDurationID                 InstrumentID = "db_duration"
	SchedulerRunsID              InstrumentID = "scheduler_runs"
	SchedulerDurationID          InstrumentID = "scheduler_duration"
	SchedulerTransitionsID       InstrumentID = "scheduler_transitions"
	ListenerTransitionsID        InstrumentID = "listener_transitions"
	IdempotencyOperationsID      InstrumentID = "idempotency_operations"
	RetentionOperationsID        InstrumentID = "retention_operations"
	RetentionDurationID          InstrumentID = "retention_duration"
	RetentionBytesID             InstrumentID = "retention_bytes"
	RetentionBacklogID           InstrumentID = "retention_backlog"
	RetentionDiskUtilizationID   InstrumentID = "retention_disk_utilization"
	LearningOperationsID         InstrumentID = "learning_operations"
	LearningSizeID               InstrumentID = "learning_size"
	LearningOldestAgeID          InstrumentID = "learning_oldest_age"
)

// Descriptor defines one stable OTel instrument and its Prometheus projection.
type Descriptor struct {
	ID             InstrumentID
	Name           string
	PrometheusName string
	Kind           InstrumentKind
	Unit           string
	Attributes     []AttributeKey
	Description    string
	Buckets        []float64
}

var fastDurationBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
var slowDurationBuckets = []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120}

var descriptors = []Descriptor{
	count(AgentBudgetStepsID, "aura.agent.budget.consume.step", "aura_agent_budget_consume_step_total", nil, "Total agent budget steps consumed."),
	{ID: BoundaryInFlightID, Name: "aura.boundary.in_flight", PrometheusName: "aura_boundary_in_flight", Kind: KindUpDown, Unit: "1", Attributes: []AttributeKey{AttributeOperation, AttributeToolClass, AttributeTransport}, Description: "Current in-flight instrumented boundaries."},
	count(AgentToolDispatchID, "aura.agent.tool.dispatch", "aura_agent_tool_dispatch_total", nil, "Total tool calls dispatched by the agent."),
	count(AgentLLMStreamOpenID, "aura.agent.llm.stream.open", "aura_agent_llm_stream_open_total", nil, "Total LLM stream open attempts."),
	count(AgentLLMStreamRetryID, "aura.agent.llm.stream.retry", "aura_agent_llm_stream_retry_total", nil, "Total LLM stream open retries."),
	histogram(AgentToolDurationID, "aura.agent.tool.duration", "aura_agent_tool_duration_seconds", []AttributeKey{AttributeToolClass, AttributeOutcome}, "Tool execution duration.", fastDurationBuckets),
	count(AgentTurnsID, "aura.agent.turn", "aura_agent_turn_total", []AttributeKey{AttributeOutcome}, "Total agent turns by terminal outcome."),
	histogram(AgentLLMDurationID, "aura.agent.llm.call.duration", "aura_agent_llm_call_duration_seconds", []AttributeKey{AttributeOutcome, AttributeErrorClass}, "LLM call duration from stream open through full drain.", slowDurationBuckets),
	count(AgentLLMErrorsID, "aura.agent.llm.errors", "aura_agent_llm_errors_total", []AttributeKey{AttributeErrorClass}, "Total LLM errors by bounded class."),
	count(AgentToolErrorsID, "aura.agent.tool.errors", "aura_agent_tool_errors_total", []AttributeKey{AttributeToolClass, AttributeErrorClass}, "Total tool errors by bounded class."),
	count(AgentHooksID, "aura.agent.hook", "aura_agent_hook_total", []AttributeKey{AttributeOperation, AttributeOutcome}, "Total hook outcomes by bounded point and outcome."),
	count(AgentPromptTokensID, "aura.agent.prompt.tokens", "aura_agent_prompt_tokens_total", nil, "Total prompt tokens reported by LLM usage."),
	count(AgentCompletionTokensID, "aura.agent.completion.tokens", "aura_agent_completion_tokens_total", nil, "Total completion tokens reported by LLM usage."),
	count(AgentCacheReadTokensID, "aura.agent.cached.tokens", "aura_agent_cached_tokens_total", nil, "Total cached prompt tokens reported by LLM usage."),
	count(AgentCostUSDID, "aura.agent.cost.usd", "aura_agent_cost_usd_total", nil, "Total reported LLM cost in USD."),
	count(AgentSpanExportFailuresID, "aura.agent.span.export.failures", "aura_agent_span_export_failures_total", nil, "Total OpenTelemetry span export failures."),
	count(AgentSpanIDEntropyFailuresID, "aura.agent.span.id.entropy.failures", "aura_agent_span_id_entropy_failures_total", nil, "Total recovered span identifier entropy failures."),
	count(AgentPrefixDriftID, "aura.agent.prefix.drift", "aura_agent_prefix_drift_total", nil, "Total cache-stable message prefix drift events."),
	count(AgentLLMCallsID, "aura.agent.llm.call", "aura_agent_llm_call_total", []AttributeKey{AttributeOutcome, AttributeErrorClass}, "Total completed LLM call boundaries."),
	count(AgentPauseTransitionsID, "aura.agent.pause.transition", "aura_agent_pause_transition_total", []AttributeKey{AttributeOperation, AttributeState, AttributeOutcome}, "Total pause lifecycle transitions."),
	count(RunnerResumeCallsID, "aura.runner.resume", "aura_runner_resume_total", []AttributeKey{AttributeOperation, AttributeOutcome, AttributeErrorClass}, "Total runner resume boundaries."),
	histogram(RunnerResumeDurationID, "aura.runner.resume.duration", "aura_runner_resume_duration_seconds", []AttributeKey{AttributeOperation, AttributeOutcome}, "Runner resume boundary duration.", fastDurationBuckets),
	count(MCPCallsID, "aura.mcp.call", "aura_mcp_call_total", []AttributeKey{AttributeOperation, AttributeTransport, AttributeOutcome, AttributeErrorClass}, "Total MCP client and bridge calls."),
	histogram(MCPDurationID, "aura.mcp.call.duration", "aura_mcp_call_duration_seconds", []AttributeKey{AttributeOperation, AttributeTransport, AttributeOutcome}, "MCP client and bridge call duration.", fastDurationBuckets),
	count(DBCallsID, "aura.db.call", "aura_db_call_total", []AttributeKey{AttributeOperation, AttributeOutcome, AttributeErrorClass}, "Total database boundaries."),
	histogram(DBDurationID, "aura.db.call.duration", "aura_db_call_duration_seconds", []AttributeKey{AttributeOperation, AttributeOutcome}, "Database boundary duration.", fastDurationBuckets),
	count(SchedulerRunsID, "aura.scheduler.run", "aura_scheduler_run_total", []AttributeKey{AttributeOperation, AttributeOutcome, AttributeErrorClass}, "Total scheduler loop and job runs."),
	histogram(SchedulerDurationID, "aura.scheduler.run.duration", "aura_scheduler_run_duration_seconds", []AttributeKey{AttributeOperation, AttributeOutcome}, "Scheduler run duration.", fastDurationBuckets),
	count(SchedulerTransitionsID, "aura.scheduler.transition", "aura_scheduler_transition_total", []AttributeKey{AttributeState, AttributeOutcome}, "Total scheduler lifecycle transitions."),
	count(ListenerTransitionsID, "aura.listener.transition", "aura_listener_transition_total", []AttributeKey{AttributeState, AttributeOutcome, AttributeErrorClass}, "Total serving listener lifecycle transitions."),
	count(IdempotencyOperationsID, "aura.idempotency.operation", "aura_idempotency_operation_total", []AttributeKey{AttributeOperation, AttributeState, AttributeOutcome}, "Total idempotency registry outcomes."),
	count(RetentionOperationsID, "aura.retention.operation", "aura_retention_operation_total", []AttributeKey{AttributeOperation, AttributeState, AttributeOutcome, AttributeErrorClass}, "Total retention policy operations."),
	histogram(RetentionDurationID, "aura.retention.operation.duration", "aura_retention_operation_duration_seconds", []AttributeKey{AttributeOperation, AttributeOutcome}, "Retention operation duration.", slowDurationBuckets),
	count(RetentionBytesID, "aura.retention.bytes", "aura_retention_bytes_total", []AttributeKey{AttributeOperation, AttributeOutcome}, "Total bytes processed by retention."),
	gauge(RetentionBacklogID, "aura.retention.backlog", "aura_retention_backlog_items", nil, "Current durable non-terminal retention item backlog.", "items"),
	gauge(RetentionDiskUtilizationID, "aura.retention.disk.utilization", "aura_retention_disk_utilization_ratio", []AttributeKey{AttributeState}, "Current bounded retention disk utilization ratio.", "1"),
	count(LearningOperationsID, "aura.learning.operation", "aura_learning_operation_total", []AttributeKey{AttributeOperation, AttributeToolClass, AttributeOutcome, AttributeErrorClass}, "Total bounded learning-store operations."),
	gauge(LearningSizeID, "aura.learning.size", "aura_learning_size", []AttributeKey{AttributeToolClass, AttributeState}, "Current bounded learning-store size.", "1"),
	gauge(LearningOldestAgeID, "aura.learning.oldest.age", "aura_learning_oldest_age_seconds", []AttributeKey{AttributeToolClass, AttributeState}, "Oldest bounded learning-store item age.", "s"),
}

func count(id InstrumentID, name, prom string, attrs []AttributeKey, description string) Descriptor {
	return Descriptor{ID: id, Name: name, PrometheusName: prom, Kind: KindCounter, Unit: "1", Attributes: attrs, Description: description}
}

func histogram(id InstrumentID, name, prom string, attrs []AttributeKey, description string, buckets []float64) Descriptor {
	return Descriptor{ID: id, Name: name, PrometheusName: prom, Kind: KindHistogram, Unit: "s", Attributes: attrs, Description: description, Buckets: buckets}
}

func gauge(id InstrumentID, name, prom string, attrs []AttributeKey, description, unit string) Descriptor {
	return Descriptor{ID: id, Name: name, PrometheusName: prom, Kind: KindGauge, Unit: unit, Attributes: attrs, Description: description}
}

// Catalog returns a deep copy of the stable metric descriptor catalog.
func Catalog() []Descriptor {
	out := make([]Descriptor, len(descriptors))
	for i, descriptor := range descriptors {
		out[i] = descriptor
		out[i].Attributes = append([]AttributeKey(nil), descriptor.Attributes...)
		out[i].Buckets = append([]float64(nil), descriptor.Buckets...)
	}
	return out
}

// DescriptorFor resolves a stable instrument ID.
func DescriptorFor(id InstrumentID) (Descriptor, bool) {
	for _, descriptor := range descriptors {
		if descriptor.ID == id {
			return descriptor, true
		}
	}
	return Descriptor{}, false
}

// MustDescriptor resolves id or panics for a programmer-owned catalog bug.
func MustDescriptor(id InstrumentID) Descriptor {
	descriptor, ok := DescriptorFor(id)
	if !ok {
		panic(fmt.Sprintf("unknown metric descriptor %q", id))
	}
	return descriptor
}

// CatalogManifest renders the deterministic verifier/dashboard contract.
func CatalogManifest() string {
	var out strings.Builder
	for _, descriptor := range descriptors {
		attrs := make([]string, len(descriptor.Attributes))
		for i, key := range descriptor.Attributes {
			attrs[i] = string(key)
		}
		buckets := make([]string, len(descriptor.Buckets))
		for i, boundary := range descriptor.Buckets {
			buckets[i] = strconv.FormatFloat(boundary, 'g', -1, 64)
		}
		_, _ = fmt.Fprintf(&out, "%s|%s|%s|%s|%s|%s|%s|%s\n",
			descriptor.ID, descriptor.Name, descriptor.PrometheusName, descriptor.Kind,
			descriptor.Unit, strings.Join(attrs, ","), descriptor.Description, strings.Join(buckets, ","))
	}
	return out.String()
}

// ValidateCatalog checks ownership, naming, attribute, and bucket invariants.
func ValidateCatalog() error {
	seenIDs := map[InstrumentID]bool{}
	seenNames := map[string]bool{}
	seenProm := map[string]bool{}
	for _, descriptor := range descriptors {
		if descriptor.ID == "" || descriptor.Name == "" || descriptor.PrometheusName == "" || descriptor.Description == "" {
			return fmt.Errorf("incomplete metric descriptor: %+v", descriptor)
		}
		if seenIDs[descriptor.ID] || seenNames[descriptor.Name] || seenProm[descriptor.PrometheusName] {
			return fmt.Errorf("duplicate metric descriptor owner: %s", descriptor.ID)
		}
		seenIDs[descriptor.ID], seenNames[descriptor.Name], seenProm[descriptor.PrometheusName] = true, true, true
		if !strings.HasPrefix(descriptor.Name, "aura.") || !strings.HasPrefix(descriptor.PrometheusName, "aura_") {
			return fmt.Errorf("metric %s has non-Aura name", descriptor.ID)
		}
		for _, key := range descriptor.Attributes {
			if !IsBoundedAttribute(key) {
				return fmt.Errorf("metric %s has unbounded attribute %q", descriptor.ID, key)
			}
		}
		if descriptor.Kind == KindHistogram {
			if len(descriptor.Buckets) == 0 || !sort.Float64sAreSorted(descriptor.Buckets) {
				return fmt.Errorf("histogram %s has invalid buckets", descriptor.ID)
			}
			for i := 1; i < len(descriptor.Buckets); i++ {
				if descriptor.Buckets[i] <= descriptor.Buckets[i-1] {
					return fmt.Errorf("histogram %s buckets are not strictly increasing", descriptor.ID)
				}
			}
		} else if len(descriptor.Buckets) != 0 {
			return fmt.Errorf("non-histogram %s has buckets", descriptor.ID)
		}
	}
	return nil
}

func catalogViews() []sdkmetric.View {
	views := make([]sdkmetric.View, 0)
	for _, descriptor := range descriptors {
		if descriptor.Kind != KindHistogram {
			continue
		}
		views = append(views, sdkmetric.NewView(
			sdkmetric.Instrument{Name: descriptor.Name, Kind: sdkmetric.InstrumentKindHistogram},
			sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: descriptor.Buckets}},
		))
	}
	return views
}

var allowedAttributeValues = map[AttributeKey]map[string]struct{}{
	AttributeOperation: finiteSet(
		"turn", "llm_call", "tool_dispatch", "turn_start", "before_model", "before_tool", "after_tool", "turn_end",
		"pause_create", "pause_answer", "resume", "resume_commit", "mcp_initialize", "mcp_list_tools", "mcp_call_tool", "mcp_ping", "mcp_bridge",
		"db_query", "db_transaction", "scheduler_tick", "scheduler_claim", "scheduler_job", "listener_serve", "idempotency_reserve", "idempotency_complete",
		"idempotency_replay", "retention_plan", "retention_apply", "retention_delete", "learning_load", "learning_write", "learning_compact", ValueOther,
	),
	AttributeToolClass: finiteSet(ToolClassBuiltin, ToolClassFilesystem, ToolClassShell, ToolClassWeb, ToolClassInteraction, ToolClassMCP, ToolClassScheduler, ToolClassData, ValueOther),
	AttributeTransport: finiteSet("grpc", "http", "stdio", "sse", "streamable_http", "in_process", ValueOther),
	AttributeOutcome: finiteSet(
		"success", "error", "canceled", "timeout", "paused", "content_stop", "text_response", "max_steps", "budget_exhausted",
		"panic", "hook_error", "breaker_open", "consumer_stopped", "empty_response", "tool_args_truncated", "tool_terminal", "denied",
		"accepted", "declined", "replayed", "conflict", "in_progress", "indeterminate", "skipped", "allow", "result", "fail_open", ValueOther,
	),
	AttributeErrorClass: finiteSet("none", "canceled", "timeout", "unavailable", "invalid", "conflict", "permission", "panic", "internal", ValueOther),
	AttributeState:      finiteSet("starting", "running", "ready", "degraded", "draining", "stopped", "pending", "in_progress", "completed", "failed", "indeterminate", "expired", ValueOther),
}

func finiteSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

// AllAttributeKeys returns the complete finite metric-dimension vocabulary.
func AllAttributeKeys() []AttributeKey {
	return []AttributeKey{AttributeOperation, AttributeToolClass, AttributeTransport, AttributeOutcome, AttributeErrorClass, AttributeState}
}

// IsBoundedAttribute reports whether key is catalog-approved.
func IsBoundedAttribute(key AttributeKey) bool {
	_, ok := allowedAttributeValues[key]
	return ok
}

// AllowedAttributeValue reports whether value belongs to key's finite enum.
func AllowedAttributeValue(key AttributeKey, value string) bool {
	_, ok := allowedAttributeValues[key][value]
	return ok
}

// NormalizeAttribute preserves approved values and folds everything else to other.
func NormalizeAttribute(key AttributeKey, value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if AllowedAttributeValue(key, value) {
		return value
	}
	return ValueOther
}

// ClassifyTool maps raw tool names to a small semantic class without retaining names.
func ClassifyTool(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if AllowedAttributeValue(AttributeToolClass, name) {
		return name
	}
	switch {
	case name == "ask_user" || strings.HasPrefix(name, "approval"):
		return ToolClassInteraction
	case strings.HasPrefix(name, "web_") || strings.HasPrefix(name, "browser_"):
		return ToolClassWeb
	case strings.HasPrefix(name, "shell_") || strings.HasPrefix(name, "exec_") || strings.HasPrefix(name, "terminal_"):
		return ToolClassShell
	case strings.HasPrefix(name, "file_") || strings.HasPrefix(name, "filesystem_"):
		return ToolClassFilesystem
	case strings.HasPrefix(name, "mcp.") || strings.HasPrefix(name, "mcp_"):
		return ToolClassMCP
	case strings.HasPrefix(name, "cron_") || strings.HasPrefix(name, "scheduler_"):
		return ToolClassScheduler
	case strings.HasPrefix(name, "db_") || strings.HasPrefix(name, "sql_") || strings.HasPrefix(name, "postgres_") || strings.HasPrefix(name, "neo4j_"):
		return ToolClassData
	case name == "calculator" || name == "time" || name == "date":
		return ToolClassBuiltin
	default:
		return ValueOther
	}
}

// ClassifyError maps an error kind/string to a finite non-sensitive class.
func ClassifyError(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case value == "" || value == "none":
		return "none"
	case strings.Contains(value, "cancel"):
		return "canceled"
	case strings.Contains(value, "deadline") || strings.Contains(value, "timeout"):
		return "timeout"
	case strings.Contains(value, "breaker") || strings.Contains(value, "retry") || strings.Contains(value, "unavailable"):
		return "unavailable"
	case strings.Contains(value, "invalid") || strings.Contains(value, "argument"):
		return "invalid"
	case strings.Contains(value, "conflict"):
		return "conflict"
	case strings.Contains(value, "permission") || strings.Contains(value, "denied") || strings.Contains(value, "unauthorized"):
		return "permission"
	case strings.Contains(value, "panic"):
		return "panic"
	case strings.Contains(value, "internal") || strings.Contains(value, "stream"):
		return "internal"
	default:
		return ValueOther
	}
}

// ErrorClass classifies err without returning its raw message.
func ErrorClass(err error) string {
	if err == nil {
		return "none"
	}
	return ClassifyError(err.Error())
}
