package display

// Status values a swarm ChildReport carries (mirror of swarm.Status*, report.go
// :18-23). Redeclared here for the same cycle-break reason ChildReport is (see
// payload.go) — the strings match the swarm package byte-for-byte.
const (
	StatusOK             = "ok"
	StatusFailed         = "failed"
	StatusNeedsUserInput = "needs_user_input"
)

// normalizeSwarm maps a swarm_spawn result ([]ChildReport) to a swarm_report
// Payload (SWARM-01 / D-08), carrying every ChildReport field (status/summary/
// error/question/options) so the cockpit table can expand a row without a
// round-trip. ZERO new backend — the type mirrors swarm.ChildReport. An empty
// report set is still a recognized (true) swarm_report.
func normalizeSwarm(toolCallID string, reports []ChildReport) (Payload, bool) {
	out := make([]ChildReport, len(reports))
	copy(out, reports)
	return Payload{
		Type:       KindSwarmReport,
		ToolCallID: toolCallID,
		Swarm:      out,
	}, true
}
