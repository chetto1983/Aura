package toolindex

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/qdrant"
	"github.com/google/uuid"
)

// Reason classifies why a reconcile was triggered. Surfaced in the JSON
// report so operators can correlate dashboard reindex clicks with the
// resulting embedding-call burst in logs.
type Reason string

const (
	ReasonBoot          Reason = "boot"
	ReasonSkillsChanged Reason = "skills_changed"
	ReasonMCPConfig     Reason = "mcp_config_changed"
	ReasonManual        Reason = "manual"
	ReasonPeriodic      Reason = "periodic"
)

// Report describes the outcome of one Reconcile pass. All slices are
// sorted for stable comparison + readable logs.
type Report struct {
	Reason    Reason   `json:"reason"`
	Upserted  []string `json:"upserted"`
	Deleted   []string `json:"deleted"`
	Unchanged int      `json:"unchanged"`
	Errors    []string `json:"errors,omitempty"`
	ElapsedMs int64    `json:"elapsed_ms"`
}

// Config wires the Reconciler. All fields are required except Logger,
// EmbedTextFn (a sensible default fallback), Debounce, and Periodic.
type Config struct {
	Provider   ToolProvider
	Qdrant     QdrantClient
	Embedder   Embedder
	State      StateStore
	Collection string
	VectorDim  int
	EmbedModel string

	// EmbedTextFn renders the per-tool embedding input. Defaults to a
	// minimal "<name>: <description>" if nil; production callers pass
	// tools.SearchableEmbeddingText or equivalent for full schema-aware
	// text.
	EmbedTextFn EmbeddingTextFn

	Logger   *slog.Logger
	Debounce time.Duration // default 500 ms
	Periodic time.Duration // default 10 min; <=0 disables periodic ticker

	// PointIDNamespace seeds the uuidv5 derivation so reading from disk a
	// year later (with a wiped Qdrant) regenerates the SAME point IDs.
	// Tests can override; production leaves it at the default.
	PointIDNamespace uuid.UUID

	// PointIDFn overrides the default uuidv5 derivation. The boot wiring
	// passes tools.ToolQdrantPointID so the Reconciler matches the point
	// IDs the search-side reader expects. When nil, falls back to
	// uuidv5(PointIDNamespace, name).
	PointIDFn func(toolName string) string
}

// DefaultPointIDNamespace is the uuidv5 namespace used to derive deterministic
// Qdrant point IDs from tool_name. Drilling this constant into the binary
// means a wiped DB + collection can be re-indexed and produce the same
// point IDs as before — useful when external clients have cached those IDs.
//
// Generated once by `uuid.NewSHA1(uuid.NameSpaceURL,
// []byte("aura-tool-index-2026"))` and pinned.
var DefaultPointIDNamespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("aura-tool-index-2026"))

// Reconciler is the diff engine. Construct one per process via New(); call
// Reconcile() once synchronously at boot, then Notify() from change hooks
// and let Run() drain the queue with debounce.
type Reconciler struct {
	cfg Config

	mu sync.Mutex

	notify chan Reason
}

// New validates Config and returns a Reconciler. Any missing required
// field is a configuration bug, not a runtime degrade — the caller should
// have caught this at startup.
func New(cfg Config) (*Reconciler, error) {
	if cfg.Provider == nil {
		return nil, errors.New("toolindex: Provider required")
	}
	if cfg.Qdrant == nil {
		return nil, errors.New("toolindex: Qdrant required")
	}
	if cfg.Embedder == nil {
		return nil, errors.New("toolindex: Embedder required")
	}
	if cfg.State == nil {
		return nil, errors.New("toolindex: State required")
	}
	if cfg.Collection == "" {
		return nil, errors.New("toolindex: Collection required")
	}
	if cfg.VectorDim <= 0 {
		return nil, errors.New("toolindex: VectorDim must be positive")
	}
	if cfg.EmbedModel == "" {
		return nil, errors.New("toolindex: EmbedModel required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.EmbedTextFn == nil {
		cfg.EmbedTextFn = defaultEmbedText
	}
	if cfg.Debounce <= 0 {
		cfg.Debounce = 500 * time.Millisecond
	}
	if cfg.Periodic == 0 {
		cfg.Periodic = 10 * time.Minute
	}
	if cfg.PointIDNamespace == uuid.Nil {
		cfg.PointIDNamespace = DefaultPointIDNamespace
	}
	return &Reconciler{
		cfg:    cfg,
		notify: make(chan Reason, 16),
	}, nil
}

func defaultEmbedText(def llm.ToolDefinition) string {
	return def.Name + ": " + def.Description
}

// PointID derives a stable Qdrant point ID from the tool name. Exposed so
// tests + the manual reindex handler can compute it the same way. Honors
// the optional Config.PointIDFn override (used by the boot wiring to match
// the search-side reader's IDs); otherwise falls back to uuidv5 against
// PointIDNamespace.
func (r *Reconciler) PointID(toolName string) string {
	if r.cfg.PointIDFn != nil {
		return r.cfg.PointIDFn(toolName)
	}
	return uuid.NewSHA1(r.cfg.PointIDNamespace, []byte(toolName)).String()
}

// Notify enqueues a reconcile request. Non-blocking: if the channel is
// full the notification is dropped (the next Reconcile pass will pick up
// the changes anyway). The Run() goroutine merges multiple notifications
// within the debounce window so a burst of skill installs results in one
// reconcile pass.
func (r *Reconciler) Notify(reason Reason) {
	select {
	case r.notify <- reason:
	default:
		// Channel full — a reconcile is already queued; the merge in Run()
		// will pick up the same wanted set we'd see now.
		r.cfg.Logger.Debug("toolindex: notify dropped (queue full)", "reason", reason)
	}
}

// Run is the long-lived goroutine. Drains the notify channel with
// debounce, runs Reconcile, sleeps until next event or periodic tick.
// Stops cleanly on ctx.Done().
func (r *Reconciler) Run(ctx context.Context) {
	var periodic *time.Ticker
	if r.cfg.Periodic > 0 {
		periodic = time.NewTicker(r.cfg.Periodic)
		defer periodic.Stop()
	}

	for {
		// Block waiting for the next event of any kind.
		var primary Reason
		select {
		case <-ctx.Done():
			return
		case reason := <-r.notify:
			primary = reason
		case <-tickerChan(periodic):
			primary = ReasonPeriodic
		}

		// Debounce: drain any additional notifications that arrive within
		// the debounce window. The "winner" reason is the first one in;
		// subsequent reasons are merged into the same pass.
		debounceTimer := time.NewTimer(r.cfg.Debounce)
		coalesced := 0
	drain:
		for {
			select {
			case <-ctx.Done():
				debounceTimer.Stop()
				return
			case <-r.notify:
				coalesced++
			case <-debounceTimer.C:
				break drain
			}
		}
		if coalesced > 0 {
			r.cfg.Logger.Debug("toolindex: coalesced notifications",
				"first_reason", primary, "merged", coalesced)
		}

		report := r.Reconcile(ctx, primary)
		r.logReport(report)
	}
}

// tickerChan returns a nil-safe channel from an optional ticker. nil
// channels block forever, which is the right behavior when the periodic
// safety-net is disabled.
func tickerChan(t *time.Ticker) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

// Reconcile is the synchronous diff-and-apply primitive. Safe to call
// directly (the manual /api/tools/reindex endpoint uses this) or via the
// Notify+Run path. A mutex serializes concurrent callers; the second
// caller blocks until the first completes, then runs its own pass — this
// keeps Qdrant + state consistent without losing notifications.
func (r *Reconciler) Reconcile(ctx context.Context, reason Reason) Report {
	r.mu.Lock()
	defer r.mu.Unlock()

	start := time.Now()
	report := Report{Reason: reason}

	// Step 1 — snapshot wanted (Registry) and indexed (SQLite state).
	defs := r.cfg.Provider.Definitions()
	wanted := make(map[string]llm.ToolDefinition, len(defs))
	wantedHash := make(map[string]string, len(defs))
	wantedText := make(map[string]string, len(defs))
	for _, def := range defs {
		text := r.cfg.EmbedTextFn(def)
		hash, err := ContentHash(def, text, r.cfg.EmbedModel)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("hash %s: %v", def.Name, err))
			continue
		}
		wanted[def.Name] = def
		wantedHash[def.Name] = hash
		wantedText[def.Name] = text
	}

	indexed, err := r.cfg.State.LoadAll(ctx)
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("state load: %v", err))
		report.ElapsedMs = time.Since(start).Milliseconds()
		return report
	}

	// Drift recovery: if SQLite state says we have N indexed rows but
	// Qdrant says the collection has zero points, an operator dropped
	// the collection out-of-band (or a Qdrant volume got wiped). Trusting
	// the state would leave most tools as "unchanged" and Qdrant would
	// stay empty. Detect this and wipe state so the diff treats every
	// tool as new. CollectionInfo error here is non-fatal — we proceed
	// with the diff and let the upsert path surface any real Qdrant
	// problem.
	if len(indexed) > 0 {
		if info, infoErr := r.cfg.Qdrant.CollectionInfo(ctx, r.cfg.Collection); infoErr == nil {
			if info.Status == "" || info.PointsCount == 0 {
				r.cfg.Logger.Warn("toolindex: state/qdrant drift detected, wiping state",
					"state_rows", len(indexed),
					"qdrant_points", info.PointsCount,
					"qdrant_status", info.Status)
				for name := range indexed {
					if remErr := r.cfg.State.Remove(ctx, name); remErr != nil {
						report.Errors = append(report.Errors,
							fmt.Sprintf("state drift wipe %s: %v", name, remErr))
					}
				}
				indexed = map[string]StateRow{}
			}
		}
	}

	// Step 2 — compute the diff buckets.
	var toUpsert []string
	var toDelete []string
	for name, hash := range wantedHash {
		row, ok := indexed[name]
		if !ok || row.ContentHash != hash || row.EmbedModel != r.cfg.EmbedModel {
			toUpsert = append(toUpsert, name)
		} else {
			report.Unchanged++
		}
	}
	for name := range indexed {
		if _, ok := wanted[name]; !ok {
			toDelete = append(toDelete, name)
		}
	}
	sort.Strings(toUpsert)
	sort.Strings(toDelete)

	// Step 3 — apply deletes BEFORE upserts. Order doesn't matter for
	// correctness but doing deletes first means a renamed tool (where the
	// old point ID == old uuidv5 from the old name) never coexists with
	// its replacement, even briefly. Tighter consistency for free.
	if len(toDelete) > 0 {
		ids := make([]string, 0, len(toDelete))
		for _, name := range toDelete {
			row := indexed[name]
			ids = append(ids, row.PointID)
		}
		if err := r.cfg.Qdrant.Delete(ctx, r.cfg.Collection, ids); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("qdrant delete: %v", err))
			// Don't return — try to do as much useful work as possible.
		} else {
			for _, name := range toDelete {
				if err := r.cfg.State.Remove(ctx, name); err != nil {
					report.Errors = append(report.Errors,
						fmt.Sprintf("state remove %s: %v", name, err))
				}
			}
			report.Deleted = toDelete
		}
	}

	// Step 4 — apply upserts. Embed each tool individually so the embed
	// cache deduplicates by content_text. Bunching into one Qdrant.Upsert
	// minimizes round-trips.
	if len(toUpsert) > 0 {
		// Pre-flight: ensure the collection exists with the right vector
		// dim. qdrant.httpClient.CollectionInfo treats 404 as the
		// "doesn't exist yet" pitfall and returns (zero value, nil) —
		// the API contract is "status == \"\" means missing", NOT
		// "err != nil means missing". A real error is always wrapped.
		info, err := r.cfg.Qdrant.CollectionInfo(ctx, r.cfg.Collection)
		if err != nil {
			report.Errors = append(report.Errors,
				fmt.Sprintf("qdrant collection info: %v", err))
			report.ElapsedMs = time.Since(start).Milliseconds()
			return report
		}
		if info.Status == "" {
			if createErr := r.cfg.Qdrant.CreateCollection(ctx, r.cfg.Collection, r.cfg.VectorDim); createErr != nil {
				report.Errors = append(report.Errors,
					fmt.Sprintf("qdrant create collection: %v", createErr))
				report.ElapsedMs = time.Since(start).Milliseconds()
				return report
			}
		}

		points := make([]qdrant.Point, 0, len(toUpsert))
		successfulNames := make([]string, 0, len(toUpsert))
		for _, name := range toUpsert {
			def := wanted[name]
			text := wantedText[name]
			vec, err := r.cfg.Embedder.Embed(ctx, text)
			if err != nil {
				report.Errors = append(report.Errors,
					fmt.Sprintf("embed %s: %v", name, err))
				continue
			}
			points = append(points, qdrant.Point{
				ID:     r.PointID(name),
				Vector: vec,
				Payload: map[string]string{
					"name":         def.Name,
					"description":  def.Description,
					"content_hash": wantedHash[name],
				},
			})
			successfulNames = append(successfulNames, name)
		}

		if len(points) > 0 {
			if err := r.cfg.Qdrant.Upsert(ctx, r.cfg.Collection, points); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("qdrant upsert: %v", err))
			} else {
				now := time.Now().UTC().Format(time.RFC3339)
				for _, name := range successfulNames {
					row := StateRow{
						ToolName:    name,
						ContentHash: wantedHash[name],
						PointID:     r.PointID(name),
						EmbedModel:  r.cfg.EmbedModel,
						IndexedAt:   now,
					}
					if err := r.cfg.State.Upsert(ctx, row); err != nil {
						report.Errors = append(report.Errors,
							fmt.Sprintf("state upsert %s: %v", name, err))
					}
				}
				report.Upserted = successfulNames
			}
		}
	}

	report.ElapsedMs = time.Since(start).Milliseconds()
	return report
}

func (r *Reconciler) logReport(rep Report) {
	level := slog.LevelInfo
	attrs := []any{
		"reason", string(rep.Reason),
		"upserted", len(rep.Upserted),
		"deleted", len(rep.Deleted),
		"unchanged", rep.Unchanged,
		"errors", len(rep.Errors),
		"elapsed_ms", rep.ElapsedMs,
	}
	if len(rep.Errors) > 0 {
		level = slog.LevelWarn
		// Surface error detail in the same log line. Without this, operators
		// see only the count and have no way to know WHICH tool failed or why.
		attrs = append(attrs, "error_details", rep.Errors)
	}
	r.cfg.Logger.Log(context.Background(), level, "toolindex reconcile", attrs...)
}
