//go:build arcadedb_integration

package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	liveCapturePreviewSentinel   = "capture-preview-sentinel-49-14"
	liveCaptureFinalSentinel     = "capture-final-sentinel-49-14"
	liveCaptureSummarySentinel   = "capture-summary-sentinel-49-14"
	liveCaptureReasoningSentinel = "capture-reasoning-sentinel-49-14"
)

type liveMemoryUpsertSource struct {
	MemoryIDs []string `json:"memory_ids,omitempty"`
}

type liveMemoryUpsertInput struct {
	Subject   string                 `json:"subject"`
	Predicate string                 `json:"predicate"`
	Object    string                 `json:"object"`
	Statement string                 `json:"statement"`
	Source    liveMemoryUpsertSource `json:"source"`
}

type liveMemoryUpsertOutput struct {
	Statement string `json:"statement"`
	Refused   bool   `json:"refused"`
	Preview   string `json:"preview"`
	Summary   string `json:"summary"`
}

type liveCaptureBox struct {
	mu    sync.Mutex
	files map[string][]byte
}

func (b *liveCaptureBox) Resolve(context.Context, usersandbox.SandboxSpec) (usersandbox.BoxHandle, error) {
	return usersandbox.BoxHandle{ContainerID: "capture-box", IdentityID: "capture-owner"}, nil
}

func (b *liveCaptureBox) Exec(_ context.Context, _ usersandbox.BoxHandle, request usersandbox.ExecRequest) (usersandbox.ExecResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for path, content := range b.files {
		if strings.Contains(request.Command, path) && strings.HasPrefix(request.Command, "sha256sum ") {
			sum := sha256.Sum256(content)
			return usersandbox.ExecResult{Stdout: []byte(hex.EncodeToString(sum[:]) + "  " + path + "\n")}, nil
		}
	}
	return usersandbox.ExecResult{ExitCode: 1}, nil
}

func (*liveCaptureBox) Suspend(context.Context, usersandbox.BoxHandle) error { return nil }
func (*liveCaptureBox) Resume(context.Context, usersandbox.BoxHandle) error  { return nil }
func (*liveCaptureBox) Stop(context.Context, usersandbox.BoxHandle) error    { return nil }

func (b *liveCaptureBox) CopyFileIn(
	_ context.Context,
	_ usersandbox.BoxHandle,
	path string,
	content []byte,
	_ int64,
) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.files == nil {
		b.files = make(map[string][]byte)
	}
	b.files[path] = slices.Clone(content)
	return nil
}

type liveCaptureGateSink struct {
	client  *arcadedb.Client
	durable chan AcceptedCapture
	release chan struct{}
}

type liveAlwaysLoadedTool struct{ tools.Tool }

func (t liveAlwaysLoadedTool) Spec() tools.Spec {
	spec := t.Tool.Spec()
	spec.Deferred = false
	return spec
}

func (s *liveCaptureGateSink) ApplyAcceptedCapture(ctx context.Context, capture AcceptedCapture) error {
	if err := s.client.ApplyAcceptedCapture(ctx, capture); err != nil {
		return err
	}
	select {
	case s.durable <- capture:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type liveCaptureFixture struct {
	identityID     string
	conversationID string
	client         *arcadedb.Client
	registry       *tools.Registry
}

func newLiveCaptureFixture(t *testing.T) liveCaptureFixture {
	t.Helper()
	baseURL := strings.TrimSpace(os.Getenv("ARCADEDB_URL"))
	if baseURL == "" {
		baseURL = "http://127.0.0.1:2480"
	}
	password := strings.TrimSpace(os.Getenv("ARCADEDB_ADMIN_PASSWORD"))
	if password == "" {
		password = requireLiveCaptureEnv(t, "ARCADEDB_PASSWORD")
	}
	adminUser := strings.TrimSpace(os.Getenv("ARCADEDB_ADMIN_USER"))
	if adminUser == "" {
		adminUser = "root"
	}
	requireLiveCaptureEnv(t, "AURA_ARCADEDB_TENANT_SECRET")
	admin, err := arcadedb.New(arcadedb.Config{
		BaseURL: baseURL, Database: "unused", User: adminUser, Password: password, Timeout: time.Minute,
	})
	if err != nil {
		t.Fatalf("build ArcadeDB admin: %v", err)
	}
	if err := admin.VerifySecureVersion(t.Context()); err != nil {
		t.Fatalf("ArcadeDB secure version: %v", err)
	}
	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		t.Fatalf("tenant credentials: %v", err)
	}
	identityID := uuid.NewString()
	clients := arcadedb.NewTenantClients(arcadedb.Config{BaseURL: baseURL, Timeout: time.Minute}, admin, nil, credentials)
	client, err := clients.For(t.Context(), identityID)
	if err != nil {
		t.Fatalf("provision capture tenant: %v", err)
	}
	database, err := arcadedb.DatabaseFor(identityID)
	if err != nil {
		t.Fatalf("capture database name: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, dropErr := admin.DropDatabase(ctx, database); dropErr != nil {
			t.Errorf("drop capture database %s: %v", database, dropErr)
		}
		if dropErr := admin.DropUser(ctx, arcadedb.TenantUserFor(database)); dropErr != nil {
			t.Errorf("drop capture tenant user: %v", dropErr)
		}
	})

	registry := tools.NewRegistry()
	registry.Register(tools.TextResponse{})
	box := &liveCaptureBox{}
	registry.Register(&tools.WriteFile{Router: usersandbox.NewSandboxRouter(
		box,
		config.ProfileSingleUserHardened,
		config.SandboxConfig{Image: "capture-live", CPULimit: 1, MemoryLimit: 1 << 30, PidsLimit: 64},
	)})
	mountLiveMemoryUpsert(t, registry, client)
	return liveCaptureFixture{
		identityID: identityID, conversationID: uuid.NewString(), client: client, registry: registry,
	}
}

func requireLiveCaptureEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("arcadedb_integration requires %s; live capture proof may not skip", name)
	}
	return value
}

func mountLiveMemoryUpsert(t *testing.T, registry *tools.Registry, client *arcadedb.Client) {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "memory-capture-live", Version: "0.0.1"}, nil)
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name: "memory_upsert_fact", Description: "Store one durable fact.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: false},
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, in liveMemoryUpsertInput) (*sdkmcp.CallToolResult, liveMemoryUpsertOutput, error) {
		written, err := client.UpsertFact(ctx, arcadedb.Fact{
			Subject: in.Subject, Predicate: in.Predicate, Object: in.Object, Statement: in.Statement,
			Source: arcadedb.FactSource{
				RunID: "foreground-memory-call", WriterRole: arcadedb.WriterParent,
				MemoryIDs: slices.Clone(in.Source.MemoryIDs),
			},
		}, time.Now().UTC())
		if err != nil {
			return nil, liveMemoryUpsertOutput{}, err
		}
		return nil, liveMemoryUpsertOutput{
			Statement: written.Statement, Refused: written.Refused,
			Preview: liveCapturePreviewSentinel, Summary: liveCaptureSummarySentinel,
		}, nil
	})

	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatalf("connect live MCP server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	sdkClient := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "capture-live-client", Version: "0.0.1"}, nil)
	clientSession, err := sdkClient.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect live MCP client: %v", err)
	}
	mounted := mcptools.NewMountedServer("memory", nil)
	mounted.Attach(clientSession)
	t.Cleanup(func() { _ = mounted.Close() })
	if _, err := mcptools.Mount(t.Context(), registry, "memory", mounted); err != nil {
		t.Fatalf("mount live memory tool: %v", err)
	}
	tool, ok := registry.Get(memoryUpsertFactModelName)
	if !ok {
		t.Fatal("production memory tool unavailable to the live agent")
	}
	if tool.Spec().Deferred {
		registry.Adopt([]tools.Tool{liveAlwaysLoadedTool{Tool: tool}})
	}
}

func (f liveCaptureFixture) runner(
	t *testing.T,
	client llm.Client,
	queue *MemoryCaptureQueue,
) *Runner {
	t.Helper()
	conv := newFakeConvStore()
	if _, err := conv.Create(t.Context(), conversations.CreateParams{
		ID: f.conversationID, IdentityID: f.identityID,
	}); err != nil {
		t.Fatalf("create capture conversation: %v", err)
	}
	pause := newFakePauseStore()
	return New(Deps{
		Conv: conv, Pause: pause, ApprovalExpiry: pause, Identity: newFakeIdentityStore(),
		CacheMetrics: newFakeCacheMetricStore(), ToolInvocations: newFakeToolInvocationStore(),
		Client: client, Registry: f.registry, MemoryCaptureQueue: queue,
		LLM:    llm.Config{Model: "capture-live", ContextWindow: 1_000_000, MaxOutputTokens: 32_768},
		RunDir: t.TempDir(), PreviewCap: 16_384, TitleTimeout: time.Second, StopTimeout: time.Second,
	})
}

func liveMemoryCall(f liveCaptureFixture, suffix string) (llm.ToolCall, string, string) {
	subject := "capture-live-operator-" + suffix
	object := "capture-live-city-" + suffix
	userTurnRef := "user-turn:" + f.conversationID + ":1"
	args, _ := json.Marshal(liveMemoryUpsertInput{
		Subject: subject, Predicate: "lives_in", Object: object,
		Statement: subject + " lives in " + object + ".",
		Source:    liveMemoryUpsertSource{MemoryIDs: []string{userTurnRef}},
	})
	return agenttest.MakeToolCall("call-memory-"+suffix, memoryUpsertFactModelName, string(args)), subject, userTurnRef
}

func liveWriteCall(f liveCaptureFixture, suffix string) (llm.ToolCall, string) {
	path := "/workspace/capture-live-" + suffix + ".md"
	args, _ := json.Marshal(map[string]string{"path": path, "content": "durable capture " + suffix})
	return agenttest.MakeToolCall("call-write-"+suffix, "write_file", string(args)), path
}

func liveToolTurn(calls ...llm.ToolCall) agenttest.FakeTurn {
	turn := agenttest.ToolCallTurn(calls...)
	turn.Chunks = append([]llm.Chunk{{Reasoning: liveCaptureReasoningSentinel}}, turn.Chunks...)
	return turn
}

func liveFinalTurn() agenttest.FakeTurn {
	return agenttest.ToolCallTurn(agenttest.MakeToolCall(
		"call-final", "text_response", `{"text":"`+liveCaptureFinalSentinel+`"}`,
	))
}

func toolEndEvent(t *testing.T, events []*agent.Event, name string) *agent.Event {
	t.Helper()
	for _, event := range events {
		if event != nil && event.Actions.ToolInvocation != nil &&
			event.Actions.ToolInvocation.Event == agent.ToolInvocationEnd &&
			event.Actions.ToolInvocation.ToolName == name {
			return event
		}
	}
	t.Fatalf("zero real ToolInvocationEnd events for %s", name)
	return nil
}

func captureSourceFor(
	t *testing.T,
	client *arcadedb.Client,
	subject, predicate, object string,
	kind CaptureSourceKind,
) (arcadedb.FactSource, arcadedb.FactCaptureSource) {
	t.Helper()
	hits, err := client.FactsAbout(t.Context(), subject, predicate, 20, time.Time{})
	if err != nil {
		t.Fatalf("immediate recall %s/%s: %v", subject, predicate, err)
	}
	if len(hits) == 0 {
		query := subject
		if predicate == "durable_artifact" {
			query = "durable artifact persisted"
		}
		hits, err = client.SearchFacts(t.Context(), query, 20, time.Time{})
		if err != nil {
			t.Fatalf("immediate semantic recall %s: %v", subject, err)
		}
	}
	for _, hit := range hits {
		if hit.Object != object {
			continue
		}
		for _, source := range hit.Sources {
			for _, capture := range source.Captures {
				if capture.SourceKind == kind {
					return source, capture
				}
			}
		}
	}
	rows, queryErr := client.Query(t.Context(),
		"SELECT statement, predicate, valid_from, valid_to, sources, outV().name AS subject, inV().name AS object FROM FACT", nil)
	t.Fatalf("immediate recall found no %s capture for %s/%s/%s: hits=%+v rows=%+v query_err=%v",
		kind, subject, predicate, object, hits, rows, queryErr)
	return arcadedb.FactSource{}, arcadedb.FactCaptureSource{}
}

func assertNoCaptureSentinels(t *testing.T, client *arcadedb.Client) {
	t.Helper()
	rows, err := client.Query(t.Context(), "SELECT statement, sources FROM FACT", nil)
	if err != nil {
		t.Fatalf("read stored capture graph: %v", err)
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("marshal stored capture graph: %v", err)
	}
	for _, sentinel := range []string{
		liveCapturePreviewSentinel,
		liveCaptureFinalSentinel,
		liveCaptureSummarySentinel,
		liveCaptureReasoningSentinel,
	} {
		if strings.Contains(string(raw), sentinel) {
			t.Fatalf("generated/reasoning sentinel persisted in memory graph: %q in %s", sentinel, raw)
		}
	}
}

func TestMemoryCaptureLive_ExplicitUserEvent(t *testing.T) {
	fixture := newLiveCaptureFixture(t)
	memoryCall, subject, userTurnRef := liveMemoryCall(fixture, "explicit")
	client := agenttest.NewFakeClient(liveToolTurn(memoryCall), liveFinalTurn())
	queue := NewMemoryCaptureQueue(fixture.client, MemoryCaptureQueueConfig{WriteTimeout: 30 * time.Second})
	t.Cleanup(func() { _ = queue.Close(t.Context()) })
	events, err := drain(fixture.runner(t, client, queue).Turn(t.Context(), fixture.conversationID, new("remember this explicitly")))
	if err != nil {
		t.Fatalf("explicit live turn: %v", err)
	}
	event := toolEndEvent(t, events, memoryUpsertFactModelName)
	source, capture := captureSourceFor(t, fixture.client, subject, "lives_in", "capture-live-city-explicit", CaptureSourceExplicitFact)
	wantRefs := []string{"conversation:" + fixture.conversationID, "memory:" + userTurnRef, "tool_call:call-memory-explicit"}
	if source.RunID != event.RequestID.String() || source.WriterRole != arcadedb.WriterParent ||
		capture.ConversationID != fixture.conversationID || capture.ToolCallID != "call-memory-explicit" ||
		!slices.Equal(capture.SourceRefs, wantRefs) {
		t.Fatalf("explicit capture provenance = source %+v capture %+v, want run %s refs %v", source, capture, event.RequestID, wantRefs)
	}
	assertNoCaptureSentinels(t, fixture.client)
}

func TestMemoryCaptureLive_DurableArtifactEvent(t *testing.T) {
	fixture := newLiveCaptureFixture(t)
	writeCall, path := liveWriteCall(fixture, "artifact")
	client := agenttest.NewFakeClient(liveToolTurn(writeCall), liveFinalTurn())
	queue := NewMemoryCaptureQueue(fixture.client, MemoryCaptureQueueConfig{WriteTimeout: 30 * time.Second})
	t.Cleanup(func() { _ = queue.Close(t.Context()) })
	events, err := drain(fixture.runner(t, client, queue).Turn(t.Context(), fixture.conversationID, new("write the durable artifact")))
	if err != nil {
		t.Fatalf("artifact live turn: %v", err)
	}
	event := toolEndEvent(t, events, "write_file")
	if event.Actions.ToolInvocation.Status != "ok" || event.Actions.ToolInvocation.Error != "" {
		t.Fatalf("write_file ToolInvocationEnd = %+v", event.Actions.ToolInvocation)
	}
	if queue.AcceptedSequence() == 0 {
		t.Fatalf("real write_file event produced no accepted sequence: %+v", event.Actions.ToolInvocation)
	}
	source, capture := captureSourceFor(t, fixture.client, path, "durable_artifact", "write", CaptureSourceDurableArtifact)
	wantRefs := []string{"artifact:" + path, "conversation:" + fixture.conversationID, "tool_call:call-write-artifact"}
	if source.RunID != event.RequestID.String() || source.WriterRole != arcadedb.WriterParent ||
		capture.ConversationID != fixture.conversationID || capture.ToolCallID != "call-write-artifact" ||
		!slices.Equal(capture.SourceRefs, wantRefs) {
		t.Fatalf("artifact capture provenance = source %+v capture %+v, want run %s refs %v", source, capture, event.RequestID, wantRefs)
	}
	assertNoCaptureSentinels(t, fixture.client)
}

func TestMemoryCaptureLive_TerminalBarrier(t *testing.T) {
	fixture := newLiveCaptureFixture(t)
	memoryCall, subject, _ := liveMemoryCall(fixture, "barrier")
	writeCall, path := liveWriteCall(fixture, "barrier")
	client := agenttest.NewFakeClient(liveToolTurn(memoryCall, writeCall), liveFinalTurn())
	gate := &liveCaptureGateSink{
		client: fixture.client, durable: make(chan AcceptedCapture, 2), release: make(chan struct{}),
	}
	t.Cleanup(func() { close(gate.release) })
	queue := NewMemoryCaptureQueue(gate, MemoryCaptureQueueConfig{Capacity: 2, WriteTimeout: 30 * time.Second})
	t.Cleanup(func() { _ = queue.Close(t.Context()) })
	r := fixture.runner(t, client, queue)

	terminal := make(chan struct{})
	done := make(chan error, 1)
	var events []*agent.Event
	go func() {
		for event, err := range r.Turn(t.Context(), fixture.conversationID, new("capture both before completion")) {
			if err != nil {
				done <- err
				return
			}
			events = append(events, event)
			if event != nil && event.LLMResponse != nil && event.LLMResponse.FinishReason != "" {
				close(terminal)
			}
		}
		done <- nil
	}()

	first := <-gate.durable
	if first.SourceKind != CaptureSourceExplicitFact {
		t.Fatalf("first durable sequence = %+v, want explicit fact", first)
	}
	captureSourceFor(t, fixture.client, subject, "lives_in", "capture-live-city-barrier", CaptureSourceExplicitFact)
	select {
	case <-terminal:
		t.Fatal("terminal completion became visible before the first durable sequence released")
	default:
	}
	gate.release <- struct{}{}

	second := <-gate.durable
	if second.SourceKind != CaptureSourceDurableArtifact {
		t.Fatalf("second durable sequence = %+v, want durable artifact", second)
	}
	captureSourceFor(t, fixture.client, path, "durable_artifact", "write", CaptureSourceDurableArtifact)
	select {
	case <-terminal:
		t.Fatal("terminal completion became visible before every accepted sequence released")
	default:
	}
	gate.release <- struct{}{}

	if err := <-done; err != nil {
		t.Fatalf("terminal live turn after durability: %v", err)
	}
	toolEndEvent(t, events, memoryUpsertFactModelName)
	toolEndEvent(t, events, "write_file")
	assertNoCaptureSentinels(t, fixture.client)
}
