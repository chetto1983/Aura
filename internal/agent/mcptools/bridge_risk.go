package mcptools

import (
	"log/slog"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/mcp"
)

type mcpActionClass uint8

const (
	mcpActionUnknown mcpActionClass = iota
	mcpActionRead
	mcpActionMutate
	mcpActionDestructive
)

const (
	calendarRecipeSource = "recipe:calendar"
	whatsAppRecipeSource = "recipe:whatsapp"
)

var trustedRecipeActions = map[string]map[string]mcpActionClass{
	calendarRecipeSource: {
		"list_accounts":              mcpActionRead,
		"get_emails":                 mcpActionRead,
		"get_email_details":          mcpActionRead,
		"search_emails":              mcpActionRead,
		"list_calendars":             mcpActionRead,
		"get_calendar_events":        mcpActionRead,
		"get_calendar_event_details": mcpActionRead,
		"get_contacts":               mcpActionRead,
		"search_contacts":            mcpActionRead,
		"get_contact_details":        mcpActionRead,
		"create_event":               mcpActionMutate,
		"update_event":               mcpActionMutate,
		"respond_to_event":           mcpActionDestructive,
		"send_email":                 mcpActionDestructive,
	},
	whatsAppRecipeSource: {
		"list_chats":                 mcpActionRead,
		"list_messages":              mcpActionRead,
		"search_contacts":            mcpActionRead,
		"get_contact":                mcpActionRead,
		"get_chat":                   mcpActionRead,
		"get_contact_chats":          mcpActionRead,
		"get_direct_chat_by_contact": mcpActionRead,
		"get_last_interaction":       mcpActionRead,
		"get_message_context":        mcpActionRead,
		"download_media":             mcpActionMutate,
		"send_audio_message":         mcpActionDestructive,
		"send_file":                  mcpActionDestructive,
		"send_message":               mcpActionDestructive,
		"send_reaction":              mcpActionDestructive,
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
		"graph_schema":          mcpActionRead,
		"memory_recall":         mcpActionRead,
		"memory_search":         mcpActionRead,
		"memory_entities":       mcpActionRead,
		"memory_facts_about":    mcpActionRead,
		"memory_digest":         mcpActionRead,
		"memory_upsert_fact":    mcpActionMutate,
		"memory_merge_entities": mcpActionMutate,
		"memory_reembed":        mcpActionMutate,
		"memory_forget":         mcpActionMutate,
	},
}

func managedBridgePolicy(server mcp.ManagedServer) bridgePolicy {
	policy := bridgePolicy{memory: mcp.IsSharedAdminGoverned(server)}
	_, trust, err := mcp.Classify(server)
	if err != nil {
		return policy
	}
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
func classifyToolRisk(policy bridgePolicy, t *sdkmcp.Tool) (mutating, destructive bool) {
	if actions := trustedRecipeActions[policy.recipeSource]; actions != nil {
		if action, ok := actions[t.Name]; ok {
			if explicitDestructive(t.Annotations) {
				return true, true
			}
			switch action {
			case mcpActionRead:
				return false, false
			case mcpActionMutate:
				return true, false
			case mcpActionDestructive:
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
