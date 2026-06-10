package agent

import "expvar"

var (
	metricBudgetConsumeStepTotal = expvar.NewInt("aura_agent_budget_consume_step_total")
	metricToolDispatchTotal      = expvar.NewInt("aura_agent_tool_dispatch_total")
	metricLLMStreamOpenTotal     = expvar.NewInt("aura_agent_llm_stream_open_total")
	metricLLMStreamRetryTotal    = expvar.NewInt("aura_agent_llm_stream_retry_total")
)
