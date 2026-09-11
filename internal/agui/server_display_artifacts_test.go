package agui

import (
	"reflect"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/chetto1983/aura/internal/agent/display"
	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/llm"
)

// sendFileSnapshot is the shape of the conversation the defect was measured on (2026-09-11,
// "Ciao, presentazione dell'assistente"): a greeting answer, then a later turn whose
// send_file call delivered bundle.html. On reload the file rendered under the greeting.
func sendFileSnapshot() displaySnapshotEvent {
	return displaySnapshotEvent{Messages: []displaySnapshotMessage{
		{ID: "msg-0", Role: types.Role(llm.RoleUser), Content: "ciao, che sei?"},
		{ID: "msg-1", Role: types.Role(llm.RoleAssistant), Content: "Ciao! Sono Aura."},
		{ID: "msg-2", Role: types.Role(llm.RoleUser), Content: "crea un artifact del meteo"},
		{ID: "msg-3", Role: types.Role(llm.RoleAssistant), ToolCalls: []displaySnapshotToolCall{
			{ID: "call-send", Type: "function", Function: types.FunctionCall{Name: "send_file"}},
		}},
		{ID: "msg-4", Role: types.Role(llm.RoleTool), ToolCallID: "call-send", Content: "queued bundle.html for delivery"},
		{ID: "msg-5", Role: types.Role(llm.RoleAssistant), Content: "Fatto."},
	}}
}

func TestAttachToolArtifactsPutsTheFileOnTheCallThatDeliveredIt(t *testing.T) {
	snap := sendFileSnapshot()

	attachToolArtifacts(&snap, []assets.Asset{{
		ID: "asset-1", ToolCallID: "call-send", Status: assets.StatusAccepted,
		FileName: "bundle.html", MIMEType: "text/html; charset=utf-8", SizeBytes: 2048,
	}})

	want := display.Payload{Type: display.KindLocalArtifact, ToolCallID: "call-send", Artifact: &display.Artifact{
		Filename: "bundle.html", SizeBytes: 2048, AssetID: "asset-1", MIMEType: "text/html; charset=utf-8",
	}}
	if got := snap.Messages[3].ToolCalls[0].Display; got == nil || !reflect.DeepEqual(*got, want) {
		t.Fatalf("send_file display = %+v, want %+v", got, want)
	}
}

// A file whose call is not in this history (a branch not selected) and a file no call
// produced (a delegation report) have no place in the transcript. The Artifacts panel
// lists both; the transcript does not guess.
func TestAttachToolArtifactsPlacesNothingWithoutItsCall(t *testing.T) {
	snap := sendFileSnapshot()

	attachToolArtifacts(&snap, []assets.Asset{
		{ID: "other-branch", ToolCallID: "call-elsewhere", Status: assets.StatusAccepted, FileName: "a.html"},
		{ID: "report", Status: assets.StatusAccepted, FileName: "child.md"},
	})

	for _, m := range snap.Messages {
		for _, call := range m.ToolCalls {
			if call.Display != nil {
				t.Fatalf("call %s got %+v, want nothing placed", call.ID, call.Display)
			}
		}
	}
}

// Live, a delivery whose bytes were never stored shows a card with no download (D-02). The
// replay says the same, rather than offering a download of nothing.
func TestAttachToolArtifactsOffersNoDownloadForAFileThatWasNotStored(t *testing.T) {
	snap := sendFileSnapshot()

	attachToolArtifacts(&snap, []assets.Asset{{
		ID: "asset-1", ToolCallID: "call-send", Status: assets.StatusFailed, FileName: "bundle.html",
	}})

	got := snap.Messages[3].ToolCalls[0].Display
	if got == nil || got.Artifact == nil || got.Artifact.Filename != "bundle.html" || got.Artifact.AssetID != "" {
		t.Fatalf("display = %+v, want a card for bundle.html with no asset to download", got)
	}
}
