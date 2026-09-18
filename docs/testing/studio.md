# Studio acceptance

What was measured when the cockpit Studio shipped, on the stack it will run on. Every number
below was read back from the live deployment after the run, not from a test's own assertion.

Measured **2026-09-18** against `aura:acceptance`, built from `0d18964fd`.

## The run

`web/e2e/studio-live.spec.ts`, gated on `AURA_E2E_STUDIO=1` and skipped otherwise, so neither CI
nor a routine local run can fire it. It is `mode: 'serial', retries: 0`: the config's CI retry
cannot re-buy a generation, and a helper refuses more than one record per click.

```
AURA_E2E_ORIGIN=https://localhost AURA_E2E_STUDIO=1 \
  npx playwright test e2e/studio-live.spec.ts --project=chrome --reporter=line
3 passed (1.4m)
```

## What it bought

| | Image | Video |
|---|---|---|
| record | `9778389b-dc2a-4baa-93ec-4636ac8d52ec` | `9a26b64e-756e-4c89-a184-15d1d230c27f` |
| model | `sourceful/riverflow-v2.5-fast` | `google/veo-3.1-lite` |
| status | `completed` | `completed` |
| asset | `f011ff2c-62a3-4aa3-9119-b85a86a208b5` | `07257b31-7390-428d-9ba4-6140a8cc9d79` |
| stored as | `generated.webp`, `image/webp`, 61 994 B | `generated.mp4`, `video/mp4`, 3 408 236 B |
| estimate | the catalog's `image_min_usd` | `≈ $0.12` |
| **billed** | **$0.019095** | **$0.118800** |

Total **$0.137895**, against the $0.17 the operator approved.

The video's billed cost lands within a fifth of a cent of the estimate the composer showed before
the click, which is the number the operator decides on. The image cost less than the plan's
working figure of $0.05 because the cheapest *priced* image row the catalog declared is cheaper
than the one the plan guessed at; the spec reads the model from the catalog rather than naming one.

## What the rows prove

Both `aura.media_job` rows carry `surface = 'studio'`, an empty `conversation_id` and an empty
`tool_call_id`, and both are `delivered_at`-stamped. Both assets carry an empty `thread_id`.

`aura.conversations` held 8 rows before the run and 8 after — the Studio generates outside every
conversation, which is the property the surface exists for, and the spec asserts it independently
by reading `GET /api/conversations` on both sides of the run.

## Gates at the same commit

| Gate | Result |
|---|---|
| `make quality` (vet, file-size, lint+dupl, deadcode, test-race, govulncheck, build) | passed, 0 vulnerabilities |
| `scripts/coverage_docker.sh`, disposable `aura_cov` only | owned-source 41 763/47 453 = **88.0%** ≥ 85%, package policy passed |
| web: typecheck, lint, format, vitest, dup, deadcode, contrast | 279 files / 2 435 tests, 0 clones, 102/102 AA |
| migration head on the deployed stack | **129** |

`internal/assets`' pinned baseline moved from 757/932 to 769/944 (81.22% → 81.46%): the Studio's
input path added statements, the fail-closed policy stopped the gate, and the new ratio was
recorded rather than the bar lowered.

## What this run does NOT prove

- **One model per kind.** `sourceful/riverflow-v2.5-fast` and `google/veo-3.1-lite` were exercised.
  Nothing here says another model's declared options, frame support or SKU naming behave the same.
- **The SKU naming beyond veo-lite.** `VideoSecondPrice`'s precedence was checked against the
  costs measured for `google/veo-3.1-lite` on 2026-09-17. A model that names its per-second SKUs
  differently reports no price rather than a wrong one, but that branch was not exercised live.
- **Seed determinism.** The seed is passed through and recorded; that the same seed reproduces the
  same clip was never tested, and the provider does not promise it.
- **The end frame.** `last_frame_asset_id` is refused correctly without a start frame and on a
  model that declares no end frame, both in unit tests. No paid run supplied one.
- **A second tab.** The double-submit guard is per page instance, so two tabs can each submit once.
  Only the server could prevent that, and it does not today.
- **Failure paths.** Every refusal code maps to a status and a sentence in both locales under test,
  but no live generation was made to fail: `no_credit`, `content_blocked` and `outcome_unknown`
  have never been seen from a real provider here.
