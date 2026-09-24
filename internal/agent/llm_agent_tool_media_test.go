package agent_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/google/uuid"
)

// photoBox is a box holding one file: every bounded read returns it whole.
type photoBox struct{ file []byte }

func (b photoBox) Resolve(context.Context, usersandbox.SandboxSpec) (usersandbox.BoxHandle, error) {
	return usersandbox.BoxHandle{ContainerID: "box-1", IdentityID: "id-1"}, nil
}

func (b photoBox) Exec(_ context.Context, _ usersandbox.BoxHandle, req usersandbox.ExecRequest) (usersandbox.ExecResult, error) {
	if !strings.HasPrefix(req.Command, "head -c ") {
		return usersandbox.ExecResult{ExitCode: 1}, nil
	}
	return usersandbox.ExecResult{Stdout: b.file}, nil
}

func (photoBox) Suspend(context.Context, usersandbox.BoxHandle) error { return nil }
func (photoBox) Resume(context.Context, usersandbox.BoxHandle) error  { return nil }
func (photoBox) Stop(context.Context, usersandbox.BoxHandle) error    { return nil }

func photoAgent(t *testing.T, fc *agenttest.FakeClient, file []byte) *agent.LlmAgent {
	t.Helper()
	router := usersandbox.NewSandboxRouter(photoBox{file: file}, config.ProfileSingleUserHardened, config.SandboxConfig{
		Image: "aura-sandbox:latest", CPULimit: 1, MemoryLimit: 1 << 30, PidsLimit: 128, IdleTTLSec: 1800,
	})
	r := tools.NewRegistry()
	r.Register(tools.TextResponse{})
	r.Register(&tools.ReadFile{Router: router})
	return agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     fc,
		LLM:        llm.Config{Model: "test-model", Provider: "test-provider", TotalTimeoutSec: 30},
		Registry:   r,
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		Workspace:  "/workspace",
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "cosa c'è nella foto che hai scaricato?"}},
	})
}

// The WhatsApp photo the model could not see: an image read_file opens in round 1 must
// reach round 2's request as media, while everything that outlives the turn — the history,
// the events the runner persists — holds only read_file's text.
func photoPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A budget that trips right after the image was read ends the turn in finalize: the tool-free
// synthesis request must still carry the photo, or the model answers about an image the
// history says is "attached below" with nothing below it.
func TestReadFileImageReachesTheFinalizeRequest(t *testing.T) {
	const photo = "/workspace/mcp-files/r1/whatsapp/photo.png"
	raw := photoPNG(t)
	read := func(id string) agenttest.FakeTurn {
		return agenttest.ToolCallTurn(agenttest.MakeToolCall(id, "read_file", `{"path":"`+photo+`"}`))
	}
	// Three identical reads trip the window-3 dedup ring (recover); the recovery turn repeats
	// it and trips again (finalize), as in TestFinalize_DedupTrip.
	fc := agenttest.NewFakeClient(read("c1"), read("c2"), read("c3"), read("c4"),
		agenttest.TextChunks("stop", "Un'immagine vuota 3x2."))
	if _, err := collect(photoAgent(t, fc, raw).Run(newIC(t, agent.BudgetOptions{MaxSteps: new(50), DedupWindow: new(3)}))); err != nil {
		t.Fatalf("Run: %v", err)
	}
	final := fc.LastRequest()
	if final.ToolChoice != "none" {
		t.Fatalf("last request ToolChoice = %q, want the finalize synthesis (none)", final.ToolChoice)
	}
	var carried bool
	for _, parts := range final.ToolMedia {
		for _, part := range parts {
			carried = carried || bytes.Equal(part.Bytes, raw)
		}
	}
	if !carried {
		t.Fatalf("finalize request media = %+v, want the photo read this turn", final.ToolMedia)
	}
}

func TestReadFileImageReachesTheNextRoundAndNothingElse(t *testing.T) {
	const photo = "/workspace/mcp-files/r1/whatsapp/photo.png"
	raw := photoPNG(t)
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "read_file", `{"path":"`+photo+`"}`)),
		agenttest.TextChunks("stop", "Un'immagine vuota 3x2."),
	)
	ic := newIC(t, agent.BudgetOptions{})
	evs, err := collect(photoAgent(t, fc, raw).Run(ic))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	requests := fc.RecordedRequests()
	if len(requests) != 2 {
		t.Fatalf("model calls = %d, want 2", len(requests))
	}
	if requests[0].ToolMedia != nil {
		t.Fatalf("round 1 carries tool media before any tool ran: %+v", requests[0].ToolMedia)
	}
	parts := requests[1].ToolMedia["c1"]
	if len(parts) != 1 || parts[0].MIMEType != "image/png" || parts[0].Text != photo || !bytes.Equal(parts[0].Bytes, raw) {
		t.Fatalf("round 2 media = %+v, want the photo under c1", requests[1].ToolMedia)
	}

	encoded := base64.StdEncoding.EncodeToString(raw)
	leaks := func(s string) bool { return strings.Contains(s, encoded) || strings.Contains(s, string(raw)) }
	var sawText bool
	for _, m := range requests[1].Messages {
		if leaks(m.Content) {
			t.Fatalf("image bytes reached the history as a %s message", m.Role)
		}
		if m.Role == llm.RoleTool && m.ToolCallID == "c1" {
			sawText = strings.Contains(m.Content, photo+" is an image (image/png, 3x2)")
		}
	}
	if !sawText {
		t.Fatal("round 2 history lacks read_file's text result")
	}
	for _, ev := range evs {
		wire, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		if leaks(string(wire)) {
			t.Fatalf("image bytes reached an event the runner persists: %s", wire)
		}
	}
	if llm.ToolMediaFromContext(ic.Ctx) != nil {
		t.Fatal("the turn's carrier leaked into the caller's context")
	}

	next := agenttest.NewFakeClient(agenttest.TextChunks("stop", "ok"))
	if _, err := collect(photoAgent(t, next, raw).Run(newIC(t, agent.BudgetOptions{}))); err != nil {
		t.Fatalf("next turn: %v", err)
	}
	if media := next.LastRequest().ToolMedia; media != nil {
		t.Fatalf("the next turn inherited tool media: %+v", media)
	}
}
