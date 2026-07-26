package tools

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
)

type skillRoutingControlFunc func(
	context.Context,
	SkillRoutingInput,
	SkillRoutingExecutor,
) ([]string, error)

func (fn skillRoutingControlFunc) OrderSkillRouting(
	ctx context.Context,
	input SkillRoutingInput,
	execute SkillRoutingExecutor,
) ([]string, error) {
	return fn(ctx, input, execute)
}

func TestSkillToolAdaptsOnlyNonblankListQueries(t *testing.T) {
	loader := newFakeLoader()
	var mu sync.Mutex
	calls := 0
	control := skillRoutingControlFunc(func(
		ctx context.Context,
		input SkillRoutingInput,
		execute SkillRoutingExecutor,
	) ([]string, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		if input.QueryLength == 0 {
			t.Fatal("blank query crossed the adaptive port")
		}
		return execute(ctx, SkillRoutingStatic)
	})
	tool := &SkillTool{Loader: loader, Adaptive: control}
	ctx := withTestToolCallCtx(context.Background())

	exogenous := []json.RawMessage{
		json.RawMessage(`{"action":"list","query":"   "}`),
		json.RawMessage(`{"action":"info","name":"alpha"}`),
		json.RawMessage(`{"action":"use","name":"alpha"}`),
		json.RawMessage(`{"action":"create","name":"new","description":"new","body":"body"}`),
		json.RawMessage(`{"action":"update","name":"alpha","description":"new","body":"body"}`),
		json.RawMessage(`{"action":"delete","name":"alpha"}`),
		json.RawMessage(`{"action":"save_snippet","name":"new","language":"python","code":"print(1)"}`),
		json.RawMessage(`{"action":"restore","name":"alpha"}`),
		json.RawMessage(`{"action":"archive","name":"alpha"}`),
	}
	for _, raw := range exogenous {
		_, _ = tool.Execute(ctx, raw)
	}
	mu.Lock()
	before := calls
	mu.Unlock()
	if before != 0 {
		t.Fatalf("exogenous skill actions armed %d decisions", before)
	}

	if _, err := tool.Execute(
		ctx,
		json.RawMessage(`{"action":"list","query":"excel"}`),
	); err != nil {
		t.Fatalf("queried list: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("queried list decisions = %d, want 1", calls)
	}
}

func TestSkillToolFreezesCandidatesBeforeAdaptiveRanking(t *testing.T) {
	loader := newFakeLoader()
	tool := &SkillTool{
		Loader: loader,
		Adaptive: skillRoutingControlFunc(func(
			ctx context.Context,
			input SkillRoutingInput,
			execute SkillRoutingExecutor,
		) ([]string, error) {
			if !slices.Equal(
				input.CandidateIDs,
				[]string{"alpha", "bravo"},
			) {
				t.Fatalf("candidate IDs = %v", input.CandidateIDs)
			}
			loader.skills[0].Description = "tampered after freeze"
			return execute(ctx, SkillRoutingCatalog)
		}),
	}

	result, err := tool.Execute(
		withTestToolCallCtx(context.Background()),
		json.RawMessage(`{"action":"list","query":"document"}`),
	)
	if err != nil {
		t.Fatalf("queried list: %v", err)
	}
	if strings.Contains(result.Preview, "tampered after freeze") ||
		!strings.Contains(
			result.Preview,
			"Alpha excel spreadsheet skill.",
		) {
		t.Fatalf("result did not use frozen candidates: %q", result.Preview)
	}
}

func TestSkillToolAdaptiveFailureAndTamperMatchStaticBytes(t *testing.T) {
	ctx := withTestToolCallCtx(context.Background())
	raw := json.RawMessage(
		`{"action":"list","query":"excel spreadsheet"}`,
	)
	baseline, err := (&SkillTool{
		Loader: newFakeLoader(),
	}).Execute(ctx, raw)
	if err != nil {
		t.Fatalf("static list: %v", err)
	}

	tests := []struct {
		name    string
		control SkillRoutingControl
	}{
		{
			name: "control failure",
			control: skillRoutingControlFunc(func(
				context.Context,
				SkillRoutingInput,
				SkillRoutingExecutor,
			) ([]string, error) {
				return nil, errors.New("adaptive unavailable")
			}),
		},
		{
			name: "unknown skill",
			control: skillRoutingControlFunc(func(
				context.Context,
				SkillRoutingInput,
				SkillRoutingExecutor,
			) ([]string, error) {
				return []string{"unknown"}, nil
			}),
		},
		{
			name: "duplicate skill",
			control: skillRoutingControlFunc(func(
				context.Context,
				SkillRoutingInput,
				SkillRoutingExecutor,
			) ([]string, error) {
				return []string{"alpha", "alpha"}, nil
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := (&SkillTool{
				Loader:   newFakeLoader(),
				Adaptive: test.control,
			}).Execute(ctx, raw)
			if err != nil {
				t.Fatalf("adaptive fallback: %v", err)
			}
			if result.Preview != baseline.Preview {
				t.Fatalf(
					"fallback bytes differ:\n%s\n---\n%s",
					result.Preview,
					baseline.Preview,
				)
			}
		})
	}
}

func TestSkillCatalogRevisionUsesOnlyOrderedRegisteredIDs(t *testing.T) {
	first := []SkillMeta{
		{Name: "alpha", Description: "first description"},
		{Name: "bravo", Description: "second description"},
	}
	second := []SkillMeta{
		{Name: "alpha", Description: "changed private content"},
		{Name: "bravo", Description: "also changed"},
	}
	if got, want := freezeSkills(first).catalogRevision,
		freezeSkills(second).catalogRevision; got != want {
		t.Fatalf("catalog revision changed with descriptions: %q != %q", got, want)
	}
}
