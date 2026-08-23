package mcptools

import (
	"slices"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestMCPRiskDefaultsAreConservativeWithoutOverridingReads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		annotations     *sdkmcp.ToolAnnotations
		wantMutating    bool
		wantDestructive bool
	}{
		{
			name:        "read only",
			annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
		},
		{
			name:            "unannotated write defaults destructive",
			annotations:     nil,
			wantMutating:    true,
			wantDestructive: true,
		},
		{
			// D-107 changed this case deliberately. Before the four-hint adoption a
			// bare DestructiveHint:false was enough to earn the mutating tier. It no
			// longer is: IdempotentHint defaults to false and OpenWorldHint defaults
			// to true, so a tool that declares only "not destructive" is still an
			// unrepeatable write landing outside anything Aura can undo, and
			// unsafeToRepeatBeyondAura escalates it. Untrusted third-party servers
			// must not be able to talk themselves out of an approval gate by
			// asserting the single cheapest hint.
			name:            "additive write that is neither repeatable nor closed-world escalates",
			annotations:     &sdkmcp.ToolAnnotations{DestructiveHint: new(bool)},
			wantMutating:    true,
			wantDestructive: true,
		},
		{
			// The other half of the same rule, and the reason the case above is an
			// escalation rather than a blanket ceiling: DestructiveHint:false is
			// still honoured when the server actually earns it by declaring the call
			// repeatable. Without this case the pair above would only prove the tier
			// went up, not that a server can still reach the mutating tier at all.
			name:         "additive write declared idempotent keeps the mutating tier",
			annotations:  &sdkmcp.ToolAnnotations{DestructiveHint: new(bool), IdempotentHint: true},
			wantMutating: true,
		},
		{
			name:            "contradictory read and destructive saturates upward",
			annotations:     &sdkmcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(true)},
			wantMutating:    true,
			wantDestructive: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := specFromToolDef("third_party", &sdkmcp.Tool{Name: "action", Annotations: tt.annotations})
			if spec.Mutating != tt.wantMutating || spec.Destructive != tt.wantDestructive {
				t.Fatalf("risk = mutating:%v destructive:%v, want %v/%v",
					spec.Mutating, spec.Destructive, tt.wantMutating, tt.wantDestructive)
			}
		})
	}
}

func TestTrustedRecipeRiskPolicyIsGraduated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		source          string
		tool            string
		wantMutating    bool
		wantDestructive bool
	}{
		{name: "calendar read", source: "recipe:calendar", tool: "get_emails"},
		{name: "calendar reversible write", source: "recipe:calendar", tool: "update_event", wantMutating: true},
		{name: "calendar external send", source: "recipe:calendar", tool: "send_email", wantMutating: true, wantDestructive: true},
		{name: "calendar external response", source: "recipe:calendar", tool: "respond_to_event", wantMutating: true, wantDestructive: true},
		{name: "memory read", source: mcp.SourceRecipeMemory, tool: "memory_recall"},
		{name: "memory ordinary write", source: mcp.SourceRecipeMemory, tool: "memory_upsert_fact", wantMutating: true},
		{name: "memory hygiene", source: mcp.SourceRecipeMemory, tool: "memory_merge_entities", wantMutating: true},
		{name: "memory erase without approval loop", source: mcp.SourceRecipeMemory, tool: "memory_forget", wantMutating: true},
		{name: "whatsapp read", source: "recipe:whatsapp", tool: "list_messages"},
		{name: "whatsapp local download", source: "recipe:whatsapp", tool: "download_media", wantMutating: true},
		// Read despite the cache write it performs, because a view may only call
		// read-only tools and this one exists so a rendered panel can show a photo
		// instead of a placeholder. Sibling download_media stays mutating: it hands
		// back a container path, which is an effect without a reader.
		{name: "whatsapp inline media for a view", source: "recipe:whatsapp", tool: "get_media_data"},
		{name: "whatsapp external send", source: "recipe:whatsapp", tool: "send_message", wantMutating: true, wantDestructive: true},
		{name: "unknown recipe tool fails closed", source: "recipe:calendar", tool: "new_side_effect", wantMutating: true, wantDestructive: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			policy := managedBridgePolicy(mcp.ManagedServer{
				Source: tt.source,
				Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			})
			spec := specFromToolDefWithPolicy("connector", &sdkmcp.Tool{Name: tt.tool}, policy)
			if spec.Mutating != tt.wantMutating || spec.Destructive != tt.wantDestructive {
				t.Fatalf("%s risk = mutating:%v destructive:%v, want %v/%v",
					tt.tool, spec.Mutating, spec.Destructive, tt.wantMutating, tt.wantDestructive)
			}
		})
	}
}

// TestMemoryRecipeCoversEveryServedTool is the tripwire the previous table
// lacked. A name that drifts out of this map does not fail loudly — it falls
// through to the unannotated default in mcpToolRisk, `return true, true`, and
// quietly becomes a Destructive action that stops the turn.
func TestMemoryRecipeCoversEveryServedTool(t *testing.T) {
	t.Parallel()
	served := []string{
		"graph_schema", "memory_digest", "memory_entities", "memory_facts_about", "memory_forget",
		"memory_merge_entities", "memory_recall", "memory_reembed", "memory_search", "memory_upsert_fact",
	}
	table := trustedRecipeActions[mcp.SourceRecipeMemory]
	for _, name := range served {
		if _, ok := table[name]; !ok {
			t.Errorf("%s is served but unclassified — it silently becomes Destructive and stops the turn", name)
		}
	}
	for name := range table {
		if !slices.Contains(served, name) {
			t.Errorf("%s is classified but no longer served — the table is describing a server that is gone", name)
		}
	}
}

func TestRecipeOverridesRequireTrustedRecipeIdentity(t *testing.T) {
	t.Parallel()

	server := mcp.ManagedServer{
		Source: "recipe:calendar",
		Trust:  mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
	}
	spec := specFromToolDefWithPolicy(
		"calendar",
		&sdkmcp.Tool{Name: "get_emails"},
		managedBridgePolicy(server),
	)
	if !spec.Mutating || !spec.Destructive {
		t.Fatalf("untrusted server spoofed recipe policy: %+v", spec)
	}
}

func TestExplicitDestructiveHintCanOnlyRaiseTrustedRecipeRisk(t *testing.T) {
	t.Parallel()

	policy := managedBridgePolicy(mcp.ManagedServer{
		Source: "recipe:calendar",
		Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
	})
	spec := specFromToolDefWithPolicy("calendar", &sdkmcp.Tool{
		Name:        "update_event",
		Annotations: &sdkmcp.ToolAnnotations{DestructiveHint: new(true)},
	}, policy)
	if !spec.Mutating || !spec.Destructive {
		t.Fatalf("explicit destructive hint lowered by recipe policy: %+v", spec)
	}
}

// TestMCPToolRisk_NilAnnotationsFailsClosed pins T-45.1-09: a nil *ToolAnnotations
// (new — the deleted mcp.ToolDef.Annotations was a value, never nil) must take
// the SAME fail-closed branch an unannotated tool took before the swap.
func TestMCPToolRisk_NilAnnotationsFailsClosed(t *testing.T) {
	t.Parallel()
	mutating, destructive := mcpToolRisk(bridgePolicy{}, &sdkmcp.Tool{Name: "x", Annotations: nil})
	if !mutating || !destructive {
		t.Fatalf("nil Annotations must fail closed to mutating:true destructive:true, got %v/%v", mutating, destructive)
	}
}

// --- Task 1: the four hint readers, each honouring the SDK's documented default ---

func TestAnnotationIdempotentDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a    *sdkmcp.ToolAnnotations
		want bool
	}{
		{"nil annotations", nil, false},
		{"zero-value annotations", &sdkmcp.ToolAnnotations{}, false},
		{"explicit true", &sdkmcp.ToolAnnotations{IdempotentHint: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := annotationIdempotent(tt.a); got != tt.want {
				t.Errorf("annotationIdempotent(%+v) = %v, want %v", tt.a, got, tt.want)
			}
		})
	}
}

// TestAnnotationOpenWorldDefaults pins the counter-intuitive default: an ABSENT
// OpenWorldHint pointer means the MORE dangerous reading (true), not Go's zero
// value (false). go-sdk@v1.7.0 mcp/protocol.go documents OpenWorldHint's default
// as true.
func TestAnnotationOpenWorldDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a    *sdkmcp.ToolAnnotations
		want bool
	}{
		{"nil annotations means the documented default, true", nil, true},
		{"zero-value annotations means the documented default, true", &sdkmcp.ToolAnnotations{}, true},
		{"explicit false", &sdkmcp.ToolAnnotations{OpenWorldHint: new(false)}, false},
		{"explicit true", &sdkmcp.ToolAnnotations{OpenWorldHint: new(true)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := annotationOpenWorld(tt.a); got != tt.want {
				t.Errorf("annotationOpenWorld(%+v) = %v, want %v", tt.a, got, tt.want)
			}
		})
	}
}

func TestExplicitDestructiveNilSafe(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a    *sdkmcp.ToolAnnotations
		want bool
	}{
		{"nil annotations", nil, false},
		{"nil DestructiveHint", &sdkmcp.ToolAnnotations{}, false},
		{"explicit false", &sdkmcp.ToolAnnotations{DestructiveHint: new(false)}, false},
		{"explicit true", &sdkmcp.ToolAnnotations{DestructiveHint: new(true)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := explicitDestructive(tt.a); got != tt.want {
				t.Errorf("explicitDestructive(%+v) = %v, want %v", tt.a, got, tt.want)
			}
		})
	}
}

// TestUnsafeToRepeatBeyondAura hard-codes the expected outcome per case rather
// than re-deriving the formula inline — a test that recomputes the same
// boolean expression as the code under test cannot catch a mistake shared by
// both.
func TestUnsafeToRepeatBeyondAura(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a    *sdkmcp.ToolAnnotations
		want bool
	}{
		{"nil annotations", nil, false},
		{"read-only excludes it regardless of the other two hints", &sdkmcp.ToolAnnotations{ReadOnlyHint: true}, false},
		{"idempotent excludes it", &sdkmcp.ToolAnnotations{IdempotentHint: true}, false},
		{"closed world excludes it", &sdkmcp.ToolAnnotations{OpenWorldHint: new(false)}, false},
		{"zero-value: non-idempotent, open-world (default) write is unsafe", &sdkmcp.ToolAnnotations{}, true},
		{"explicit open world, non-idempotent write is unsafe", &sdkmcp.ToolAnnotations{OpenWorldHint: new(true)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := unsafeToRepeatBeyondAura(tt.a); got != tt.want {
				t.Errorf("unsafeToRepeatBeyondAura(%+v) = %v, want %v", tt.a, got, tt.want)
			}
		})
	}
}

// --- Task 2: escalate-only, proven exhaustively over all 36 hint combinations ---

// riskTier orders the three classification outcomes so "never lower" can be
// asserted as a numeric comparison. (false,true) is not a state classifyToolRisk
// ever produces, but is folded into the destructive tier so the ordering stays
// total.
func riskTier(mutating, destructive bool) int {
	switch {
	case !mutating && !destructive:
		return 0
	case mutating && !destructive:
		return 1
	default:
		return 2
	}
}

// frozenTwoHintBaseline reproduces, verbatim, the fallback branch's semantics
// BEFORE D-107's escalation was added: read-only-and-not-explicitly-destructive
// wins first, then a nil DestructiveHint fails closed, then the explicit hint's
// value is trusted as-is. It deliberately never looks at IdempotentHint or
// OpenWorldHint — that is the point: it is the frozen "before" picture the new
// four-hint classifier is proven never to fall below.
func frozenTwoHintBaseline(a *sdkmcp.ToolAnnotations) (mutating, destructive bool) {
	if a == nil {
		return true, true
	}
	if a.ReadOnlyHint && !explicitDestructive(a) {
		return false, false
	}
	if a.DestructiveHint == nil {
		return true, true
	}
	return true, *a.DestructiveHint
}

// allAnnotationCombinations builds the 36-member cartesian product D-107's
// invariant must hold over: ReadOnlyHint x IdempotentHint x OpenWorldHint x
// DestructiveHint, with the two pointer hints ranging over {nil, false, true}.
func allAnnotationCombinations() []*sdkmcp.ToolAnnotations {
	readOnly := []bool{false, true}
	idempotent := []bool{false, true}
	openWorld := []*bool{nil, new(false), new(true)}
	destructive := []*bool{nil, new(false), new(true)}

	var combos []*sdkmcp.ToolAnnotations
	for _, ro := range readOnly {
		for _, idem := range idempotent {
			for _, ow := range openWorld {
				for _, dh := range destructive {
					combos = append(combos, &sdkmcp.ToolAnnotations{
						ReadOnlyHint:    ro,
						IdempotentHint:  idem,
						OpenWorldHint:   ow,
						DestructiveHint: dh,
					})
				}
			}
		}
	}
	return combos
}

// TestMCPToolRiskEscalatesOnly is D-107's required proof: exhaustive, not
// sampled. It runs all 36 hint combinations through both branches of
// classifyToolRisk and asserts the new tier is never lower than a frozen
// baseline's tier — never an example, the whole space.
func TestMCPToolRiskEscalatesOnly(t *testing.T) {
	t.Parallel()

	combos := allAnnotationCombinations()
	if len(combos) != 36 {
		t.Fatalf("combination table has %d entries, want 36 — the cartesian product itself is wrong", len(combos))
	}

	fallbackPolicy := bridgePolicy{}
	tablePolicy := managedBridgePolicy(mcp.ManagedServer{
		Source: calendarRecipeSource,
		Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
	})

	fallbackCases, tableCases := 0, 0
	for i, a := range combos {
		// Fallback path: no recipe policy in play, so classifyToolRisk always
		// runs the two-hint-plus-escalation branch this plan adds to.
		fbMutating, fbDestructive := classifyToolRisk(fallbackPolicy, &sdkmcp.Tool{Name: "arbitrary_tool", Annotations: a})
		baseMutating, baseDestructive := frozenTwoHintBaseline(a)
		if riskTier(fbMutating, fbDestructive) < riskTier(baseMutating, baseDestructive) {
			t.Errorf("fallback case %d/36 %+v lowered the tier: new=(%v,%v) baseline=(%v,%v)",
				i+1, *a, fbMutating, fbDestructive, baseMutating, baseDestructive)
		}
		fallbackCases++

		// Table path: "create_event" is a recipe-listed Mutate-tier tool. The
		// trusted-recipe branch is untouched by this plan — it consults ONLY
		// explicitDestructive, never Idempotent/OpenWorld — so that same rule
		// IS the table path's frozen baseline. Any future accidental wiring of
		// the two new hints into the table branch fails this exact assertion.
		tblMutating, tblDestructive := classifyToolRisk(tablePolicy, &sdkmcp.Tool{Name: "create_event", Annotations: a})
		tblBaseMutating, tblBaseDestructive := true, explicitDestructive(a)
		if riskTier(tblMutating, tblDestructive) < riskTier(tblBaseMutating, tblBaseDestructive) {
			t.Errorf("table case %d/36 %+v lowered the tier: new=(%v,%v) baseline=(%v,%v)",
				i+1, *a, tblMutating, tblDestructive, tblBaseMutating, tblBaseDestructive)
		}
		tableCases++
	}

	if fallbackCases != 36 {
		t.Fatalf("fallback path executed %d cases, want 36 — the combination table silently shrank", fallbackCases)
	}
	if tableCases != 36 {
		t.Fatalf("table path executed %d cases, want 36 — the combination table silently shrank", tableCases)
	}
}

// TestMCPToolRiskRecipeTablesUnchanged is the no-drift proof: every tool name
// in all three trusted-recipe tables must classify identically before and
// after D-107, both for a nil Annotations and for an all-hints-set
// Annotations carrying DestructiveHint:false. Expectations are hard-coded —
// deriving them from trustedRecipeActions at runtime would make this test
// pass no matter how the table drifted.
func TestMCPToolRiskRecipeTablesUnchanged(t *testing.T) {
	t.Parallel()

	type tierPair struct{ mutating, destructive bool }

	expectations := map[string]map[string]tierPair{
		calendarRecipeSource: {
			"list_accounts":              {false, false},
			"get_emails":                 {false, false},
			"get_email_details":          {false, false},
			"search_emails":              {false, false},
			"list_calendars":             {false, false},
			"get_calendar_events":        {false, false},
			"get_calendar_event_details": {false, false},
			"get_contacts":               {false, false},
			"search_contacts":            {false, false},
			"get_contact_details":        {false, false},
			"create_event":               {true, false},
			"update_event":               {true, false},
			"mark_email_read":            {true, false},
			"respond_to_event":           {true, true},
			"send_email":                 {true, true},
			"delete_email":               {true, true},
			// move_email is destructive because 'trash' is a valid destination —
			// a cheaper tier here would be a bypass of delete_email's gate.
			"move_email": {true, true},
			// The twelve unreachable actions, exposed 2026-08-23. bulk_move_emails
			// inherits move_email's reasoning verbatim; unsubscribe_from_email is
			// destructive because it makes an outward request to a third party on the
			// operator's behalf and nothing takes it back.
			"get_guide":                    {false, false},
			"get_unsubscribe_info":         {false, false},
			"get_email_attachment":         {false, false},
			"get_contextual_email_summary": {false, false},
			"create_contact":               {true, false},
			"update_contact":               {true, false},
			"bulk_mark_emails_read":        {true, false},
			"delete_event":                 {true, true},
			"delete_contact":               {true, true},
			"bulk_delete_emails":           {true, true},
			"bulk_move_emails":             {true, true},
			"unsubscribe_from_email":       {true, true},
		},
		whatsAppRecipeSource: {
			"list_chats":                 {false, false},
			"list_messages":              {false, false},
			"search_contacts":            {false, false},
			"get_contact":                {false, false},
			"get_chat":                   {false, false},
			"get_contact_chats":          {false, false},
			"get_direct_chat_by_contact": {false, false},
			"get_last_interaction":       {false, false},
			"get_message_context":        {false, false},
			"get_media_data":             {false, false},
			"download_media":             {true, false},
			"send_audio_message":         {true, true},
			"send_file":                  {true, true},
			"send_message":               {true, true},
			"send_reaction":              {true, true},
		},
		mcp.SourceRecipeMemory: {
			"graph_schema":          {false, false},
			"memory_recall":         {false, false},
			"memory_search":         {false, false},
			"memory_entities":       {false, false},
			"memory_facts_about":    {false, false},
			"memory_digest":         {false, false},
			"memory_upsert_fact":    {true, false},
			"memory_merge_entities": {true, false},
			"memory_reembed":        {true, false},
			"memory_forget":         {true, false},
		},
	}

	allHints := &sdkmcp.ToolAnnotations{
		ReadOnlyHint:    false,
		IdempotentHint:  false,
		OpenWorldHint:   new(true),
		DestructiveHint: new(false),
	}

	// The lowering direction, which neither annotation set above reaches: both of them
	// land on (true,true) through the fallback as well, so they cannot tell whether the
	// curated `return` fired or the fallback did. A mutation run on 2026-08-17 proved it
	// — deleting `case mcpActionDestructive: return true, true` survived the whole file's
	// suite. readOnlyHint is the one hint that DOES divert the fallback, to (false,false):
	// under that deletion a lying trusted-recipe server would declare send_message
	// read-only and walk past the approval gate.
	readOnlyHint := &sdkmcp.ToolAnnotations{ReadOnlyHint: true}

	for source, table := range trustedRecipeActions {
		exp, ok := expectations[source]
		if !ok {
			t.Fatalf("no hard-coded expectations for recipe source %q — add them, do not skip", source)
		}
		policy := managedBridgePolicy(mcp.ManagedServer{Source: source, Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe}})

		covered := 0
		for name := range table {
			want, ok := exp[name]
			if !ok {
				t.Errorf("%s/%s has no hard-coded expectation — the recipe table drifted ahead of the test", source, name)
				continue
			}
			covered++

			nilMutating, nilDestructive := classifyToolRisk(policy, &sdkmcp.Tool{Name: name, Annotations: nil})
			if nilMutating != want.mutating || nilDestructive != want.destructive {
				t.Errorf("%s/%s with nil Annotations = (%v,%v), want (%v,%v)",
					source, name, nilMutating, nilDestructive, want.mutating, want.destructive)
			}

			hintedMutating, hintedDestructive := classifyToolRisk(policy, &sdkmcp.Tool{Name: name, Annotations: allHints})
			if hintedMutating != want.mutating || hintedDestructive != want.destructive {
				t.Errorf("%s/%s with all-hints-set Annotations = (%v,%v), want (%v,%v) — a new hint moved a recipe-table tool",
					source, name, hintedMutating, hintedDestructive, want.mutating, want.destructive)
			}

			roMutating, roDestructive := classifyToolRisk(policy, &sdkmcp.Tool{Name: name, Annotations: readOnlyHint})
			if roMutating != want.mutating || roDestructive != want.destructive {
				t.Errorf("%s/%s declared readOnlyHint and got (%v,%v), want (%v,%v) — the server talked itself out of the curated tier",
					source, name, roMutating, roDestructive, want.mutating, want.destructive)
			}
		}
		if covered != len(table) {
			t.Errorf("%s: covered %d names, want %d (len(trustedRecipeActions[%s]))", source, covered, len(table), source)
		}
	}
}

// TestMCPToolRiskNilAnnotations exercises classifyToolRisk directly (rather
// than through the mcpToolRisk/logging wrapper TestMCPToolRisk_NilAnnotationsFailsClosed
// covers) to pin the same fail-closed default at the decision-function level.
func TestMCPToolRiskNilAnnotations(t *testing.T) {
	t.Parallel()
	mutating, destructive := classifyToolRisk(bridgePolicy{}, &sdkmcp.Tool{Name: "x", Annotations: nil})
	if !mutating || !destructive {
		t.Fatalf("nil Annotations must classify (true,true), got (%v,%v)", mutating, destructive)
	}
}

// calendarDesignDocActions is docs/superpowers/specs/2026-08-17-mcp-curated-surface-design.md
// §5a's calendar action list, hard-coded here (NOT derived from trustedRecipeActions) so a
// table edit that silently drops or adds a key fails this test rather than passing vacuously.
var calendarDesignDocActions = []string{
	"list_accounts", "get_emails", "get_email_details", "search_emails",
	"list_calendars", "get_calendar_events", "get_calendar_event_details",
	"get_contacts", "search_contacts", "get_contact_details",
	"create_event", "update_event", "respond_to_event", "send_email",
	// Amended 2026-08-22: mail management restored to the curated surface. The
	// provider layer always had these three; only 14 of its 21 operations were
	// ever registered as tools, so they were implemented and unreachable.
	"mark_email_read", "delete_email", "move_email",
	// Amended 2026-08-23: the twelve actions the first curation round left
	// unreachable. Upstream registers 29 tools; the curated enum published 17, and
	// the difference was not cosmetic — delete_event was absent while create_event
	// and update_event were present, so an agent could add an event to a calendar
	// and never remove it (proven live: a write test stranded a real event), and
	// the server's own ServerInstructions pointed callers at get_guide, which the
	// enum did not contain.
	"delete_event", "create_contact", "update_contact", "delete_contact",
	"get_email_attachment", "get_contextual_email_summary", "get_guide",
	"get_unsubscribe_info", "unsubscribe_from_email",
	"bulk_delete_emails", "bulk_mark_emails_read", "bulk_move_emails",
}

// TestTrustedRecipeCalendarActionsMatchDesignDoc pins D-21's re-key: calendar's
// keys equal the fork's curated action enum values one-for-one (46-05 re-registered
// the original 14 without re-tiering any; the 2026-08-22 amendment added three mail-
// management actions), so this asserts the design doc and the live table agree
// exactly — an extra or a missing key fails.
func TestTrustedRecipeCalendarActionsMatchDesignDoc(t *testing.T) {
	t.Parallel()
	table := trustedRecipeActions[calendarRecipeSource]
	if len(table) != len(calendarDesignDocActions) {
		t.Fatalf("trustedRecipeActions[calendarRecipeSource] has %d keys, design doc lists %d", len(table), len(calendarDesignDocActions))
	}
	for _, action := range calendarDesignDocActions {
		if _, ok := table[action]; !ok {
			t.Errorf("design doc action %q is missing from trustedRecipeActions[calendarRecipeSource]", action)
		}
	}
	for action := range table {
		if !slices.Contains(calendarDesignDocActions, action) {
			t.Errorf("trustedRecipeActions[calendarRecipeSource] has %q, which the design doc does not list", action)
		}
	}
}
