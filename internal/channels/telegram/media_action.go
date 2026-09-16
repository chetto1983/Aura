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

// mediaActionController tracks the media work in flight for one turn and selects the
// chat action the turn's single pulse sends. Two goroutines reach it — the status
// consumer, which drives the tool lifecycle and owns start/Stop, and the artifact
// consumer, which holds the action across an upload — plus the pulse, which only
// reads the selection; the mutex covers exactly that, and is never held across a
// Notify. A nil receiver is a no-op (a pane driven outside consume has no controller).
type mediaActionController struct {
	mu       sync.Mutex
	active   map[string]tele.ChatAction // tool calls the agent is running
	held     map[string]tele.ChatAction // uploads in flight, outliving their tool call
	selected tele.ChatAction
	stopped  bool

	notifier botNotifier
	to       tele.Recipient
	stop     func()
}

// newMediaActionController builds the turn's controller on "typing". Nothing pulses
// until start: the controller exists from the moment the consumers are built, because
// the artifact consumer shares it, but the turn's indicator belongs to the status
// consumer's lifetime.
func newMediaActionController(n botNotifier, to tele.Recipient) *mediaActionController {
	return &mediaActionController{
		active:   make(map[string]tele.ChatAction),
		held:     make(map[string]tele.ChatAction),
		selected: tele.Typing,
		notifier: n,
		to:       to,
	}
}

// start opens the turn's single chat-action pulse, which re-reads the selection at
// every tick so an upload longer than Telegram's action expiry keeps showing as an
// upload. Calling it twice, or after Stop, does nothing — there is one ticker.
func (c *mediaActionController) start(ctx context.Context) {
	if c == nil {
		return
	}
	c.mu.Lock()
	begin := !c.stopped && c.stop == nil
	c.mu.Unlock()
	if !begin {
		return
	}
	// Outside the lock: the pulse notifies immediately, reading the selection through
	// action(), which takes the same mutex.
	stop := pulseChatActionFunc(ctx, c.notifier, c.to, c.action)
	c.mu.Lock()
	stopped := c.stopped
	if !stopped {
		c.stop = stop
	}
	c.mu.Unlock()
	if stopped {
		stop() // Stop landed while the pulse was opening: join it now, never leak it
	}
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
// stops on the terminal run event and again when the event channel closes. It is also
// the backstop for a hold whose upload never returned — nothing notifies after it, and
// no later Start or Hold can restart it.
func (c *mediaActionController) Stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.stopped = true
	stop := c.stop
	c.stop = nil
	c.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// Hold claims the action for an upload that is ALREADY under way. It exists because the
// tool call ends the instant the bytes start moving: the translator emits
// TOOL_CALL_END, TOOL_CALL_RESULT and the artifact descriptor from the SAME source
// event, so without a hold the chat would fall back to typing for the heaviest part of
// the turn. Held claims live apart from the in-flight tool calls, so the pane's Finish
// — which fires on both END and RESULT — cannot drop them.
func (c *mediaActionController) Hold(callID string, action tele.ChatAction) {
	if c == nil || callID == "" {
		return
	}
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.held[callID] = action
	next, changed := c.reselect()
	c.mu.Unlock()
	c.notify(next, changed)
}

// Release ends a hold when the upload returns, whether it delivered or failed.
func (c *mediaActionController) Release(callID string) {
	if c == nil || callID == "" {
		return
	}
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	delete(c.held, callID)
	next, changed := c.reselect()
	c.mu.Unlock()
	c.notify(next, changed)
}

// action is what the pulse sends at each tick.
func (c *mediaActionController) action() tele.ChatAction {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.selected
}

// reselect recomputes the action from the running tool calls and the uploads in flight
// and reports whether it changed. Caller holds the mutex.
func (c *mediaActionController) reselect() (tele.ChatAction, bool) {
	next := tele.Typing
	for _, action := range c.active {
		next = outrank(next, action)
	}
	for _, action := range c.held {
		next = outrank(next, action)
	}
	if next == c.selected {
		return next, false
	}
	c.selected = next
	return next, true
}

// outrank picks the action worth showing between two claims. Video wins over image: it
// is the longer, heavier upload, so it is the one worth describing while both run.
func outrank(current, candidate tele.ChatAction) tele.ChatAction {
	if current == tele.UploadingVideo || candidate == tele.UploadingVideo {
		return tele.UploadingVideo
	}
	return candidate
}

// notify pushes a changed action out of band. Best-effort, like the pulse itself — a
// Notify error (no permission, stale chat) never touches the turn.
func (c *mediaActionController) notify(action tele.ChatAction, changed bool) {
	if !changed || c.notifier == nil || c.to == nil {
		return
	}
	_ = c.notifier.Notify(c.to, action)
}

// notifierFor resolves the chat-action seam out of a render sender: the live *tele.Bot
// satisfies both, exactly as keepWorking and the HITL resume path resolve it; a
// render-only double yields nil and the controller degrades to a no-op.
func notifierFor(bot botSender) botNotifier {
	n, _ := bot.(botNotifier)
	return n
}

// holdUploadAction keeps the chat's upload action alive for the whole Send and returns
// the release the caller MUST defer, so a failed upload frees the action exactly like a
// delivered one. A descriptor without a tool_call_id, or a payload that is not a media
// upload (a document, the cockpit announcement), holds nothing.
func (a *artifact) holdUploadAction(desc map[string]any, payload any) (release func()) {
	callID := stringField(desc, "tool_call_id")
	action, ok := uploadActionFor(payload)
	if callID == "" || !ok {
		return func() {}
	}
	a.actions.Hold(callID, action)
	return func() { a.actions.Release(callID) }
}

// uploadActionFor names what the chat should say while these bytes go up. Only the
// native media uploads claim an action — they are the ones this turn promised.
func uploadActionFor(payload any) (tele.ChatAction, bool) {
	switch payload.(type) {
	case *tele.Photo:
		return tele.UploadingPhoto, true
	case *tele.Video:
		return tele.UploadingVideo, true
	}
	return "", false
}
