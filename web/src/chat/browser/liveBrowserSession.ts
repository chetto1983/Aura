// Which agent-browser session a turn leaves open for the user to see (prd.md §12). The chat
// shows the live view itself rather than waiting for the model to write its link: measured
// 2026-09-27 on the lab VM, a model that never loaded the browser skill opened a login page and
// told the user to sign in, with no link and no session name.

const BROWSER_TOOL_PREFIX = 'browser__agent_browser_';
const CLOSE_TOOL = `${BROWSER_TOOL_PREFIX}close`;
// agent-browser's own name for a call that names none (AGENT_BROWSER_SESSION).
const DEFAULT_SESSION = 'default';
// The relay and the server accept no other name.
const SESSION_NAME = /^[A-Za-z0-9_-]{1,48}$/;

interface ToolCallPart {
  readonly type?: unknown;
  readonly toolName?: unknown;
  readonly argsText?: unknown;
  readonly result?: unknown;
  readonly isError?: unknown;
}

interface TurnMessage {
  readonly role: string;
  readonly content: unknown;
}

/**
 * The session the thread's latest turn left open: the one of its last settled browser call, or
 * null when that call closed it or the turn made none. The turn is every message after the user's
 * last one, since a replayed turn arrives as one assistant message per model call. Running and
 * failed calls are passed over: neither leaves a page to show.
 */
export function turnBrowserSession(messages: readonly TurnMessage[]): string | null {
  let start = messages.length;
  while (start > 0 && messages[start - 1]?.role !== 'user') start -= 1;
  const parts = messages
    .slice(start)
    .flatMap((message) => (Array.isArray(message.content) ? (message.content as unknown[]) : []));
  return lastBrowserSession(parts);
}

function lastBrowserSession(content: readonly unknown[]): string | null {
  for (let i = content.length - 1; i >= 0; i -= 1) {
    const part = content[i] as ToolCallPart | null;
    if (part?.type !== 'tool-call' || typeof part.toolName !== 'string') continue;
    if (!part.toolName.startsWith(BROWSER_TOOL_PREFIX)) continue;
    if (typeof part.result !== 'string' || part.isError === true) continue;
    if (part.toolName === CLOSE_TOOL) return null;
    return sessionArg(part.argsText);
  }
  return null;
}

function sessionArg(argsText: unknown): string | null {
  let args: unknown;
  try {
    args = JSON.parse(typeof argsText === 'string' && argsText !== '' ? argsText : '{}');
  } catch {
    return null;
  }
  const session = (args as { session?: unknown } | null)?.session ?? DEFAULT_SESSION;
  return typeof session === 'string' && SESSION_NAME.test(session) ? session : null;
}
