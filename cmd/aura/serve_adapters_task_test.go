package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
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

func TestCreateScheduledTaskFailsBeforeStoreWhenUnscoped(t *testing.T) {
	t.Parallel()

	store := newCronTaskStore(nil, nil)
	if _, err := store.CreateScheduledTask(context.Background(), tools.CreateTaskInput{}); err == nil {
		t.Fatal("unscoped scheduled task creation must fail before reaching the store")
	}
}
