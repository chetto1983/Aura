import { describe, expect, it } from 'vitest';
import {
  messageParts,
  newAssistantTurn,
  reduceFrame,
  toThreadMessage,
  type AguiFrame,
} from '../sseAdapter';

describe('sseAdapter MCP views', () => {
  it('keeps a trusted read card and MCP Apps view on the same live call in either event order', () => {
    const display = {
      type: 'table',
      tool_call_id: 'c1',
      title: 'calendar_accounts',
      table: { columns: ['Account', 'Provider', 'Name'], rows: [['a1', 'google', 'Work']] },
    };
    const view = { server: 'pim', resource_uri: 'ui://calendar/view.html', tool_call_id: 'c1' };
    for (const frames of [
      [
        { type: 'CUSTOM', name: 'aura.display', value: display },
        { type: 'CUSTOM', name: 'aura.mcp_view', value: view },
      ],
      [
        { type: 'CUSTOM', name: 'aura.mcp_view', value: view },
        { type: 'CUSTOM', name: 'aura.display', value: display },
      ],
    ]) {
      const state = newAssistantTurn('a1');
      for (const frame of frames) reduceFrame(state, frame as AguiFrame);
      const part = messageParts(toThreadMessage(state)).find((item) => item.type === 'tool-call');
      expect(part).toMatchObject({ display, mcpView: view });
    }
  });
});
