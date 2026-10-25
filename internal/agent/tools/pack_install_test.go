package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/packs"
	"github.com/chetto1983/aura/internal/plugins"
)

const (
	pluginRef      = "chetto1983/broker-analysis-mcp/broker-analysis"
	pluginCommit   = "0123456789abcdef0123456789abcdef01234567"
	pluginCommitB  = "fedcba9876543210fedcba9876543210fedcba98"
	pluginDigest   = "1111111111111111111111111111111111111111111111111111111111111111"
	pluginAdmin    = "admin-1"
	pluginSession  = "conv-plugin"
	pluginProposal = `{"action":"install","ref":"` + pluginRef + `"}`
)

// fakePluginBackend answers as internal/plugins.Backend does, recording what reached it.
type fakePluginBackend struct {
	review    plugins.Review
	reviewErr error
	installErr,
	removeErr error
	// partial makes a failing write fail after it began, as one past the claim does: the report
	// then says what was done. Otherwise a failure is a refusal, with an empty report.
	partial  bool
	records  map[string]plugins.Record
	reviews  int
	installs []plugins.InstallRequest
	removes  []string
	actors   []string
}

func newFakePluginBackend() *fakePluginBackend {
	return &fakePluginBackend{
		review: plugins.Review{
			Ref: pluginRef, Commit: pluginCommit, Digest: pluginDigest,
			Name: "broker-analysis", Version: "0.1.1", Format: packs.FormatAgentPlugins,
			Skills:     []plugins.SkillEntry{{Name: "broker-report", Hash: "h"}},
			Connectors: []plugins.Connector{{Name: "broker-analysis", Placement: plugins.PlacementBox}},
		},
		records: map[string]plugins.Record{},
	}
}

func (f *fakePluginBackend) Review(_ context.Context, _ string) (plugins.Review, error) {
	f.reviews++
	return f.review, f.reviewErr
}

func (f *fakePluginBackend) Install(_ context.Context, actor string, req plugins.InstallRequest) (plugins.Report, error) {
	f.installs = append(f.installs, req)
	f.actors = append(f.actors, actor)
	if f.installErr != nil && !f.partial {
		return plugins.Report{}, f.installErr
	}
	return plugins.Report{
		Review: f.review,
		Skills: []plugins.Step{{Name: "broker-report", Outcome: plugins.OutcomeOK}},
		Connectors: []plugins.Step{
			{Name: "broker-analysis", Outcome: plugins.OutcomeOK},
			{Name: "broker-cloud", Outcome: plugins.OutcomeAttention, Reason: "401 Unauthorized"},
		},
	}, f.installErr
}

func (f *fakePluginBackend) Remove(_ context.Context, actor, name string, deleteData bool) (plugins.RemoveReport, error) {
	f.removes = append(f.removes, name+" data="+strconv.FormatBool(deleteData))
	f.actors = append(f.actors, actor)
	if f.removeErr != nil && !f.partial {
		return plugins.RemoveReport{}, f.removeErr
	}
	return plugins.RemoveReport{
		Name:        name,
		Skills:      []plugins.Step{{Name: "broker-report", Outcome: plugins.OutcomeSkipped, Reason: "changed since install"}},
		DataDeleted: deleteData,
	}, f.removeErr
}

func (f *fakePluginBackend) Get(name string) (plugins.Record, bool, error) {
	rec, ok := f.records[name]
	return rec, ok, nil
}

func (f *fakePluginBackend) MayAdminister(_ context.Context, identityID string) (bool, error) {
	return identityID == pluginAdmin, nil
}

func pluginTool(b PluginBackend) *PackTool {
	return &PackTool{Backend: b, Approvals: &PluginApprovals{}}
}

func callPlugin(t *testing.T, tool *PackTool, identity, args string) (ToolResult, error) {
	t.Helper()
	ctx := WithToolCallContext(t.Context(), pluginSession, "call-1", t.TempDir(), 1<<16)
	return tool.Execute(identityctx.WithIdentityID(ctx, identity), json.RawMessage(args))
}

type pluginChallenge struct {
	Error          string            `json:"error"`
	ApprovalSHA256 string            `json:"approval_sha256"`
	Question       string            `json:"question"`
	Repeat         map[string]any    `json:"repeat"`
	ResumeContext  map[string]string `json:"resume_context"`
	Message        string            `json:"message"`
}

func challengeOf(t *testing.T, res ToolResult, err error) pluginChallenge {
	t.Helper()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var c pluginChallenge
	if jerr := json.Unmarshal([]byte(res.Preview), &c); jerr != nil || c.Error != "plugin_approval_required" {
		t.Fatalf("not a plugin approval challenge (%v): %s", jerr, res.Preview)
	}
	return c
}

func repeatArgs(t *testing.T, c pluginChallenge) string {
	t.Helper()
	raw, err := json.Marshal(c.Repeat)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func sha(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func TestPluginPackInstallAsksFirst(t *testing.T) {
	t.Parallel()
	b := newFakePluginBackend()
	tool := pluginTool(b)

	res, err := callPlugin(t, tool, pluginAdmin, pluginProposal)
	c := challengeOf(t, res, err)

	digest := sha("install", pluginRef, pluginCommit, pluginDigest)
	want, qerr := b.review.Question(digest)
	if qerr != nil {
		t.Fatal(qerr)
	}
	if c.ApprovalSHA256 != digest || c.Question != want {
		t.Errorf("challenge digest %q question %q, want %q %q", c.ApprovalSHA256, c.Question, digest, want)
	}
	if !maps.Equal(c.Repeat, map[string]any{"action": "install", "ref": pluginRef, "commit": pluginCommit, "digest": pluginDigest}) {
		t.Errorf("repeat = %v", c.Repeat)
	}
	if !maps.Equal(c.ResumeContext, map[string]string{"type": "plugin_pack_approval", "approval_sha256": digest}) {
		t.Errorf("resume_context = %v", c.ResumeContext)
	}
	if res.Provenance == nil || res.Provenance.Trust != TrustTrusted {
		t.Errorf("provenance = %+v, want trusted", res.Provenance)
	}
	if !strings.Contains(c.Message, "ask_user") {
		t.Errorf("message does not say how to relay it: %q", c.Message)
	}
	if pending, ok := tool.Approvals.PendingChallenge(pluginSession, digest); !ok || pending.Question != want {
		t.Errorf("pending = %+v %v", pending, ok)
	}
	if len(b.installs) != 0 {
		t.Errorf("installed before the operator agreed: %v", b.installs)
	}
}

func TestPluginPackInstallRunsOnceAfterAccept(t *testing.T) {
	t.Parallel()
	b := newFakePluginBackend()
	tool := pluginTool(b)
	res, err := callPlugin(t, tool, pluginAdmin, pluginProposal)
	c := challengeOf(t, res, err)
	if err := tool.Approvals.ApproveChallenge(pluginSession, c.ApprovalSHA256, c.Question); err != nil {
		t.Fatal(err)
	}

	res, err = callPlugin(t, tool, pluginAdmin, repeatArgs(t, c))
	if err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if len(b.installs) != 1 || b.installs[0] != (plugins.InstallRequest{Ref: pluginRef, Commit: pluginCommit, Digest: pluginDigest}) || b.actors[0] != pluginAdmin {
		t.Fatalf("installs %v by %v", b.installs, b.actors)
	}
	for _, want := range []string{"broker-analysis", "broker-report: ok", "broker-cloud: attention — 401 Unauthorized", "Governance → MCP servers"} {
		if !strings.Contains(res.Preview, want) {
			t.Errorf("report misses %q:\n%s", want, res.Preview)
		}
	}

	res, err = callPlugin(t, tool, pluginAdmin, repeatArgs(t, c))
	challengeOf(t, res, err)
	if len(b.installs) != 1 {
		t.Fatalf("one approval installed %d times", len(b.installs))
	}
}

func TestPluginPackRelayedQuestionMismatchRecordsNothing(t *testing.T) {
	t.Parallel()
	b := newFakePluginBackend()
	tool := pluginTool(b)
	res, err := callPlugin(t, tool, pluginAdmin, pluginProposal)
	c := challengeOf(t, res, err)

	if err := tool.Approvals.ApproveChallenge(pluginSession, c.ApprovalSHA256, "Approve a harmless read?"); err == nil {
		t.Fatal("a different question was approved")
	}
	res, err = callPlugin(t, tool, pluginAdmin, repeatArgs(t, c))
	challengeOf(t, res, err)
	if len(b.installs) != 0 {
		t.Fatalf("installed on a mismatched question: %v", b.installs)
	}
}

func TestPluginPackRepeatWithAnotherCommitIsNotApproved(t *testing.T) {
	t.Parallel()
	b := newFakePluginBackend()
	tool := pluginTool(b)
	res, err := callPlugin(t, tool, pluginAdmin, pluginProposal)
	c := challengeOf(t, res, err)
	if err := tool.Approvals.ApproveChallenge(pluginSession, c.ApprovalSHA256, c.Question); err != nil {
		t.Fatal(err)
	}

	c.Repeat["commit"] = pluginCommitB
	res, err = callPlugin(t, tool, pluginAdmin, repeatArgs(t, c))
	challengeOf(t, res, err)
	if len(b.installs) != 0 {
		t.Fatalf("an approval for one commit installed another: %v", b.installs)
	}
}

func TestPluginPackRefusesAMember(t *testing.T) {
	t.Parallel()
	b := newFakePluginBackend()
	b.records["broker-analysis"] = plugins.Record{Name: "broker-analysis", Ref: pluginRef, Commit: pluginCommit}
	tool := pluginTool(b)

	for _, args := range []string{pluginProposal, `{"action":"remove","name":"broker-analysis"}`} {
		_, err := callPlugin(t, tool, "member-1", args)
		if err == nil || !strings.Contains(err.Error(), "Governance → Plugin") {
			t.Fatalf("%s: err = %v, want a refusal naming Governance → Plugins", args, err)
		}
	}
	if b.reviews != 0 {
		t.Errorf("a member's request reached the review %d times", b.reviews)
	}
	if _, ok := tool.Approvals.PendingChallenge(pluginSession, sha("install", pluginRef, pluginCommit, pluginDigest)); ok {
		t.Error("a member's request left a challenge")
	}
	if _, ok := tool.Approvals.PendingChallenge(pluginSession, sha("remove", "broker-analysis", pluginCommit, "false")); ok {
		t.Error("a member's removal left a challenge")
	}
}

func TestPluginPackRemoveAsksFirstAndBindsTheCommit(t *testing.T) {
	t.Parallel()
	b := newFakePluginBackend()
	rec := plugins.Record{Name: "broker-analysis", Ref: pluginRef, Commit: pluginCommit, Version: "0.1.1",
		Skills: []plugins.SkillEntry{{Name: "broker-report"}}, Connectors: []string{"broker-analysis"}}
	b.records[rec.Name] = rec
	tool := pluginTool(b)

	res, err := callPlugin(t, tool, pluginAdmin, `{"action":"remove","name":"broker-analysis","delete_data":true}`)
	c := challengeOf(t, res, err)
	digest := sha("remove", "broker-analysis", pluginCommit, "true")
	want, qerr := plugins.RemovalQuestion(rec, true, digest)
	if qerr != nil {
		t.Fatal(qerr)
	}
	if c.ApprovalSHA256 != digest || c.Question != want {
		t.Errorf("challenge %q %q, want %q %q", c.ApprovalSHA256, c.Question, digest, want)
	}
	if !maps.Equal(c.Repeat, map[string]any{"action": "remove", "name": "broker-analysis", "commit": pluginCommit, "delete_data": true}) {
		t.Errorf("repeat = %v", c.Repeat)
	}
	if err := tool.Approvals.ApproveChallenge(pluginSession, c.ApprovalSHA256, c.Question); err != nil {
		t.Fatal(err)
	}

	reinstalled := rec
	reinstalled.Commit = pluginCommitB
	b.records[rec.Name] = reinstalled
	res, err = callPlugin(t, tool, pluginAdmin, repeatArgs(t, c))
	challengeOf(t, res, err)
	if len(b.removes) != 0 {
		t.Fatalf("an approval for one install removed another: %v", b.removes)
	}

	b.records[rec.Name] = rec
	res, err = callPlugin(t, tool, pluginAdmin, repeatArgs(t, c))
	if err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if !slices.Equal(b.removes, []string{"broker-analysis data=true"}) || b.actors[0] != pluginAdmin {
		t.Fatalf("removes %v by %v", b.removes, b.actors)
	}
	for _, want := range []string{"broker-analysis", "deleted", "broker-report: skipped — changed since install"} {
		if !strings.Contains(res.Preview, want) {
			t.Errorf("report misses %q:\n%s", want, res.Preview)
		}
	}
}

func TestPluginPackWithoutABackendSaysSo(t *testing.T) {
	t.Parallel()
	tool := &PackTool{Approvals: &PluginApprovals{}}
	for _, args := range []string{pluginProposal, `{"action":"remove","name":"broker-analysis"}`} {
		if _, err := callPlugin(t, tool, pluginAdmin, args); err == nil || !strings.Contains(err.Error(), "not available") {
			t.Errorf("%s: err = %v, want it to say installing is not available", args, err)
		}
	}
}

// A proposal and its repeat must not share an operation fingerprint: the idempotency layer
// would otherwise answer the repeat with the recorded challenge instead of running it.
func TestPluginPackRepeatFingerprintDiffersFromTheProposal(t *testing.T) {
	t.Parallel()
	b := newFakePluginBackend()
	b.records["broker-analysis"] = plugins.Record{Name: "broker-analysis", Ref: pluginRef, Commit: pluginCommit}
	tool := pluginTool(b)
	spec := tool.Spec()

	for _, proposal := range []string{pluginProposal, `{"action":"remove","name":"broker-analysis"}`} {
		res, err := callPlugin(t, tool, pluginAdmin, proposal)
		c := challengeOf(t, res, err)
		first, ferr := OperationFingerprint(spec, json.RawMessage(proposal))
		second, serr := OperationFingerprint(spec, json.RawMessage(repeatArgs(t, c)))
		if ferr != nil || serr != nil {
			t.Fatalf("fingerprint: %v %v", ferr, serr)
		}
		if first == second {
			t.Errorf("%s: the repeat has the proposal's fingerprint", proposal)
		}
	}
}

func TestPluginPackSurfacesBackendFailures(t *testing.T) {
	t.Parallel()
	boom := errors.New("fetch chetto1983/broker-analysis-mcp: exit status 128")

	b := newFakePluginBackend()
	b.reviewErr = boom
	if _, err := callPlugin(t, pluginTool(b), pluginAdmin, pluginProposal); !errors.Is(err, boom) {
		t.Errorf("review failure: err = %v", err)
	}

	b = newFakePluginBackend()
	tool := pluginTool(b)
	res, err := callPlugin(t, tool, pluginAdmin, pluginProposal)
	c := challengeOf(t, res, err)
	if err := tool.Approvals.ApproveChallenge(pluginSession, c.ApprovalSHA256, c.Question); err != nil {
		t.Fatal(err)
	}
	b.installErr = boom
	if _, err := callPlugin(t, tool, pluginAdmin, repeatArgs(t, c)); !errors.Is(err, boom) {
		t.Errorf("install failure: err = %v", err)
	}

	if _, err := callPlugin(t, tool, pluginAdmin, `{"action":"remove","name":"nope"}`); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("remove of an unknown plugin: err = %v", err)
	}
}

// A write that stopped part way changed something, so the agent reads what was done beside why
// it stopped, and can tell the operator.
func TestPluginPackReportsWhatAPartialWriteDid(t *testing.T) {
	t.Parallel()
	stop := errors.New("save the connectors of broker-analysis: database is unreachable")
	b := newFakePluginBackend()
	b.installErr, b.removeErr, b.partial = stop, stop, true
	b.records["broker-analysis"] = plugins.Record{Name: "broker-analysis", Ref: pluginRef, Commit: pluginCommit, Version: "0.1.1"}
	tool := pluginTool(b)
	for _, proposal := range []string{pluginProposal, `{"action":"remove","name":"broker-analysis"}`} {
		res, err := callPlugin(t, tool, pluginAdmin, proposal)
		c := challengeOf(t, res, err)
		if err := tool.Approvals.ApproveChallenge(pluginSession, c.ApprovalSHA256, c.Question); err != nil {
			t.Fatal(err)
		}
		res, err = callPlugin(t, tool, pluginAdmin, repeatArgs(t, c))
		if err != nil {
			t.Fatalf("%s: a partial write answered only its error: %v", proposal, err)
		}
		for _, want := range []string{"stopped part way", "database is unreachable", "broker-report: "} {
			if !strings.Contains(res.Preview, want) {
				t.Errorf("%s: the report misses %q:\n%s", proposal, want, res.Preview)
			}
		}
	}
}
