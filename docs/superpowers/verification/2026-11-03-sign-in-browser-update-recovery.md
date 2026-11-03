# Aura update recovery — 2026-11-03

During preparation of the sign-in browser's live acceptance, the operator reported
that Aura could no longer be opened in the browser.

## Measured cause

The VM remained reachable by ping and SSH with an unchanged uptime. No kernel OOM
events were found in the preceding fifteen minutes. Aura answered internally, but
Caddy's container was `created`, had never started, and nothing listened on port 443.

The updater journal recorded an admin-requested update to `2fceb229c`. Its first
attempt failed at 09:38:13 UTC because Docker could not stop Grafana: its process
was reported as a zombie. The interrupted Compose reconciliation left Caddy stopped.
A subsequent updater retry was already running when recovery began.

Root disk usage was 94% (57 GB filesystem, 3.6 GB available). No inode exhaustion
was present. This measurement does not establish disk pressure as the zombie's cause.

## Recovery and verification

Temporarily stopped the updater timer and request-path triggers, then started the
existing Caddy container. No VM reboot, manual image pruning or data deletion was
performed. The in-flight updater retry completed successfully and its own image
cleanup reduced disk usage to 67%, leaving 19 GB available.

Verified:

- Aura and Caddy healthy; the native readiness endpoint returned `ready: true`.
- No created or unhealthy containers remained; Grafana was healthy.
- HTTPS from the operator PC reached `/login` with HTTP 200.
- Updater status was `current`, revision `2fceb229c`, with no error.
- Restored both previously active periodic-update and request-path triggers.

The planned MCP integration had not been deployed. Its local publication checks
were stopped during recovery. These checks establish service recovery; they do not
close the pending TLS or provider sign-in acceptance.
