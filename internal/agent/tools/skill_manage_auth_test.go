package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

const testSkillManageIdentity = "2e5bc3c7-5d4a-48c7-b605-cfdf2d0ddb0c"

type skillManageCapabilityStub struct {
	allow  bool
	err    error
	calls  int
	gotID  string
	gotCap string
}

func (s *skillManageCapabilityStub) HasCapability(_ context.Context, identityID, capability string) (bool, error) {
	s.calls++
	s.gotID = identityID
	s.gotCap = capability
	return s.allow, s.err
}

func authorizedSkillManageCtx(ctx context.Context) context.Context {
	return identityctx.WithIdentityID(ctx, testSkillManageIdentity)
}

func allowedSkillManageCaps() *skillManageCapabilityStub {
	return &skillManageCapabilityStub{allow: true}
}

func skillManageCreateArgs() json.RawMessage {
	return json.RawMessage(`{"action":"create","name":"shared","description":"shared skill","body":"# instructions"}`)
}

func TestSkillManageRequiresGovernanceWriteBeforeMutation(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		caps capabilityChecker
	}{
		{name: "missing capability checker", ctx: authorizedSkillManageCtx(context.Background())},
		{name: "missing authenticated identity", ctx: context.Background(), caps: allowedSkillManageCaps()},
		{name: "capability denied", ctx: authorizedSkillManageCtx(context.Background()), caps: &skillManageCapabilityStub{}},
		{name: "capability store error", ctx: authorizedSkillManageCtx(context.Background()), caps: &skillManageCapabilityStub{err: errors.New("store unavailable")}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			writer := &fakeSkillWriter{}
			tool := &SkillManageTool{Skills: &SkillTool{Writer: writer}, Caps: tc.caps}

			_, err := tool.Execute(tc.ctx, skillManageCreateArgs())
			if err == nil || !strings.Contains(err.Error(), "governance.write") {
				t.Fatalf("Execute error = %v, want governance.write denial", err)
			}
			if writer.calls != 0 {
				t.Fatalf("writer calls = %d, want zero on denied execution", writer.calls)
			}
		})
	}
}

func TestSkillManageGovernanceWriteAllowsMutation(t *testing.T) {
	writer := &fakeSkillWriter{}
	caps := allowedSkillManageCaps()
	tool := &SkillManageTool{Skills: &SkillTool{Writer: writer}, Caps: caps}
	ctx := authorizedSkillManageCtx(withTestToolCallCtx(context.Background()))

	if _, err := tool.Execute(ctx, skillManageCreateArgs()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if writer.calls != 1 {
		t.Fatalf("writer calls = %d, want one", writer.calls)
	}
	if caps.calls != 1 || caps.gotID != testSkillManageIdentity || caps.gotCap != skillManageCapability {
		t.Fatalf("capability check = calls:%d id:%q cap:%q, want 1, %q, %q",
			caps.calls, caps.gotID, caps.gotCap, testSkillManageIdentity, skillManageCapability)
	}
}

func TestSkillManageAuthorizesBeforeParsingArguments(t *testing.T) {
	writer := &fakeSkillWriter{}
	tool := &SkillManageTool{Skills: &SkillTool{Writer: writer}, Caps: &skillManageCapabilityStub{}}
	ctx := authorizedSkillManageCtx(context.Background())

	_, err := tool.Execute(ctx, json.RawMessage(`{"action":`))
	if err == nil || !strings.Contains(err.Error(), "governance.write") {
		t.Fatalf("Execute error = %v, want authorization denial before argument parsing", err)
	}
	if writer.calls != 0 {
		t.Fatalf("writer calls = %d, want zero", writer.calls)
	}
}

func TestSkillManageSpecNamesAdministratorCapability(t *testing.T) {
	spec := (&SkillManageTool{}).Spec()
	if !strings.Contains(spec.Description, skillManageCapability) {
		t.Fatalf("Description = %q, want %q authorization boundary", spec.Description, skillManageCapability)
	}
}
