# Worker EventSource event-name measurement

Date: 2026-08-30
Phase: 51-12b live cockpit drive
Privacy: conversation, identity, job, child, and channel identifiers are intentionally omitted.

## Measured

- The worker pane opened its child EventSource and showed the connecting state.
- The completed worker transcript contained one `shell_exec` start and one `shell_exec` end event.
- The server's shared SSE writer emits named records such as `event: TOOL_CALL_START`,
  `event: TOOL_CALL_RESULT`, and `event: CUSTOM`; the JSON body repeats the AG-UI `type`.
- `openWorkerStream` and `openWorkerStatusStream` registered only a `message` listener.
- Native EventSource dispatches a named SSE record only to listeners registered for that event
  name, not to the default `message` listener.
- Two browser observations, including a three-minute replay wait, therefore showed no
  `shell_exec` card despite the transcript's two tool lifecycle records.
- The unit-test fake's `push` helper always dispatched `message`, masking the production wire
  semantics.

## Decision boundary

The child stream registers the reducer-relevant AG-UI event names and keeps `message` as a
compatibility fallback. The status stream registers `CUSTOM` plus the same fallback. Close removes
every registered listener before closing the EventSource. The fake dispatches each frame using its
own `type`, matching a native named SSE event.

This measurement does not prove the post-fix pane rendering or the final EventSource connection
count; both remain browser checks after deployment.
