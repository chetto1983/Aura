package agui

import (
	"github.com/chetto1983/aura/internal/agent/display"
	"github.com/chetto1983/aura/internal/assets"
)

// attachToolArtifacts puts each agent file back on the tool call that delivered it.
//
// Live, send_file's card rides the aura.artifact frame onto its call by tool_call_id, and that
// frame is never persisted. The asset records the call instead (migration 0126), so the replay
// says what the stream said: the same local_artifact display, on the same call. A file whose
// call is not in this history -- a branch not selected, or a file no call produced -- is placed
// nowhere; the Artifacts panel lists it.
//
// files arrive oldest first, and calls are walked in history order, so a call id that repeats
// (a provider that sends none gets a deterministic one, llm_agent_call_dedup.go) pairs each
// call with its own delivery rather than every call with the last.
func attachToolArtifacts(snap *displaySnapshotEvent, files []assets.Asset) {
	byCall := make(map[string][]assets.Asset)
	for _, file := range files {
		if file.ToolCallID != "" {
			byCall[file.ToolCallID] = append(byCall[file.ToolCallID], file)
		}
	}
	for i := range snap.Messages {
		calls := snap.Messages[i].ToolCalls
		for j := range calls {
			queued := byCall[calls[j].ID]
			if len(queued) == 0 || calls[j].Display != nil {
				continue
			}
			byCall[calls[j].ID] = queued[1:]
			calls[j].Display = &display.Payload{
				Type: display.KindLocalArtifact, ToolCallID: calls[j].ID, Artifact: artifactOf(queued[0]),
			}
		}
	}
}

// artifactOf mirrors the live descriptor: a file whose bytes were stored carries the asset id
// the download route serves, one that was not keeps its name and offers nothing (D-02).
func artifactOf(file assets.Asset) *display.Artifact {
	artifact := &display.Artifact{Filename: file.FileName, SizeBytes: file.SizeBytes, MIMEType: file.MIMEType}
	if file.Status == assets.StatusAccepted {
		artifact.AssetID = file.ID
	}
	return artifact
}
