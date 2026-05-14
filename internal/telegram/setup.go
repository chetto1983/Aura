package telegram

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/concurrency"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/mcp"

	"github.com/aura/aura/internal/agent/tools/index"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/cron"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/storage/search"

	tele "gopkg.in/telebot.v4"
)

type Option func(*options)

type options struct {
	Restart func(context.Context) error
}

func WithRestart(fn func(context.Context) error) Option {
	return func(o *options) {
		o.Restart = fn
	}
}

// New creates a new Telegram bot from pre-built Phase A deps (built by
// cmd/aura/newApp). Phase B: creates the tele.Bot. Phase C: performs
// composition wiring (UserGate, scheduler, archive, api router, handlers).
//
// deps.SettingsStore may be nil (tests) — in that case the dashboard
// /settings endpoints respond 503.
func New(deps Deps, opts ...Option) (*Bot, error) {
	if deps.Pool == nil {
		return nil, fmt.Errorf("telegram: db pool required")
	}
	var opt options
	for _, apply := range opts {
		if apply != nil {
			apply(&opt)
		}
	}

	// Convenience aliases so Phase C code reads naturally.
	cfg := deps.Cfg
	logger := deps.Logger
	loc := deps.Loc

	// ---- Phase B: Telegram-specific setup -----------------------------------
	pref := tele.Settings{
		Token: cfg.TelegramToken,
	}
	tb, err := tele.NewBot(pref)
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}
	deps.Bot = tb

	b := NewBot(deps)

	// ---- Phase C: post-construction composition wiring ----------------------

	// Create UserGate with real callbacks (D-16).
	// OnEvict: persists conversation snapshot and clears session (D-10, T-01-22).
	// OnOverflow: sends Telegram notice to user in a separate goroutine (D-03, T-01-20, Pitfall 4).
	// OnQueueNotice: sends still-processing notice when entry waits > InboxQueueNoticeAfter (CONC-01 gap).
	userGate := concurrency.New(concurrency.Config{
		InboxSize:         cfg.InboxSize,
		EvictionThreshold: cfg.InactivityThreshold,
		SweepInterval:     cfg.InactivitySweepInterval,
		QueueNoticeAfter:  cfg.InboxQueueNoticeAfter,
		OnEvict: func(userID string) {
			b.logger.Info("user evicted from gate; clearing session", "user_id", userID)
			b.sessionStore().Clear(userID)
		},
		OnOverflow: func(userID string) {
			go func() {
				chatID, err := strconv.ParseInt(userID, 10, 64)
				if err != nil {
					b.logger.Warn("overflow notice: invalid userID", "user_id", userID, "error", err)
					return
				}
				msg := "I'm still processing your previous message. Your new message was dropped. Please try again in a moment."
				if _, err := b.bot.Send(tele.ChatID(chatID), msg); err != nil {
					b.logger.Warn("overflow notice delivery failed", "user_id", userID, "error", err)
				}
			}()
		},
		OnQueueNotice: func(userID string) {
			// Pitfall 4: hand off to a separate goroutine -- the gate's timer
			// goroutine must never block on the Telegram API call (T-01-28).
			go func() {
				chatID, err := strconv.ParseInt(userID, 10, 64)
				if err != nil {
					b.logger.Warn("queue notice: invalid userID", "user_id", userID, "error", err)
					return
				}
				msg := "Still working on your previous message -- I'll get to this one shortly."
				if _, err := b.bot.Send(tele.ChatID(chatID), msg); err != nil {
					b.logger.Warn("queue notice delivery failed", "user_id", userID, "error", err)
				}
			}()
		},
	})
	// Wire gate and gate-aware session store into the bot (D-16).
	b.gate = userGate
	b.sessions = agent.NewSessionStore(userGate)

	// Wave 2.7b: wire the run_now action of the unified task tool now that
	// *Bot (which implements ScheduledTaskRunner) is available.
	if deps.TaskTool != nil {
		deps.TaskTool.SetRunner(b)
	}

	// Sandbox tools
	if tool := tools.NewExecuteCodeToolWithStoreAndRegistry(deps.SandboxMgr, b, deps.Sources, deps.Tools); tool != nil {
		deps.Tools.Register(tool)
	}
	if tool := tools.NewExecuteShellTool(deps.SandboxMgr); tool != nil {
		deps.Tools.Register(tool)
	}
	if tool := tools.NewDevToolTool(deps.ToolReg); tool != nil {
		deps.Tools.Register(tools.WithCategory(tool, tools.CategoryAutonomous))
	}

	// Deferred-tools rollout: tool_search is the always-on seed of the
	// per-turn agent pool. The model uses it to fetch input schemas for
	// tools advertised in the system-prompt manifest. Registered last so
	// it sees every other tool in Registry.Search().
	if tool := tools.NewToolSearchTool(deps.Tools); tool != nil {
		deps.Tools.Register(tool)
	}

	// Tool vector index backs the hybrid backend of Registry.Search.
	// toolindex.Reconciler is the source of truth for the embedding index;
	// this path wires the search-side reader.
	toolReaderConfig := tools.ToolVectorConfig{
		Backend:      cfg.ToolSearchBackend,
		TopK:         cfg.ToolSearchTopK,
		QdrantURL:    cfg.QdrantURL,
		QdrantAPIKey: cfg.QdrantAPIKey,
		Collection:   tools.ToolSearchCollection,
		EmbedBaseURL: cfg.EmbeddingBaseURL,
		EmbedAPIKey:  cfg.EmbeddingAPIKey,
		EmbedModel:   cfg.EmbeddingModel,
	}
	if deps.QdrantClient != nil && deps.EmbedCache != nil && cfg.ToolSearchBackend != "fts" {
		reconciler, err := toolindex.New(toolindex.Config{
			Provider:    deps.Tools,
			Qdrant:      deps.QdrantClient,
			Embedder:    deps.EmbedCache,
			State:       toolindex.NewSQLiteStateStore(deps.Pool),
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
			rep := reconciler.Reconcile(context.Background(), toolindex.ReasonBoot)
			logger.Info("tool index reconciled at boot",
				"upserted", len(rep.Upserted), "deleted", len(rep.Deleted),
				"unchanged", rep.Unchanged, "errors", len(rep.Errors),
				"elapsed_ms", rep.ElapsedMs)
			for i, e := range rep.Errors {
				logger.Warn("tool index reconcile error", "idx", i, "err", e)
			}
			b.toolReconciler = reconciler
			b.bgWg.Add(1)
			go func() {
				defer b.bgWg.Done()
				reconciler.Run(b.bgCtx)
			}()

			// fsnotify on mcp.json — debounced notification when the
			// operator edits the file.
			if cfg.MCPServersPath != "" {
				watcher, werr := mcp.New(mcp.Config{
					Path:     cfg.MCPServersPath,
					Callback: func() { reconciler.Notify(toolindex.ReasonMCPConfig) },
					Logger:   logger.With("component", "mcpwatch"),
				})
				if werr != nil {
					logger.Warn("mcp.json watcher unavailable", "error", werr)
				} else {
					b.bgWg.Add(1)
					go func() {
						defer b.bgWg.Done()
						if rerr := watcher.Run(b.bgCtx); rerr != nil {
							logger.Warn("mcpwatch exited with error", "error", rerr)
						}
					}()
				}
			}
		}
	}

	// Wire the *ToolVectorIndex on the registry for the search path.
	deps.Tools.PrepareVectorReader(toolReaderConfig)

	// Slice 12b/12c: conversation archive. Open the ArchiveStore on the same
	// SQLite file as the scheduler (migration is idempotent).
	if cfg.ConvArchiveEnabled {
		archiveStore, err := conversation.NewArchiveStore(deps.SchedDB.DB())
		if err != nil {
			logger.Warn("conversation archive unavailable", "error", err)
		} else {
			if deps.MemoryStore != nil {
				b.archiveDB = memoryindex.NewIndexingArchiveRepository(archiveStore, deps.MemoryStore)
			} else {
				b.archiveDB = archiveStore
			}
			b.archiver = conversation.NewBufferedAppender(b.archiveDB, 100)
		}
	}
	if deps.MemoryStore != nil {
		rebuildCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		report, err := memoryindex.Rebuild(rebuildCtx, deps.MemoryStore, memoryindex.RebuildInput{
			Sources:    deps.Sources,
			Archive:    b.archiveDB,
			Proposals:  deps.SummariesStore,
			SkipVector: deps.CompactVector != nil,
		})
		cancel()
		if err != nil {
			logger.Warn("compact memory index rebuild failed", "error", err)
		} else {
			logger.Info("compact memory index rebuilt", "sources", report.SourcesIndexed, "archive", report.ArchiveIndexed, "proposals", report.ProposalsIndexed, "vector_collection", report.Vector.Collection, "vector_docs", report.Vector.DocsIndexed)
		}
		if deps.CompactVector != nil {
			go func() {
				vectorCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				logger.Info("compact memory vector mirror sync started")
				deps.CompactVectorHealth.Started()
				report, err := deps.MemoryStore.SyncVector(vectorCtx)
				if err != nil {
					deps.CompactVectorHealth.Failed(err)
					logger.Warn("compact memory vector mirror sync failed", "error", err)
					return
				}
				deps.CompactVectorHealth.Succeeded(report)
				logger.Info("compact memory vector mirror synced", "vector_collection", report.Collection, "vector_docs", report.DocsIndexed, "vector_size", report.VectorSize)
			}()
		}
	}
	if tool := tools.NewSearchMemoryToolConfigured(deps.SearchRepo, deps.MemoryStore,
		time.Duration(cfg.MemorySearchTimeoutMS)*time.Millisecond,
		cfg.RecencyHalfLifeWikiDays, cfg.RecencyHalfLifeArchiveDays); tool != nil {
		deps.Tools.Register(tool)
	}

	// Slice 12h/12l.1: shared wiki_issues store. Both the API maintenance
	// handlers and the nightly maintenance job read/write the same queue.
	b.issues = cron.NewIssuesStore(deps.SchedDB.DB())
	if tool := tools.NewDailyBriefingTool(deps.SchedDB, deps.Sources, deps.SummariesStore, b.issues, b.archiveDB, loc); tool != nil {
		deps.Tools.Register(tool)
	}

	// Scheduler dispatcher closes over b so reminder/wiki_maintenance
	// tasks can invoke the bot's send + the wiki store. Built after b
	// is initialized.
	sched, err := cron.New(cron.Config{
		Store:      deps.SchedDB,
		Dispatcher: b.dispatchTask,
		Logger:     logger,
		Location:   loc,
	})
	if err != nil {
		return nil, fmt.Errorf("creating scheduler: %w", err)
	}
	b.sched = sched

	// Bootstrap the autonomous nightly wiki-maintenance task. Idempotent
	// upsert keyed by name so restarting the bot won't duplicate it.
	nightlyAt, err := cron.NextDailyRun("03:00", loc, time.Now())
	if err != nil {
		return nil, fmt.Errorf("computing nightly run: %w", err)
	}
	if _, err := deps.SchedDB.Upsert(context.Background(), &cron.Task{
		Name:          "nightly-wiki-maintenance",
		Kind:          cron.KindWikiMaintenance,
		ScheduleKind:  cron.ScheduleDaily,
		ScheduleDaily: "03:00",
		NextRunAt:     nightlyAt,
		Status:        cron.StatusActive,
	}); err != nil {
		logger.Warn("failed to bootstrap nightly maintenance task", "err", err)
	}

	b.docs = newDocHandler(docHandlerConfig{
		Bot:        tb,
		Sources:    deps.Sources,
		OCR:        deps.OCR,
		Markitdown: deps.Markitdown,
		MaxFileMB:  cfg.OCRMaxFileMB,
		Allowlist:  b.isAllowlisted,
		Logger:     logger,
		// Slice 6: auto-ingest hook.
		AfterOCR: deps.Ingest.AfterOCR,
	})

	// Slice 10a: build the read-only HTTP API.
	// Slice 11c: skill install/delete adapters.
	skillRoots := SkillSearchRoots(cfg)
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

	b.api = api.NewRouter(api.Deps{
		Wiki:        deps.WikiStore,
		Sources:     deps.Sources,
		Scheduler:   deps.SchedDB,
		OCR:         deps.OCR,
		Ingest:      deps.Ingest,
		Markitdown:  deps.Markitdown,
		Auth:        deps.AuthDB,
		Allowlist:   b.isAllowlisted,
		MaxUploadMB: cfg.OCRMaxFileMB,
		Location:    loc,
		// PHASE 2 WARNING 12: surface reindex worker health via /api/health.
		ReindexHealth: b.ReindexHealth,
		// Keep in sync with cmd/aura/main.go's auraVersion.
		Version:   "3.0",
		StartedAt: time.Now().UTC(),
		Logger:    logger,
		// Slice 11b: skills + MCP dashboard panels read off these.
		Skills: deps.Skills,
		MCP:    deps.MCPClients,
		// Slice 11c: skills.sh catalog + admin-gated install/delete.
		SkillsCatalog:   deps.SkillsCatalog,
		SkillsInstaller: skillsInstaller,
		SkillsDeleter:   skillsDeleterAdapter{inner: skillsDeleter},
		SkillProposals:  skillProposalApplierAdapter{inner: skillProposalApplier},
		SkillsAdmin:     cfg.SkillsAdmin,
		// Slice 11j: surface cache hit/miss counters in /api/health.
		EmbedCache:    deps.EmbedCache,
		CompactMemory: deps.CompactVectorHealth,
		Sandbox:       deps.SandboxHealth,
		// ToolReconciler backs POST /api/tools/reindex.
		ToolReconciler: b.toolReconciler,
		// SourcePurger lets DELETE /sources/{id} clean up compact memoryindex.
		SourcePurger: deps.MemoryStore,
		// Multi-root file manager (dashboard).
		WikiDir:      cfg.WikiPath,
		WorkspaceDir: cfg.WorkspaceRoot,
		SkillsDir:    cfg.SkillsPath,
		// Reindex hook: wiki writes/renames/deletes trigger Qdrant reindex.
		WikiSearch: deps.SearchRepo,
		// Pending-approval pipeline.
		PendingApprover: b,
		// Slice 12c: conversation archive read API.
		Archive: b.archiveDB,
		// Slice 12k.1: summaries review queue.
		Summaries:     deps.SummariesStore,
		SummariesWiki: deps.WikiStore,
		// Slice 12l.1: wiki maintenance issue queue.
		Issues: b.issues,
		// Slice 14d: runtime settings page surface.
		Settings:      deps.SettingsStore,
		RuntimeConfig: cfg,
		ApplyRuntimeSettings: func(ctx context.Context) error {
			return applyRuntimeSettings(ctx, deps.SettingsStore, deps.Cfg, deps.AgentRunner, deps.SwarmMgr, b.budget, deps.Logger)
		},
		Restart: opt.Restart,
		// Slice 17d: AuraBot swarm observability.
		Swarm: deps.SwarmStore,
		Chat:  NewWebChatService(deps.AgentRunner, deps.Tools),
	})

	// Slice 10d: request_dashboard_token tool. Registered after b is
	// constructed so the bot can satisfy tools.TokenSender via its own
	// SendToUser method.
	if tokenTool := tools.NewRequestDashboardTokenTool(deps.AuthDB, b, b.isAllowlisted); tokenTool != nil {
		deps.Tools.Register(tokenTool)
	}

	// Wave 2.7d: unified doc tool.
	if docTool := tools.NewDocTool(deps.Sources, b); docTool != nil {
		deps.Tools.Register(docTool)
	}
	// Wave 2.6: wiki_page tool. Uses the concrete WikiStore (not interface)
	// consistent with BLOCKER 3 of 2026-05-11 plan revision 2.
	if t := tools.NewWikiPageTool(deps.WikiStore, deps.ReindexWorker); t != nil {
		deps.Tools.Register(t)
	}

	b.registerHandlers()
	b.installBotCommands()

	// Wave 2.10.b — late notify. Fire one final Notify here so the debounced
	// reconcile picks up every tool that landed in the registry during setup.
	if b.toolReconciler != nil {
		b.toolReconciler.Notify(toolindex.ReasonBoot)
	}
	return b, nil
}

