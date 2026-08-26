package mcptools

import (
	"log/slog"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/mcp"
)

// MCPActionClass is the per-action risk tier a trusted-recipe MCP source's
// action table assigns. Exported (renamed from the unexported mcpActionClass) so
// internal/gateway's per-action classifier can map it straight to a
// scoring.RiskTier without inventing a second vocabulary (D-21).
type MCPActionClass uint8

// The four MCPActionClass values, in ascending risk order: MCPActionUnknown is
// the fail-closed zero value (an action absent from the table, or a tool that is
// not a known curated multiplex), MCPActionRead never gates, MCPActionMutate
// gates as an ordinary reversible write, and MCPActionDestructive gates as an
// externally irreversible action.
const (
	MCPActionUnknown MCPActionClass = iota
	MCPActionRead
	MCPActionMutate
	MCPActionDestructive
)

const (
	calendarRecipeSource = "recipe:calendar"
	whatsAppRecipeSource = "recipe:whatsapp"
)

// trustedRecipeActions is keyed by whatever discriminator that source's surface
// exposes — the keys are NOT uniformly "the raw MCP tool name" (D-35). calendar
// is ACTION-keyed: its 14 keys already equal the fork's curated `action` enum
// values (D-21/46-05), so no relabeling was needed here — this half of the table
// is now read by internal/gateway's classifyCalendarAction via
// mcptools.MCPActionClassFor, NOT by classifyToolRisk's t.Name lookup below,
// because the curated tool's own advertised raw name ("calendar") never appears
// as a key in this map. whatsapp stays RAW-TOOL-NAME-keyed for now (plan 46-08
// re-keys it alongside its own image pin); memory stays RAW-TOOL-NAME-keyed
// permanently, because it is not merged — each memory_* call is its own MCP tool
// name, still classified by classifyToolRisk's t.Name lookup unchanged. One
// table, one lookup site below, mixed key spaces documented here so a future
// reader does not assume a uniformity that was never true.
var trustedRecipeActions = map[string]map[string]MCPActionClass{
	calendarRecipeSource: {
		"list_accounts":              MCPActionRead,
		"get_emails":                 MCPActionRead,
		"get_email_details":          MCPActionRead,
		"search_emails":              MCPActionRead,
		"list_calendars":             MCPActionRead,
		"get_calendar_events":        MCPActionRead,
		"get_calendar_event_details": MCPActionRead,
		"get_contacts":               MCPActionRead,
		"search_contacts":            MCPActionRead,
		"get_contact_details":        MCPActionRead,
		"create_event":               MCPActionMutate,
		"update_event":               MCPActionMutate,
		"mark_email_read":            MCPActionMutate,
		"respond_to_event":           MCPActionDestructive,
		"send_email":                 MCPActionDestructive,
		"delete_email":               MCPActionDestructive,
		// move_email is Destructive even though a move is nominally reversible,
		// because 'trash' is one of its accepted destinations: tiering it Mutate
		// would let move_email(destination:"trash") reach the same outcome as
		// delete_email while bypassing delete_email's approval gate, making that
		// gate decorative. Same reasoning as escalate-only — the tier follows the
		// worst reachable outcome, not the usual one.
		"move_email": MCPActionDestructive,

		// The twelve the fork's first curation round left unreachable. Upstream
		// serves 29 tools; the curated enum published 17, and the difference was
		// not cosmetic -- delete_event was missing while create_event and
		// update_event were present, so an agent could create an event and never
		// remove it. Tiered here on the same escalate-only rule as their siblings.
		"get_guide":                    MCPActionRead,
		"get_unsubscribe_info":         MCPActionRead,
		"get_email_attachment":         MCPActionRead,
		"get_contextual_email_summary": MCPActionRead,
		"create_contact":               MCPActionMutate,
		"update_contact":               MCPActionMutate,
		"bulk_mark_emails_read":        MCPActionMutate,
		"delete_event":                 MCPActionDestructive,
		"delete_contact":               MCPActionDestructive,
		"bulk_delete_emails":           MCPActionDestructive,
		// bulk_move_emails inherits move_email's reasoning exactly: 'trash' is an
		// accepted destination, so tiering it Mutate would route around
		// bulk_delete_emails' gate. unsubscribe_from_email is Destructive because it
		// makes an outward request to a third party on the operator's behalf, and no
		// second call takes it back.
		"bulk_move_emails":       MCPActionDestructive,
		"unsubscribe_from_email": MCPActionDestructive,
	},
	whatsAppRecipeSource: {
		"list_chats":                 MCPActionRead,
		"list_messages":              MCPActionRead,
		"search_contacts":            MCPActionRead,
		"get_contact":                MCPActionRead,
		"get_chat":                   MCPActionRead,
		"get_contact_chats":          MCPActionRead,
		"get_direct_chat_by_contact": MCPActionRead,
		"get_last_interaction":       MCPActionRead,
		"get_message_context":        MCPActionRead,
		// Read, and deliberately so, even though it warms the bridge's media cache on
		// the way: it sends nothing, alters no remote state, and returns bytes the
		// caller can already read the message for. That classification is what lets a
		// rendered view fetch a photo at all (CallForView admits read-only tools only),
		// which is the whole reason the tool exists — download_media next to it hands
		// back a container path no browser can open, so a view could only ever show a
		// placeholder where the picture belongs. The incidental write is a cache, not
		// an effect the user would want approval over.
		"get_media_data":     MCPActionRead,
		"download_media":     MCPActionMutate,
		"send_audio_message": MCPActionDestructive,
		"send_file":          MCPActionDestructive,
		"send_message":       MCPActionDestructive,
		"send_reaction":      MCPActionDestructive,
	},
	// The tools cmd/arcadedb-mcp actually serves. The names here were the
	// PREVIOUS memory server's — memory_add_fact, memory_add_entity, memory_update,
	// memory_add_preference, memory_create_relationship, memory_get_entity — and
	// none of them exists any more. A tool absent from this table falls through to
	// the unannotated default in mcpToolRisk, which is `return true, true`: so
	// every write to her own memory was classified Destructive and stopped the turn
	// to ask the operator whether Aura may remember something. Seen live in the
	// cockpit on 2026-08-03 on memory_upsert_fact.
	//
	// Memory mutations execute in the requesting turn. The server still refuses an
	// empty forget filter, isolates tenants, and audits the call; the gateway must not
	// turn an explicit memory operation into a second confirmation loop.
	mcp.SourceRecipeMemory: {
		"graph_schema":          MCPActionRead,
		"memory_recall":         MCPActionRead,
		"memory_search":         MCPActionRead,
		"memory_entities":       MCPActionRead,
		"memory_facts_about":    MCPActionRead,
		"memory_digest":         MCPActionRead,
		"memory_upsert_fact":    MCPActionMutate,
		"memory_merge_entities": MCPActionMutate,
		"memory_reembed":        MCPActionMutate,
		"memory_forget":         MCPActionMutate,
	},
}

func managedBridgePolicy(server mcp.ManagedServer) bridgePolicy {
	policy := bridgePolicy{memorySurface: mcp.IsSharedAdminGoverned(server)}
	serverType, trust, err := mcp.Classify(server)
	if err != nil {
		return policy
	}
	settings, oauthErr := mcp.OAuthSettingsFromEnv(server.Env)
	policy.identityScoped = serverType == mcp.ServerTypeStreamableHTTP && oauthErr == nil && mcp.UsesOAuth(server, settings)
	// Rendering a server's own document is a larger grant than calling its tools,
	// so it is gated on the trust class and on nothing else (mcp.TrustMayRenderViews).
	policy.views = mcp.TrustMayRenderViews(trust)
	if trust != mcp.TrustTrustedRecipe {
		return policy
	}
	source := strings.TrimSpace(server.Source)
	if _, ok := trustedRecipeActions[source]; ok {
		policy.recipeSource = source
	}
	return policy
}

// mcpToolRisk classifies risk from the SDK's own *sdkmcp.Tool. t.Annotations is a
// POINTER (unlike the deleted mcp.ToolDef.Annotations, which was a value) and may
// be nil — a nil Annotations must take the same branch an unannotated ToolDef
// took: the fail-closed `return true, true` default, asserted below.
func mcpToolRisk(policy bridgePolicy, t *sdkmcp.Tool) (mutating, destructive bool) {
	mutating, destructive = classifyToolRisk(policy, t)
	logToolAnnotations(t.Name, t.Annotations, mutating, destructive)
	return mutating, destructive
}

// classifyToolRisk is the decision itself, kept separate from the logging so every
// branch can return directly and the log record is emitted exactly once.
//
// For the calendar curated tool this lookup deliberately MISSES: t.Name is the
// curated tool's own raw name ("calendar"), which is never a key in
// trustedRecipeActions[calendarRecipeSource] (that map is keyed by ACTION, not
// by the tool's own name) — so it falls through to this function's fail-closed
// default below, landing Mutating:true. That is deliberate, not a gap: bridge.go's
// specFromToolDefWithPolicy also sets Multiplexed:true for it, and
// internal/gateway's per-action classifier (not this function) is what actually
// grades each call by its `action` argument (D-21).
func classifyToolRisk(policy bridgePolicy, t *sdkmcp.Tool) (mutating, destructive bool) {
	if actions := trustedRecipeActions[policy.recipeSource]; actions != nil {
		if action, ok := actions[t.Name]; ok {
			if explicitDestructive(t.Annotations) {
				return true, true
			}
			switch action {
			case MCPActionRead:
				return false, false
			case MCPActionMutate:
				return true, false
			case MCPActionDestructive:
				return true, true
			}
		}
	}

	// a is captured once so every read below goes through this single pointer —
	// no scattered `t.Annotations.X` dot chains that would panic on a nil pointer
	// if the guard above ever moved.
	a := t.Annotations
	if a == nil {
		return true, true
	}
	if a.ReadOnlyHint && !explicitDestructive(a) {
		// Reading an open world is still reading: the two new hints cannot move a
		// declared read-only tool, and must not be allowed to.
		return false, false
	}
	if a.DestructiveHint == nil {
		return true, true
	}
	// The one escalation D-107 adds. It sits AFTER the read-only and nil-destructive
	// cases so it can only ever raise a tier, never lower one, and only in the
	// fallback branch — a recipe-listed tool never reaches here.
	if unsafeToRepeatBeyondAura(a) {
		return true, true
	}
	return true, *a.DestructiveHint
}

// logToolAnnotations makes the adoption visible in a boot log, so an operator can see
// the two-hint truncation is gone without reading code.
func logToolAnnotations(name string, a *sdkmcp.ToolAnnotations, mutating, destructive bool) {
	slog.Debug("mcp tool annotations",
		"tool", name,
		"read_only", a != nil && a.ReadOnlyHint,
		"destructive", explicitDestructive(a),
		"idempotent", annotationIdempotent(a),
		"open_world", annotationOpenWorld(a),
		"mutating", mutating,
		"tier_destructive", destructive,
	)
}

func explicitDestructive(annotations *sdkmcp.ToolAnnotations) bool {
	return annotations != nil && annotations.DestructiveHint != nil && *annotations.DestructiveHint
}

// annotationIdempotent reads IdempotentHint, whose documented default is FALSE
// (go-sdk@v1.7.0 mcp/protocol.go, ToolAnnotations struct, field IdempotentHint:
// "Default: false"). It is a plain bool, so the zero value already is the default;
// the reader exists so every hint is read through one named place.
func annotationIdempotent(a *sdkmcp.ToolAnnotations) bool {
	return a != nil && a.IdempotentHint
}

// annotationOpenWorld reads OpenWorldHint, whose documented default is TRUE
// (go-sdk@v1.7.0 mcp/protocol.go, ToolAnnotations struct, field OpenWorldHint:
// "Default: true").
//
// The default is counter-intuitive and is the whole reason this reader exists: an
// ABSENT pointer means the MORE dangerous reading, not the safer one. Taking Go's
// zero value here would silently treat every unannotated tool as closed-world.
func annotationOpenWorld(a *sdkmcp.ToolAnnotations) bool {
	if a == nil || a.OpenWorldHint == nil {
		return true
	}
	return *a.OpenWorldHint
}

// This predicate names the condition the new escalation turns on: a tool that
// writes (not read-only), whose repetition is not declared harmless (not
// idempotent), and whose effect lands outside anything Aura can undo (open world).
//
// Non-idempotent means a redial-and-reissue can never be proven safe; open-world means
// the effect is beyond Aura's reach once it has happened. Together they describe a call
// that deserves the operator's eyes even though the server declined to call it
// destructive.
func unsafeToRepeatBeyondAura(a *sdkmcp.ToolAnnotations) bool {
	return a != nil && !a.ReadOnlyHint && !annotationIdempotent(a) && annotationOpenWorld(a)
}
