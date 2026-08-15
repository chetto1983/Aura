package agent

// Detection for the two user-facing failures 45-08 measured on the running stack
// (45-09): deliberation that leaked into the reply, and an identifier the reply
// presents as real that no tool ever returned.
//
// 45-05 closed D-21 with a system-prompt rule. That rule is present in the shipped
// binary -- verified by grep inside aura:local at build ed252f6b6 -- and the model
// violated it anyway on a long verbatim-report turn, in English, to an operator
// writing Italian, while inventing fact_key values it then admitted inventing in the
// same visible text. A rule the model is asked to follow is necessary and, measured,
// not sufficient; this is the part the harness can check itself.

// leakedDeliberation reports the first self-directed deliberation marker found in an
// operator-facing reply.
//
// RED stub: reports nothing, so the call site compiles and today's behaviour is
// unchanged until the GREEN commit gives it a body.
func leakedDeliberation(answer string) (marker string, leaked bool) {
	return "", false
}

// unsourcedIdentifiers returns fact_key-shaped tokens (64 lowercase hex) that appear
// in the reply but in none of the turn's tool results.
//
// RED stub: reports nothing.
func unsourcedIdentifiers(answer string, toolResults []string) []string {
	return nil
}
