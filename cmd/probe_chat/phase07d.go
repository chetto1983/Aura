package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
)

func phase07DMixedTierRecallCase(stamp string) Case {
	safeStamp := strings.NewReplacer("-", "_", ":", "_").Replace(stamp)
	fixture := &phase07DMixedTierFixture{
		query:              "phase07d live mixedtier " + safeStamp,
		userApprovedToken:  "PHASE07D_LIVE_USER_APPROVED_" + safeStamp,
		userPendingToken:   "PHASE07D_LIVE_USER_PENDING_LEAK_" + safeStamp,
		operationalToken:   "PHASE07D_LIVE_OPERATIONAL_APPROVED_" + safeStamp,
		operationalPending: "PHASE07D_LIVE_OPERATIONAL_PENDING_LEAK_" + safeStamp,
		sourceToken:        "PHASE07D_LIVE_SOURCE_LEAK_" + safeStamp,
		archiveToken:       "PHASE07D_LIVE_ARCHIVE_LEAK_" + safeStamp,
		proposalToken:      "PHASE07D_LIVE_PROPOSAL_LEAK_" + safeStamp,
		docIDs: []string{
			"user_memory:phase07d-live-" + safeStamp,
			"user_memory:phase07d-live-pending-" + safeStamp,
			"operational:phase07d-live-" + safeStamp,
			"operational:phase07d-live-pending-" + safeStamp,
			"source:phase07d-live-" + safeStamp,
			"archive:phase07d-live-" + safeStamp,
			"proposal:phase07d-live-" + safeStamp,
		},
	}

	return Case{
		Name:     "phase07d-mixed-tier-recall",
		Category: "tools-memory",
		Prompt: fmt.Sprintf(
			"Phase07D live golden. Usa prima lo strumento recall_user_memory con query %q, poi usa recall_operational con la stessa query. Non usare search_memory. Rispondi citando esattamente questi due token e nessun altro token PHASE07D_LIVE: USER=%s OPERATIONAL=%s.",
			fixture.query,
			fixture.userApprovedToken,
			fixture.operationalToken,
		),
		Setup:   fixture.setup,
		Verify:  fixture.verify,
		Cleanup: fixture.cleanup,
	}
}

type phase07DMixedTierFixture struct {
	query              string
	userApprovedToken  string
	userPendingToken   string
	operationalToken   string
	operationalPending string
	sourceToken        string
	archiveToken       string
	proposalToken      string
	docIDs             []string
	startedAt          time.Time
}

func (f *phase07DMixedTierFixture) setup(env *Env) error {
	if f == nil {
		return fmt.Errorf("phase07d fixture unavailable")
	}
	if strings.TrimSpace(env.DBPath) == "" {
		return fmt.Errorf("phase07d fixture requires -db path")
	}
	ctx := context.Background()
	f.startedAt = time.Now().UTC().Add(-2 * time.Second)
	updatedAt := time.Now().UTC()
	docs := []memoryindex.Document{
		{
			ID:               f.docIDs[0],
			Kind:             memoryindex.KindUserMemory,
			Title:            "Phase07D live approved user fact",
			Body:             f.query + " " + f.userApprovedToken,
			Handle:           f.docIDs[0],
			Status:           "active",
			Tags:             []string{"preference"},
			UpdatedAt:        updatedAt,
			EmbeddingModelID: "embeddinggemma-300m@256-mrl",
			IndexBuildID:     "phase07d-live-probe",
		},
		{
			ID:               f.docIDs[1],
			Kind:             memoryindex.KindUserMemory,
			Title:            "Phase07D live pending user fact",
			Body:             f.query + " " + f.userPendingToken,
			Handle:           f.docIDs[1],
			Status:           "pending",
			Tags:             []string{"preference"},
			UpdatedAt:        updatedAt.Add(time.Second),
			EmbeddingModelID: "embeddinggemma-300m@256-mrl",
			IndexBuildID:     "phase07d-live-probe",
		},
		{
			ID:               f.docIDs[2],
			Kind:             memoryindex.KindOperational,
			Title:            "Lesson: phase07d live probe",
			Body:             f.query + " " + f.operationalToken,
			Handle:           f.docIDs[2],
			Status:           "active",
			UpdatedAt:        updatedAt,
			EmbeddingModelID: "embeddinggemma-300m@256-mrl",
			IndexBuildID:     "phase07d-live-probe",
		},
		{
			ID:               f.docIDs[3],
			Kind:             memoryindex.KindOperational,
			Title:            "Lesson: phase07d live pending probe",
			Body:             f.query + " " + f.operationalPending,
			Handle:           f.docIDs[3],
			Status:           "pending",
			UpdatedAt:        updatedAt.Add(time.Second),
			EmbeddingModelID: "embeddinggemma-300m@256-mrl",
			IndexBuildID:     "phase07d-live-probe",
		},
		{
			ID:               f.docIDs[4],
			Kind:             memoryindex.KindSource,
			Title:            "Phase07D live source trap",
			Body:             f.query + " " + f.sourceToken,
			Handle:           f.docIDs[4] + "#page=1",
			SourceID:         "src_phase07d_live_" + strings.ReplaceAll(f.query, " ", "_"),
			Page:             1,
			Status:           "active",
			UpdatedAt:        updatedAt,
			EmbeddingModelID: "embeddinggemma-300m@256-mrl",
			IndexBuildID:     "phase07d-live-probe",
		},
		{
			ID:               f.docIDs[5],
			Kind:             memoryindex.KindArchive,
			Title:            "Phase07D live archive trap",
			Body:             f.query + " " + f.archiveToken,
			Handle:           "conversation:phase07d-live",
			Status:           "active",
			UpdatedAt:        updatedAt,
			EmbeddingModelID: "embeddinggemma-300m@256-mrl",
			IndexBuildID:     "phase07d-live-probe",
		},
		{
			ID:               f.docIDs[6],
			Kind:             memoryindex.KindProposal,
			Title:            "Phase07D live proposal trap",
			Body:             f.query + " " + f.proposalToken,
			Handle:           "proposal:phase07d-live",
			Status:           "pending",
			UpdatedAt:        updatedAt,
			EmbeddingModelID: "embeddinggemma-300m@256-mrl",
			IndexBuildID:     "phase07d-live-probe",
		},
	}
	for _, doc := range docs {
		if err := env.upsertCompactMemoryDocument(ctx, doc); err != nil {
			return fmt.Errorf("upsert %s: %w", doc.ID, err)
		}
	}
	return nil
}

func (f *phase07DMixedTierFixture) verify(r ChatReply, env *Env) []string {
	var miss []string
	if r.ToolCalls < 2 {
		miss = append(miss, fmt.Sprintf("expected at least 2 tool calls for typed recall, got %d", r.ToolCalls))
	}
	for _, want := range []string{f.userApprovedToken, f.operationalToken} {
		if !strings.Contains(r.Reply, want) {
			miss = append(miss, fmt.Sprintf("reply missing approved token %q", want))
		}
	}
	for _, reject := range []string{
		f.userPendingToken,
		f.operationalPending,
		f.sourceToken,
		f.archiveToken,
		f.proposalToken,
	} {
		if strings.Contains(r.Reply, reject) {
			miss = append(miss, fmt.Sprintf("reply leaked wrong-tier token %q", reject))
		}
	}
	if env == nil || env.DB == nil {
		miss = append(miss, "DB unavailable for tool_attempts ground truth")
		return miss
	}
	counts, err := env.toolAttemptsSince(f.startedAt, "recall_user_memory", "recall_operational", "search_memory")
	if err != nil {
		miss = append(miss, fmt.Sprintf("tool_attempts query: %v", err))
		return miss
	}
	for _, toolName := range []string{"recall_user_memory", "recall_operational"} {
		if counts[toolName] == 0 {
			miss = append(miss, fmt.Sprintf("tool_attempts missing ok row for %s since %s", toolName, f.startedAt.Format(time.RFC3339Nano)))
		}
	}
	if counts["search_memory"] > 0 {
		miss = append(miss, "probe used search_memory; expected explicit typed recall tools only")
	}
	return miss
}

func (f *phase07DMixedTierFixture) cleanup(env *Env) {
	if f == nil || env == nil || strings.TrimSpace(env.DBPath) == "" {
		return
	}
	_ = env.deleteCompactMemoryDocuments(context.Background(), f.docIDs)
}
