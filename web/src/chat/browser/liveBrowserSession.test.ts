import { describe, expect, it } from 'vitest';
import { turnBrowserSession } from './liveBrowserSession';

function call(toolName: string, args: object | string, result?: string, isError?: boolean) {
  return {
    type: 'tool-call',
    toolCallId: `${toolName}-${String(Math.random())}`,
    toolName,
    argsText: typeof args === 'string' ? args : JSON.stringify(args),
    ...(result !== undefined ? { result } : {}),
    ...(isError !== undefined ? { isError } : {}),
  };
}

const open = (args: object | string, result = 'YouTube') =>
  call('browser__agent_browser_open', args, result);
const user = (text: string) => ({ role: 'user', content: [{ type: 'text', text }] });
const assistant = (...content: unknown[]) => ({ role: 'assistant', content });
/** One turn: the user's message and a single assistant message holding `parts`. */
const turn = (...parts: unknown[]) => turnBrowserSession([user('go'), assistant(...parts)]);

describe('turnBrowserSession', () => {
  it('names the session the turn opened', () => {
    expect(turn(open({ url: 'https://example.com', session: 'bank' }))).toBe('bank');
  });

  it("falls back to agent-browser's default when the call names none", () => {
    // The VM turn of 2026-09-27: `open` with a url only.
    expect(turn(open({ url: 'https://www.youtube.com/' }))).toBe('default');
    expect(turn(open(''))).toBe('default');
    expect(turn(open('null'))).toBe('default');
  });

  it('follows the last settled browser call, whatever its tool', () => {
    expect(
      turn(
        open({ session: 'docs' }),
        { type: 'text', text: 'Opened.' },
        call('browser__agent_browser_snapshot', { session: 'bank' }, '- button "Sign in"'),
        call('web_search', { query: 'q', session: 'nope' }, 'results'),
      ),
    ).toBe('bank');
  });

  it('reads a turn replayed as one assistant message per model call', () => {
    const messages = [
      user('open youtube'),
      assistant(open({ session: 'yt' })),
      assistant({ type: 'text', text: 'Sign in when ready.' }),
    ];
    expect(turnBrowserSession(messages)).toBe('yt');
  });

  it("never shows an earlier turn's browser", () => {
    const messages = [
      user('open the bank'),
      assistant(open({ session: 'bank' })),
      user('what time is it?'),
      assistant({ type: 'text', text: 'Noon.' }),
    ];
    expect(turnBrowserSession(messages)).toBeNull();
  });

  it('passes over a running or failed call to the one before it', () => {
    expect(
      turn(
        open({ session: 'docs' }),
        call('browser__agent_browser_click', { session: 'other' }),
        call('browser__agent_browser_click', { session: 'broken' }, 'boom', true),
      ),
    ).toBe('docs');
  });

  it('shows nothing once the last browser call closed the session', () => {
    expect(
      turn(
        open({ session: 'bank' }),
        call('browser__agent_browser_close', { session: 'bank' }, 'closed'),
      ),
    ).toBeNull();
  });

  it('shows nothing for a turn without browser calls', () => {
    expect(turnBrowserSession([])).toBeNull();
    expect(turnBrowserSession([assistant({ type: 'text', text: 'hi' })])).toBeNull();
    expect(turn({ type: 'text', text: 'hi' }, call('shell_exec', { cmd: 'ls' }, 'a'), null)).toBe(
      null,
    );
    expect(turnBrowserSession([user('hi'), { role: 'assistant', content: 'plain text' }])).toBe(
      null,
    );
  });

  it('refuses a name the relay would refuse, and arguments it cannot read', () => {
    expect(turn(open({ session: '../other' }))).toBeNull();
    expect(turn(open({ session: 'x'.repeat(49) }))).toBeNull();
    expect(turn(open({ session: 7 }))).toBeNull();
    expect(turn(open('{"session":'))).toBeNull();
  });
});
