package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// fakeSpend is the spendBackend double. It records whether /cost consulted the provider
// at all, which is the property under test: the token estimate must not be reached when
// a billed figure is available.
type fakeSpend struct {
	spend llm.Spend
	err   error
	calls int
}

func (f *fakeSpend) Spend(context.Context) (llm.Spend, error) {
	f.calls++
	return f.spend, f.err
}

// countingCost fails the test if the estimate is consulted when it must not be.
type countingCost struct {
	fakeCost
	calls int
}

func (c *countingCost) TodayUsage(ctx context.Context) (int, int, *float64, error) {
	c.calls++
	return c.fakeCost.TodayUsage(ctx)
}

// The provider figure wins outright. The estimate prices cache reads at the full input
// rate and cannot see vision/rerank/embedding traffic at all, so consulting it when a
// billed number exists would replace a measurement with a guess.
func TestCostPrefersTheProviderBilledFigure(t *testing.T) {
	t.Parallel()
	est := &countingCost{fakeCost: fakeCost{prompt: 1_000_000, completion: 1_000_000}}
	sp := &fakeSpend{spend: llm.Spend{Daily: 0.0206, Weekly: 0.0361, Monthly: 0.7442, Total: 2.7434}}

	cmds := newTestCommands(commandDeps{
		Search: &fakeSearch{}, Cost: est, Spend: sp,
		Prices: map[string]llm.Price{"m": {InputPer1M: 0.14, OutputPer1M: 0.28}}, Model: "m",
	})

	handled, reply := cmds.dispatch(context.Background(), 42, "/cost")
	if !handled {
		t.Fatal("/cost must be handled")
	}
	if sp.calls != 1 {
		t.Errorf("provider consulted %d times, want exactly 1", sp.calls)
	}
	if est.calls != 0 {
		t.Errorf("token estimate consulted %d times, want 0 when a billed figure exists", est.calls)
	}
	for _, want := range []string{"$0.0206", "$0.0361", "$0.7442", "$2.7434"} {
		if !strings.Contains(reply, want) {
			t.Errorf("reply missing %q\n  got: %s", want, reply)
		}
	}
	if strings.Contains(reply, "stimata") {
		t.Errorf("a billed figure must not be labelled an estimate: %s", reply)
	}
}

// A local backend bills nothing, so there is no provider figure to prefer. The estimate
// is then the honest answer — and must SAY it is an estimate, because it is derived from
// a rate table rather than measured.
func TestCostFallsBackToTheEstimateAndLabelsIt(t *testing.T) {
	t.Parallel()
	est := &countingCost{fakeCost: fakeCost{prompt: 1_000_000, completion: 1_000_000}}
	sp := &fakeSpend{err: llm.ErrSpendNotApplicable}

	cmds := newTestCommands(commandDeps{
		Search: &fakeSearch{}, Cost: est, Spend: sp,
		Prices: map[string]llm.Price{"m": {InputPer1M: 0.14, OutputPer1M: 0.28}}, Model: "m",
	})

	_, reply := cmds.dispatch(context.Background(), 42, "/cost")
	if est.calls != 1 {
		t.Errorf("token estimate consulted %d times, want 1 when the provider has no figure", est.calls)
	}
	if !strings.Contains(reply, "stimata") {
		t.Errorf("the fallback must declare itself an estimate: %s", reply)
	}
	// 1M in @0.14 + 1M out @0.28 = $0.42
	if !strings.Contains(reply, "$0.420000") {
		t.Errorf("reply missing the table-derived figure: %s", reply)
	}
}

// A nil backend is the pre-wiring state and must degrade to the estimate, never panic.
func TestCostWithoutASpendBackendUsesTheEstimate(t *testing.T) {
	t.Parallel()
	est := &countingCost{fakeCost: fakeCost{prompt: 1000, completion: 1000}}
	cmds := newTestCommands(commandDeps{
		Search: &fakeSearch{}, Cost: est, Spend: nil,
		Prices: map[string]llm.Price{"m": {InputPer1M: 0.14, OutputPer1M: 0.28}}, Model: "m",
	})

	if _, reply := cmds.dispatch(context.Background(), 42, "/cost"); !strings.Contains(reply, "stimata") {
		t.Errorf("nil Spend must fall through to the labelled estimate, got: %s", reply)
	}
	if est.calls != 1 {
		t.Errorf("estimate consulted %d times, want 1", est.calls)
	}
}

// A provider that errors for a real reason (network, 401) must not be silently swallowed
// into a wrong-looking zero — it degrades to the estimate exactly like the local case.
func TestCostDegradesOnProviderError(t *testing.T) {
	t.Parallel()
	est := &countingCost{fakeCost: fakeCost{prompt: 1000, completion: 1000}}
	sp := &fakeSpend{err: errors.New("boom")}
	cmds := newTestCommands(commandDeps{
		Search: &fakeSearch{}, Cost: est, Spend: sp,
		Prices: map[string]llm.Price{"m": {InputPer1M: 0.14, OutputPer1M: 0.28}}, Model: "m",
	})

	_, reply := cmds.dispatch(context.Background(), 42, "/cost")
	if est.calls != 1 {
		t.Errorf("estimate consulted %d times, want 1 after a provider error", est.calls)
	}
	if strings.Contains(reply, "$0.0000") {
		t.Errorf("a failed lookup must not render as zero spend: %s", reply)
	}
}
