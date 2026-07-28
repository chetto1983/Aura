package config

// Built-in defaults: the bottom tier of the load order (default → .env →
// ~/.aura/llm.json → AURA_* env, with aura.settings rows overlaid onto the env at
// daemon boot). They live together, apart from Load's plumbing, because a default
// is a decision that needs its reasoning next to it — not a literal buried in an
// envutil call three hundred lines down.

// OTel exporter knob defaults (D-05/D-06). The default OTLP target silent-drops
// without a collector; "none" is a true no-op provider.
//
// The endpoint is localhost on purpose and must stay that way for the compose
// deployment: tempo and prometheus run with `network_mode: "service:aura"`, i.e.
// INSIDE Aura's network namespace, so they are reachable on loopback and NOT by
// service DNS. (Changing this to `tempo:4317` fails with "produced zero addresses".)
const (
	defaultOtelExporter         = "otlp"
	defaultOtelEndpoint         = "localhost:4317"
	defaultObjectStoreAccessKey = "GK000000000000000000000000"
	defaultObjectStoreSecretKey = "0000000000000000000000000000000000000000000000000000000000000000"
)

// defaultToolPreviewCapBytes is how much of a tool result reaches the model before
// the rest spills to a sidecar it must page back with read_tool_output.
//
// It was 2048. Measured on the live deployment (aura.tool_invocations, n=185): the
// MEDIAN result is 233 bytes, yet 26% of calls were truncated — so the old cap did
// nothing for the common case and taxed only the tail, buying an extra model
// round-trip each time. p90 is 6.2 KB and p99 is 55 KB, so 30000 admits the whole
// body of an ordinary call and still spills the genuinely large ones.
//
// The number is Claude Code's own Bash ceiling ("if the output exceeds 30000
// characters, output will be truncated"), which is the closest analog: arbitrary
// tool output entering a context window. Paging should be the exception, not the
// normal path.
const defaultToolPreviewCapBytes = 30000
