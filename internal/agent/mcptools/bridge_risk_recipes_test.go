package mcptools

// bridge_risk_recipes_test.go pins every trusted-recipe table, tool by tool; it is split
// from bridge_risk_test.go because the tables alone are most of a file.

import (
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

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
		mcpmanager.BrowserRecipeSource: {
			// Driving the box's own browser is a reversible write; reads never gate.
			"agent_browser_tools_profiles":    {false, false},
			"agent_browser_read":              {false, false},
			"agent_browser_snapshot":          {false, false},
			"agent_browser_screenshot":        {false, false},
			"agent_browser_get_text":          {false, false},
			"agent_browser_get_url":           {false, false},
			"agent_browser_get_title":         {false, false},
			"agent_browser_wait_ms":           {false, false},
			"agent_browser_wait_for_selector": {false, false},
			"agent_browser_wait_for_text":     {false, false},
			"agent_browser_wait_for_load":     {false, false},
			"agent_browser_tab_list":          {false, false},
			"agent_browser_open":              {true, false},
			"agent_browser_click":             {true, false},
			"agent_browser_fill":              {true, false},
			"agent_browser_type":              {true, false},
			"agent_browser_press":             {true, false},
			"agent_browser_check":             {true, false},
			"agent_browser_uncheck":           {true, false},
			"agent_browser_select":            {true, false},
			"agent_browser_scroll":            {true, false},
			"agent_browser_eval":              {true, false},
			"agent_browser_back":              {true, false},
			"agent_browser_forward":           {true, false},
			"agent_browser_reload":            {true, false},
			"agent_browser_tab_new":           {true, false},
			"agent_browser_tab_switch":        {true, false},
			"agent_browser_tab_close":         {true, false},
			"agent_browser_close":             {true, false},
		},
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
			"graph_diagnostics":     {false, false},
			"graph_path":            {false, false},
			"memory_batch":          {true, false},
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
