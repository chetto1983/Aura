package obs

import (
	"strings"
	"testing"
)

const catalogGolden = `agent_budget_steps|aura.agent.budget.consume.step|aura_agent_budget_consume_step_total|counter|1||Total agent budget steps consumed.|
boundary_in_flight|aura.boundary.in_flight|aura_boundary_in_flight|updowncounter|1|operation,tool_class,transport|Current in-flight instrumented boundaries.|
agent_tool_dispatch|aura.agent.tool.dispatch|aura_agent_tool_dispatch_total|counter|1||Total tool calls dispatched by the agent.|
agent_llm_stream_open|aura.agent.llm.stream.open|aura_agent_llm_stream_open_total|counter|1||Total LLM stream open attempts.|
agent_llm_stream_retry|aura.agent.llm.stream.retry|aura_agent_llm_stream_retry_total|counter|1||Total LLM stream open retries.|
agent_tool_duration|aura.agent.tool.duration|aura_agent_tool_duration_seconds|histogram|s|tool_class,outcome|Tool execution duration.|0.001,0.005,0.01,0.025,0.05,0.1,0.25,0.5,1,2.5,5,10,30,60
agent_turns|aura.agent.turn|aura_agent_turn_total|counter|1|outcome|Total agent turns by terminal outcome.|
agent_llm_duration|aura.agent.llm.call.duration|aura_agent_llm_call_duration_seconds|histogram|s|outcome,error_class|LLM call duration from stream open through full drain.|0.01,0.05,0.1,0.25,0.5,1,2.5,5,10,30,60,120
agent_llm_errors|aura.agent.llm.errors|aura_agent_llm_errors_total|counter|1|error_class|Total LLM errors by bounded class.|
agent_tool_errors|aura.agent.tool.errors|aura_agent_tool_errors_total|counter|1|tool_class,error_class|Total tool errors by bounded class.|
agent_hooks|aura.agent.hook|aura_agent_hook_total|counter|1|operation,outcome|Total hook outcomes by bounded point and outcome.|
agent_prompt_tokens|aura.agent.prompt.tokens|aura_agent_prompt_tokens_total|counter|1||Total prompt tokens reported by LLM usage.|
agent_completion_tokens|aura.agent.completion.tokens|aura_agent_completion_tokens_total|counter|1||Total completion tokens reported by LLM usage.|
agent_cached_tokens|aura.agent.cached.tokens|aura_agent_cached_tokens_total|counter|1||Total cached prompt tokens reported by LLM usage.|
agent_cost_usd|aura.agent.cost.usd|aura_agent_cost_usd_total|counter|1||Total reported LLM cost in USD.|
agent_span_export_failures|aura.agent.span.export.failures|aura_agent_span_export_failures_total|counter|1||Total OpenTelemetry span export failures.|
agent_span_id_entropy_failures|aura.agent.span.id.entropy.failures|aura_agent_span_id_entropy_failures_total|counter|1||Total recovered span identifier entropy failures.|
agent_prefix_drift|aura.agent.prefix.drift|aura_agent_prefix_drift_total|counter|1||Total cache-stable message prefix drift events.|
agent_llm_calls|aura.agent.llm.call|aura_agent_llm_call_total|counter|1|outcome,error_class|Total completed LLM call boundaries.|
agent_pause_transitions|aura.agent.pause.transition|aura_agent_pause_transition_total|counter|1|operation,state,outcome|Total pause lifecycle transitions.|
runner_resume_calls|aura.runner.resume|aura_runner_resume_total|counter|1|operation,outcome,error_class|Total runner resume boundaries.|
runner_resume_duration|aura.runner.resume.duration|aura_runner_resume_duration_seconds|histogram|s|operation,outcome|Runner resume boundary duration.|0.001,0.005,0.01,0.025,0.05,0.1,0.25,0.5,1,2.5,5,10,30,60
mcp_calls|aura.mcp.call|aura_mcp_call_total|counter|1|operation,transport,outcome,error_class|Total MCP client and bridge calls.|
mcp_duration|aura.mcp.call.duration|aura_mcp_call_duration_seconds|histogram|s|operation,transport,outcome|MCP client and bridge call duration.|0.001,0.005,0.01,0.025,0.05,0.1,0.25,0.5,1,2.5,5,10,30,60
db_calls|aura.db.call|aura_db_call_total|counter|1|operation,outcome,error_class|Total database boundaries.|
db_duration|aura.db.call.duration|aura_db_call_duration_seconds|histogram|s|operation,outcome|Database boundary duration.|0.001,0.005,0.01,0.025,0.05,0.1,0.25,0.5,1,2.5,5,10,30,60
scheduler_runs|aura.scheduler.run|aura_scheduler_run_total|counter|1|operation,outcome,error_class|Total scheduler loop and job runs.|
scheduler_duration|aura.scheduler.run.duration|aura_scheduler_run_duration_seconds|histogram|s|operation,outcome|Scheduler run duration.|0.001,0.005,0.01,0.025,0.05,0.1,0.25,0.5,1,2.5,5,10,30,60
scheduler_transitions|aura.scheduler.transition|aura_scheduler_transition_total|counter|1|state,outcome|Total scheduler lifecycle transitions.|
listener_transitions|aura.listener.transition|aura_listener_transition_total|counter|1|state,outcome,error_class|Total serving listener lifecycle transitions.|
idempotency_operations|aura.idempotency.operation|aura_idempotency_operation_total|counter|1|operation,state,outcome|Total idempotency registry outcomes.|
retention_operations|aura.retention.operation|aura_retention_operation_total|counter|1|operation,state,outcome,error_class|Total retention policy operations.|
retention_duration|aura.retention.operation.duration|aura_retention_operation_duration_seconds|histogram|s|operation,outcome|Retention operation duration.|0.01,0.05,0.1,0.25,0.5,1,2.5,5,10,30,60,120
retention_bytes|aura.retention.bytes|aura_retention_bytes_total|counter|1|operation,outcome|Total bytes processed by retention.|
retention_backlog|aura.retention.backlog|aura_retention_backlog_items|gauge|items||Current durable non-terminal retention item backlog.|
retention_disk_utilization|aura.retention.disk.utilization|aura_retention_disk_utilization_ratio|gauge|1|state|Current bounded retention disk utilization ratio.|
ingestion_jobs|aura.ingestion.job|aura_ingestion_job_total|counter|1|outcome|Total durable ingestion job terminal outcomes.|
ingestion_embed_duration|aura.ingestion.embed.duration|aura_ingestion_embed_duration_seconds|histogram|s|outcome|Embedding worker per-document processing duration.|0.01,0.05,0.1,0.25,0.5,1,2.5,5,10,30,60,120
ingestion_queue_depth|aura.ingestion.queue.depth|aura_ingestion_queue_depth_items|gauge|items||Current durable queued ingestion job backlog.|
rerank_calls|aura.rerank.call|aura_rerank_call_total|counter|1|outcome,error_class|Total rerank calls, including explicit fail-soft degradation.|
rerank_duration|aura.rerank.call.duration|aura_rerank_call_duration_seconds|histogram|s|outcome|Rerank call duration, including cache hits and degraded calls.|0.001,0.005,0.01,0.025,0.05,0.1,0.25,0.5,1,2.5,5,10,30,60
`

func TestCatalogDescriptorGolden(t *testing.T) {
	if got := CatalogManifest(); got != catalogGolden {
		t.Fatalf("catalog manifest drifted\n--- got ---\n%s\n--- want ---\n%s", got, catalogGolden)
	}
	if err := ValidateCatalog(); err != nil {
		t.Fatalf("ValidateCatalog: %v", err)
	}
}

func TestCatalogDescriptorsHaveOneOwnerAndFiniteAttributes(t *testing.T) {
	seenID := map[InstrumentID]bool{}
	seenOTel := map[string]bool{}
	seenProm := map[string]bool{}
	for _, descriptor := range Catalog() {
		if seenID[descriptor.ID] || seenOTel[descriptor.Name] || seenProm[descriptor.PrometheusName] {
			t.Fatalf("overlapping descriptor ownership: %+v", descriptor)
		}
		seenID[descriptor.ID] = true
		seenOTel[descriptor.Name] = true
		seenProm[descriptor.PrometheusName] = true
		for _, key := range descriptor.Attributes {
			if !IsBoundedAttribute(key) {
				t.Fatalf("descriptor %s has unbounded attribute %q", descriptor.ID, key)
			}
		}
	}
}

func TestCatalogNormalizersFoldUnknownAndSensitiveValuesToOther(t *testing.T) {
	sentinel := "tenant/request/op/tool/raw-error/secret-123"
	for _, key := range AllAttributeKeys() {
		got := NormalizeAttribute(key, sentinel)
		if got != ValueOther {
			t.Errorf("NormalizeAttribute(%q, sentinel) = %q, want other", key, got)
		}
		if strings.Contains(got, "secret") || strings.Contains(got, "request") {
			t.Errorf("NormalizeAttribute leaked sentinel as %q", got)
		}
	}

	toolCases := map[string]string{
		"ask_user":        ToolClassInteraction,
		"web_fetch":       ToolClassWeb,
		"shell_execute":   ToolClassShell,
		"filesystem_read": ToolClassFilesystem,
		"mcp.github.call": ToolClassMCP,
		"secret-tool-123": ValueOther,
	}
	for input, want := range toolCases {
		if got := ClassifyTool(input); got != want {
			t.Errorf("ClassifyTool(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCatalogPreservesBoundedIdempotencyConflictOutcome(t *testing.T) {
	if got := NormalizeAttribute(AttributeOutcome, "conflict"); got != "conflict" {
		t.Fatalf("idempotency conflict outcome normalized to %q, want conflict", got)
	}
}

func FuzzCatalogNormalizersRemainFinite(f *testing.F) {
	for _, seed := range []string{"", "success", "../secret", strings.Repeat("x", 4096), "mcp.github.call"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		for _, key := range AllAttributeKeys() {
			got := NormalizeAttribute(key, value)
			if !AllowedAttributeValue(key, got) {
				t.Fatalf("NormalizeAttribute(%q, %q) produced non-finite %q", key, value, got)
			}
		}
		if got := ClassifyTool(value); !AllowedAttributeValue(AttributeToolClass, got) {
			t.Fatalf("ClassifyTool(%q) produced non-finite %q", value, got)
		}
	})
}
