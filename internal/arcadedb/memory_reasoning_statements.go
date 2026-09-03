package arcadedb

// Every SQL statement the reasoning graph reads and writes.
//
// Split out of memory_reasoning.go on touch (600-LOC cap), and they belong together for a
// reason beyond size: the graph they build is a shape, and reading the shape means reading
// these constants side by side. INITIATED_BY in particular is created from BOTH ends --
// once here from the trace and once from the conversation projector -- and that pairing is
// invisible unless the statements sit in one place.

const upsertReasoningTraceStatement = "UPDATE " + reasoningTraceType +
	" SET identity_id = :identity_id, trace_id = :trace_id, source_ref = :source_ref," +
	" conversation_id = :conversation_id, turn_seq = :turn_seq," +
	" provider_summary = :provider_summary, status = :status, created_at = :created_at," +
	" terminal_at = :terminal_at, expires_at = :expires_at"

const reasoningTraceWhere = " UPSERT RETURN AFTER WHERE identity_id = :identity_id AND trace_id = :trace_id"

const clearReasoningEmbeddingStatement = "UPDATE " + reasoningTraceType +
	" REMOVE embedding WHERE identity_id = :identity_id AND trace_id = :trace_id"

const createReasoningInitiatorStatement = "CREATE EDGE INITIATED_BY" +
	" FROM (SELECT FROM " + reasoningTraceType + " WHERE identity_id = :identity_id AND trace_id = :trace_id)" +
	" TO (SELECT FROM " + conversationTurnType + " WHERE identity_id = :identity_id" +
	" AND conversation_id = :conversation_id AND turn_seq = :turn_seq) IF NOT EXISTS"

// linkReasoningInitiatorFromTurnStatement is the SAME edge as
// createReasoningInitiatorStatement, attempted from the other end.
//
// It exists because the one-sided version never once succeeded. A reasoning trace is
// written synchronously the moment the answer commits (runner.commitSourceTurn), while the
// ConversationTurn vertex it points at is created by the conversation projector, which is
// only OFFERED to a queue at that point. So the trace always arrives first, the TO side
// selects nothing, and ArcadeDB creates zero edges WITHOUT an error -- measured 2026-09-03
// on a live memory: 89 traces, 0 INITIATED_BY, and the turn one trace named present in the
// graph all along, just later.
//
// Nothing repaired it either: reconciliation replays the TURN projection, and no pass ever
// re-attempted the edge. Attempting it from both ends makes the link eventually consistent
// whichever side lands first, and IF NOT EXISTS keeps the second attempt free.
const linkReasoningInitiatorFromTurnStatement = "CREATE EDGE INITIATED_BY" +
	" FROM (SELECT FROM " + reasoningTraceType + " WHERE identity_id = :identity_id" +
	" AND conversation_id = :conversation_id AND turn_seq = :turn_seq)" +
	" TO (SELECT FROM " + conversationTurnType + " WHERE identity_id = :identity_id" +
	" AND conversation_id = :conversation_id AND turn_seq = :turn_seq) IF NOT EXISTS"

const upsertReasoningStepStatement = "UPDATE " + reasoningStepType +
	" SET identity_id = :identity_id, trace_id = :trace_id, step_index = :step_index," +
	" provider_summary = :provider_summary, created_at = :created_at" +
	" UPSERT RETURN AFTER WHERE identity_id = :identity_id AND trace_id = :trace_id AND step_index = :step_index"

const createReasoningStepStatement = "CREATE EDGE HAS_STEP" +
	" FROM (SELECT FROM " + reasoningTraceType + " WHERE identity_id = :identity_id AND trace_id = :trace_id)" +
	" TO (SELECT FROM " + reasoningStepType + " WHERE identity_id = :identity_id" +
	" AND trace_id = :trace_id AND step_index = :step_index) IF NOT EXISTS"

const createNextReasoningStepStatement = "CREATE EDGE NEXT" +
	" FROM (SELECT FROM " + reasoningStepType + " WHERE identity_id = :identity_id" +
	" AND trace_id = :trace_id AND step_index < :step_index ORDER BY step_index DESC LIMIT 1)" +
	" TO (SELECT FROM " + reasoningStepType + " WHERE identity_id = :identity_id" +
	" AND trace_id = :trace_id AND step_index = :step_index) IF NOT EXISTS"

const upsertReasoningToolStatement = "UPDATE " + reasoningToolCallType +
	" SET identity_id = :identity_id, trace_id = :trace_id, call_id = :call_id," +
	" tool_name = :tool_name, status = :status, duration_ms = :duration_ms," +
	" argument_digest = :argument_digest, observation = :observation," +
	" artifact_refs = :artifact_refs, entity_refs = :entity_refs, source_ref = :source_ref" +
	" UPSERT RETURN AFTER WHERE identity_id = :identity_id AND trace_id = :trace_id AND call_id = :call_id"

const createInvokedReasoningToolStatement = "CREATE EDGE INVOKED" +
	" FROM (SELECT FROM " + reasoningStepType + " WHERE identity_id = :identity_id" +
	" AND trace_id = :trace_id AND step_index = :step_index)" +
	" TO (SELECT FROM " + reasoningToolCallType + " WHERE identity_id = :identity_id" +
	" AND trace_id = :trace_id AND call_id = :call_id) IF NOT EXISTS"

const createReasoningTouchedStatement = "CREATE EDGE TOUCHED" +
	" FROM (SELECT FROM " + reasoningStepType + " WHERE identity_id = :identity_id" +
	" AND trace_id = :trace_id AND step_index = :step_index)" +
	" TO (SELECT FROM Entity WHERE name = :entity_name) IF NOT EXISTS"

const reasoningTraceFields = "@rid, identity_id, trace_id, source_ref, conversation_id," +
	" turn_seq, provider_summary, status, created_at, terminal_at, expires_at"

const activeReasoningTraceFilter = " AND (expires_at IS NULL OR expires_at > :now)"

const searchReasoningLexicalStatement = "SELECT " + reasoningTraceFields + " FROM " + reasoningTraceType +
	" WHERE identity_id = :identity_id" + activeReasoningTraceFilter +
	" AND SEARCH_INDEX('" + reasoningTraceType + "[provider_summary]', :query) = true" +
	" ORDER BY $score DESC LIMIT :limit"

const searchReasoningHybridStatement = "SELECT @rid AS rid FROM (SELECT expand(`vector.fuse`(" +
	"`vector.neighbors`('" + reasoningTraceType + "[embedding]', :vector, :candidates," +
	" { filter: (SELECT @rid FROM " + reasoningTraceType + " WHERE identity_id = :identity_id" +
	activeReasoningTraceFilter + ").@rid })," +
	" (SELECT @rid, $score FROM " + reasoningTraceType + " WHERE identity_id = :identity_id" +
	activeReasoningTraceFilter + " AND SEARCH_INDEX('" + reasoningTraceType +
	"[provider_summary]', :query) = true LIMIT :candidates), { fusion: 'RRF' }))) LIMIT :candidates"

const hydrateReasoningTracesStatement = "SELECT " + reasoningTraceFields + " FROM " + reasoningTraceType +
	" WHERE identity_id = :identity_id" + activeReasoningTraceFilter + " AND @rid IN :rids"

const getReasoningTraceStatement = "SELECT " + reasoningTraceFields + " FROM " + reasoningTraceType +
	" WHERE identity_id = :identity_id AND trace_id = :trace_id" + activeReasoningTraceFilter + " LIMIT 1"

const getReasoningStepsStatement = "SELECT identity_id, trace_id, step_index, provider_summary, created_at" +
	" FROM " + reasoningStepType + " WHERE identity_id = :identity_id AND trace_id = :trace_id" +
	" ORDER BY step_index LIMIT 64"

const getReasoningToolsStatement = "SELECT outV().step_index AS step_index," +
	" inV().identity_id AS identity_id, inV().trace_id AS trace_id, inV().call_id AS call_id," +
	" inV().tool_name AS tool_name, inV().status AS status, inV().duration_ms AS duration_ms," +
	" inV().argument_digest AS argument_digest, inV().observation AS observation," +
	" inV().artifact_refs AS artifact_refs, inV().entity_refs AS entity_refs," +
	" inV().source_ref AS source_ref FROM INVOKED" +
	" WHERE inV().identity_id = :identity_id AND inV().trace_id = :trace_id" +
	" ORDER BY step_index, call_id LIMIT 2048"
