---
phase: 02-two-roles-and-a-budget
plan: 10
subsystem: verification
tags: [rbac, live-run, deprovisioning, authula, playwright, e2e]

# Dependency graph
requires:
  - phase: 02-two-roles-and-a-budget
    provides: "Plans 02-01..02-09: the capability declarations and the uniform provisioning grant, the capability-denial ledger and its audit projection, the credit cap read/write, the removal route, and the roster the cockpit drives"
provides:
  - "web/e2e/two-role-live.spec.ts — the closing two-role witness: a member writes a skill, runs a shell command in its own box, is refused identity.create and identity.delete, and reads both refusals out of its own audit feed"
  - "web/e2e/identities.ts — the shared cockpit-driving helpers (create, credit, sign in as, remove an identity) both live multi-identity specs now use"
  - ".planning/phases/02-two-roles-and-a-budget/02-LIVE-RUN-EVIDENCE.md — what the run measured, plane by plane, and what it does not demonstrate"
  - "agui.Deprovisioner.SetAuthulaTeardown + a memoized buildDeprovisioner — the daemon's removal route and its grace-window sweep now share one saga whose Authula legs arrive after boot"
affects: []

# Actuals
actuals:
  tasks: 3
  commits: 1

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "A closing live run is worth its cost only if it observes the planes it claims: the plane-by-plane before/after is what found an Authula account outliving every other teardown."
    - "A nil port that means 'this plane is not wired' silently becomes 'this plane is not torn down' when the composition root builds the saga before the provider exists. The wiring arrives after construction, on one memoized instance, rather than being assumed at build time."
---

# Plan 02-10 — Summary

The closing witness for Two Roles and a Budget, driven through the cockpit against the
installed appliance rather than as a Go live test: the standing rule is that end-to-end proof
goes through the UI, and a Go test would have reached the same routes by another door.

**What it proved live.** A member holds exactly the four provisioned capabilities and uses
them — it wrote a skill and ran a shell command in its own sandbox — while both administrative
capabilities are refused at the route and both refusals come back out of the member's own audit
feed. An identity created and removed through the cockpit leaves nothing behind on Postgres,
ArcadeDB, Garage, the sandbox or OpenRouter.

**What it found.** It left its Authula account behind, and so had every identity removed from
the cockpit before it — six accounts. `aura serve` builds the de-provisioning saga before the
Authula provider exists, so both Authula legs were nil and the saga's nil-skip made the gap
silent; only `aura identity purge` wired them. Fixed here: the legs are attached after
construction to one memoized saga, so the removal route and the grace-window sweep both carry
them. Covered by three unit tests plus one that pins the shared instance.

**What was deliberately not done.** The mutation gate was not extended with the two new scopes
(operator's call: the gate is green and scores one file per scope, so this was box-ticking
rather than a defect). A member installing an MCP server and approving a destructive tool call
were not driven live — both mutate a deployment in daily use beyond what the run cleans up.
Both exclusions, and everything else the run does not demonstrate, are recorded in
02-LIVE-RUN-EVIDENCE.md.
