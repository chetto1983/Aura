package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

const (
	// DynamicTailFallbackContextBudget records whole-item budget omission.
	DynamicTailFallbackContextBudget = "context_budget"
	// DynamicTailFallbackInvalid records a final exposure invariant failure.
	DynamicTailFallbackInvalid = "state_invalid"
)

// DynamicTailResult is one ordered, stable Agent Memory result reference.
type DynamicTailResult struct {
	Kind  string
	ID    string
	Order int
}

// DynamicTailLimit binds one result kind to its requested, effective, and
// returned cardinalities.
type DynamicTailLimit struct {
	RequestedK int
	EffectiveK int
	Count      int
}

// DynamicTailRevisions freeze the retrieval stack used for this recall.
type DynamicTailRevisions struct {
	Retriever string
	Reranker  string
	Embedding string
	Index     string
}

// DynamicTailOutcome is the only information returned to the ledger port.
// Delivered marks learned-tail exposure; both false fields mark static serving.
// Recalled text and query bytes never cross this boundary.
type DynamicTailOutcome struct {
	Delivered      bool
	FallbackReason string
}

// DynamicTail is the exact non-persisted message item guarded at exposure time.
type DynamicTail struct {
	Content           string
	BeforeCurrentUser bool
}

// DynamicTailExposure holds one already-assigned recall until the model-bound
// request has passed its final exposure checks.
type DynamicTailExposure struct {
	Tail                  DynamicTail
	HardCapTokens         int
	Included              bool
	Results               []DynamicTailResult
	Limits                map[string]DynamicTailLimit
	Revisions             DynamicTailRevisions
	CorpusEpochBefore     *uint64
	CorpusEpochAfter      *uint64
	Coherent              bool
	Commit                func(context.Context, DynamicTailOutcome) error
	ValidateRequest       func([]llm.Message, int) error
	deliveryCommitted     bool
	disabled              bool
	boundHistoryPosition  int
	staticRequestMessages []llm.Message
}

func (exposure *DynamicTailExposure) bind(messages []llm.Message) {
	if exposure == nil {
		return
	}
	exposure.boundHistoryPosition = -1
	for index, message := range messages {
		if message.Role == llm.RoleUser && message.Content == exposure.Tail.Content {
			exposure.boundHistoryPosition = index
			break
		}
	}
}

func (exposure *DynamicTailExposure) validate() error {
	if exposure == nil || exposure.Commit == nil {
		return errors.New("dynamic tail delivery port is unavailable")
	}
	if exposure.HardCapTokens <= 0 || !exposure.Included {
		return errors.New("dynamic tail is not model-visible")
	}
	if !exposure.Coherent || exposure.CorpusEpochBefore == nil ||
		exposure.CorpusEpochAfter == nil || *exposure.CorpusEpochBefore == 0 ||
		*exposure.CorpusEpochBefore != *exposure.CorpusEpochAfter {
		return errors.New("dynamic tail corpus epoch is incoherent")
	}
	for _, revision := range []string{
		exposure.Revisions.Retriever,
		exposure.Revisions.Reranker,
		exposure.Revisions.Embedding,
		exposure.Revisions.Index,
	} {
		if !validDynamicRevision(revision) {
			return errors.New("dynamic tail revision is invalid")
		}
	}
	counts := make(map[string]int)
	if len(exposure.Results) == 0 {
		return errors.New("dynamic tail ordered results are missing")
	}
	seenResultIDs := make(map[string]struct{}, len(exposure.Results))
	for index, result := range exposure.Results {
		if result.Order != index || !validDynamicResultKind(result.Kind) {
			return errors.New("dynamic tail result order or kind is invalid")
		}
		parsed, err := uuid.Parse(result.ID)
		if err != nil || parsed == uuid.Nil || parsed.String() != result.ID {
			return fmt.Errorf("dynamic tail result %d ID is invalid", index)
		}
		if _, duplicate := seenResultIDs[result.ID]; duplicate {
			return fmt.Errorf("dynamic tail result %d ID is duplicated", index)
		}
		seenResultIDs[result.ID] = struct{}{}
		counts[result.Kind]++
	}
	if len(exposure.Limits) != 2 {
		return errors.New("dynamic tail long-term limits are incomplete")
	}
	for _, kind := range []string{"memory_preference", "memory_entity"} {
		if _, ok := exposure.Limits[kind]; !ok {
			return errors.New("dynamic tail long-term limit is missing")
		}
	}
	total := 0
	for kind, limit := range exposure.Limits {
		if !validDynamicResultKind(kind) || limit.RequestedK <= 0 ||
			limit.EffectiveK <= 0 || limit.RequestedK < limit.EffectiveK ||
			limit.EffectiveK < limit.Count || limit.Count != counts[kind] {
			return fmt.Errorf("dynamic tail %s limits are invalid", kind)
		}
		total += limit.Count
		delete(counts, kind)
	}
	if len(counts) != 0 || total != len(exposure.Results) {
		return errors.New("dynamic tail result counts are incomplete")
	}
	return nil
}

// ValidateMetadata lets the runner reject malformed provider responses before
// they enter context budgeting; the final guard repeats the same validation.
func (exposure *DynamicTailExposure) ValidateMetadata() error {
	return exposure.validate()
}

func validDynamicResultKind(kind string) bool {
	switch kind {
	case "memory_entity", "memory_preference":
		return true
	default:
		return false
	}
}

func validDynamicRevision(revision string) bool {
	if revision == "" || len(revision) > 128 || strings.TrimSpace(revision) != revision {
		return false
	}
	for _, character := range revision {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func (a *LlmAgent) guardDynamicTail(
	ctx context.Context,
	request llm.Request,
) llm.Request {
	exposure := a.dynamicTail
	if exposure == nil || exposure.disabled {
		return request
	}
	if !exposure.Included {
		exposure.commitFallback(ctx, DynamicTailFallbackContextBudget)
		exposure.disabled = true
		return a.restoreStaticDynamicTailRequest(request)
	}
	if err := exposure.validate(); err != nil {
		exposure.commitFallback(ctx, DynamicTailFallbackInvalid)
		exposure.disabled = true
		return a.restoreStaticDynamicTailRequest(request)
	}
	request.Messages = placeDynamicTailAtRequestTail(request.Messages, exposure)
	if exposure.ValidateRequest == nil ||
		exposure.ValidateRequest(request.Messages, exposure.HardCapTokens) != nil {
		exposure.commitFallback(ctx, DynamicTailFallbackInvalid)
		exposure.disabled = true
		return a.restoreStaticDynamicTailRequest(request)
	}
	if exposure.deliveryCommitted {
		return request
	}
	if err := exposure.Commit(ctx, DynamicTailOutcome{Delivered: true}); err != nil {
		exposure.disabled = true
		return a.restoreStaticDynamicTailRequest(request)
	}
	exposure.deliveryCommitted = true
	return request
}

func (a *LlmAgent) attachDynamicTail(request llm.Request) llm.Request {
	exposure := a.dynamicTail
	if exposure == nil || exposure.disabled || !exposure.Included {
		return request
	}
	exposure.staticRequestMessages = append(
		[]llm.Message(nil),
		request.Messages...,
	)
	item := llm.Message{Role: llm.RoleUser, Content: exposure.Tail.Content}
	index := len(request.Messages)
	if exposure.Tail.BeforeCurrentUser {
		index = exposure.boundHistoryPosition
		if index < 0 || index > len(request.Messages) {
			index = len(request.Messages)
		}
	}
	messages := make([]llm.Message, 0, len(request.Messages)+1)
	messages = append(messages, request.Messages[:index]...)
	messages = append(messages, item)
	messages = append(messages, request.Messages[index:]...)
	request.Messages = messages
	return request
}

func (a *LlmAgent) restoreStaticDynamicTailRequest(
	request llm.Request,
) llm.Request {
	exposure := a.dynamicTail
	a.history = stripDynamicTail(a.history, exposure)
	if exposure != nil && exposure.staticRequestMessages != nil {
		request.Messages = append(
			[]llm.Message(nil),
			exposure.staticRequestMessages...,
		)
		return request
	}
	request.Messages = stripDynamicTail(request.Messages, exposure)
	return request
}

func placeDynamicTailAtRequestTail(
	messages []llm.Message,
	exposure *DynamicTailExposure,
) []llm.Message {
	if exposure == nil || exposure.Tail.BeforeCurrentUser {
		return messages
	}
	index := -1
	for candidate, message := range messages {
		if message.Role == llm.RoleUser &&
			message.Content == exposure.Tail.Content {
			if index >= 0 {
				return messages
			}
			index = candidate
		}
	}
	if index < 0 || index == len(messages)-1 {
		return messages
	}
	out := make([]llm.Message, 0, len(messages))
	out = append(out, messages[:index]...)
	out = append(out, messages[index+1:]...)
	out = append(out, messages[index])
	return out
}

func (exposure *DynamicTailExposure) commitFallback(
	ctx context.Context,
	reason string,
) {
	if exposure.deliveryCommitted || exposure.Commit == nil {
		return
	}
	if err := exposure.Commit(ctx, DynamicTailOutcome{
		FallbackReason: reason,
	}); err == nil {
		exposure.deliveryCommitted = true
	}
}

func stripDynamicTail(
	messages []llm.Message,
	exposure *DynamicTailExposure,
) []llm.Message {
	if exposure == nil {
		return messages
	}
	index := -1
	for candidate, message := range messages {
		if message.Role == llm.RoleUser && message.Content == exposure.Tail.Content {
			index = candidate
			break
		}
	}
	if index < 0 {
		return messages
	}
	out := make([]llm.Message, 0, len(messages)-1)
	out = append(out, messages[:index]...)
	out = append(out, messages[index+1:]...)
	return out
}
