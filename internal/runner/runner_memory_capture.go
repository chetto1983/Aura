package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/secret"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/google/uuid"
)

var errMemoryCaptureClosed = errors.New("memory capture queue is closed")

const (
	defaultMemoryCaptureCapacity     = 64
	defaultMemoryCaptureMaxAttempts  = 3
	defaultMemoryCaptureWriteTimeout = 5 * time.Second
	defaultMemoryCaptureRetryDelay   = 25 * time.Millisecond
)

// CaptureSourceKind is the graph sink's closed direct-evidence enum.
type CaptureSourceKind = arcadedb.CaptureSourceKind

const (
	// CaptureSourceExplicitFact identifies canonical memory_upsert_fact evidence.
	CaptureSourceExplicitFact = arcadedb.CaptureSourceExplicitFact
	// CaptureSourceDurableArtifact identifies an allowlisted persisted file.
	CaptureSourceDurableArtifact = arcadedb.CaptureSourceDurableArtifact
)

// AcceptedCapture is the runner-boundary alias of the graph sink contract.
// Sequence is assigned by MemoryCaptureQueue, never by a producer.
type AcceptedCapture = arcadedb.AcceptedCapture

// MemoryCaptureSink durably applies one accepted capture. Calls are serialized
// by MemoryCaptureQueue.
type MemoryCaptureSink interface {
	ApplyAcceptedCapture(ctx context.Context, capture AcceptedCapture) error
}

// MemoryCaptureQueueConfig bounds queue capacity, write attempts, and each sink
// attempt. Zero values select safe defaults.
type MemoryCaptureQueueConfig struct {
	Capacity     int
	MaxAttempts  int
	WriteTimeout time.Duration
	RetryDelay   time.Duration
}

// MemoryCaptureQueue is the single ordered durability queue owned by a Runner.
// Its worker is lazy and exits whenever the queue drains, so an idle Runner does
// not retain a goroutine.
type MemoryCaptureQueue struct {
	sink MemoryCaptureSink
	cfg  MemoryCaptureQueueConfig

	mu          sync.Mutex
	pending     []AcceptedCapture
	next        uint64
	completed   uint64
	failed      uint64
	terminalErr error
	working     bool
	closed      bool
	changed     chan struct{}
}

// NewMemoryCaptureQueue constructs one bounded ordered queue.
func NewMemoryCaptureQueue(sink MemoryCaptureSink, cfg MemoryCaptureQueueConfig) *MemoryCaptureQueue {
	if cfg.Capacity <= 0 {
		cfg.Capacity = defaultMemoryCaptureCapacity
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaultMemoryCaptureMaxAttempts
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = defaultMemoryCaptureWriteTimeout
	}
	if cfg.RetryDelay < 0 {
		cfg.RetryDelay = 0
	} else if cfg.RetryDelay == 0 {
		cfg.RetryDelay = defaultMemoryCaptureRetryDelay
	}
	return &MemoryCaptureQueue{sink: sink, cfg: cfg, changed: make(chan struct{})}
}

// Accept assigns a monotonic sequence and admits one capture with backpressure.
func (q *MemoryCaptureQueue) Accept(ctx context.Context, capture AcceptedCapture) (uint64, error) {
	if q == nil || q.sink == nil {
		return 0, errors.New("memory capture queue has no durable sink")
	}
	if err := validateAcceptedCapture(capture); err != nil {
		return 0, err
	}
	for {
		q.mu.Lock()
		switch {
		case q.closed:
			q.mu.Unlock()
			return 0, errMemoryCaptureClosed
		case q.terminalErr != nil:
			err := q.terminalErr
			q.mu.Unlock()
			return 0, err
		case len(q.pending) < q.cfg.Capacity:
			q.next++
			capture.Sequence = q.next
			capture.SourceRefs = append([]string(nil), capture.SourceRefs...)
			q.pending = append(q.pending, capture)
			if !q.working {
				q.working = true
				go q.run()
			}
			q.signalLocked()
			sequence := capture.Sequence
			q.mu.Unlock()
			return sequence, nil
		default:
			changed := q.changed
			q.mu.Unlock()
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-changed:
			}
		}
	}
}

// AcceptedSequence returns the highest sequence admitted so far.
func (q *MemoryCaptureQueue) AcceptedSequence() uint64 {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.next
}

// FlushThrough waits until every capture through sequence is durable.
func (q *MemoryCaptureQueue) FlushThrough(ctx context.Context, sequence uint64) error {
	if q == nil || sequence == 0 {
		return nil
	}
	for {
		q.mu.Lock()
		switch {
		case sequence > q.next:
			q.mu.Unlock()
			return fmt.Errorf("memory capture sequence %d was not accepted (watermark %d)", sequence, q.next)
		case q.terminalErr != nil && q.failed <= sequence:
			err := q.terminalErr
			q.mu.Unlock()
			return err
		case q.completed >= sequence:
			q.mu.Unlock()
			return nil
		case q.closed && !q.working:
			q.mu.Unlock()
			return fmt.Errorf("memory capture queue closed before sequence %d became durable", sequence)
		default:
			changed := q.changed
			q.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-changed:
			}
		}
	}
}

// Close stops admission, drains accepted work, and joins the queue worker.
func (q *MemoryCaptureQueue) Close(ctx context.Context) error {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	q.closed = true
	q.signalLocked()
	q.mu.Unlock()
	for {
		q.mu.Lock()
		if !q.working {
			err := q.terminalErr
			q.mu.Unlock()
			return err
		}
		changed := q.changed
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (q *MemoryCaptureQueue) run() {
	for {
		q.mu.Lock()
		if len(q.pending) == 0 {
			q.working = false
			q.signalLocked()
			q.mu.Unlock()
			return
		}
		capture := q.pending[0]
		q.pending[0] = AcceptedCapture{}
		q.pending = q.pending[1:]
		q.signalLocked()
		q.mu.Unlock()

		err := q.apply(capture)
		q.mu.Lock()
		if err != nil {
			q.failed = capture.Sequence
			q.terminalErr = fmt.Errorf("memory capture sequence %d: %w", capture.Sequence, err)
			q.pending = nil
			q.working = false
			q.signalLocked()
			q.mu.Unlock()
			return
		}
		q.completed = capture.Sequence
		q.signalLocked()
		q.mu.Unlock()
	}
}

func (q *MemoryCaptureQueue) apply(capture AcceptedCapture) error {
	var lastErr error
	for attempt := 1; attempt <= q.cfg.MaxAttempts; attempt++ {
		writeCtx, cancel := context.WithTimeout(context.Background(), q.cfg.WriteTimeout)
		err := q.sink.ApplyAcceptedCapture(writeCtx, capture)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == q.cfg.MaxAttempts {
			break
		}
		timer := time.NewTimer(q.cfg.RetryDelay)
		<-timer.C
	}
	return lastErr
}

func (q *MemoryCaptureQueue) signalLocked() {
	close(q.changed)
	q.changed = make(chan struct{})
}

func validateAcceptedCapture(capture AcceptedCapture) error {
	if capture.IdempotencyKey == "" || capture.IdentityID == "" || capture.ActorRunID == "" ||
		(capture.ActorRole != "parent" && capture.ActorRole != "worker") || capture.SourceKind == "" ||
		capture.ConversationID == "" || capture.ToolCallID == "" || capture.ObservedAt.IsZero() ||
		capture.Confidence <= 0 || capture.Confidence > 1 {
		return errors.New("memory capture is missing required direct provenance")
	}
	return nil
}

func (r *Runner) acceptMemoryCapture(ctx context.Context, tracker *turnTracker, ev *agent.Event) error {
	if r == nil || r.memoryCaptures == nil || tracker == nil {
		return nil
	}
	capture, ok := acceptedCaptureFromEvent(ctx, tracker.convID, ev)
	if !ok {
		return nil
	}
	sequence, err := r.memoryCaptures.Accept(ctx, capture)
	if err != nil {
		return fmt.Errorf("accept memory capture: %w", err)
	}
	tracker.lastAcceptedCapture = sequence
	return nil
}

func (r *Runner) flushMemoryCaptures(ctx context.Context, sequence uint64) error {
	if r == nil || r.memoryCaptures == nil || sequence == 0 {
		return nil
	}
	timeout := r.memoryCaptureFlushTimeout
	if timeout <= 0 {
		timeout = defaultMemoryCaptureFlushTimeout
	}
	flushCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := r.memoryCaptures.FlushThrough(flushCtx, sequence); err != nil {
		return fmt.Errorf("memory capture durability barrier through %d: %w", sequence, err)
	}
	return nil
}

const memoryUpsertFactModelName = "memory__memory_upsert_fact"

func acceptedCaptureFromEvent(ctx context.Context, conversationID string, ev *agent.Event) (AcceptedCapture, bool) {
	if ev == nil || ev.Actions.DiscardStreamed || ev.RequestID == uuid.Nil || ev.Timestamp.IsZero() {
		return AcceptedCapture{}, false
	}
	ti := ev.Actions.ToolInvocation
	identityID := strings.TrimSpace(identityctx.IdentityID(ctx))
	if ti == nil || ti.Event != agent.ToolInvocationEnd || ti.Status != "ok" || ti.Error != "" ||
		identityID == "" || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(ti.ToolCallID) == "" {
		return AcceptedCapture{}, false
	}
	base := AcceptedCapture{
		IdentityID: identityID, ConversationID: conversationID, ToolCallID: ti.ToolCallID,
		ObservedAt: ev.Timestamp.UTC(), Confidence: 1,
		SourceRefs: []string{"conversation:" + conversationID, "tool_call:" + ti.ToolCallID},
	}
	switch ti.ToolName {
	case memoryUpsertFactModelName:
		evidence, ok := ti.Meta[tools.MetaAcceptedFact].(tools.AcceptedFactEvidence)
		if !ok || !validCaptureActor(ev, evidence.ActorRunID, evidence.ActorRole) ||
			captureTextUnsafe(evidence.Subject, evidence.Predicate, evidence.Object, evidence.Statement) {
			return AcceptedCapture{}, false
		}
		for _, ref := range evidence.SourceMemoryIDs {
			if captureTextUnsafe(ref) {
				return AcceptedCapture{}, false
			}
			base.SourceRefs = append(base.SourceRefs, "memory:"+ref)
		}
		base.SourceKind = CaptureSourceExplicitFact
		base.ActorRunID, base.ActorRole = evidence.ActorRunID, evidence.ActorRole
		base.Subject, base.Predicate, base.Object, base.Statement =
			evidence.Subject, evidence.Predicate, evidence.Object, evidence.Statement
		validFrom, validFromOK := acceptedCaptureTime(evidence.ValidFrom)
		validTo, validToOK := acceptedCaptureTime(evidence.ValidTo)
		if !validFromOK || !validToOK {
			return AcceptedCapture{}, false
		}
		base.ValidFrom, base.ValidTo = validFrom, validTo
		base.Supersedes = evidence.Supersedes || strings.TrimSpace(evidence.SupersedesFactKey) != ""
		base.TargetFactKey = strings.TrimSpace(evidence.SupersedesFactKey)
	case "write_file", "patch":
		evidence, ok := ti.Meta[tools.MetaDurableArtifact].(tools.DurableArtifactEvidence)
		expectedOperation := "write"
		if ti.ToolName == "patch" {
			expectedOperation = "patch"
		}
		if !ok || evidence.Operation != expectedOperation ||
			!strings.HasPrefix(evidence.Path, "/workspace/") ||
			!validCaptureActor(ev, evidence.ActorRunID, evidence.ActorRole) || captureTextUnsafe(evidence.Path) {
			return AcceptedCapture{}, false
		}
		base.SourceKind = CaptureSourceDurableArtifact
		base.ActorRunID, base.ActorRole = evidence.ActorRunID, evidence.ActorRole
		base.ArtifactRef = evidence.Path
		base.Subject, base.Predicate, base.Object = evidence.Path, "durable_artifact", evidence.Operation
		base.SourceRefs = append(base.SourceRefs, "artifact:"+evidence.Path)
	default:
		return AcceptedCapture{}, false
	}
	base.IdempotencyKey = captureIdempotencyKey(base)
	return base, true
}

func validCaptureActor(ev *agent.Event, runID, role string) bool {
	return runID != "" && runID == ev.RequestID.String() && (role == "parent" || role == "worker")
}

func captureTextUnsafe(values ...string) bool {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || secret.RedactConfigured(trimmed) != trimmed ||
			toolinvocations.RedactForLedger(trimmed, 0) != trimmed {
			return true
		}
	}
	return false
}

func captureIdempotencyKey(capture AcceptedCapture) string {
	parts := []string{
		capture.IdentityID, string(capture.SourceKind), capture.ActorRunID, capture.ActorRole,
		capture.ConversationID, capture.ToolCallID, capture.Subject, capture.Predicate,
		capture.Object, capture.Statement, capture.ArtifactRef,
		capture.ValidFrom.UTC().Format(time.RFC3339Nano),
		capture.ValidTo.UTC().Format(time.RFC3339Nano),
		fmt.Sprint(capture.Supersedes), capture.TargetFactKey,
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func acceptedCaptureTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, true
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return parsed.UTC(), err == nil
}
