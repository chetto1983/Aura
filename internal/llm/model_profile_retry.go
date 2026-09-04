package llm

// Bounding a TRANSIENT catalogue failure at boot, for the same reason mcptools/mount_retry.go
// bounds a transient mount failure: the process starts alongside its dependencies and a
// single blip at that moment used to disable a capability for the whole run.
//
// Measured 2026-09-04. One warning at boot --
//
//	model profile unresolved; using configured fallback provider=openrouter
//	model=z-ai/glm-5.3-flash err="model profile unavailable: GET /models failed"
//
// -- and then, hours later, every turn the classifier called easy died with HTTP 400
// "Reasoning is mandatory for this endpoint and cannot be disabled". The chain: the
// catalogue never loaded, so SupportedReasoningEfforts stayed empty, so
// ClampReasoningEffort had nothing to clamp against and let `none` onto the wire. That
// model publishes ["max","high","low"] with mandatory:true and WOULD have been clamped to
// low. Checked from inside the container the same day, GET /models answered 200 in 0.25s
// with and without a key: nothing was misconfigured, the fetch simply lost a race at
// startup and was never asked again.
//
// Fail-soft at boot stays: an unreachable catalogue must not stop Aura from starting on
// its validated deployment fallback. What changes is that "unreachable" now has to be
// true several seconds running, rather than true for one instant and believed forever.
//
// Only the FETCH retries. A catalogue that answers and does not list the model, or lists
// it without a context window, is a settled answer -- asking again produces the same one,
// and retrying it would turn a fast, legible failure into a slow one.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ErrModelCatalogueUnreachable marks the transport half of a profile failure: the
// catalogue could not be read at all, as opposed to answering and not describing the
// model. Only this half is worth asking again.
var ErrModelCatalogueUnreachable = errors.New("model catalogue unreachable")

// modelProfileRetry bounds the boot-time retry. Four attempts over roughly seven seconds
// covers the startup race that produced the measurement above without making a genuinely
// offline boot wait meaningfully longer than the single 10s fetch timeout it already had.
var modelProfileRetry = struct {
	Attempts  int
	BaseDelay time.Duration
	MaxDelay  time.Duration
}{Attempts: 4, BaseDelay: 500 * time.Millisecond, MaxDelay: 4 * time.Second}

// fetchModelProfileWithRetry runs FetchModelProfile under that bound, retrying only an
// unreachable catalogue and returning every other outcome -- success or settled failure --
// on the first attempt.
func fetchModelProfileWithRetry(
	ctx context.Context,
	client *http.Client,
	provider, baseURL, apiKey, model string,
) (ModelProfileMetadata, error) {
	var err error
	for attempt := range modelProfileRetry.Attempts {
		var metadata ModelProfileMetadata
		metadata, err = FetchModelProfile(ctx, client, provider, baseURL, apiKey, model)
		if err == nil || !errors.Is(err, ErrModelCatalogueUnreachable) {
			return metadata, err
		}
		if attempt == modelProfileRetry.Attempts-1 {
			break
		}
		if waitErr := sleepModelProfileBackoff(ctx, attempt); waitErr != nil {
			return ModelProfileMetadata{}, fmt.Errorf("%w: %w", err, waitErr)
		}
	}
	return ModelProfileMetadata{}, fmt.Errorf("%w (after %d attempts)", err, modelProfileRetry.Attempts)
}

// sleepModelProfileBackoff waits out one capped exponential interval, or returns as soon
// as the caller's context ends. The whole retry budget rides on that one context, so a
// boot that is being cancelled does not first sit through its remaining backoffs.
func sleepModelProfileBackoff(ctx context.Context, attempt int) error {
	timer := time.NewTimer(min(modelProfileRetry.BaseDelay<<attempt, modelProfileRetry.MaxDelay))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
