# API Coverage — Phase 52 (Mid-turn steering)

No external API integration: this phase adds one **first-party** HTTP route
(`POST /agent/runs/{runID}/steer`) on Aura's own AG-UI mux, one in-process Go package
(`internal/steer`), and a branch in Aura's own Telegram dispatch path. No third-party API, SDK,
service, or vendor client is introduced, wrapped, or widened — the only external protocol already
in the tree (telebot / the Bot API) is consumed through its existing, unchanged seam.

The deterministic detector fires on this phase's scope because the words *endpoint*, *route* and
*integration* appear in the ROADMAP/CONTEXT prose describing an internal HTTP surface. Confirmed by
re-reading the phase scope: every file in `files_modified` across the six plans is Aura-owned Go,
Aura-owned docs, or Aura-owned config. There is no external capability surface to enumerate, so no
matrix row is fabricated.

**What this declaration does NOT cover, stated so it is not read as broader than it is:** the phase
does depend on Aura's *existing* provider path (OpenRouter / llama.cpp) accepting a `user`-role
message at the steer position. That is a message-shape question against an already-integrated
provider, not a new integration, and it is tracked as an explicit flagged assumption in
`52-02-PLAN.md` (`FA-1`) with a live E2E in `52-06-PLAN.md`, not as an un-decided coverage hole.
