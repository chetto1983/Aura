package manager

import (
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestRiskLabelsInferConservativeDefaultsAndOverrides(t *testing.T) {
	server := mcp.ManagedServer{RiskLabels: []string{RiskPrivateData}}

	tests := []struct {
		name string
		def  mcp.ToolDef
		want []string
	}{
		{
			name: "external send",
			def:  mcp.ToolDef{Name: "send_email", Description: "Send an email to a recipient."},
			want: []string{RiskExternalSend, RiskPrivateData, RiskWrite},
		},
		{
			name: "calendar read",
			def:  mcp.ToolDef{Name: "list_events", Description: "List calendar events."},
			want: []string{RiskPrivateData, RiskRead},
		},
		{
			name: "destructive filesystem",
			def:  mcp.ToolDef{Name: "delete_file", Description: "Permanently delete a file."},
			want: []string{RiskDestructive, RiskFilesystem, RiskPrivateData, RiskWrite},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RiskLabelsForTool(tt.def, server)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("RiskLabelsForTool = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestRiskLabelsUnknownFallback(t *testing.T) {
	got := RiskLabelsForTool(mcp.ToolDef{Name: "frobnicate", Description: "Do the thing."}, mcp.ManagedServer{})
	if !reflect.DeepEqual(got, []string{RiskUnknown}) {
		t.Fatalf("unknown labels = %#v, want unknown", got)
	}
}

func TestPolicyDecisionsApplyAllowDenyAndDenyRisk(t *testing.T) {
	server := mcp.ManagedServer{
		RiskLabels: []string{RiskPrivateData},
		ToolPolicy: mcp.ManagedToolPolicy{
			Allow:    []string{"send_email", "fetch_emails", "delete_mailbox", "mystery"},
			Deny:     []string{"delete_mailbox"},
			DenyRisk: []string{RiskUnknown},
		},
	}

	allowed := PolicyDecisionForTool(mcp.ToolDef{Name: "fetch_emails", Description: "Fetch recent email."}, server)
	if !allowed.Allowed || !containsRisk(allowed.RiskLabels, RiskRead) || allowed.BlockReason != "" {
		t.Fatalf("fetch decision = %+v", allowed)
	}

	denied := PolicyDecisionForTool(mcp.ToolDef{Name: "delete_mailbox", Description: "Delete a mailbox."}, server)
	if denied.Allowed || !strings.Contains(denied.BlockReason, "denied by tool policy") {
		t.Fatalf("delete decision = %+v", denied)
	}

	unknown := PolicyDecisionForTool(mcp.ToolDef{Name: "mystery", Description: "Does something undocumented."}, server)
	if unknown.Allowed || !strings.Contains(unknown.BlockReason, "risk unknown denied") {
		t.Fatalf("unknown decision = %+v", unknown)
	}

	notAllowed := PolicyDecisionForTool(mcp.ToolDef{Name: "upload_file", Description: "Upload a file."}, server)
	if notAllowed.Allowed || !strings.Contains(notAllowed.BlockReason, "not in allowlist") {
		t.Fatalf("not-allowed decision = %+v", notAllowed)
	}
}

func TestPolicyDecisionsForTools(t *testing.T) {
	server := mcp.ManagedServer{
		ToolPolicy: mcp.ManagedToolPolicy{
			Allow: []string{"list_events", "send_email"},
			Deny:  []string{"send_email"},
		},
	}
	defs := []mcp.ToolDef{
		{Name: "list_events", Description: "List calendar events."},
		{Name: "send_email", Description: "Send an email."},
		{Name: "delete_thing", Description: "Delete a thing."},
	}

	got := PolicyDecisionsForTools(defs, server)
	if len(got) != len(defs) {
		t.Fatalf("PolicyDecisionsForTools returned %d decisions, want %d", len(got), len(defs))
	}
	if !got[0].Allowed || got[0].ToolName != "list_events" {
		t.Fatalf("list_events decision = %+v", got[0])
	}
	if got[1].Allowed || !strings.Contains(got[1].BlockReason, "denied by tool policy") {
		t.Fatalf("send_email decision = %+v", got[1])
	}
	if got[2].Allowed || !strings.Contains(got[2].BlockReason, "not in allowlist") {
		t.Fatalf("delete_thing decision = %+v", got[2])
	}
}

func TestPolicyDecisionsForToolsEmpty(t *testing.T) {
	got := PolicyDecisionsForTools(nil, mcp.ManagedServer{})
	if len(got) != 0 {
		t.Fatalf("PolicyDecisionsForTools(nil) = %#v, want empty", got)
	}
}

func TestConfiguredPolicyBlocks(t *testing.T) {
	server := mcp.ManagedServer{
		ToolPolicy: mcp.ManagedToolPolicy{
			Deny:     []string{"send_email", "   ", ""},
			DenyRisk: []string{RiskExternalSend},
		},
	}
	blocks := ConfiguredPolicyBlocks(server)

	var tools, risks []string
	for _, b := range blocks {
		if b.Reason == "" {
			t.Fatalf("block missing reason: %+v", b)
		}
		if b.ToolName != "" {
			tools = append(tools, b.ToolName)
		}
		if b.Risk != "" {
			risks = append(risks, b.Risk)
		}
	}
	if !reflect.DeepEqual(tools, []string{"send_email"}) {
		t.Fatalf("blocked tools = %#v, want [send_email] (blank entries skipped)", tools)
	}
	// effectiveDenyRisk always includes destructive + unknown defaults plus the configured external_send.
	for _, want := range []string{RiskDestructive, RiskExternalSend, RiskUnknown} {
		if !containsRisk(risks, want) {
			t.Fatalf("blocked risks = %#v, missing %q", risks, want)
		}
	}
}

func TestConfiguredPolicyBlocksDefaultRisksOnly(t *testing.T) {
	blocks := ConfiguredPolicyBlocks(mcp.ManagedServer{})
	var risks []string
	for _, b := range blocks {
		if b.ToolName != "" {
			t.Fatalf("unexpected tool block: %+v", b)
		}
		risks = append(risks, b.Risk)
	}
	if !reflect.DeepEqual(risks, []string{RiskDestructive, RiskUnknown}) {
		t.Fatalf("default risk blocks = %#v, want [destructive unknown]", risks)
	}
}

func containsRisk(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}
