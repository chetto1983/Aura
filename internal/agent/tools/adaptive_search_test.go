package tools

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

type toolDiscoveryControlFunc func(
	context.Context,
	ToolDiscoveryInput,
	ToolDiscoveryExecutor,
) ([]string, error)

func (fn toolDiscoveryControlFunc) OrderToolDiscovery(
	ctx context.Context,
	input ToolDiscoveryInput,
	execute ToolDiscoveryExecutor,
) ([]string, error) {
	return fn(ctx, input, execute)
}

func TestToolSearchAdaptsOnlyFreeTextDiscovery(t *testing.T) {
	ts, ctx := newSearch(t,
		bm25Tool{name: "calculator", desc: "evaluate arithmetic expressions"},
		bm25Tool{name: "calendar", desc: "list meetings and appointments"},
	)
	calls := 0
	ts.Adaptive = toolDiscoveryControlFunc(func(
		ctx context.Context,
		input ToolDiscoveryInput,
		execute ToolDiscoveryExecutor,
	) ([]string, error) {
		calls++
		return execute(ctx, ToolDiscoveryStatic)
	})

	if _, err := ts.Execute(
		ctx,
		json.RawMessage(`{"query":"select:calculator"}`),
	); err != nil {
		t.Fatalf("select Execute: %v", err)
	}
	if calls != 0 {
		t.Fatalf("select: invoked adaptive control %d time(s)", calls)
	}

	if _, err := ts.Execute(
		ctx,
		json.RawMessage(`{"query":"arithmetic","max_results":1}`),
	); err != nil {
		t.Fatalf("free-text Execute: %v", err)
	}
	if calls != 1 {
		t.Fatalf("free-text adaptive calls = %d, want 1", calls)
	}
}

func TestToolSearchFreezesCandidatesBeforeAdaptiveRanking(t *testing.T) {
	reg := NewRegistry()
	reg.Register(bm25Tool{name: "alpha_tool", desc: "shared alpha capability"})
	reg.Register(bm25Tool{name: "beta_tool", desc: "shared beta capability"})
	ts := &ToolSearch{Registry: reg, Embed: &bagEmbedder{}}
	ctx := ctxWith(t, "adaptive-freeze", "call-freeze")

	ts.Adaptive = toolDiscoveryControlFunc(func(
		ctx context.Context,
		input ToolDiscoveryInput,
		execute ToolDiscoveryExecutor,
	) ([]string, error) {
		wantCandidates := []string{"alpha_tool", "beta_tool"}
		if !slices.Equal(input.CandidateIDs, wantCandidates) {
			t.Fatalf("candidate IDs = %v, want %v", input.CandidateIDs, wantCandidates)
		}
		if !strings.HasPrefix(input.CatalogRevision, "tool-catalog-") {
			t.Fatalf("catalog revision = %q", input.CatalogRevision)
		}
		reg.Register(bm25Tool{
			name: "late_tool",
			desc: "shared capability mounted after candidate freeze",
		})
		return execute(ctx, ToolDiscoveryStatic)
	})

	result, err := ts.Execute(
		ctx,
		json.RawMessage(`{"query":"shared capability","max_results":5}`),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(result.Preview, "late_tool") {
		t.Fatalf("post-freeze tool leaked into adaptive result: %q", result.Preview)
	}
	names, ok := activatedNames(result)
	if !ok || !slices.Equal(names, []string{"alpha_tool", "beta_tool"}) {
		t.Fatalf("activated names = %v, ok=%t", names, ok)
	}
}

func TestToolSearchAdaptiveBoundaryPrecedesPromotion(t *testing.T) {
	ts, ctx := newSearch(t,
		bm25Tool{name: "calculator", desc: "evaluate arithmetic expressions"},
	)
	var order []string
	ts.Adaptive = toolDiscoveryControlFunc(func(
		ctx context.Context,
		_ ToolDiscoveryInput,
		execute ToolDiscoveryExecutor,
	) ([]string, error) {
		order = append(order, "assignment")
		ids, err := execute(ctx, ToolDiscoveryStatic)
		order = append(order, "rank")
		if err != nil {
			return nil, err
		}
		order = append(order, "delivery")
		return ids, nil
	})

	result, err := ts.Execute(
		ctx,
		json.RawMessage(`{"query":"arithmetic","max_results":1}`),
	)
	order = append(order, "exposure")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !slices.Equal(order, []string{
		"assignment", "rank", "delivery", "exposure",
	}) {
		t.Fatalf("order = %v", order)
	}
	names, ok := activatedNames(result)
	if !ok || !slices.Equal(names, []string{"calculator"}) {
		t.Fatalf("activated names = %v, ok=%t", names, ok)
	}
}

func TestToolSearchAdaptiveFailureAndTamperReturnStaticOrdering(t *testing.T) {
	makeSearch := func(t *testing.T) (*ToolSearch, context.Context) {
		t.Helper()
		return newSearch(t,
			bm25Tool{name: "calculator", desc: "evaluate arithmetic expressions"},
			bm25Tool{name: "calendar", desc: "list meetings and appointments"},
		)
	}
	staticSearch, staticCtx := makeSearch(t)
	staticResult, err := staticSearch.Execute(
		staticCtx,
		json.RawMessage(`{"query":"arithmetic","max_results":2}`),
	)
	if err != nil {
		t.Fatalf("static Execute: %v", err)
	}
	staticNames, _ := activatedNames(staticResult)

	tests := []struct {
		name    string
		control toolDiscoveryControlFunc
	}{
		{
			name: "control failure",
			control: func(
				context.Context,
				ToolDiscoveryInput,
				ToolDiscoveryExecutor,
			) ([]string, error) {
				return nil, errors.New("assignment persistence failed")
			},
		},
		{
			name: "unknown returned ID",
			control: func(
				context.Context,
				ToolDiscoveryInput,
				ToolDiscoveryExecutor,
			) ([]string, error) {
				return []string{"unregistered_tool"}, nil
			},
		},
		{
			name: "duplicate returned ID",
			control: func(
				context.Context,
				ToolDiscoveryInput,
				ToolDiscoveryExecutor,
			) ([]string, error) {
				return []string{"calculator", "calculator"}, nil
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts, ctx := makeSearch(t)
			ts.Adaptive = test.control
			result, err := ts.Execute(
				ctx,
				json.RawMessage(`{"query":"arithmetic","max_results":2}`),
			)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			names, ok := activatedNames(result)
			if !ok || !slices.Equal(names, staticNames) ||
				result.Preview != staticResult.Preview {
				t.Fatalf(
					"fallback = names %v preview %q, want %v / %q",
					names, result.Preview, staticNames, staticResult.Preview,
				)
			}
		})
	}
}
