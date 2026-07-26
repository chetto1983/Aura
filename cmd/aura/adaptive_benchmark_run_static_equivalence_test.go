package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/documents"
	auraeval "github.com/chetto1983/aura/internal/eval"
)

func TestAdaptiveBenchmarkValidateStaticEquivalenceRejectsServedByteDrift(
	t *testing.T,
) {
	t.Parallel()
	result := adaptiveBenchmarkStaticEquivalenceResult()
	if err := adaptiveBenchmarkValidateStaticEquivalence(
		[]byte("414"),
		[]byte("414\n"),
		result,
	); err == nil {
		t.Fatal("static equivalence accepted different served champion bytes")
	}
}

func TestAdaptiveBenchmarkValidateStaticEquivalenceRequiresExactProbability(
	t *testing.T,
) {
	t.Parallel()
	result := adaptiveBenchmarkStaticEquivalenceResult()
	result.ActionProbabilities[3].Probability = 0.9999999999999999
	if err := adaptiveBenchmarkValidateStaticEquivalence(
		[]byte("414"),
		[]byte("414"),
		result,
	); err == nil {
		t.Fatal("static equivalence accepted an inexact champion probability")
	}
}

func TestAdaptiveBenchmarkValidateStaticEquivalenceAllowsDiagnosticRecommendation(
	t *testing.T,
) {
	t.Parallel()
	result := adaptiveBenchmarkStaticEquivalenceResult()
	result.RecommendedActionID = "reasoning_high"
	if err := adaptiveBenchmarkValidateStaticEquivalence(
		[]byte{0x34, 0x31, 0x34},
		[]byte{0x34, 0x31, 0x34},
		result,
	); err != nil {
		t.Fatalf("static equivalence rejected exact served bytes: %v", err)
	}
}

func TestAdaptiveBenchmarkStaticRegistryRemovesEveryAdaptiveControl(
	t *testing.T,
) {
	t.Parallel()
	full := adaptiveBenchmarkRegistryFixture(t)
	search, _ := full.Get("tool_search")
	search.(*tools.ToolSearch).Adaptive =
		adaptiveBenchmarkToolDiscoveryControlStub{}
	documentSearch, _ := full.Get("document_search")
	documentSearch.(*tools.DocumentSearch).Adaptive =
		adaptiveBenchmarkDocumentRetrievalControlStub{}
	skill, _ := full.Get("skill")
	skill.(*tools.SkillTool).Adaptive =
		adaptiveBenchmarkSkillRoutingControlStub{}

	static, err := adaptiveBenchmarkStaticRegistry(full)
	if err != nil {
		t.Fatalf("adaptiveBenchmarkStaticRegistry: %v", err)
	}
	staticSearch, _ := static.Get("tool_search")
	staticDocumentSearch, _ := static.Get("document_search")
	staticSkill, _ := static.Get("skill")
	restrictedSkill := staticSkill.(adaptiveBenchmarkRestrictedSkill)
	if staticSearch.(*tools.ToolSearch).Adaptive != nil ||
		staticDocumentSearch.(*tools.DocumentSearch).Adaptive != nil ||
		restrictedSkill.delegate.Adaptive != nil {
		t.Fatal("static baseline registry retained an adaptive control")
	}
	if search.(*tools.ToolSearch).Adaptive == nil ||
		documentSearch.(*tools.DocumentSearch).Adaptive == nil ||
		skill.(*tools.SkillTool).Adaptive == nil {
		t.Fatal("static baseline registry mutated the shadow registry")
	}
}

func TestAdaptiveBenchmarkStaticRegistryAcceptsRestrictedProductionRegistry(
	t *testing.T,
) {
	t.Parallel()
	full := adaptiveBenchmarkRegistryFixture(t)
	restricted, err := restrictAdaptiveBenchmarkRegistry(full)
	if err != nil {
		t.Fatalf("restrictAdaptiveBenchmarkRegistry: %v", err)
	}

	static, err := adaptiveBenchmarkStaticRegistry(restricted)
	if err != nil {
		t.Fatalf("adaptiveBenchmarkStaticRegistry: %v", err)
	}
	staticSkill, ok := static.Get("skill")
	if !ok {
		t.Fatal("static registry lacks skill")
	}
	skill, ok := staticSkill.(adaptiveBenchmarkRestrictedSkill)
	if !ok || skill.delegate == nil || skill.delegate.Adaptive != nil {
		t.Fatalf("static skill = %#v", staticSkill)
	}
}

func adaptiveBenchmarkStaticEquivalenceResult() auraeval.AdaptiveBenchmarkDriverResult {
	return auraeval.AdaptiveBenchmarkDriverResult{
		ChampionActionID: adaptive.StaticActionID,
		IntendedActionID: adaptive.StaticActionID,
		ActualActionID:   adaptive.StaticActionID,
		ActionProbabilities: []auraeval.AdaptiveBenchmarkActionProbability{
			{ActionID: "reasoning_high", Probability: 0},
			{ActionID: "reasoning_low", Probability: 0},
			{ActionID: "reasoning_none", Probability: 0},
			{ActionID: adaptive.StaticActionID, Probability: 1},
		},
	}
}

type adaptiveBenchmarkToolDiscoveryControlStub struct{}

func (adaptiveBenchmarkToolDiscoveryControlStub) OrderToolDiscovery(
	context.Context,
	tools.ToolDiscoveryInput,
	tools.ToolDiscoveryExecutor,
) ([]string, error) {
	panic("static baseline invoked adaptive tool discovery")
}

type adaptiveBenchmarkSkillRoutingControlStub struct{}

func (adaptiveBenchmarkSkillRoutingControlStub) OrderSkillRouting(
	context.Context,
	tools.SkillRoutingInput,
	tools.SkillRoutingExecutor,
) ([]string, error) {
	panic("static baseline invoked adaptive skill routing")
}

type adaptiveBenchmarkDocumentRetrievalControlStub struct{}

func (adaptiveBenchmarkDocumentRetrievalControlStub) RetrieveDocuments(
	context.Context,
	tools.DocumentRetrievalInput,
	tools.DocumentRetrievalExecutor,
	tools.DocumentRetrievalExecutor,
) (documents.RetrievalResult, error) {
	panic("static baseline invoked adaptive document retrieval")
}
