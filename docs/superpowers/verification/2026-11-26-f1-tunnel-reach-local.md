# F-1, re-measured in a Linux container (2026-11-26)

Finding F-1 (`2026-09-21-cloudflare-remote-access-review.md`, §F-1) measured on Docker Desktop, Engine
29.7.2, WSL2: from a container on the tunnel network, every published container port was reachable at the
container's own IP, because publishing installs an `accept` that matches any input interface. The review
expected the Ubuntu appliance to behave the same and did not measure it. This file measures the same probe
on a Linux engine. Plan: `docs/superpowers/plans/2026-11-26-f1-tunnel-reach.md`.

## Where

| | |
|---|---|
| Engine | Docker 29.9.3, Linux, `Firewall Backend: iptables` (`docker info`), no `/etc/docker/daemon.json` |
| Host | a cloud session's container, not the lab VM and not an appliance |
| Networks | two plain user-defined bridges, as `compose.yaml` declares `aura-tunnel`: `f1-data` (172.19.0.0/16), `f1-tunnel` (172.20.0.0/16) |
| Listeners | `busybox:stable` running `nc -l` on `f1-data`: one with `-p 127.0.0.1:15432:5432`, one with `-p 15433:5432` (all host addresses), one unpublished; one on `f1-tunnel`, unpublished |
| Probe | `nc -z -w 2` from a throwaway `busybox` container on `f1-tunnel`, as the review's probe block |

## What was measured

| From the tunnel network to | Result |
|---|---|
| the container IP of the port published on `127.0.0.1` | blocked |
| the container IP of the port published on all addresses | blocked |
| the container IP of the unpublished listener | blocked |
| a closed port and an empty address (controls) | blocked |
| the tunnel bridge's gateway, at the port published on `127.0.0.1` | blocked |
| the tunnel bridge's gateway, at the port published on all addresses | **reachable** |
| a listener on the tunnel network itself | reachable |

The mechanism, from `iptables -S` and `iptables -t raw -S`:

```
-t raw -A PREROUTING -d 172.19.0.2/32 ! -i br-f67f8cf6c8c1 -j DROP
-A DOCKER -d 172.19.0.2/32 ! -i br-f67f8cf6c8c1 -o br-f67f8cf6c8c1 -p tcp -m tcp --dport 5432 -j ACCEPT
```

The `filter` accept the review measured is there, but the `raw` table drops every packet addressed to the
container that did not arrive on its own bridge, other bridges included, before `filter` sees it. Docker's
documentation states it: in the default `nat` gateway mode, "containers in other bridge networks can only
access published ports via the host addresses they are published to. Direct routing from other networks is
not allowed" (https://docs.docker.com/engine/network/port-publishing/), and direct-routed packets "are
dropped by a rule in the `raw` table's `PREROUTING` chain" (https://docs.docker.com/engine/network/firewall-iptables/).

So on this engine the path F-1 measured is closed by Docker, and another stays open: a port published on all
host addresses is reachable from the tunnel network through the host's own addresses. In `compose.yaml`
that is `caddy`'s `0.0.0.0:443`, and `postgres` whenever `POSTGRES_HOST` is set to a non-loopback address.
Every other published port binds `127.0.0.1`.

## What this does not show

- **The appliance.** The lab VM's engine version, firewall backend and rules were not read. Whether the 2026-09-21
  result came from Docker Desktop or from 29.7.2 is not settled either: nothing here ran 29.7.2 on Linux.
- **Host services.** A service of the host itself listening on all addresses (sshd, for example) is reached
  through the same gateway, by the host's `INPUT` chain, which Docker does not manage. No host service was
  probed here.
- **The internet.** This container's egress goes through a proxy, so the probe's reach to the internet,
  which cloudflared needs, was not measured.
- **The nftables backend**, experimental in 29.x, and IPv6.
- **Persistence.** No daemon restart or host reboot was done; the `raw` rule is Docker's own, written when
  the container starts.
