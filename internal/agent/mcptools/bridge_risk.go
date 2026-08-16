package mcptools

import (
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
	if err != nil || trust != mcp.TrustTrustedRecipe {
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

	if t.Annotations == nil {
		return true, true
	}
	if t.Annotations.ReadOnlyHint && !explicitDestructive(t.Annotations) {
		return false, false
	}
	if t.Annotations.DestructiveHint == nil {
		return true, true
	}
	return true, *t.Annotations.DestructiveHint
}

func explicitDestructive(annotations *sdkmcp.ToolAnnotations) bool {
	return annotations != nil && annotations.DestructiveHint != nil && *annotations.DestructiveHint
}
