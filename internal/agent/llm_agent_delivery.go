// The deliver-on-stop gate (PRD amendment #192).
//
// Measured on the Telegram ledger over 30 days: 11 write_file deliverables under
// /workspace/artifacts/, 9 delivered with send_file in the same turn, 2 only after the
// operator asked ("Devi sempre mandarmelo"). Both misses had the same shape — the model
// reported the path and stopped — and the operator cannot open a /workspace path from
// Telegram or the cockpit. This gate makes "wrote an artifact, did not deliver it" a
// deterministic one-shot nudge at both voluntary-termination seams, like the
// verify-on-stop gate: it spends no model call itself and is silent for a run that wrote
// nothing under artifacts/ or already delivered everything.
package agent

import (
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// deliverOnStopNudgePrefix is the prefix isAgentNudge recognises, so the completion
// critic never grades the turn against Aura's own injection.
const deliverOnStopNudgePrefix = "[System: You wrote"

// deliveryMaxAttempts bounds the gate to ONE nudge per run: the model either delivers
// or states why no delivery is needed, and a second round could only repeat itself.
const deliveryMaxAttempts = 1

// defaultWorkspace is the box home the prompt's <workspace> doctrine names; a run
// constructed without a workspace (tests, standalone) still gates paths under it.
const defaultWorkspace = "/workspace"

const artifactsDir = "artifacts"

// sendFilePathArgs mirrors the argument names send_file accepts (tools/send_file.go),
// first match wins in the same order the tool resolves them.
var sendFilePathArgs = []string{"path", "file_path", "file", "filepath", "absolute_path"}

// recordDeliveredPath accumulates the path one dispatched send_file call delivered. A
// call that produced no artifact descriptor (sandbox unavailable, missing file, policy
// veto) delivered nothing and must not silence the gate.
//
// Called only from the SERIAL result loop in dispatch (like recordEditedPath), so the
// slice needs no lock while a batch runs concurrently.
func (a *LlmAgent) recordDeliveredPath(call llm.ToolCall, result tools.ToolResult) {
	if call.Function.Name != "send_file" {
		return
	}
	if _, delivered := metaMap(result.Meta, "artifact"); !delivered {
		return
	}
	p, ok := sendFilePath(call.Function.Arguments)
	if !ok || slices.Contains(a.deliveredPaths, p) {
		return
	}
	a.deliveredPaths = append(a.deliveredPaths, p)
}

func sendFilePath(raw string) (string, bool) {
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", false
	}
	for _, key := range sendFilePathArgs {
		if p, _ := args[key].(string); strings.TrimSpace(p) != "" {
			return p, true
		}
	}
	return "", false
}

// gateDelivery returns the follow-up for a voluntary termination that wrote artifacts
// this run and delivered none of them, and ok=false when there is nothing to say. Like
// gateVerification it is deterministic and free, so the loop runs it BEFORE the
// completion critic.
func (a *LlmAgent) gateDelivery() (string, bool) {
	if a.deliveryAttempts >= deliveryMaxAttempts {
		return "", false
	}
	pending := undeliveredArtifacts(a.workspace, a.editedPaths, a.deliveredPaths)
	if len(pending) == 0 {
		return "", false
	}
	a.deliveryAttempts++
	return buildDeliverOnStopNudge(pending), true
}

// undeliveredArtifacts is the edited paths under <workspace>/artifacts/ that no
// send_file delivered. Paths are box paths, hence path (POSIX) and never filepath; a
// relative path resolves against the workspace, which is the box cwd.
func undeliveredArtifacts(workspace string, edited, delivered []string) []string {
	if strings.TrimSpace(workspace) == "" {
		workspace = defaultWorkspace
	}
	resolve := func(p string) string {
		if !path.IsAbs(p) {
			p = path.Join(workspace, p)
		}
		return path.Clean(p)
	}
	prefix := path.Join(workspace, artifactsDir) + "/"
	sent := make([]string, 0, len(delivered))
	for _, d := range delivered {
		sent = append(sent, resolve(d))
	}
	var pending []string
	for _, e := range edited {
		artifact := resolve(e)
		if !strings.HasPrefix(artifact, prefix) || slices.Contains(sent, artifact) || slices.Contains(pending, artifact) {
			continue
		}
		pending = append(pending, artifact)
	}
	return pending
}

func buildDeliverOnStopNudge(pending []string) string {
	return fmt.Sprintf("%s %s but did not deliver it. The operator cannot open a /workspace path "+
		"(Telegram, the cockpit): call send_file for each deliverable now, or state in one line why "+
		"no delivery is needed, then finish.]", deliverOnStopNudgePrefix, strings.Join(pending, ", "))
}
