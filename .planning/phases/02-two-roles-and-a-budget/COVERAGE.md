# API Coverage — OpenRouter (Provisioning API + inference)

> Full coverage by default. Opt-outs are explicit, reasoned decisions.
> Surface enumerated from `02-OPENROUTER-API.md`, which was written against the live account
> on 2026-09-08 — not from the vendor's landing page.

| capability | decision | reason |
|---|---|---|
| `POST /api/v1/keys` (mint a key) | INTEGRATE | |
| `GET /api/v1/keys/{hash}` (read one key) | INTEGRATE | |
| `PATCH /api/v1/keys/{hash}` (set limit + reset interval) | INTEGRATE | |
| `DELETE /api/v1/keys/{hash}` (revoke) | INTEGRATE | |
| `POST /api/v1/chat/completions` (inference under an identity's key) | INTEGRATE | |
| `GET /api/v1/keys` (the whole roster in one call) | INTEGRATE | **Reclassified 2026-09-08 by `/gsd-plan-phase 2`** — was OPT-OUT ("no roster sweep is required to address a key"), which is still true for *addressing* a key. It is not true for the surface the approved `02-UI-SPEC.md` §Admin spend dashboard — Overview requires: the **Top Identities by spend** ranked list is sourced field-by-field from this call (`data.external_user` joins the row to an Aura identity, `data.label` is the masked display string, `data.usage` is lifetime spend). Aura's own store carries the hash and the cap but has no lifetime-spend column, so the ranked list is not derivable from it. Integrated **server-side only**, behind `GET /api/admin/spend/overview` — plan `02-09` |
| `GET /api/v1/key` (what a key says about itself) | OPT-OUT | not needed — its usage counter lags a spend by 30–40s (measured 2026-09-08); CRED-06 requires the displayed figure come from Aura's own in-band ledger instead. **Still opted out after the 2026-09-08 reclassification below**: the Overview's reconciliation tier reads the *management* side (`GET /keys`, `/credits`, `/analytics/query`), never a per-identity inference key's self-report, so nothing in the phase needs this endpoint |
| `GET /api/v1/credits` (the account pool) | INTEGRATE | **Reclassified 2026-09-08 by `/gsd-plan-phase 2`** — was OPT-OUT ("no Phase 2 requirement reads it"). `02-UI-SPEC.md`'s **over-allocation advisory banner** is exactly a requirement that reads it: its trigger is `Σ(cap across identities) > total_credits − total_usage` (M-12 — OpenRouter does not prevent over-allocation, so the operator is the only control). The banner cannot exist without this call. Integrated **server-side only**, in the same `GET /api/admin/spend/overview` response — plan `02-09` |
| `POST /api/v1/analytics/query` (per-identity spend history) | INTEGRATE | **Reclassified 2026-09-08 by `/gsd-plan-phase 2`** — was OPT-OUT ("not needed yet"). The approved `02-UI-SPEC.md` §KPI row specifies five tiles (`total_usage`, `request_count`, `tokens_total`, `cache_hit_rate`, `blended_cost_per_million_tokens`), their day-granularity sparklines and their prior-window deltas, all from this one endpoint. **The lag argument is unchanged and still binding**: this is reconciliation tier only (D-08), and CRED-05's pre-flight refusal and CRED-06's cap/remaining/spend figures still come from Aura's in-band ledger (`aura.cache_metrics.cost_usd`), never from here. Integrated **server-side only** — plan `02-09` |
| `GET\|POST /workspaces` | OPT-OUT | explicitly out of scope — a second ceiling above the per-key one; recorded in `02-OPENROUTER-API.md` so a later phase starts from the surface rather than a search |
| `GET\|PUT\|DELETE /workspaces/{id}/budgets/{interval}` | OPT-OUT | explicitly out of scope — same as `/workspaces`. Note the documented constraint for whoever picks it up: limits must strictly decrease as the interval narrows |
| `GET\|POST /guardrails` | OPT-OUT | explicitly out of scope — policy guardrails are a different control from a spend cap; no Phase 2 requirement asks for them |
| `/guardrails/{id}/assignments/keys` | OPT-OUT | explicitly out of scope — depends on `/guardrails`, opted out above |
| `external.api_key` (partner-supplied key material) | OPT-OUT | not needed — every identity's key is minted by Aura. Whether partner-supplied material behaves differently with respect to caps is recorded as unmeasured in `02-OPENROUTER-API.md`; opting out avoids depending on an unmeasured behaviour |

## Notes on the INTEGRATE rows

- **Credential kinds are not interchangeable.** A *management* key calls `/keys`, `/analytics/*`,
  `/workspaces`, `/credits` and cannot call completion endpoints; an *inference* key calls
  `/chat/completions` and its own `/key` record and cannot call `/keys`. The five INTEGRATE rows
  therefore span two distinct credentials, and the plan must not treat them as one.
- **`DELETE` is verified, not assumed** (CRED-08): the measured contract is `DELETE` →
  `{"deleted": true}`, then `GET /keys/{hash}` → 404, with inference returning 401 within ~5s.
  That is why `GET /keys/{hash}` is INTEGRATE rather than opted out alongside the roster call.
- **`PATCH` latency is asymmetric** (measured 2026-09-08): lowering a limit denies within ~5s;
  raising one on an exhausted key takes ~25s before inference recovers. The cockpit states this
  rather than appearing broken.
- **The three reconciliation rows are server-side only.** `GET /keys`, `GET /credits` and
  `POST /analytics/query` all require the **management** credential, which can mint and revoke
  keys account-wide. It never leaves the daemon: the browser calls
  `GET /api/admin/spend/overview` (gated by `RequireCapability(governance.write)`) and the
  daemon makes the three provider calls with the management key. A browser-reachable route
  that accepts a provider key, echoes one, or proxies an arbitrary provider path is out of
  bounds — threat `T-02-11` in plan `02-09`'s `<threat_model>`.

## Reclassification log

| date | capability | from → to | who | why |
|---|---|---|---|---|
| 2026-09-08 | `GET /api/v1/keys` | OPT-OUT → INTEGRATE | `/gsd-plan-phase 2` | Top Identities by spend list (`02-UI-SPEC.md` §How per-identity fits item 3) needs `external_user`/`label`/`usage`; not derivable from Aura's own store |
| 2026-09-08 | `GET /api/v1/credits` | OPT-OUT → INTEGRATE | `/gsd-plan-phase 2` | over-allocation advisory banner's trigger is `Σ(cap) > total_credits − total_usage` (M-12) |
| 2026-09-08 | `POST /api/v1/analytics/query` | OPT-OUT → INTEGRATE | `/gsd-plan-phase 2` | the five Overview KPI tiles, their sparklines and their prior-window deltas (`02-UI-SPEC.md` §KPI row) |

All three were opted out before the UI-SPEC was revised (post-approval, at the human's
direction) to add §Admin spend dashboard — Overview. The revision made them load-bearing.
Nothing else moved: the seven remaining OPT-OUT rows keep their original reasons, and the
in-band-ledger rule that motivated two of the three original opt-outs (D-08, M-07) is
**unchanged and still enforced** — the reclassification widens the *reconciliation* tier only.
