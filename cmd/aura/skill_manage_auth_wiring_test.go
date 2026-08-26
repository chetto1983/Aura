package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
)

type skillManageWiringCaps struct {
	calls int
	id    string
	cap   string
}

func (c *skillManageWiringCaps) HasCapability(_ context.Context, identityID, capability string) (bool, error) {
	c.calls++
	c.id = identityID
	c.cap = capability
	return true, nil
}

func TestSkillManageRegistryHandleIsExactAndCapabilityWired(t *testing.T) {
	reg, handles := buildBaseRegistryWithHandles(&config.Config{MaxSwarmGoals: 1}, nil, nil)
	if handles.SkillManage == nil {
		t.Fatal("runtime handles did not retain skill_manage")
	}
	registered, ok := reg.Get("skill_manage")
	if !ok {
		t.Fatal("registry has no skill_manage")
	}
	if registered != handles.SkillManage {
		t.Fatal("runtime handle is not the exact skill_manage instance in the registry")
	}

	caps := &skillManageWiringCaps{}
	wireSkillManageCapability(&handles, caps)
	identityID := "69f08576-c19f-4656-ac85-d359c848c6ac"
	ctx := identityctx.WithIdentityID(context.Background(), identityID)
	_, err := handles.SkillManage.Execute(ctx, json.RawMessage(`{"action":"create","name":"x","description":"x","body":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "no writer") {
		t.Fatalf("Execute error = %v, want authorization to pass and pool-free writer to fail", err)
	}
	if caps.calls != 1 || caps.id != identityID || caps.cap != governanceWriteCapability {
		t.Fatalf("capability check = calls:%d id:%q cap:%q, want 1, %q, %q",
			caps.calls, caps.id, caps.cap, identityID, governanceWriteCapability)
	}
}
