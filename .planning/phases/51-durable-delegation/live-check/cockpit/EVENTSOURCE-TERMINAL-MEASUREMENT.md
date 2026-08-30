# Worker EventSource terminal-close measurement

Date: 2026-08-30
Phase: 51-12b live cockpit replay
Privacy: conversation, identity, job, child, channel identifiers, and transcript text are intentionally omitted.

## Measured

- The rebuilt `aura:local` bundle contained the named AG-UI EventSource listeners introduced by
  Amendment #180.
- A native browser EventSource replay received one `RUN_STARTED`, one `TOOL_CALL_START`, one
  `TOOL_CALL_RESULT`, two `STATE_DELTA`, and one `RUN_FINISHED` frame. JSON parsing had zero errors,
  and the tool-start frame named `shell_exec`.
- The worker pane initially showed its connecting state and received the replay, but after the
  terminal stream closed it rendered the report-artifact error fallback: `role=alert` was present,
  `role=status` was absent, and zero read-only messages remained visible.
- The endpoint deliberately returns after the transcript's terminal state. Native EventSource
  reports that EOF through its `error` event and otherwise attempts to reconnect.
- `openWorkerStream` treated every `error` event as a transport failure. It did not remember that
  `RUN_FINISHED` or `RUN_ERROR` had already closed the AG-UI run, so a correct terminal EOF replaced
  the reconstructed transcript with the error fallback.
- The unit fake never emitted the EventSource error that follows a normal terminal server close,
  so its named-event tests did not exercise this lifecycle boundary.

## Decision boundary

On `RUN_FINISHED` or `RUN_ERROR`, the worker client marks the stream terminal and closes the
EventSource after publishing the reduced message. A later browser `error` for that terminal close
is ignored. An `error` before a terminal frame remains a real transport failure and still surfaces
the report-artifact fallback.

This measurement does not prove live mid-tool rendering, reconnection after a non-terminal network
failure, or the final two-child connection count. Those remain browser checks after deployment.
