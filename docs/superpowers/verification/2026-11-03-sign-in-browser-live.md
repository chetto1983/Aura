# Sign-in browser — 2026-11-03 live acceptance

Status: remaining resource, crash-cleanup and release gates in progress.

## Operator acceptance and retained scope

The appliance acceptance revision is `dd053799714beb60515e9976cd110a2d89ad877a`.
The operator accepted Linear and Notion and confirmed that the requested Canva
trial already works and has been tried (“funziona già provato”). Reuse those
accepted trials; do not infer additional unreported interactions. Saved grants,
38/72 discovered tools and removal of all temporary containers are recorded in
the [existing verification](2026-11-02-sign-in-browser-spike.md).

The operator excluded GitHub and keeps Google's working Web-client flow through
the Cloudflare Google callback. Tasks 8–10 and the Google Desktop/next-day-refresh
trial are withdrawn, not passed. The Google relay remains live.

## Certificate failure in the actual cockpit bundle

Ran `node D:/tmp/signin-spike/task-6-tls-negative-edge.mjs` against the built
cockpit bundle in an isolated headless Edge instance. Browser TLS validation was
enabled (`ignoreHTTPSErrors: false`); the dedicated HTTPS fixture presented an
untrusted certificate and produced a real certificate request failure.

The page displayed the exact dedicated-origin link in a new tab, the retry
button and the own-browser action. No iframe was shown as ready. The run passed
in 2.682 seconds. The fixture used synthetic session/flow IDs and no provider
sign-in. Its parent was HTTP on loopback, a secure browser context; this run does
not establish a new trusted HTTPS-parent certificate installation. Successful
ordinary Edge framing is the separate operator acceptance above. No renewed
certificate-import attempt is used as evidence.

## Resource baseline

At 11:46:45 UTC the lab VM had 15,798,872 KiB RAM, eight CPUs and
14,658,224,128 bytes free on its 61,106,941,952-byte root filesystem (75% used).
It is the available appliance under test; smaller hosts remain unmeasured.

The configured Chromium digest remains
`sha256:b147169cc5ff21c62df977f314598537eecf566591c13ad0726b1e5cbd0d01ca`.
Docker reports 4,661,934,168 image bytes. Its earlier cold pull took 225 seconds;
this already-cached image has not been deleted merely to repeat that pull.
Compressed layer size is 1,166,237,615 bytes, measured from its amd64 manifest.
No minimum-host capacity claim follows from this baseline.

## S6 — native backend and cgroup peaks

The temporary probe called the product's existing `StartSignIn` and `StopSignIn`
APIs with the configured images and limits, real egress floor and the public Canva
landing page. No consent, screenshot or page-content inspection was performed.
The desktop became ready in 3.610 seconds and remained open for five minutes.
An external observer sampled cgroup-v2 `memory.peak`/`memory.current` and
`memory.events` about once per second, retaining each role's high-water mark.

| Role | Peak bytes | Samples | OOM kills |
|---|---:|---:|---:|
| Desktop | 695,791,616 | 285 | 0 |
| Callback sidecar | 20,705,280 | 286 | 0 |
| Egress floor | 5,844,992 | 285 | 0 |

The desktop peak is 663.56 MiB, 43.20% of its 1.5 GiB cap; shared memory was capped
at 1 GiB. Retain the cap for consent-page headroom. Retain the 8 GiB disk preflight:
the measured image plus compressed download totals 5.83 GB before extraction and
runtime headroom. This is a conservative budget, not a measured minimum capable
host or a bound for every provider. Smaller hosts and authenticated-page peak
memory remain unmeasured by this probe; the earlier consent trials are separate.

All three containers disappeared after normal teardown. At 12:08:01 UTC Docker's
filesystem had 14,662,205,440 bytes free (75% used); Aura and Caddy were healthy.

## Native process interruption and sweep

A second temporary process called the same native backend and reached ready in
3.767 seconds. With its three containers running, sent SIGKILL to that probe
process and observed exit 137. A fresh backend invocation called `SweepSignIns`
after checking that no other sign-in session was present. It removed all three
orphan containers and the desktop's anonymous profile volume. Aura and Caddy
remained healthy. This measures the exact cleanup API used at daemon boot, without
killing the appliance daemon or measuring its full restart sequence.

## Shared-origin privacy measurement

An isolated Edge fixture used the unmodified `internal/agui/mcp_sandbox.html`,
its nested `allow-scripts allow-same-origin` view and a synthetic desktop with
the product's sandbox CSP. With both on one origin, the view read a synthetic
private marker from the sibling desktop through the frame tree. The cockpit
was on a different origin. The corrected fixture reproduced this in 2.040 seconds;
its first two attempts had only a parent-script HTML escaping error.

This measures browser same-origin access, not exposure of a person's credentials
or content. It establishes a privacy gap when an enabled MCP Apps origin is also
used for sign-in. The required mitigation is to use the existing own-browser
fallback for that configuration and refuse shared-origin stream requests before
any exec. The default case with no MCP Apps origin remains separate. Verification
of the mitigation is recorded below.

## Privacy mitigation and final review

The reserved Apps origin now selects the existing own-browser flow before image
preparation or admission. Shared-origin stream requests are refused before exec;
owner cancellation remains available. The raw programmable sandbox is served only
on its configured origin. The three regressions were observed RED then GREEN;
scoped vet/build and full race suites passed (`cmd/aura` 68.949 seconds,
`internal/agui` 37.564 seconds), followed by passing post-merge regressions.

The single independent final review found two additional Important cases, both
reproduced before fixing. Both supported Caddy configurations now refuse
`/mcp-sandbox` on the full cockpit frontdoors even when an operator collapses the
configured Apps origin onto the cockpit. A native Caddy routing fixture returned
404 on all six named/LAN/tunnel cockpit cases and 200 on both dedicated Apps
routes. It used the adapted production routes with synthetic upstream content;
TLS and listening ports were replaced only in the test transport.

The callback listener now accepts its exact authority and the native URL-canonical
authority, including omitted default HTTP port 80. The real Docker test covers
IPv4, localhost and IPv6 with nondefault, omitted and explicit default ports,
retaining wrong-state and duplicate-state rejection. All nine variants and the
ChatGPT passkey regression passed in 11.072 seconds. The focused frontend suite
passed 20/20 tests. No image digest changed.

## Local gates and remaining scope

`make quality-full` passed in WSL with the native migrated disposable database,
including race, lint, vet, build, dead-code and vulnerability gates. The owned
internal coverage aggregate was 52,646/59,937 statements (87.8% displayed), above
85%; package-local policy passed. The separate native `coverage_docker.sh` gate
also passed with its own migrated disposable database: 52,638/59,937 statements
(87.8% displayed), package-local policy green. Both disposable databases were
cleaned up. The full affected-package post-fix race suites passed (`cmd/aura`
110.904 seconds, `internal/agui` cached). The final callback/Caddy changes add no
production Go statements; their own Docker and routing tests are recorded above.
Fresh post-merge regressions also passed on shared master: Docker callback,
ChatGPT and frontdoor/start-policy tests in 11.520 seconds, plus the AG-UI privacy
race regressions in 1.207 seconds. The payload gate verified all 35 hashes.
Actual mutants run in CI, as requested by the operator; no local mutation score
or all-green final CI result is claimed before publication.

ElevenLabs retains the existing relay. Its optional direct-loopback trial is
unmeasured, and the Cloudflare site is unchanged. The lab cockpit currently has
no plain-HTTP listener; automated adapter/UI tests cover paste-back policy, which
does not establish a separate human plain-HTTP appliance trial.

A count-only scan of the current Aura container's logs for the S7 interval
11:09–11:22 UTC returned zero retained lines. It cannot certify historical log
contents; the zero secret-pattern counts are not evidence of that acceptance.
No raw log lines were returned to the agent. The three temporary resource-probe
JSON files were removed after the resource observation had been copied locally.
