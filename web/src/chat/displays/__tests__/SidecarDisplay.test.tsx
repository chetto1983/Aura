import { act, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import i18n from '../../../i18n/i18n';
import { DisplayRouter } from '../DisplayRouter';
import type { DisplayPayload } from '../types';

afterEach(async () => {
  await act(async () => {
    await i18n.changeLanguage('en');
  });
});

describe('trusted sidecar read tables', () => {
  it('shows localized calendar events and keeps IDs as escaped text', async () => {
    await act(async () => {
      await i18n.changeLanguage('it');
    });
    const payload = {
      type: 'table',
      title: 'calendar_events',
      tool_call_id: 'cal-1',
      table: {
        columns: ['Event', 'Account', 'Subject', 'Start', 'End'],
        rows: [['<event-ref>', 'a1', 'Riunione', '2026-09-28 10:00', '2026-09-28 11:00']],
      },
    } as DisplayPayload;
    render(<DisplayRouter payload={payload} toolName="pim__calendar" result="raw" />);
    const table = screen.getByRole('table', { name: 'Eventi del calendario' });
    expect(within(table).getByText('<event-ref>')).toBeTruthy();
    expect(document.querySelector('event-ref')).toBeNull();
  });

  it('shows WhatsApp chats with row count and bounded omission', () => {
    const payload = {
      type: 'table',
      title: 'whatsapp_chats',
      tool_call_id: 'wa-1',
      table: {
        columns: ['Chat', 'Name', 'Last active', 'Last message'],
        rows: [['123@s.whatsapp.net', 'Ada', '2026-09-28', 'Hello']],
        omitted_rows: 4,
      },
    } as DisplayPayload;
    render(<DisplayRouter payload={payload} toolName="wa__list_chats" result="raw" />);
    expect(screen.getByRole('table', { name: 'WhatsApp chats' })).toBeTruthy();
    expect(screen.getByText('4 more rows not shown')).toBeTruthy();
  });

  it('uses escaped raw fallback for a malformed sidecar table', () => {
    const payload = {
      type: 'table',
      title: 'calendar_events',
      tool_call_id: 'cal-2',
      table: { columns: ['Event'], rows: [['<script>']] },
    } as DisplayPayload;
    render(<DisplayRouter payload={payload} toolName="pim__calendar" result="raw event" />);
    expect(screen.getByText('raw event').tagName.toLowerCase()).toBe('pre');
  });
});
