// Package telegram — this file is the turn's chat-action owner. A generation tool
// runs for tens of seconds, so the chat must say "sending photo…" / "sending video…"
// instead of the generic "typing…", and it must keep saying it: Telegram expires a
// chat action after ~5s. There is exactly ONE pulse per turn (bot_typing.go) and this
// controller decides what it carries at every tick, so a second ticker can never
// overwrite the first one's action.
package telegram

import (
	"context"
	"sync"

	tele "gopkg.in/telebot.v4"
)

// mediaUploadActions maps a media-generation tool to the chat action that describes
// it. A tool absent from the map never claims the action — a document search has
// nothing to upload.
var mediaUploadActions = map[string]tele.ChatAction{
	"image_generate": tele.UploadingPhoto,
	"video_generate": tele.UploadingVideo,
}

// mediaActionController tracks the media tool calls in flight for one turn and
// selects the chat action the turn's single pulse sends. It is safe for concurrent
// use and safe on a nil receiver (a pane driven outside consume has no controller).
type mediaActionController struct {
	mu       sync.Mutex
	active   map[string]tele.ChatAction
	selected tele.ChatAction
	stopped  bool

	notifier botNotifier
	to       tele.Recipient
	stop     func()
	stopOnce sync.Once
}

// newMediaActionController starts the turn's chat-action pulse on "typing" and
// returns the controller that owns it until Stop. The pulse re-reads the selection at
// every tick, so an upload longer than Telegram's action expiry keeps showing as an
// upload.
func newMediaActionController(ctx context.Context, n botNotifier, to tele.Recipient) *mediaActionController {
	c := &mediaActionController{
		active:   make(map[string]tele.ChatAction),
		selected: tele.Typing,
		notifier: n,
		to:       to,
	}
	c.stop = pulseChatActionFunc(ctx, n, to, c.action)
	return c
}

// Start records a media tool call as in flight. When it changes what the chat should
// say the new action goes out IMMEDIATELY — waiting for the next tick would leave the
// user on "typing…" for up to the refresh period.
func (c *mediaActionController) Start(callID, toolName string) {
	if c == nil {
		return
	}
	action, ok := mediaUploadActions[toolName]
	if !ok {
		return
	}
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.active[callID] = action
	next, changed := c.reselect()
	c.mu.Unlock()
	c.notify(next, changed)
}

// Finish releases a tool call's claim on the action. The turn falls back to whatever
// remains active, or to typing when this was the last media call — the agent is still
// composing an answer, so the chat is never left silent.
func (c *mediaActionController) Finish(callID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	delete(c.active, callID)
	next, changed := c.reselect()
	c.mu.Unlock()
	c.notify(next, changed)
}

// Stop ends the pulse and JOINS its goroutine (goleak). It is idempotent: the pane
// stops on the terminal run event and again when the event channel closes. Nothing
// notifies after it.
func (c *mediaActionController) Stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.stopped = true
	c.mu.Unlock()
	c.stopOnce.Do(func() {
		if c.stop != nil {
			c.stop()
		}
	})
}

// action is what the pulse sends at each tick.
func (c *mediaActionController) action() tele.ChatAction {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.selected
}

// reselect recomputes the action from the in-flight calls and reports whether it
// changed. Video wins over image: it is the longer, heavier upload, so it is the one
// worth describing while both run. Caller holds the mutex.
func (c *mediaActionController) reselect() (tele.ChatAction, bool) {
	next := tele.Typing
	for _, action := range c.active {
		next = action
		if action == tele.UploadingVideo {
			break
		}
	}
	if next == c.selected {
		return next, false
	}
	c.selected = next
	return next, true
}

// notify pushes a changed action out of band. Best-effort, like the pulse itself — a
// Notify error (no permission, stale chat) never touches the turn.
func (c *mediaActionController) notify(action tele.ChatAction, changed bool) {
	if !changed || c.notifier == nil || c.to == nil {
		return
	}
	_ = c.notifier.Notify(c.to, action)
}

// newActions builds the pane's controller. The notifier is the very bot the pane
// already sends through — the live *tele.Bot satisfies both seams, exactly as
// keepWorking and the HITL resume path resolve it; a render-only double simply
// yields a no-op controller.
func (p *statusPane) newActions(ctx context.Context) *mediaActionController {
	n, _ := p.bot.(botNotifier)
	return newMediaActionController(ctx, n, p.to)
}
