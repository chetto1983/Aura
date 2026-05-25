package main

// wireBot lives in this file (separated from cmd/aura/app.go) so app.go can
// stay under the 600-LOC god-class threshold. wireBot is the per-bot wiring
// pass that hooks the tool index, scheduler, conversation summarizer,
// telegram channel adapter, and dashboard token command flow into the
// already-constructed App + deps + bot. It runs once, after newApp() returns
// and before Start(bot).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/agentnote"
	"github.com/aura/aura/internal/api"
	cronadapter "github.com/aura/aura/internal/channels/cron"
	silentadapter "github.com/aura/aura/internal/channels/silent"
	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/cron"
	"github.com/aura/aura/internal/learning"
	"github.com/aura/aura/internal/mcp"
	"github.com/aura/aura/internal/opsfile"
	"github.com/aura/aura/internal/release"
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

	// Wire compact memory store so InvocationBuilder can query top-N operational
	// lessons for system prompt injection at conversation start (US-OP03).
	if a.deps.MemoryStore != nil {
		b.SetMemoryStore(a.deps.MemoryStore)
	}

	// ---- Wiki orphan reconciler (Qdrant + FTS5 cleanup for deleted pages) ---
	// Purges Qdrant vectors and FTS5 rows for wiki pages that have been deleted
	// from disk (e.g. manual deletes bypassing wiki.Store.DeletePage).
	if a.deps.QdrantClient != nil && cfg.QdrantCollection != "" && cfg.WikiPath != "" {
		wikiReconciler := search.NewWikiOrphanReconciler(search.WikiOrphanReconcilerConfig{
			QdrantClient: a.deps.QdrantClient,
			Collection:   cfg.QdrantCollection,
			DB:           a.deps.Pool,
			WikiDir:      cfg.WikiPath,
			Logger:       logger.With("component", "wiki-orphan-reconciler"),
		})
		wikiReconciler.Reconcile(context.Background(), search.WikiOrphanReasonBoot)
		a.startBg(wikiReconciler.Run)
	}

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

	// Memory recall tools are registered after memory rebuild so the index is populated.
	a.registerMemoryRecallTools(cfg)
	// ProposePatchTool — operational proposals auto-accept (Phase-OP / US-OP01);
	// wiki and user_memory proposals remain review-gated via proposed_updates.
	if tool := tools.NewProposePatchTool(tools.NewSQLPatchProposalStore(a.deps.SchedDB.DB())); tool != nil {
		tool.SetOperationalWriter(a.deps.MemoryStore)
		a.deps.Tools.Register(tool)
	}
	// Migration: auto-accept any pre-OP01 pending operational proposals (US-OP01).
	if a.deps.MemoryStore != nil {
		if n, migrErr := learning.MigrateOperationalProposals(context.Background(), a.deps.SchedDB.DB(), a.deps.MemoryStore); migrErr != nil {
			logger.Warn("operational proposals migration failed", "error", migrErr)
		} else if n > 0 {
			logger.Info("operational proposals migrated", "promoted", n)
		}
	}

	// ---- Operational lessons file watcher (US-OP02) ----------------------------
	// Watches <WorkspaceRoot>/data/operational_lessons.md and re-ingests it into
	// compact_memory_documents kind=operational on every write. Complements the
	// propose_patch auto-accept path so search(action=lessons) surfaces lessons
	// from BOTH sources.
	if a.deps.MemoryStore != nil && cfg.WorkspaceRoot != "" {
		opsAbsPath := opsfile.AbsPath(cfg.WorkspaceRoot)
		if err := os.MkdirAll(filepath.Dir(opsAbsPath), 0o755); err != nil {
			logger.Warn("opsfile parent directory unavailable", "path", filepath.Dir(opsAbsPath), "error", err)
		} else {
			// Boot ingest: surface any lessons already in the file before the first turn.
			if n, ingestErr := opsfile.IngestFromPath(context.Background(), opsAbsPath, a.deps.MemoryStore); ingestErr != nil {
				logger.Warn("opsfile boot ingest failed", "error", ingestErr)
			} else if n > 0 {
				logger.Info("operational lessons file ingested at boot", "count", n, "path", opsAbsPath)
			}
			// File watcher: re-ingest whenever Aura writes to the file via the file tool.
			opsWatcher, werr := mcp.New(mcp.Config{
				Path:     opsAbsPath,
				Debounce: 500 * time.Millisecond,
				Callback: func() {
					if n, ingestErr := opsfile.IngestFromPath(context.Background(), opsAbsPath, a.deps.MemoryStore); ingestErr != nil {
						logger.Warn("opsfile re-ingest failed", "error", ingestErr)
					} else if n > 0 {
						logger.Info("operational lessons re-ingested", "count", n)
					}
				},
				Logger: logger.With("component", "opsfile-watcher"),
			})
			if werr != nil {
				logger.Warn("opsfile watcher unavailable", "error", werr)
			} else {
				a.startBg(func(ctx context.Context) {
					if rerr := opsWatcher.Run(ctx); rerr != nil {
						logger.Warn("opsfile watcher exited with error", "error", rerr)
					}
				})
			}
		}
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

	// ---- Wiki issues store --------------------------------------------------
	issues := cron.NewIssuesStore(a.deps.SchedDB.DB())
	b.SetIssues(issues)

	// ---- Bot-dependent tool registrations (need *Bot as sender/runner) -----
	// Sandbox tools and wiki_page need b constructed and available. Registered before scheduler so they're
	// live when the first Telegram message arrives.
	if tool := tools.NewCreateDocumentTool(a.deps.Sources, b); tool != nil {
		a.deps.Tools.Register(tool)
	}
	if tool := tools.NewExecuteCodeToolWithStoreAndRegistry(a.deps.SandboxMgr, b, a.deps.Sources, a.deps.Tools); tool != nil {
		a.deps.Tools.Register(tool)
	}
	if tool := tools.NewExecuteShellTool(a.deps.SandboxMgr); tool != nil {
		a.deps.Tools.Register(tool)
	}
	a.deps.Tools.Register(&tools.AskUserTool{})
	a.deps.Tools.Register(&tools.TextResponseTool{})
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
	memoryDecay := &memoryDecayAdapter{store: a.deps.MemoryStore, logger: logger}
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
		MemoryDecay:     memoryDecay,
		BackupVerifier:  &backupVerifyAdapter{db: a.deps.Pool},
		WALCheckpointer: &walCheckpointAdapter{db: a.deps.Pool},
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

	// Bootstrap the daily backup-verify task (idempotent upsert).
	backupVerifyAt, err := cron.NextDailyRun("04:00", loc, time.Now())
	if err != nil {
		return fmt.Errorf("computing backup verify run: %w", err)
	}
	if _, err := a.deps.SchedDB.Upsert(context.Background(), &cron.Task{
		Name:          "daily-backup-verify",
		Kind:          cron.KindBackupVerify,
		ScheduleKind:  cron.ScheduleDaily,
		ScheduleDaily: "04:00",
		NextRunAt:     backupVerifyAt,
		Status:        cron.StatusActive,
	}); err != nil {
		logger.Warn("failed to bootstrap daily backup-verify task", "err", err)
	}

	// Bootstrap the WAL checkpoint task — every 6 hours to keep the WAL file
	// from growing unbounded (PRAGMA wal_checkpoint(TRUNCATE)).
	if _, err := a.deps.SchedDB.Upsert(context.Background(), &cron.Task{
		Name:                 "wal-checkpoint",
		Kind:                 cron.KindWALCheckpoint,
		ScheduleKind:         cron.ScheduleEvery,
		ScheduleEveryMinutes: 360,
		NextRunAt:            time.Now().UTC().Add(6 * time.Hour),
		Status:               cron.StatusActive,
	}); err != nil {
		logger.Warn("failed to bootstrap wal-checkpoint task", "err", err)
	}

	// Bootstrap the daily operational memory decay task (idempotent upsert).
	memoryDecayAt, err := cron.NextDailyRun("03:00", loc, time.Now())
	if err != nil {
		return fmt.Errorf("computing memory decay run: %w", err)
	}
	if _, err := a.deps.SchedDB.Upsert(context.Background(), &cron.Task{
		Name:          "daily-memory-decay",
		Kind:          cron.KindMemoryDecay,
		ScheduleKind:  cron.ScheduleDaily,
		ScheduleDaily: "03:00",
		NextRunAt:     memoryDecayAt,
		Status:        cron.StatusActive,
	}); err != nil {
		logger.Warn("failed to bootstrap daily memory decay task", "err", err)
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
	if tool := tools.NewSkillTool(a.deps.Skills, a.deps.SkillsCatalog, skillsInstaller, skillsDeleter, cfg.SkillsAdmin); tool != nil {
		a.deps.Tools.Register(tool)
	}
	skillProposalApplier, err := auraskills.NewFSProposalApplier(skillRoots[0])
	if err != nil {
		logger.Warn("skill proposal applier unavailable", "error", err)
	}
	sharedHub, err := newSharedChatHub(cfg, &a.deps, b, logger, archiveDB, a.archiver)
	if err != nil {
		return fmt.Errorf("wire shared chat hub: %w", err)
	}
	b.SetHub(sharedHub.hub)
	webChat, err := newWebChatService(sharedHub.hub, sharedHub.webRouter, sharedHub.webSessionStore)
	if err != nil {
		return fmt.Errorf("wire web chat service: %w", err)
	}
	webStreamChat := newWebStreamChatService(sharedHub.hub, sharedHub.webStreamRouter)
	var webChatAnswer api.ChatAnswerService
	if answer, ok := webChat.(api.ChatAnswerService); ok {
		webChatAnswer = answer
	}
	var webChatVoice api.ChatVoiceService
	var webChatAudio *api.AudioCache
	if a.deps.PocketTTSClient != nil {
		webChatVoice = webVoiceSynthesizer{client: a.deps.PocketTTSClient}
		webChatAudio = api.NewAudioCache(time.Hour, 100*1024*1024)
	}

	// ---- Entity dedup backend (US-GRAPH-03) ----------------------------------
	// Wire the Qdrant-backed DedupSearcher so DeduplicateEntities can find
	// near-duplicate entity/concept pages via cosine similarity. The adapter
	// wraps the same search.Searcher used by wiki search so no second embedder
	// is needed. Skipped when SearchRepo is nil (Qdrant not configured).
	if a.deps.SearchRepo != nil && a.deps.WikiStore != nil {
		a.deps.WikiStore.SetDedupBackend(&searcherDedupAdapter{searcher: a.deps.SearchRepo})
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
		Version:       release.Version,
		Commit:        release.Commit,
		BuildDate:     release.BuildDate,
		StartedAt:     time.Now().UTC(),
		Logger:        logger,
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
		// SourcePurger for DELETE /sources/{id} compact memoryindex cleanup.
		SourcePurger: a.deps.MemoryStore,
		WikiDir:      cfg.WikiPath, // sources moved out via Phase-FS-LAYOUT
		SourcesDir:   cfg.SourcesPath,
		WorkspaceDir: cfg.WorkspaceRoot,
		SkillsDir:    cfg.SkillsPath,
		// WikiSearch reindexes after dashboard wiki writes/renames/deletes.
		WikiSearch:   a.deps.SearchRepo,
		WikiSearcher: a.deps.SearchRepo,
		// WikiIndexRebuilder backs POST /api/wiki/reindex (admin-gated).
		WikiIndexRebuilder: func() search.WikiIndexRebuilder {
			if r, ok := a.deps.SearchRepo.(search.WikiIndexRebuilder); ok {
				return r
			}
			return nil
		}(),
		// Pending-approval pipeline.
		PendingApprover: b,
		// Conversation archive read API.
		Archive: archiveDB,
		Compactions: func() conversation.CompactionReader {
			reader, _ := archiveDB.(conversation.CompactionReader)
			return reader
		}(),
		// Summaries review queue.
		Summaries:         a.deps.SummariesStore,
		SummariesWiki:     a.deps.WikiStore,
		OperationalMemory: a.deps.MemoryStore,
		// Wiki maintenance issue queue.
		Issues: issues,
		// Runtime settings page.
		Settings:             a.deps.SettingsStore,
		RuntimeConfig:        cfg,
		ApplyRuntimeSettings: b.RuntimeSettingsApplier(a.deps),
		Restart:              a.restart,
		// Phase-H: secret writes go through SecretsStore so dashboard rotations update what boot reads.
		SecretsStore: secretspkg.NewSQLiteStore(a.deps.Pool),
		// AuraBot swarm observability.
		Swarm:      a.deps.SwarmStore,
		Chat:       webChat,
		ChatAnswer: webChatAnswer,
		ChatStream: webStreamChat,
		ChatVoice:  webChatVoice,
		ChatAudio:  webChatAudio,
		// Phase-6 US-J06: operator tool-warning channel.
		ToolWarnings: attempts.NewSQLiteRepo(a.deps.Pool),
		// US-T04: authz decisions observability.
		AuthzDecisions: api.NewSQLiteAuthzReader(a.deps.Pool),
		// US-T05: tool attempt observability.
		ToolAttemptsStats: api.NewSQLiteToolAttemptsReader(a.deps.Pool),
		// US-TOOL-10: compact memory reindex endpoint.
		CompactRebuilder: a.deps.MemoryStore,
	})

	logger.Info("tool registry built",
		"tools", len(a.deps.Tools.Definitions()),
		"tokens", tools.ManifestTokenEstimate(tools.RenderSplitManifest(a.deps.Tools.FullDefinitions())))
	return nil
}

func (a *App) registerMemoryRecallTools(cfg *config.Config) {
	if a == nil || a.deps.Tools == nil {
		return
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	searchMemTool := tools.NewSearchMemoryToolConfigured(a.deps.SearchRepo, a.deps.MemoryStore,
		time.Duration(cfg.MemorySearchTimeoutMS)*time.Millisecond,
		cfg.RecencyHalfLifeWikiDays, cfg.RecencyHalfLifeArchiveDays)
	if searchMemTool != nil {
		searchMemTool.SetFreshnessStore(a.freshnessStore)
	}
	if st := tools.NewSearchTool(searchMemTool, a.deps.WikiStore, tools.NewReadSourceTool(a.deps.Sources)); st != nil {
		operational := tools.NewRecallOperationalTool(a.deps.MemoryStore)
		if operational != nil {
			operational.SetFreshnessStore(a.freshnessStore)
		}
		userMemory := tools.NewRecallUserMemoryTool(a.deps.MemoryStore)
		if userMemory != nil {
			userMemory.SetFreshnessStore(a.freshnessStore)
		}
		st.WithRecallAndGraphActions(
			operational,
			userMemory,
			tools.NewRecallGodNodesTool(a.deps.WikiStore),
			tools.NewWikiPathTool(a.deps.WikiStore),
			tools.NewWikiSubgraphTool(a.deps.WikiStore, a.deps.SearchRepo),
			tools.NewWikiDiffTool(a.deps.WikiStore),
			tools.NewWikiSurprisesTool(a.deps.WikiStore),
		)
		a.deps.Tools.Register(st)
	}
}
