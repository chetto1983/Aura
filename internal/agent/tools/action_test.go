package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestActionRouterDispatchKnown(t *testing.T) {
	var gotArgs string
	r := NewActionRouter(map[string]ActionFunc{
		"schedule": func(_ context.Context, args json.RawMessage) (ToolResult, error) {
			gotArgs = string(args)
			return ToolResult{Preview: "scheduled"}, nil
		},
	})

	res, err := r.Dispatch(context.Background(), "schedule", json.RawMessage(`{"action":"schedule","x":1}`))
	if err != nil {
		t.Fatalf("Dispatch known action: unexpected error %v", err)
	}
	if res.Preview != "scheduled" {
		t.Errorf("Preview = %q, want %q", res.Preview, "scheduled")
	}
	if gotArgs != `{"action":"schedule","x":1}` {
		t.Errorf("handler got args %q, want the full raw object", gotArgs)
	}
}

func TestActionRouterUnknownActionIsStructuredError(t *testing.T) {
	r := NewActionRouter(map[string]ActionFunc{
		"list":   func(context.Context, json.RawMessage) (ToolResult, error) { return ToolResult{}, nil },
		"cancel": func(context.Context, json.RawMessage) (ToolResult, error) { return ToolResult{}, nil },
	})

	res, err := r.Dispatch(context.Background(), "frobnicate", nil)
	if err == nil {
		t.Fatal("Dispatch unknown action: want error, got nil")
	}
	if res != (ToolResult{}) {
		t.Errorf("Dispatch unknown action: want zero ToolResult, got %+v", res)
	}
	// The error must name the bad action and the valid set so a hallucinating
	// model self-corrects.
	if !strings.Contains(err.Error(), "frobnicate") {
		t.Errorf("error %q must name the bad action", err.Error())
	}
	for _, want := range []string{"cancel", "list"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q must list the valid action %q", err.Error(), want)
		}
	}
}

func TestActionRouterPropagatesHandlerError(t *testing.T) {
	sentinel := errors.New("boom")
	r := NewActionRouter(map[string]ActionFunc{
		"run_now": func(context.Context, json.RawMessage) (ToolResult, error) { return ToolResult{}, sentinel },
	})
	if _, err := r.Dispatch(context.Background(), "run_now", nil); !errors.Is(err, sentinel) {
		t.Errorf("Dispatch must propagate the handler error, got %v", err)
	}
}

func TestActionRouterActionsAreSorted(t *testing.T) {
	r := NewActionRouter(map[string]ActionFunc{
		"schedule": nil, "list": nil, "cancel": nil, "run_now": nil, "approve": nil,
	})
	got := r.Actions()
	want := []string{"approve", "cancel", "list", "run_now", "schedule"}
	if len(got) != len(want) {
		t.Fatalf("Actions() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Actions()[%d] = %q, want %q (must be sorted)", i, got[i], want[i])
		}
	}
}

func TestActionRouterCopiesHandlerMap(t *testing.T) {
	m := map[string]ActionFunc{"list": func(context.Context, json.RawMessage) (ToolResult, error) { return ToolResult{Preview: "ok"}, nil }}
	r := NewActionRouter(m)
	// Mutating the caller's map after construction must not change dispatch.
	delete(m, "list")
	if _, err := r.Dispatch(context.Background(), "list", nil); err != nil {
		t.Errorf("router must own a copy of the handler map; dispatch failed after caller mutated its map: %v", err)
	}
}
