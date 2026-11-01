import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import '../../../i18n/i18n';
import {
  activeRow,
  CORRECT,
  DECISIONS,
  dateInput,
  itemOf,
  openDay,
  press,
  rowOf,
  theDay,
  THREE,
  tierOf,
} from '../../../test/mailWatchDay';
import { label, mailWatchServer } from '../../../test/mailWatchServer';

// What a key does on the day: the roving tabindex, the triage keys, and where the focus lands.

let server: ReturnType<typeof mailWatchServer>;

beforeEach(() => {
  server = mailWatchServer();
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe('the keys', () => {
  beforeEach(() => {
    theDay(server, ...THREE);
  });

  it('takes the focus on the first row once the day has loaded', async () => {
    openDay();
    await screen.findByText('First');
    await waitFor(() => {
      expect(document.activeElement).toBe(rowOf('First'));
    });
  });

  it('keeps one row in the tab order, and its controls alone', async () => {
    openDay();
    await screen.findByText('First');
    const rows = Array.from(document.querySelectorAll<HTMLElement>('[data-mail-watch-row]'));
    expect(rows.filter((row) => row.tabIndex === 0)).toEqual([rowOf('First')]);
    const closed = within(itemOf('Second')).getAllByRole('radio');
    expect(closed.every((radio) => radio.tabIndex === -1)).toBe(true);
    expect(within(itemOf('Second')).getByRole('radiogroup').tabIndex).toBe(-1);
  });

  it('J and K walk the rows and stop at the ends', async () => {
    openDay();
    await screen.findByText('First');
    press('k');
    expect(document.activeElement).toBe(rowOf('First'));
    press('j');
    await waitFor(() => {
      expect(document.activeElement).toBe(rowOf('Second'));
    });
    press('J');
    await waitFor(() => {
      expect(document.activeElement).toBe(rowOf('Third'));
    });
    press('j');
    expect(document.activeElement).toBe(rowOf('Third'));
    press('K');
    await waitFor(() => {
      expect(document.activeElement).toBe(rowOf('Second'));
    });
  });

  it('the arrows walk them too', async () => {
    openDay();
    await screen.findByText('First');
    press('ArrowDown');
    await waitFor(() => {
      expect(document.activeElement).toBe(rowOf('Second'));
    });
    press('ArrowUp');
    await waitFor(() => {
      expect(document.activeElement).toBe(rowOf('First'));
    });
  });

  it('1, 2 and 3 set the tier of the focused row', async () => {
    server.on(`POST ${CORRECT}`, () => ({ body: { tier: 'later' } }));
    openDay();
    await screen.findByText('First');
    press('3');
    await waitFor(() => {
      expect(server.writes('POST', CORRECT)[0]?.body).toEqual({
        resource: 'mail',
        item_id: 'a',
        tier: 'later',
      });
    });
  });

  it('pressing the tier a row already has sends nothing', async () => {
    openDay();
    await screen.findByText('First');
    press('1');
    expect(server.writes('POST', CORRECT)).toEqual([]);
  });

  it('L opens the label menu of the focused row', async () => {
    server.on('GET /api/mail-watch/labels', () => ({ body: [label({ name: 'Client' })] }));
    openDay();
    await screen.findByText('First');
    press('l');
    expect(await screen.findByRole('option', { name: 'Client' })).toBeTruthy();
  });

  it('never fires from the date input', async () => {
    openDay();
    await screen.findByText('First');
    const field = dateInput();
    fireEvent.keyDown(field, { key: '1' });
    fireEvent.keyDown(field, { key: 'j' });
    expect(server.writes('POST', CORRECT)).toEqual([]);
    expect(activeRow()).toBe(rowOf('First'));
  });

  it('never fires from the label select, whose letters are its own type-ahead', async () => {
    server.on('GET /api/mail-watch/labels', () => ({ body: [label()] }));
    openDay();
    await screen.findByText('First');
    const select = within(itemOf('First')).getByRole('combobox', { name: 'Label' });
    fireEvent.keyDown(select, { key: '2' });
    fireEvent.keyDown(select, { key: 'j' });
    expect(server.writes('POST', CORRECT)).toEqual([]);
    expect(activeRow()).toBe(rowOf('First'));
  });

  it('leaves the focus in a field the person was typing in', async () => {
    server.on(DECISIONS, async () => {
      await Promise.resolve();
      return { body: THREE };
    });
    openDay();
    const field = await screen.findByLabelText('Day', { selector: 'input' });
    act(() => {
      field.focus();
    });
    await screen.findByText('First');
    expect(document.activeElement).toBe(field);
  });

  it('leaves a chord to the browser', async () => {
    openDay();
    await screen.findByText('First');
    fireEvent.keyDown(activeRow(), { key: '2', ctrlKey: true });
    fireEvent.keyDown(activeRow(), { key: 'l', metaKey: true });
    expect(server.writes('POST', CORRECT)).toEqual([]);
  });

  it('a key on the open menu does not reach the row, through the portal', async () => {
    server.on('GET /api/mail-watch/labels', () => ({ body: [label({ name: 'Client' })] }));
    openDay();
    await screen.findByText('First');
    press('l');
    const option = await screen.findByRole('option', { name: 'Client' });
    fireEvent.keyDown(option, { key: '2' });
    expect(server.writes('POST', CORRECT)).toEqual([]);
  });

  it('gives the focus back to the row when the menu closes, so J still walks', async () => {
    server.on('GET /api/mail-watch/labels', () => ({ body: [label({ name: 'Client' })] }));
    openDay();
    await screen.findByText('First');
    press('l');
    await screen.findByRole('option', { name: 'Client' });
    fireEvent.keyDown(screen.getByRole('listbox'), { key: 'Escape' });
    await waitFor(() => {
      expect(screen.queryByRole('listbox')).toBeNull();
    });
    await waitFor(() => {
      expect(document.activeElement).toBe(rowOf('First'));
    });
    press('j', rowOf('First'));
    await waitFor(() => {
      expect(document.activeElement).toBe(rowOf('Second'));
    });
  });

  it('takes the keys in the tier control too, leaving it the arrows', async () => {
    server.on(`POST ${CORRECT}`, () => ({ body: { tier: 'later' } }));
    openDay();
    await screen.findByText('First');
    const radio = within(itemOf('First')).getByRole('radio', { name: 'Now' });
    fireEvent.keyDown(radio, { key: 'ArrowDown' });
    expect(activeRow()).toBe(rowOf('First'));
    fireEvent.keyDown(radio, { key: '3' });
    await waitFor(() => {
      expect(server.writes('POST', CORRECT)[0]?.body).toMatchObject({ tier: 'later' });
    });
  });

  it('leaves the focus on a control in the active row, through a correction', async () => {
    server.on(`POST ${CORRECT}`, () => ({ body: { tier: 'later' } }));
    openDay();
    await screen.findByText('First');
    const radio = within(itemOf('Third')).getByRole('radio', { name: 'Later' });
    fireEvent.click(rowOf('Third'));
    act(() => {
      radio.focus();
    });
    // Third is last and stays last, so the correction moves nothing.
    fireEvent.click(radio);
    await waitFor(() => {
      expect(server.writes('POST', CORRECT)).toHaveLength(1);
    });
    expect(document.activeElement).toBe(radio);
  });

  it('activates the row a person clicks', async () => {
    openDay();
    await screen.findByText('First');
    const second = rowOf('Second');
    fireEvent.click(second);
    expect(activeRow()).toBe(second);
    await waitFor(() => {
      expect(document.activeElement).toBe(second);
    });
  });

  it('keeps the place when a correction sends the row away', async () => {
    server.on(`POST ${CORRECT}`, () => ({ body: { tier: 'now' } }));
    openDay();
    await screen.findByText('First');
    press('j');
    await waitFor(() => {
      expect(document.activeElement).toBe(rowOf('Second'));
    });
    // Second goes up to Now. The active index stays 1, which is now Second itself, under First.
    press('1');
    await waitFor(() => {
      expect(tierOf('Second')).toBe('Now');
    });
    await waitFor(() => {
      expect(document.activeElement).toBe(activeRow());
    });
  });
});
