# F-1, what the tunnel sidecar can reach: implementation plan

**Status:** v1, 2026-11-26. Not started, not reviewed. Task V1 measures on the lab VM before any code;
the remedy (Task D) is the operator's choice after V1.

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans, task by task. V1 and V4
> run on the lab VM; T1-T3 run in Aura on `master`. Steps use checkbox syntax.

**Goal:** From the `aura-cloudflared` container only `caddy:8080` and the internet are reachable, on every
installation, and stay so after a Docker restart and a host reboot (spec
`2026-10-27-remote-access-from-phone-design.md`, decision 8 and acceptance 7). `verifyTunnelNetwork()`
proves it unweakened.

**Architecture:**
- **Two paths, not one.** The 2026-09-21 review (§F-1) measured the direct path: a container on
  `aura-tunnel` reached the container IP of every published port. The 2026-11-26 local measurement
  (`verification/2026-11-26-f1-tunnel-reach-local.md`) found that path closed by Docker 29.9.3 itself, a
  `raw` `PREROUTING` drop for traffic to a container from any other interface, and found a second path open:
  the tunnel bridge's gateway, at any port published on all host addresses. On the appliance that is
  `caddy`'s `0.0.0.0:443`, `postgres` when `POSTGRES_HOST` is not loopback, and any host service listening on
  all addresses, which the host's `INPUT` chain decides and Docker does not.
- **The direct path is the engine's,** so the installer and the updater refuse an engine without the
  protection (T1).
- **The host path needs a rule Aura owns** (T3, after D).
- **The live check probes both paths** (T2).

**Tech Stack:** bash (`scripts/install.sh`, `deploy/aura-image-update.sh` and their `*_test.sh`), Compose
(`compose.yaml`), TypeScript Playwright (`web/e2e/support/remoteAccessLive.ts`), Docker Engine 29.x with the
iptables backend.

**Spec:** `docs/superpowers/specs/2026-10-27-remote-access-from-phone-design.md` (decision 8, acceptance 7).
Finding: `docs/superpowers/verification/2026-09-21-cloudflare-remote-access-review.md` §F-1. Docker:
https://docs.docker.com/engine/network/port-publishing/ (gateway modes, direct routing),
https://docs.docker.com/engine/network/firewall-iptables/ (`DOCKER-USER`, the `raw` drop),
https://docs.docker.com/engine/network/firewall-nftables/ (experimental in 29.x, no `DOCKER-USER`).

## Global Constraints

- **Measure on the VM before writing a rule.** The two measurements so far disagree, and neither ran on the
  appliance (Docker Desktop on 2026-09-21, a cloud container on 2026-11-26).
- **Nothing narrows what the sidecar needs:** `caddy:8080` on its own network, DNS through Docker's
  resolver, and the internet for the tunnel's edge connections.
- **No weakened assertion.** `verifyTunnelNetwork()` only gains probes.
- **No unpublishing of the data-plane ports.** The operator and the local tooling reach the stack through them
  (review §F-1, remedy (a), rejected there as a large appliance change). Their `127.0.0.1` binds are already
  out of the tunnel's reach on the 2026-11-26 measurement.
- **Release through the updater.** The VM is changed only by an update, as acceptance 7 says.
- **Commits.** One atomic commit per task, ending with the project's `Co-Authored-By` trailer.

## Review Focus

1. **A rule that blocks cloudflared's own needs.** DNS (`127.0.0.11`), `caddy:8080` and the edge must still
   answer; T2 probes all three positively.
2. **A rule that does not survive.** Docker rewrites its own chains on restart; a host rule is lost on reboot
   unless something reinstalls it. Acceptance 7 restarts both.
3. **An interface name that moves.** A bridge is named `br-<network id>` and the id changes when the network is
   recreated. A rule that names the interface must name one Aura fixes.
4. **The engine check.** An older engine without the `raw` drop must fail the install and the update, not warn.

## File structure

| File | Responsibility |
|---|---|
| `scripts/install.sh`, `scripts/install_preflight_test.sh` | the engine floor at install |
| `deploy/aura-image-update.sh`, `scripts/aura_image_update_test.sh` | the engine floor before an update applies |
| `web/e2e/support/remoteAccessLive.ts` | `verifyTunnelNetwork()`: the gateway probes and the positive ones |
| `compose.yaml` | `aura-tunnel`'s fixed bridge name, or the sidecar's floor (Task D) |
| `docs/superpowers/verification/<date>-f1-*.md` | V1 and V4 |

---

### Task V1: Measure on the lab VM

- [ ] **Step 1: The engine.** `docker version`, `docker info` (`Firewall Backend`), `/etc/docker/daemon.json`.
- [ ] **Step 2: The direct path.** The review's probe block, from a throwaway container on the appliance's
  `aura-tunnel`, against the container IP of every published service (`docker compose ps`, then
  `docker inspect`). Read `iptables -t raw -S PREROUTING` for each of those IPs.
- [ ] **Step 3: The host path.** From the same network: its gateway at every published port
  (`docker compose port`), and at every port the host listens on (`ss -ltn`).
- [ ] **Step 4: What the sidecar needs.** `caddy:8080`, a name through `127.0.0.11`, and the tunnel's edge
  (`nc -z -w 5` to the address `cloudflared` logs on connect).
- [ ] **Step 5: Persistence.** Repeat steps 2-4 after `systemctl restart docker` and after a reboot.
- [ ] **Step 6: Record** `docs/superpowers/verification/<date>-f1-tunnel-reach-vm.md`: counts and
  reachable/blocked, no address beyond the appliance's private ones, and what it does not show.

### Task D: The operator chooses the remedy for the host path

Decided after V1 and recorded in the spec with V1's file as evidence. The options, each to be measured before
it is chosen:

- **(a) A host rule.** A drop in the host's `INPUT` chain for packets from the tunnel bridge, except replies,
  with `aura-tunnel` given a fixed bridge name (`com.docker.network.bridge.name`) so the rule can name it.
  Installed by `install.sh` and reinstalled at boot. Host state outside Compose.
- **(b) A floor in the sidecar's own namespace.** An nft ruleset applied inside the cloudflared container's
  network namespace by a short-lived helper that shares it, dropping private, link-local and gateway
  addresses except `caddy:8080` and the resolver. It is the pattern `docker/aura-egress` already applies to
  the user boxes (`internal/sandbox/usersandbox/egress.go`), so it is reuse, not a new mechanism. It lives in
  Compose and is rebuilt with the container, so a reboot cannot lose it.
- **(c) Nothing on the host path,** if V1 finds no host service and no published port reachable from the
  tunnel. Then T3 is dropped and the plan closes on T1, T2 and V4.

### Task T1: The engine floor

- [ ] **Step 1: Read Docker's release notes** for the first Engine release that drops direct-routed traffic to
  a container in the `raw` table, and cite it. Do not take the version from this plan.
- [ ] **Step 2: Failing tests** in `install_preflight_test.sh` and `aura_image_update_test.sh`: an engine
  below the floor fails with a message naming the floor; one at it passes.
- [ ] **Step 3: Implement** in `install.sh`'s preflight (`docker_ready` today checks only that `docker compose`
  exists) and before `aura-image-update.sh` applies an update.
- [ ] **Step 4: Run** both test scripts. **Commit.**

### Task T2: The live check probes both paths

- [ ] **Step 1:** `verifyTunnelNetwork()` gains, from the throwaway probe on `aura-tunnel`: the network's
  gateway at every published port and at the ports Task D names; and three positive probes, `caddy:8080`, a
  name through the resolver, and the edge.
- [ ] **Step 2:** Run it against an appliance directory (`AURA_E2E_APPLIANCE_DIR`, which the function
  requires), and see the gateway probe fail on `443` before T3, or pass if Task D chose (c).
- [ ] **Step 3: Commit.**

### Task T3: The remedy Task D chose

Written as its own task list once D is decided; not before.

### Task V4: Close on the VM

- [ ] **Step 1:** Release through the updater.
- [ ] **Step 2:** V1's steps 2-5 and `verifyTunnelNetwork()`, all green, before and after a Docker restart and a
  reboot.
- [ ] **Step 3: Record** the verification file, and amend the spec's decision 8 and the review's §F-1 with
  date and evidence.

## Decisions taken while writing this plan

1. **The plan starts from a measurement, not from the review's remedies.** The review's remedy (b), a
   `DOCKER-USER` drop between bridges, answers the direct path; on the 2026-11-26 engine that path is already
   closed, and `DOCKER-USER` never sees the host path, which goes through `INPUT`.
2. **The engine check is part of the fix.** The direct path's closure is Docker's; without a floor, an older
   engine on a new appliance reopens it silently.
3. **Option (b) of Task D is the recommendation, not the decision:** it is reuse, it lives in Compose and it
   survives a reboot by construction. It is the operator's call.
4. **The nftables backend is out of scope** while Docker calls it experimental; T1's check reads the backend
   and fails on anything but iptables until it is measured.
