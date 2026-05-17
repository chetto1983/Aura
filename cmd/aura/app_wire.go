package main

// wireBot lives in this file (separated from cmd/aura/app.go) so app.go can
// stay under the 600-LOC god-class threshold. wireBot is the per-bot wiring
// pass that hooks the tool index, scheduler, conversation summarizer,
// telegram channel adapter, and dashboard token request hook into the
// already-constructed App + deps + bot. It runs once, after newApp() returns
// and before Start(bot).

import (
	"context"
	"fmt"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
	toolindex "github.com/aura/aura/internal/agent/tools/index"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/agentnote"
	"github.com/aura/aura/internal/api"
	cronadapter "github.com/aura/aura/internal/channels/cron"
	silentadapter "github.com/aura/aura/internal/channels/silent"
	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/cron"
	"github.com/aura/aura/internal/learning"
	"github.com/aura/aura/internal/mcp"
	secretspkg "github.com/aura/aura/internal/secrets"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/storage/search"
	"github.com/aura/aura/internal/telegram"
)

func (a *App) wireBot(b *telegram.Bot) error {
	cfg := a.deps.Cfg
	logger := a.deps.Logger
	loc := a.deps.Loc

	// ---- Agent note GC hook: clear per-conversation note when session is cleared ----
	if a.deps.AgentNoteStore != nil {
		noteStore := a.deps.AgentNoteStore
		b.SessionStore().OnClose(func(userID string) {
			if err := noteStore.Clear(context.Background(), userID); err != nil {
				logger.Warn("agent_note GC failed on session close", "user_id", "redacted", "error", err)
			}
		})
	}

	// ---- Tool vector index: reconciler + mcpwatch goroutines ----------------
	toolReaderConfig := tools.ToolVectorConfig{
		Backend:        cfg.ToolSearchBackend,
		TopK:           cfg.ToolSearchTopK,
		QdrantURL:      cfg.QdrantURL,
		QdrantAPIKey:   cfg.QdrantAPIKey,
		Collection:     tools.ToolSearchCollection,
		EmbedBaseURL:   cfg.EmbeddingBaseURL,
		EmbedAPIKey:    cfg.EmbeddingAPIKey,
		EmbedModel:     cfg.EmbeddingModel,
		EmbedOutputDim: cfg.EmbeddingOutputDim,
	}
	var reconciler *toolindex.Reconciler
	if a.deps.QdrantClient != nil && a.deps.EmbedCache != nil && cfg.ToolSearchBackend != "fts" {
		r, err := toolindex.New(toolindex.Config{
			Provider:    a.deps.Tools,
			Qdrant:      a.deps.QdrantClient,
			Embedder:    a.deps.EmbedCache,
			State:       toolindex.NewSQLiteStateStore(a.deps.Pool),
			Collection:  tools.ToolSearchCollection,
			VectorDim:   tools.ToolVectorDim(cfg.EmbeddingOutputDim),
			EmbedModel:  search.EmbedCacheNamespace(cfg.EmbeddingBaseURL, cfg.EmbeddingModel),
			EmbedTextFn: tools.SearchableEmbeddingTextForLLMDef,
			PointIDFn:   tools.ToolQdrantPointID,
			Logger:      logger.With("component", "toolindex"),
		})
		if err != nil {
			logger.Warn("tool index reconciler unavailable", "error", err)
		} else {
			rep := r.Reconcile(context.Background(), toolindex.ReasonBoot)
			logger.Info("tool index reconciled at boot",
				"upserted", len(rep.Upserted), "deleted", len(rep.Deleted),
				"unchanged", rep.Unchanged, "errors", len(rep.Errors),
				"elapsed_ms", rep.ElapsedMs)
			for i, e := range rep.Errors {
				logger.Warn("tool index reconcile error", "idx", i, "err", e)
			}
			reconciler = r
			// toolReconciler.Run goroutine lifecycle owned by App (US-A13c).
			a.startBg(r.Run)

			// fsnotify on mcp.json — debounced notification when the operator edits the file.
			// mcpwatch goroutine lifecycle owned by App (US-A13c).
			if cfg.MCPServersPath != "" {
				watcher, werr := mcp.New(mcp.Config{
					Path:     cfg.MCPServersPath,
					Callback: func() { r.Notify(toolindex.ReasonMCPConfig) },
					Logger:   logger.With("component", "mcpwatch"),
				})
				if werr != nil {
					logger.Warn("mcp.json watcher unavailable", "error", werr)
				} else {
					a.startBg(func(ctx context.Context) {
						if rerr := watcher.Run(ctx); rerr != nil {
							logger.Warn("mcpwatch exited with error", "error", rerr)
						}
					})
				}
			}
		}
	}
	// Wire the ToolVectorIndex on the registry for the search path.
	a.deps.Tools.PrepareVectorReader(toolReaderConfig)

	// ---- Conversation archive ------------------------------------------------
	var archiveDB conversation.ArchiveRepository
	if cfg.ConvArchiveEnabled {
		archiveStore, err := conversation.NewArchiveStore(a.deps.SchedDB.DB())
		if err != nil {
			logger.Warn("conversation archive unavailable", "error", err)
		} else {
			if a.deps.MemoryStore != nil {
				indexingRepo := memoryindex.NewIndexingArchiveRepository(archiveStore, a.deps.MemoryStore)
				if a.freshnessStore != nil {
					indexingRepo.WithFreshness(a.freshnessStore, cfg.EmbeddingModel)
				}
				archiveDB = indexingRepo
			} else {
				archiveDB = archiveStore
			}
			b.SetArchiveDB(archiveDB)
			archiver := conversation.NewBufferedAppender(archiveDB, 100)
			// App owns the archiver Close lifecycle (US-A13c).
			a.archiver = archiver
			b.SetArchiver(archiver)
		}
	}

	// ---- Compact memory index rebuild ----------------------------------------
	if a.deps.MemoryStore != nil {
		rebuildCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		rebuildInput := memoryindex.RebuildInput{
			Sources:    a.deps.Sources,
			Archive:    archiveDB,
			Proposals:  a.deps.SummariesStore,
			SkipVector: a.deps.CompactVector != nil,
		}
		if a.freshnessStore != nil {
			rebuildInput.FreshnessStore = a.freshnessStore
			rebuildInput.EmbeddingModelID = cfg.EmbeddingModel
			if row, found, err := a.freshnessStore.Get(rebuildCtx, "compact_memory_documents"); err == nil && found {
				rebuildInput.IndexBuildID = row.IndexBuildID
			}
		}
		report, err := memoryindex.Rebuild(rebuildCtx, a.deps.MemoryStore, rebuildInput)
		cancel()
		if err != nil {
			logger.Warn("compact memory index rebuild failed", "error", err)
		} else {
			logger.Info("compact memory index rebuilt",
				"sources", report.SourcesIndexed, "archive", report.ArchiveIndexed,
				"proposals", report.ProposalsIndexed,
				"vector_collection", report.Vector.Collection, "vector_docs", report.Vector.DocsIndexed)
		}
		if a.deps.CompactVector != nil {
			// Compact memory vector mirror sync goroutine owned by App (US-A13c).
			go func() {
				vectorCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				logger.Info("compact memory vector mirror sync started")
				a.deps.CompactVectorHealth.Started()
				syncReport, err := a.deps.MemoryStore.SyncVector(vectorCtx)
				if err != nil {
					a.deps.CompactVectorHealth.Failed(err)
					logger.Warn("compact memory vector mirror sync failed", "error", err)
					return
				}
				a.deps.CompactVectorHealth.Succeeded(syncReport)
				logger.Info("compact memory vector mirror synced",
					"vector_collection", syncReport.Collection, "vector_docs", syncReport.DocsIndexed,
					"vector_size", syncReport.VectorSize)
			}()
		}
	}

	// SearchMemoryTool — registered after memory rebuild so the index is populated.
	if tool := tools.NewSearchMemoryToolConfigured(a.deps.SearchRepo, a.deps.MemoryStore,
		time.Duration(cfg.MemorySearchTimeoutMS)*time.Millisecond,
		cfg.RecencyHalfLifeWikiDays, cfg.RecencyHalfLifeArchiveDays); tool != nil {
		tool.SetFreshnessStore(a.freshnessStore)
		a.deps.Tools.Register(tool)
	}
	// RecallOperationalTool — surfaces approved operational lessons from Phase-N pipeline.
	if tool := tools.NewRecallOperationalTool(a.deps.MemoryStore); tool != nil {
		tool.SetFreshnessStore(a.freshnessStore)
		a.deps.Tools.Register(tool)
	}
	// RecallUserMemoryTool — surfaces approved user facts/preferences from Phase-O pipeline.
	if tool := tools.NewRecallUserMemoryTool(a.deps.MemoryStore); tool != nil {
		tool.SetFreshnessStore(a.freshnessStore)
		a.deps.Tools.Register(tool)
	}
	// ProposePatchTool — write_proposal subagent mutation gate (Phase-S US-S01).
	// ALL writes are review-gated; proposals land in proposed_updates with status=pending.
	if tool := tools.NewProposePatchTool(tools.NewSQLPatchProposalStore(a.deps.SchedDB.DB())); tool != nil {
		a.deps.Tools.Register(tool)
	}
	// AgentNoteTool — per-conversation scratchpad for working memory (Phase-P, capability #4).
	// The conversationIDProvider reads from context; US-P03 will set the value via
	// tools.WithConversationID before each tool dispatch in the agent loop.
	if tool := tools.NewAgentNoteTool(
		agentnote.NewStore(a.deps.SchedDB.DB()),
		func(ctx context.Context) (string, error) {
			id := tools.ConversationIDFromContext(ctx)
			if id == "" {
				return "", fmt.Errorf("agent_note: conversation ID not in context")
			}
			return id, nil
		},
	); tool != nil {
		a.deps.Tools.Register(tool)
	}

	// ---- Wiki issues store + daily briefing tool ----------------------------
	issues := cron.NewIssuesStore(a.deps.SchedDB.DB())
	b.SetIssues(issues)
	if tool := tools.NewDailyBriefingTool(a.deps.SchedDB, a.deps.Sources, a.deps.SummariesStore, issues, archiveDB, loc); tool != nil {
		a.deps.Tools.Register(tool)
	}

	// ---- Bot-dependent tool registrations (need *Bot as sender/runner) -----
	// Sandbox tools require b as DocumentSender; ToolSearch, token, doc, wiki_page
	// need b constructed and available. Registered before scheduler so they're
	// live when the first Telegram message arrives.
	if tool := tools.NewExecuteCodeToolWithStoreAndRegistry(a.deps.SandboxMgr, b, a.deps.Sources, a.deps.Tools); tool != nil {
		a.deps.Tools.Register(tool)
	}
	if tool := tools.NewExecuteShellTool(a.deps.SandboxMgr); tool != nil {
		a.deps.Tools.Register(tool)
	}
	if tool := tools.NewDevToolTool(a.deps.ToolReg); tool != nil {
		a.deps.Tools.Register(tools.WithCategory(tool, tools.CategoryAutonomous))
	}
	if tool := tools.NewToolSearchTool(a.deps.Tools); tool != nil {
		a.deps.Tools.Register(tool)
	}
	a.deps.Tools.Register(&tools.AskUserTool{})
	if tokenTool := tools.NewRequestDashboardTokenTool(a.deps.AuthDB, b, b.IsAllowlisted); tokenTool != nil {
		a.deps.Tools.Register(tokenTool)
	}
	if docTool := tools.NewDocTool(a.deps.Sources, b); docTool != nil {
		a.deps.Tools.Register(docTool)
	}
	if t := tools.NewWikiPageTool(a.deps.WikiStore, a.deps.ReindexWorker); t != nil {
		a.deps.Tools.Register(t)
	}

	// ---- Cron scheduler -----------------------------------------------------
	// Build a Handler from injected deps so cron no longer depends on *Bot directly.
	lessonPromoter := &lessonPromoterAdapter{
		attemptsRepo:  attempts.NewSQLiteRepo(a.deps.Pool),
		proposalStore: learning.NewSQLProposalStore(a.deps.Pool),
	}
	proposalSweeper := &proposalTTLSweeperAdapter{db: a.deps.Pool}
	cronHandler := cron.NewHandler(cron.HandlerConfig{
		Notifier:        b,
		Wiki:            a.deps.Wiki,
		Issues:          issues,
		Sources:         a.deps.Sources,
		SchedDB:         a.deps.SchedDB,
		Identity:        a.deps.AuthDB,
		Logger:          logger,
		Location:        loc,
		Promoter:        lessonPromoter,
		ProposalSweeper: proposalSweeper,
	})
	// Wire run_now action now that the handler (not *Bot) implements RunNow.
	if a.deps.TaskTool != nil {
		a.deps.TaskTool.SetRunner(&scheduledTaskRunnerAdapter{h: cronHandler})
	}
	// Route KindAgentJob through the cron Hub (InboundAdapter → CronAgentLoop →
	// silent Outbound); all other kinds fall back to cronHandler.Dispatch.
	cronDispatcher := cron.Dispatcher(cronHandler.Dispatch)
	depsGetter := newAgentJobDepsGetter(cfg, &a.deps)
	if depsGetter != nil {
		cronLoop := cronadapter.NewCronAgentLoop(&agentJobRunnerAdapter{getDeps: depsGetter}, loc, cronadapter.WithIdentity(a.deps.AuthDB))
		cronHub, hubErr := chat.New(chat.Config{Loop: cronLoop, LifecycleStore: a.deps.RunStore, Logger: logger})
		if hubErr != nil {
			return fmt.Errorf("creating cron hub: %w", hubErr)
		}
		cronHub.RegisterInbound(cronadapter.New())
		cronHub.RegisterOutbound(silentadapter.NewCron(silentadapter.Config{Logger: logger}))
		cronDispatcher = cronadapter.NewHubDispatcher(cronHub, cronHandler.Dispatch)
	}
	sched, err := cron.New(cron.Config{
		Store:      a.deps.SchedDB,
		Dispatcher: cronDispatcher,
		Logger:     logger,
		Location:   loc,
	})
	if err != nil {
		return fmt.Errorf("creating scheduler: %w", err)
	}
	// App owns the scheduler lifecycle (US-A13c).
	a.sched = sched

	// Bootstrap the autonomous nightly wiki-maintenance task (idempotent upsert).
	nightlyAt, err := cron.NextDailyRun("03:00", loc, time.Now())
	if err != nil {
		return fmt.Errorf("computing nightly run: %w", err)
	}
	if _, err := a.deps.SchedDB.Upsert(context.Background(), &cron.Task{
		Name:          "nightly-wiki-maintenance",
		Kind:          cron.KindWikiMaintenance,
		ScheduleKind:  cron.ScheduleDaily,
		ScheduleDaily: "03:00",
		NextRunAt:     nightlyAt,
		Status:        cron.StatusActive,
	}); err != nil {
		logger.Warn("failed to bootstrap nightly maintenance task", "err", err)
	}

	// Bootstrap the daily lesson-promotion task (idempotent upsert).
	lessonPromotionAt, err := cron.NextDailyRun("02:00", loc, time.Now())
	if err != nil {
		return fmt.Errorf("computing lesson promotion run: %w", err)
	}
	if _, err := a.deps.SchedDB.Upsert(context.Background(), &cron.Task{
		Name:          "daily-lesson-promotion",
		Kind:          cron.KindLessonPromotion,
		ScheduleKind:  cron.ScheduleDaily,
		ScheduleDaily: "02:00",
		NextRunAt:     lessonPromotionAt,
		Status:        cron.StatusActive,
	}); err != nil {
		logger.Warn("failed to bootstrap daily lesson-promotion task", "err", err)
	}

	// Bootstrap the daily proposal TTL sweep task (idempotent upsert).
	proposalTTLAt, err := cron.NextDailyRun("03:00", loc, time.Now())
	if err != nil {
		return fmt.Errorf("computing proposal TTL run: %w", err)
	}
	if _, err := a.deps.SchedDB.Upsert(context.Background(), &cron.Task{
		Name:          "proposal_ttl_sweep",
		Kind:          cron.KindProposalTTLSweep,
		ScheduleKind:  cron.ScheduleDaily,
		ScheduleDaily: "03:00",
		NextRunAt:     proposalTTLAt,
		Status:        cron.StatusActive,
	}); err != nil {
		logger.Warn("failed to bootstrap proposal TTL sweep task", "err", err)
	}

	// ---- Skill adapters (bridge auraskills → api interfaces) ----------------
	skillRoots := skillSearchRoots(cfg)
	skillsInstaller, err := auraskills.NewNPXInstaller(cfg.SkillsPath, cfg.SkillsInstallProjectDir)
	if err != nil {
		logger.Warn("skills installer unavailable", "error", err)
	}
	skillsDeleter, err := auraskills.NewFSDeleter(skillRoots[0], skillRoots[1:]...)
	if err != nil {
		logger.Warn("skills deleter unavailable", "error", err)
	}
	skillProposalApplier, err := auraskills.NewFSProposalApplier(skillRoots[0])
	if err != nil {
		logger.Warn("skill proposal applier unavailable", "error", err)
	}
	webChat, err := newHubBackedWebChatService(cfg, &a.deps, logger)
	if err != nil {
		return fmt.Errorf("wire web chat hub: %w", err)
	}

	// ---- HTTP API router (dashboard + /api endpoints) -----------------------
	// App owns the api handler; App.APIHandler() exposes it to main.go (US-A13c).
	a.api = api.NewRouter(api.Deps{
		Wiki:        a.deps.WikiStore,
		Sources:     a.deps.Sources,
		Scheduler:   a.deps.SchedDB,
		OCR:         a.deps.OCR,
		Ingest:      a.deps.Ingest,
		Markitdown:  a.deps.Markitdown,
		Auth:        a.deps.AuthDB,
		Allowlist:   b.IsAllowlisted,
		MaxUploadMB: cfg.OCRMaxFileMB,
		Location:    loc,
		// Surface reindex worker health via /api/health (US-A13c: moved from bot).
		ReindexHealth: a.reindexHealth,
		// Keep in sync with cmd/aura/main.go's auraVersion.
		Version:   "3.0",
		StartedAt: time.Now().UTC(),
		Logger:    logger,
		// Skills + MCP dashboard panels.
		Skills: a.deps.Skills,
		MCP:    a.deps.MCPClients,
		// skills.sh catalog + admin-gated install/delete.
		SkillsCatalog:   a.deps.SkillsCatalog,
		SkillsInstaller: skillsInstaller,
		SkillsDeleter:   newSkillsDeleterAdapter(skillsDeleter),
		SkillProposals:  newSkillProposalApplierAdapter(skillProposalApplier),
		SkillsAdmin:     cfg.SkillsAdmin,
		// Embed cache hit/miss counters in /api/health.
		EmbedCache:    a.deps.EmbedCache,
		CompactMemory: a.deps.CompactVectorHealth,
		Sandbox:       a.deps.SandboxHealth,
		// ToolReconciler backs POST /api/tools/reindex.
		ToolReconciler: reconciler,
		// SourcePurger for DELETE /sources/{id} compact memoryindex cleanup.
		SourcePurger: a.deps.MemoryStore,
		// Multi-root file manager.
		WikiDir:      cfg.WikiPath,
		WorkspaceDir: cfg.WorkspaceRoot,
		SkillsDir:    cfg.SkillsPath,
		// WikiSearch reindexes after dashboard wiki writes/renames/deletes.
		WikiSearch: a.deps.SearchRepo,
		// Pending-approval pipeline.
		PendingApprover: b,
		// Conversation archive read API.
		Archive: archiveDB,
		// Summaries review queue.
		Summaries:     a.deps.SummariesStore,
		SummariesWiki: a.deps.WikiStore,
		// Wiki maintenance issue queue.
		Issues: issues,
		// Runtime settings page.
		Settings:             a.deps.SettingsStore,
		RuntimeConfig:        cfg,
		ApplyRuntimeSettings: b.RuntimeSettingsApplier(a.deps),
		Restart:              a.restart,
		// Phase-H closure: route secret-shaped dashboard writes to the
		// secrets store so dashboard rotations actually update what
		// applySecretsToConfig reads at boot
		// (docs/settings-audit-2026-05-15.md).
		SecretsStore: secretspkg.NewSQLiteStore(a.deps.Pool),
		// AuraBot swarm observability.
		Swarm: a.deps.SwarmStore,
		Chat:  webChat,
		// Phase-6 US-J06: operator tool-warning channel.
		ToolWarnings: attempts.NewSQLiteRepo(a.deps.Pool),
	})

	// Wave 2.10.b — late notify. Fire one final Notify so the debounced reconcile
	// picks up every tool that landed in the registry during setup.
	if reconciler != nil {
		reconciler.Notify(toolindex.ReasonBoot)
	}
	return nil
}
