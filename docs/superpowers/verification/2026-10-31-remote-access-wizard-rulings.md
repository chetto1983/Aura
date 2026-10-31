# Remote access wizard: rulings and deferred minors — 2026-10-31

Every decision taken on the operator's behalf while executing
`docs/superpowers/plans/2026-10-27-remote-access-wizard.md`, copied from the execution ledger, which
is scratch and goes away when the plan closes. Each ruling states its cost if wrong. A later ruling
can supersede an earlier one: the ledger's order is kept, so the last word on a subject is the
lowest line. 49 rulings, 52 deferred minors.

## Rulings

- Task 1: Ruling: promote review Minor 1 (stateFromRow swallows the `abandoned` unmarshal error) into fix round 1 — CLAUDE.md "errors never pass silently" binds this project and a mis-shaped value would erase the abandoned list on the next Advance — cost if wrong: a few lines.
- Task 1: Ruling: ⚠️ "interim exposure" (label change with resources reaches access/DNS with old IDs until Task 4 consumes owned_hostname) — nothing is pushed between Task 1 and Task 4; push only at Task 10 — cost if wrong: none while unpushed.
- Task 2: Ruling: C1 enters the fix round although client.go is outside the brief's files — spec decision 7 "VERDE DA SOLO" is the binding authority — cost if wrong: a retry classification change in cloudflareapi.
- Task 2: Ruling: I6 — domain validation returns a new sentinel ErrDomainInvalid (wrapping ErrConfiguration so existing errors.Is checks hold) → domain_invalid; any other ErrConfiguration → unknown — spec anti-goal "generic errors" and "every failure names its cause"; the plan's table was too coarse — cost if wrong: one sentinel.
- Task 2: Ruling: I7 — implementer reads Cloudflare's tunnel status documentation; if "degraded" means still serving traffic, it counts as reachable, cited in a comment — spec call "reachable means what Cloudflare reports for the tunnel's connections" — cost if wrong: a degraded tunnel shows green.
- Task 3: Ruling: a foreign DNS record already on the hostname returns ErrHostnameTaken (stored-ID branch stays ErrOwnershipConflict) — the operator's fix is on the Dominio step, not_owned sends them to Collegamento — cost if wrong: one sentinel and one test.
- Task 3: Ruling: RW-008 = one retirement call in Reconcile after the backoff check, plus the one in deleteResources; a failure on an error or disabled install is logged and leaves phase and reason alone — one obvious place, and the boot Wake guarantees a pass — cost if wrong: a retry waits for the next wake.
- Task 3: Ruling: RW-009 = per-step denial handling inside runDeletions; on the first 401/403 one ListAccounts decides: visible → abandon that resource and go on, not visible → stop retiring this pass, keep every ID, return nil — retirement runs before VerifyToken, so "sees the account" must be asked lazily — cost if wrong: one extra ListAccounts per denied pass.
- Task 3: Ruling: Advance stops writing abandoned; AddAbandoned and Abandon append through SQL — a pass now writes the list, so a dismiss must be safe from a pass in flight (closes the Task 1 minor) — cost if wrong: one query.
- Task 3: Ruling: keep "730h" — Cloudflare documents up to one month, no policy sets session_duration so nothing overrides it, third-party reports show the API accepts it (unverified here); spec acceptance item 4 reads it back live — cost if wrong: a 400 puts every install in error until the constant changes, caught at Task 11.
- Task 3: Ruling: review Minors 3, 4, 6, 7 enter the fix round — CLAUDE.md deep refactor on touch, and 7 is Review Focus 3's literal install — cost if wrong: a slightly larger diff.
- Ruling: RW-012 = no new durable intent; a retirement failure on a disabled or error install never reaches failed, so the state SaveDesired reads as deletion intent is never written — simpler than a new column — cost if wrong: a new column later.
- Ruling: RW-013 = the WARP app and policy are deleted only once the CNAME is gone; a denied CNAME abandons all three together — Cloudflare leaves a route without an Access app open — cost if wrong: a leftover the operator deletes by hand.
- Ruling: RW-014 = reconcile projects off whenever tunnel_id is empty, before validateDesired — daemon-side, derived from the row — cost if wrong: one idempotent file write per pass.
- Ruling: RW-017 = Apply failures wrap ErrProjection, transient, reason unknown; no change to Reconcile's early returns because the off projection runs first — a local file failure must not be terminal or blame Cloudflare — cost if wrong: a retry loop on a broken volume, visible as degraded.
- Ruling: RW-018 = remoteAccessDelay returns 5 minutes for disabled/error rows while LegacyWARPHostname is set — the row already holds the signal — cost if wrong: one Cloudflare read every 5 minutes on a stuck install.
- Task 4: Ruling: retireWARP persists the cleared LegacyWARPHostname once it is a column — otherwise RW-018's 5-minute tick never stops — cost if wrong: one write.
- Plan: Ruling: the plan's Decisions line 'warp_label is read only by retireWARP' goes stale at Task 4 (only the 0147 backfill reads it); left as is so the approved plan's SHA stays valid, corrected with the Status lines at Task 11 — cost if wrong: one stale line until then.
- Task 3: Ruling: what depends on a retained resource stays with it — a denied CNAME or policy also lists the app and the posture; this supersedes "the posture stays independent" (RW-013) because a retained policy still requires it and deleting it is unmeasured, while a non-401/403 refusal would block the public address — cost if wrong: one manual delete for the operator. Plan text keeps the old wording so the approved SHA stays valid; corrected with the Status lines at Task 11.
- Task 4: Ruling: review Minors (backfill columns and ELSE branch, fixture leak, OwnedZoneName survival, stale comment) enter the fix round — Task 5 reads these fields from the shared row — cost if wrong: a slightly larger diff.
- Task 5: Ruling: teardown proves it can finish before its first destructive step: accountVisible when resources exist, then the read-only preflight of the public set — a foreign resource or a blind token must leave the old address serving; closes review Minors 3 and 7 — cost if wrong: one ListAccounts and a few reads per teardown.
- Task 7: Ruling: UpsertInTx stays; the lost enable on a deleting row is accepted as SaveDesired's documented rule (Task 9's wizard must show the disabled state) — cost if wrong: the operator saves twice.
- Task 8: Ruling: namesMu accepted — run() holds c.mu across Reconcile, so Status must not wait on it — cost if wrong: a cosmetic stale name.
- Task 8: Ruling: Configure refuses an empty zone when the row has one, with ErrDomainInvalid; disable and delete are the ways to turn off or remove — the empty-domain save exists only for a first setup — cost if wrong: a client must send the address.
- Task 8: Ruling: ErrStaleGeneration sends reason empty, overriding ReasonOf(err); ErrConfiguration stays 400 — a stale save failed nothing, and a wrong delete hostname is not a Cloudflare failure — cost if wrong: one mapping.
- Task 8: Ruling: deleteResources clears AcceptedHostname; teardown and Disable keep it — after Elimina nothing exists, the app and tunnel are new, and accepted true with no tunnel is false; closes the Task 2 minor — cost if wrong: one extra phone test after a delete.
- Task 8: Ruling: no re-review of round 2 — three production lines read by the controller, RED then GREEN shown for both behaviours — cost if wrong: caught by the final whole-branch review.
- Task 10: Ruling: the implementer writes the suite but does not run it; the controller runs it through a tunnel to the lab VM, because it signs in with the operator's account and those credentials stay out of a bypass-permissions agent; the VM is not changed — cost if wrong: one extra fix round.
- Task 9: Ruling: the status gains has_resources and Elimina follows it — offering a delete on a row with nothing to delete would be a false statement, and acceptance is the wrong proxy — cost if wrong: one more status field.
- Task 9: Ruling: pressing Riprova clears the backoff, the word stays — a press that visibly does nothing is the operator's complaint — cost if wrong: extra Cloudflare calls bounded by a human hand.
- Task 9: Ruling: a replaced token walks through Collegamento and returns to green alone — the spec says it reaches green again, and acceptance survives — cost if wrong: a spinner the operator may question.
- Task 9: Ruling: failureOf keeps the reason of a 409 that has one, against the letter of 409 first — Delete's not_owned would otherwise read as a stale page — cost if wrong: one sentence.
- Task 9: Ruling: the implementer's five additions stand (hasExit, Rileggi on Collegamento, abandoned brings Cloudflare forward, StepStrip current, Passo) — the reviewer found each needed — cost if wrong: scope the operator did not ask for.
- Task 9: Ruling: commit trailers stay Opus 5.5 for opus sessions — that is the model that did the work; global-constraints predates the dispatch — cost if wrong: none.
- Task 9: Ruling: re-graded to fix now from the reviewer's Later list: domain draft lost on Indietro, green without acceptance, raw i18n key, nameservers sentence over an empty list, locked select, Passaggio, colour-only done dot, connection.running — walking back and failures are the operator's complaint — cost if wrong: one larger fix round.
- Task 9: Ruling: the token's permission list is always visible, the disclosure goes — a closed details with no marker reads as an empty bar on the step that needs it — cost if wrong: a longer card on a phone.
- Task 9: Ruling: waiting_nameservers offers Disattiva and Elimina — it can last days and a wrong domain parked there is the dead end measured on 2026-09-21 — cost if wrong: an exit the operator does not use.
- Task 9: Ruling: the domain draft survives only while the status has no saved address — walking must never change the address — cost if wrong: an unsaved edit on a configured row is lost by walking away.
- Task 11: Ruling: the stale WARP operator docs join Task 11's records, with the plan's two stale lines: docs/cloudflare-remote-access.md (WARP section, the Zero Trust permission row, CLOUDFLARE_TEAM_NAME), docs/cloudflare-remote-access-contract.md, docs/INSTALL.md:77, docs/asset-pipeline.md:16 — no task covers them and a guide telling the operator to install WARP contradicts the plan's goal — cost if wrong: one docs commit.
- Task 11: Ruling: rebuild and commit internal/webui/dist before the push (npm run build's three steps from Windows, per the repo's build(web) convention): the Docker image already rebuilds the SPA, so the VM gets the wizard either way, but the committed embed still carries the old panel — cost if wrong: one large generated commit.
- Task 10: Ruling: the live inventory drops devices/posture — Aura creates no posture rule any more and the recommended token lacks the Zero Trust read, so the attended run would stop on a 403 before testing the product; teamnet/routes stays, Cloudflare documents Tunnel Read and Write for it — cost if wrong: a leftover posture rule goes unseen on a run that cannot create one.
- Task 10: Ruling: no re-review of fix round 1 — 37 lines, the mocked half was re-run for real and the controller read the helper and fixed its race in 3b4f91b25 — cost if wrong: caught by the final whole-branch review.
- Final: Ruling: settled covers connecting — a pass wrote provisioning every ten seconds over the phone step, which is the deliverable — cost if wrong: a connecting row hides a pass in progress.
- Final: Ruling: a Conferma on a tunnel not yet serving answers tunnel_down with its own sentence, and pressing Conferma again is the way on — the row's tunnel_down sentence promises no new test, false for an unaccepted row — cost if wrong: one sentence.
- Final: Ruling: the zone lookup runs before teardown whenever the account or the zone name changes, wider than the review's account-only fix — a move that cannot finish must not stop a working address — cost if wrong: one extra ListZones on a move.
- Final: Ruling: an account change clears the acceptance, reversing my Task 8 ruling and narrowing spec decision 7's only a new domain or label, whose sentence was about a new token — Access, OTP and tunnel are all new in another organisation, and keeping green would claim a test nobody took — cost if wrong: one email PIN on a rare event; the operator can reverse it. Spec line corrected at Task 11.
- Final: Ruling: waiting_nameservers polls every 30 seconds, not 2 — the phase can last days and each poll calls GetZone — cost if wrong: the wizard moves on up to 30 seconds late.
- Final: Ruling: declined-to-judge lines all stand: the pre-existing four writes per pass, the CreateTunnel crash window, rollback needing down migrations, status reads needing governance.write, /events under c.mu, the token keys and 730h carried to Task 11, parts 2 and 3, the stale SPA and docs at Task 11, gates not runnable by a reader — none changes what a person gets beyond what Task 11 measures.
- Task 11: Ruling: findings 1 to 5 fixed in the run instead of deferred — the plan's goal is that every failure names its reason, and the live run disproved that — cost if wrong: one more CI and one more updater cycle. Ruling: two reasons, tunnel_permission and access_unavailable, step cloudflare, action retry — access_unavailable covers Zero Trust off and missing Access permissions because the API does not tell them apart — cost if wrong: the sentence names two causes. Ruling: no activation_check — activation came alone inside Cloudflare's stated range — cost if wrong: a slower activation on another account. Task 11 minor (deferred): a tab on an error row does not poll.

## Deferred minors

- Task 1: minor (deferred): delete.go LegacyWARPHostname="" exists only for the in-memory fake; fake fixture sets LegacyWARPHostname with no WARP IDs (moot after Task 3 removes the WARP leg).
- Task 1: minor (deferred): DismissAbandoned has no generation CAS and Advance writes `abandoned` back, so an in-flight reconcile can resurrect a dismissed list — carry to Task 6/7.
- Task 1: minor (deferred): TestAbandonClearsResourcesAndListsThem never seeds the warp_* IDs.
- Task 1: minor (deferred): TestSaveDesiredKeepsAcceptanceOnlyForTheSameHostname changes only the label, not the zone.
- Task 1: minor (deferred): no test ties the Go Reason constants to the SQL CHECK.
- Task 1: minor (deferred): agui remote_access_api.go still decodes warp_label nobody reads — Task 7 removes it.
- Task 2: minor (deferred): no reconcile-level test with a stale AcceptedHostname (old.example.com must stay connecting).
- Task 2: minor (deferred): acceptance_test.go:58 asserts the fake (memoryState.SaveDesired never touches the field).
- Task 2: minor (deferred): acceptance_test.go:125 rejection loop checks only ObservedHealthy, not AcceptedHostname == "".
- Task 2: minor (deferred): outage test lost the restart check (h.newReconciler()); spec decision 7 names a box restart.
- Task 2: minor (deferred): Delete keeps AcceptedHostname, so re-adding the same address after Elimina skips the phone test — product decision for the final review.
- Task 2: minor (deferred): AcceptExternal (acceptance.go:27-29) and SyncMembers (members.go:128-132, no prod caller) can write connecting on an accepted row (Review Focus 1).
- Task 2: minor (deferred): reconciler.go:40 Option comment still mentions independently measured readiness.
- Task 2: minor (deferred): reconciler.go:177-180 set-then-override healthy is harder to read than a switch.
- Task 2: minor (deferred): serve_remote_access.go:180 second copy of the hostname concatenation — Task 7 folds both (127, 180).
- Task 2: minor (deferred): acceptance.go:27 AcceptExternal still requires tunnel.Status == "healthy" while the reconciler treats degraded as serving (tunnelServes) — align it.
- Task 3: minor (deferred): warp_retire.go:44-58 listed/ids slices are tied to steps by position, nothing asserts four steps.
- Task 3: minor (deferred): a denied confirm read after a successful remove lists a resource that is probably gone (safe, dismissable).
- Task 5: minor (deferred): owned set reaches the row with ZoneID after CreateZone; no effect, zones are never deleted and ListZones finds a lost ID.
- Task 5: minor (deferred): the real-store clear of the tunnel credential is verified by reading only (sealSecret returns empty early).
- Task 5: minor (deferred): a row holding only ZoneID and OTPProviderID with an invisible account cannot be Deleted (ErrAccountNotVisible, recoverable once Task 7's abandon fires on every Configure); no test pins that shape with a visible account.
- Task 7: minor (deferred): failed-commit proof fails on Abandon's compare-and-swap only; crash-window duplicate in AbandonedOf.
- Task 7: minor (deferred): the disabled-row projection Warn has no rate limit, one line per second on a persistent failure; the deleting test's fixture has an empty Reason.
- Task 8: minor (deferred): TestDisableKeepsTheAcceptance and the two after-Delete tests passed at once with no mutation.
- Task 9: minor (deferred): verify race after editing the token (server re-verifies).
- Task 9: minor (deferred): nothing ties the TS reason list to model.go or the SQL CHECK.
- Task 9: minor (deferred): domain_invalid caused by the label marks the domain field.
- Task 9: minor (deferred): a failed Conferma sticks until pressed again.
- Task 9: minor (deferred): one-pass token_rejected shown after a good token on a failed deletion.
- Task 9: minor (deferred): the compact strip hides the overview on an iPad in portrait: the operator's look at Task 11.
- Task 9: minor (deferred): a save accepted then a failed GET shows the stale card until the next refresh.
- Task 9: minor (deferred): after Riprova ora the screen on degraded can lag by up to 30 seconds.
- Task 9: minor (deferred): the focus effect can scroll a reader when a poll changes Disattiva or Elimina with nothing focused.
- Task 9: minor (deferred): the delete dialog does not say a pending zone stays on Cloudflare after Elimina from waiting_nameservers.
- Task 9: minor (deferred): an unsaved edit on a configured row is dropped silently by walking away, as ruled.
- Task 10: minor (deferred): refresh-token wiring, disable and re-enable, and the delete dialog have no browser coverage; the brief's four titles do not include them.
- Task 10: minor (deferred): the live suite is unrun until Task 11; expectStillAccepted and the inventory change are read, not executed.
- Final: minor (deferred): a home internet outage reddens Collegamento, not the phone step; measured at Task 11.
- Final: minor (deferred): tunnel_down passes write four Cloudflare resources every ten seconds; calls per minute measured at Task 11.
- Final: minor (deferred): a failing token with a cache miss stalls Status three seconds and logs per read.
- Final: minor (deferred): a zone deleted and re-added lands on terminal unknown.
- Final: minor (deferred): account_not_visible on the row with the account select locked until a token is verified.
- Final: minor (deferred): rows mid-outage at upgrade lose their acceptance once.
- Final: minor (deferred): a database error inside a pass is classified terminal, as before.
- Final: minor (deferred): a move to a pending zone tears down the working address before the new one activates, as the plan says; the operator should know.
- Final: minor (deferred): a resumed pass can refuse a half-done move, an error row with neither address; recovery by reading is saving the old address back or Elimina.
- Final: minor (deferred): stale comment at steps_zone_tunnel.go:96-100 on what a failed lookup costs.
- Final: minor (deferred): on a refused domain move the Elimina dialog asks for the address that never served and deletes the one that does; nothing on screen shows the old address.
- Final: minor (deferred): a refused account move says use another domain, while the way back is the account.
- Final: minor (deferred): an unread nameserver list lingers up to 30 seconds.
- Final: minor (deferred): a leftover pending zone in the new account passes the guard.
- Final: minor (deferred): a refused row serving the old address gets no Access policy update while it stays parked.
