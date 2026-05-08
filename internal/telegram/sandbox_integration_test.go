package telegram_test

import "testing"

func TestSandboxIntegration_ExecuteAndSaveTool(t *testing.T) {
	t.Skip("integration test — requires full Aura stack with bundled Pyodide runtime")

	// Expected flow:
	// 1. User: "run a simulation of 100 dice rolls and compute the average"
	// 2. LLM calls execute_code with Python code for dice simulation
	// 3. Sandbox executes, returns result
	// 4. LLM answers user with the computed average
	// 5. LLM optionally calls save_tool to persist the dice simulator
}

func TestSandboxIntegration_ToolDiscovery(t *testing.T) {
	t.Skip("integration test — requires full Aura stack with bundled Pyodide runtime")

	// Expected flow:
	// 1. User uploads a CSV: "summarize this"
	// 2. LLM calls list_tools, finds csv_summarize from a previous session
	// 3. LLM calls read_tool to get the source
	// 4. LLM calls execute_code patterned after the existing tool
	// 5. Sandbox executes, returns summary
	// 6. LLM answers user
}
