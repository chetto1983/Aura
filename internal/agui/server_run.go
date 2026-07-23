package agui

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
)

// server_run.go carries the POST /agent/run handler — moved verbatim out of server.go
// on the fix-plan 1.3 Tier B touch (refactor-on-touch, 600-LOC ceiling). The ONLY
// change vs the pre-move handler is the detached-path branch after the synchronous
// pre-work: a nil registry (AURA_AGUI_RUN_DETACH off) keeps the byte-identical
// request-scoped tail below it.

// handleRun parses RunAgentInput, resolves the thread (404), applies any protocol-native
// resume entries, drives Runner.Turn over the last user message, and streams the translated
// AG-UI events as SSE. The body is size-capped (T-12-12); a malformed/empty payload is a 400.
func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	req, err := decodeRunAgentRequest(json.NewDecoder(r.Body))
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	in := req.RunAgentInput
	if err := ValidateRunInput(in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	// A syntactically-invalid thread id can never identify an existing conversation:
	// resolve it to 404 BEFORE the store round-trip so a malformed id (e.g. a non-UUID
	// "does-not-exist") returns a clean 404 instead of leaking the store's parse error
	// as a 500 (T-12-11 chokepoint; caught by the live agui smoke).
	if _, err := uuid.Parse(in.ThreadID); err != nil {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}
	// Owner-scoped thread resolution (MUSR-01 / D-06): a thread the caller does not own is
	// 404 (hide existence) — B can neither read nor DRIVE a turn on A's conversation.
	if _, err := s.conv.GetForIdentity(ctx, in.ThreadID, scopedIdentityID(ctx)); err != nil {
		if errors.Is(err, conversations.ErrConversationNotFound) {
			http.Error(w, "thread not found", http.StatusNotFound)
			return
		}
		http.Error(w, "thread lookup failed", http.StatusInternalServerError)
		return
	}
	// Two-stage reasoning-effort governance (37E / WEBMODEL-02/03, D-05/D-13) — placed AFTER the
	// owner-scope gate so a foreign thread still 404s BEFORE any effort validation (isolation
	// precedes governance, T-37E-06-ISO). On success ctx may carry a validated fixed override
	// threaded into the runner, and the symbol is persisted owner-scoped; a rejected symbol has
	// already written its 400 and we stop here.
	var effortOK bool
	if ctx, effortOK = s.applyReasoningEffort(ctx, w, in.ThreadID, req.Aura.Effort); !effortOK {
		return
	}
	userMsg, err := lastUserMessage(in.Messages)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Prepend per-turn context blocks (attachments + the thread's searchable-doc catalog)
	// to the user turn; see buildTurnUserMessage (server_context.go).
	modelUserMsg, code, emsg := s.buildTurnUserMessage(ctx, r, in.ThreadID, req.Aura.AttachmentIDs, userMsg)
	if code != 0 {
		http.Error(w, emsg, code)
		return
	}
	// Pinned skill (37D / WEBSKILL-02, D-01 Mechanism A): when the client pins a skill on the
	// aura envelope, prepend the EXACT authority frame + resolved body that `skill action=use`
	// emits (tools.UseAuthorityFrame, reused verbatim — the string IS the contract) to the
	// MODEL message, so the model treats the skill as leading instructions while the visible/
	// persisted turn stays the raw user text (the *userMsg != *modelUserMsg split below then
	// fires). The name is a loader KEY resolved via the SAME provider ActiveSkills lists —
	// never a filesystem path; an unknown/absent name resolves ok=false → clean no-op (never
	// inject client text, never a 5xx). Zero runner change, no new tool (T-37D-02/03).
	if req.Aura.Skill != "" && s.governance.Skills != nil && modelUserMsg != nil {
		if body, ok := s.governance.Skills.SkillBody(req.Aura.Skill); ok {
			framed := tools.UseAuthorityFrame + body + "\n\n" + *modelUserMsg
			modelUserMsg = &framed
		}
	}

	// Detached-run branch (fix-plan 1.3 Tier B, amendment #90 point 1): with a wired
	// registry (AURA_AGUI_RUN_DETACH=true) every synchronous gate above is unchanged,
	// but the turn runs on a context.WithoutCancel producer that survives client
	// disconnect — see server_run_detach.go. A nil registry keeps today's exact tail.
	if s.runs != nil {
		s.handleRunDetached(w, r, ctx, in, userMsg, modelUserMsg)
		return
	}

	var unlock func()
	if locker, ok := s.run.(threadTryLocker); ok {
		var locked bool
		unlock, locked = locker.TryLockThread(ctx, in.ThreadID)
		if !locked {
			http.Error(w, runner.ErrThreadBusy.Error(), http.StatusConflict)
			return
		}
		defer unlock()
		ctx = runner.WithThreadLockHeld(ctx)
	}

	if len(in.Resume) > 0 {
		if _, err := s.run.SubmitAnswers(ctx, resumeAnswers(in.Resume)); err != nil {
			http.Error(w, sanitizeErr(err), http.StatusBadRequest)
			return
		}
	}

	runID := "run-" + uuid.NewString()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	// ACAO is set centrally by withCORS when CORSPermissive is on (WR-05) so it is present
	// on the error responses above too, not only this 200 stream.

	turn := s.run.Turn(ctx, in.ThreadID, modelUserMsg)
	if userMsg != nil && modelUserMsg != nil && *userMsg != *modelUserMsg {
		if split, ok := s.run.(modelUserMessageRunner); ok {
			turn = split.TurnWithModelUserMessage(ctx, in.ThreadID, *userMsg, *modelUserMsg)
		}
	}
	// D-01: the cockpit SSE stream surfaces live REASONING_* delta text (showReasoning
	// =true). This is a deliberate operator override of the conservative redacted web
	// default, justified by the whole-origin-private single-operator cockpit (Phase 24
	// D-03): the ONLY viewer is the authenticated operator. This is the live cockpit
	// STREAM, distinct from trace persistence — the reasoning trace still does NOT
	// persist verbatim by default (HARDEN-05 is unchanged). The flip is cockpit-scoped:
	// Telegram has its OWN agui.Translate(…, t.deps.ShowReasoning) call site (a per-
	// channel config-driven flag, agui_subscriber.go) and the programmatic pump uses
	// Subscribe/NewFanout (client.go) — neither flows through handleRun, so neither
	// posture is affected by this true.
	s.streamSSE(ctx, w, Translate(in.ThreadID, runID, s.idgen, turn, true))
}
