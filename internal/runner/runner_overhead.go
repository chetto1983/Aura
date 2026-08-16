package runner

import (
	"encoding/json"
	"sync"

	"github.com/pkoukk/tiktoken-go"
)

// manifestOverheadTokens is what every request carries besides the conversation: the tool
// definitions the agent renders on each turn.
//
// The context ladder is handed turns, so it cannot see this. Measured on the live
// deployment (2026-08-16) that blind spot was worth roughly 19,000 tokens: a request whose
// replayed history the ladder counted at ~19k arrived at the provider as 38,941, so a
// trigger set to "30% of the window" was in truth measuring 30% of half the request.
//
// Counted once and cached: the manifest is rebuilt per turn but its SIZE only changes when
// tools are registered, which happens at boot. Tokenizing thirty-odd JSON schemas on every
// turn to learn a number that does not move would be a strange way to save tokens.
func (r *Runner) manifestOverheadTokens() int {
	r.overheadOnce.Do(func() {
		if r.registry == nil {
			return
		}
		enc, err := tiktoken.GetEncoding("cl100k_base")
		if err != nil {
			return
		}
		// The deferred roster plus the always-visible defs is what actually goes on the
		// wire: a deferred tool contributes its one-line summary, not its schema, which is
		// the whole point of the deferred-tool pattern.
		defs := r.registry.RenderToolDefs(nil)
		encoded, err := json.Marshal(defs)
		if err != nil {
			return
		}
		r.overheadTokens = len(enc.Encode(string(encoded), nil, nil)) +
			len(enc.Encode(r.registry.DeferredRoster(nil), nil, nil))
	})
	return r.overheadTokens
}

// overheadFields are embedded in Runner; kept here so the caching lives beside the function
// that owns it.
type overheadFields struct {
	overheadOnce   sync.Once
	overheadTokens int
}
