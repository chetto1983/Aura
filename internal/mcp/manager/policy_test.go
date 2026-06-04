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

func containsRisk(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}
