package mcptools

import (
	"context"
	"fmt"
	"regexp"

	"github.com/chetto1983/aura/internal/identityctx"
)

// BrowserControl is what the bridge reads of the live view's control registry
// (internal/browsercontrol, prd.md §12, "The operator takes the browser over"). It is a
// consumer-declared seam so this package imports nothing from the cockpit.
type BrowserControl interface {
	Held(identityID, session string) bool
	Stale(identityID, session string) bool
	Refresh(identityID, session string)
}

// elementRef is an agent-browser element reference as a selector: "@e3". A CSS selector
// does not depend on a snapshot's numbering, so only a reference can go stale.
var elementRef = regexp.MustCompile(`^@e\d+$`)

// snapshotRef is a reference as a snapshot prints it: `textbox "Email" [ref=e2]`
// (spikes/agent-browser-auth/FINDINGS.md, A1). A result carrying one is a fresh read.
var snapshotRef = regexp.MustCompile(`\[ref=e\d+\]`)

// browserIdentity is the identity whose box the call drives: the same fallback the sandbox
// router and the live view use for a call with no principal.
func browserIdentity(ctx context.Context) string {
	if id := identityctx.IdentityID(ctx); id != "" {
		return id
	}
	return identityctx.LocalOperatorIdentity
}

// browserGuard refuses a browser call the operator's hand forbids. While the operator holds
// the session in the live view, any write is refused; reads pass, so the agent can confirm
// the page after the handoff. After the operator lets go, an element action by reference is
// refused until the agent has read fresh references. Both refusals are ordinary tool errors:
// the turn continues and the model reads why.
func browserGuard(ctrl BrowserControl, identityID, tool string, args map[string]any) error {
	if ctrl == nil {
		return nil
	}
	session, _ := args["session"].(string)
	if ctrl.Held(identityID, session) && browserRecipeActions[tool] != MCPActionRead {
		return fmt.Errorf("browser session %q is held by the operator in the live view: "+
			"wait for them to let go and tell you, then take a snapshot", session)
	}
	if selector, _ := args["selector"].(string); elementRef.MatchString(selector) && ctrl.Stale(identityID, session) {
		return fmt.Errorf("references of browser session %q are stale, the operator drove it: "+
			"take a snapshot first", session)
	}
	return nil
}

// refreshBrowserReferences marks the session fresh when the result carries references.
func refreshBrowserReferences(ctrl BrowserControl, identityID string, args map[string]any, text string) {
	if ctrl == nil || !snapshotRef.MatchString(text) {
		return
	}
	session, _ := args["session"].(string)
	ctrl.Refresh(identityID, session)
}
