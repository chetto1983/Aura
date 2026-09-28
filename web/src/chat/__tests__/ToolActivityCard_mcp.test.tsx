import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { ToolActivityCard } from '../ToolActivityCard';

vi.mock('../mcpapps/McpViewFrame', () => ({
  McpViewFrame: () => <div data-testid="mcp-view-frame">Interactive view</div>,
}));

describe('ToolActivityCard MCP read', () => {
  it('keeps the read table and MCP Apps view in the same expanded result', () => {
    render(
      <ToolActivityCard
        toolName="pim__calendar"
        argsText='{"action":"list_accounts"}'
        result='{"accounts":[{"accountId":"work","displayName":"Work"}]}'
        display={{
          type: 'table',
          title: 'calendar_accounts',
          tool_call_id: 'calendar-1',
          table: { columns: ['account', 'provider', 'name'], rows: [['work', 'google', 'Work']] },
        }}
        mcpView={{
          server: 'pim',
          resource_uri: 'ui://calendar/view.html',
          tool_call_id: 'calendar-1',
          tool_name: 'pim__calendar',
        }}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: /pim__calendar activity/ }));
    const body = screen.getByTestId('tool-body');
    expect(within(body).getAllByText('Work').length).toBeGreaterThan(0);
    expect(within(body).getByTestId('mcp-view-frame')).toBeTruthy();
    expect(within(body).queryByText('Request')).toBeNull();
  });
});
