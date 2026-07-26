package main

import (
	"bytes"
	"os"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/cachemetrics"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/runner"
)

func adaptiveBenchmarkValidateStaticEquivalence(
	baseline []byte,
	served []byte,
	result auraeval.AdaptiveBenchmarkDriverResult,
) error {
	if err := adaptiveBenchmarkValidateStaticResult(result); err != nil {
		return err
	}
	if !bytes.Equal(baseline, served) {
		return adaptiveBenchmarkControlError(
			"adaptive benchmark served champion bytes drifted from static baseline",
		)
	}
	return nil
}

func adaptiveBenchmarkStaticRegistry(
	full *tools.Registry,
) (*tools.Registry, error) {
	if _, err := restrictAdaptiveBenchmarkRegistry(full); err != nil {
		return nil, err
	}
	textResponse, _ := full.Get("text_response")
	sourceDocument, _ := full.Get("document_search")
	documentSearch := sourceDocument.(*tools.DocumentSearch)
	sourceSkill, _ := full.Get("skill")
	skill := sourceSkill.(*tools.SkillTool)
	sourceSearch, _ := full.Get("tool_search")
	search := sourceSearch.(*tools.ToolSearch)

	static := tools.NewRegistry()
	static.Register(textResponse)
	static.Register(&tools.DocumentSearch{Searcher: documentSearch.Searcher})
	if readOutput, exists := full.Get("read_tool_output"); exists {
		static.Register(readOutput)
	}
	staticSkill := &tools.SkillTool{Loader: skill.Loader}
	static.Register(adaptiveBenchmarkRestrictedSkill{delegate: staticSkill})
	static.Register(&tools.ToolSearch{Registry: static, Embed: search.Embed})
	if err := static.Validate(); err != nil {
		return nil, err
	}
	return static, nil
}

func adaptiveBenchmarkStaticRunner(env *chatEnv) (*runner.Runner, error) {
	if env == nil || env.cfg == nil || env.pool == nil || env.conv == nil ||
		env.pause == nil || env.identity == nil || env.client == nil ||
		env.reg == nil || env.gateway == nil || env.toolInvocations == nil {
		return nil, adaptiveBenchmarkControlError(
			"adaptive benchmark static baseline is unavailable",
		)
	}
	registry, err := adaptiveBenchmarkStaticRegistry(env.reg)
	if err != nil {
		return nil, err
	}
	hooks, err := agent.CommandHookManagerFromEnv(os.LookupEnv)
	if err != nil {
		return nil, err
	}
	cfg := env.cfg
	return runner.New(runner.Deps{
		Conv: env.conv, Pause: env.pause, Identity: env.identity,
		CacheMetrics:    cachemetrics.New(env.pool),
		ToolInvocations: env.toolInvocations,
		ResumeCommitter: runner.NewPoolResumeCommitter(
			env.pool, env.conv, env.pause,
		),
		Client: env.client, Registry: registry, LLM: cfg.LLM,
		RunDir: cfg.RunDir, Workspace: cfg.WorkspaceDir,
		PreviewCap:               cfg.ToolPreviewCap,
		EvictAfter:               cfg.ContextToolEvictAfterTurns,
		ReasoningPersistMaxRunes: cfg.ReasoningPersistMaxRunes,
		RecallMaxItems:           cfg.MemoryRecallMaxItems,
		AlwaysBlock:              alwaysBlockProvider(cfg),
		HookManager:              hooks, Gateway: env.gateway,
		Embedder: embeddingClient(cfg, documentHTTPClient(cfg)),
	}), nil
}
