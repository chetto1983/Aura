package main

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/identityctx"
)

func TestTaskIdentityFailsClosedAndReturnsCaller(t *testing.T) {
	t.Parallel()

	if _, err := taskIdentity(context.Background()); err == nil {
		t.Fatal("unscoped task operation must fail closed")
	}
	const owner = "10000000-0000-4000-8000-000000000001"
	got, err := taskIdentity(identityctx.WithIdentityID(context.Background(), owner))
	if err != nil {
		t.Fatal(err)
	}
	if got != owner {
		t.Fatalf("task identity = %q, want %q", got, owner)
	}
}

// TestTaskToolChecksRoutesOnlyWithAStore: the pool-free manifest path builds the tool to
// read its Spec; only the live path, which persists, asks the scheduler notifier first,
// and that notifier refuses an external route no mounted MCP tool can send.
func TestTaskToolChecksRoutesOnlyWithAStore(t *testing.T) {
	t.Parallel()

	notifier := newSchedulerNotifier(&config.Config{}, tools.NewRegistry())
	if got := newTaskTool(nil, notifier); got.Destinations != nil {
		t.Fatal("the pool-free task tool must not carry a route check")
	}
	if got := newTaskTool(newCronTaskStore(nil, nil), notifier); got.Destinations == nil {
		t.Fatal("the live task tool must check a route before persisting")
	}
	owner := identityctx.WithIdentityID(context.Background(), "10000000-0000-4000-8000-000000000001")
	if _, err := notifier.Destination(owner, cron.RouteWhatsApp); err == nil || !strings.Contains(err.Error(), "no mounted MCP tool") {
		t.Fatalf("Destination(whatsapp) with nothing mounted = %v, want the missing-tool reason", err)
	}
}

func TestCreateScheduledTaskFailsBeforeStoreWhenUnscoped(t *testing.T) {
	t.Parallel()

	store := newCronTaskStore(nil, nil)
	if _, err := store.CreateScheduledTask(context.Background(), tools.CreateTaskInput{}); err == nil {
		t.Fatal("unscoped scheduled task creation must fail before reaching the store")
	}
}
