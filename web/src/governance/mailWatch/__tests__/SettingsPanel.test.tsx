import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';
import '../../../i18n/i18n';
import { chooseOption, shownOption } from '../../../test/chooseOption';
import { hang, mailWatchServer, renderWatch, watchStatus } from '../../../test/mailWatchServer';
import { dayAndTime } from '../mailWatchLogic';
import { SettingsPanel } from '../SettingsPanel';

// The two numbers a person sets, and what the panel says about the rest.

const SETTINGS = '/api/mail-watch/settings';

let server: ReturnType<typeof mailWatchServer>;
beforeEach(() => {
  server = mailWatchServer();
});
afterEach(() => {
  vi.unstubAllGlobals();
});

function capField() {
  return screen.getByLabelText('Alerts a day per account');
}

function save() {
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
}

function answers() {
  server.on(`PUT ${SETTINGS}`, ({ body }) => ({
    body: { ...watchStatus().settings, ...(body as object) },
  }));
}

describe('SettingsPanel', () => {
  it('opens on the saved values', async () => {
    renderWatch(<SettingsPanel />);
    await screen.findByText('Digest hour');
    expect(shownOption('Digest hour')).toBe('18:00');
    expect((capField() as HTMLInputElement).value).toBe('5');
  });

  it('saves both numbers, as numbers', async () => {
    answers();
    renderWatch(<SettingsPanel />);
    await screen.findByText('Digest hour');
    await chooseOption('Digest hour', '07:00');
    fireEvent.change(capField(), { target: { value: '12' } });
    save();
    await waitFor(() => {
      expect(server.writes('PUT', SETTINGS)[0]?.body).toEqual({
        digest_hour: 7,
        daily_alert_cap: 12,
      });
    });
    expect((await screen.findByRole('status')).textContent).toBe('Saved.');
  });

  it('offers every hour of the day', async () => {
    renderWatch(<SettingsPanel />);
    await screen.findByText('Digest hour');
    fireEvent.click(screen.getByRole('combobox', { name: 'Digest hour' }));
    const hours = await screen.findAllByRole('option');
    expect(hours).toHaveLength(24);
    expect(hours[0]?.textContent).toBe('00:00');
    expect(hours[23]?.textContent).toBe('23:00');
  });

  it.each([['0'], ['100']])('accepts a cap of %s', async (cap) => {
    answers();
    renderWatch(<SettingsPanel />);
    await screen.findByText('Digest hour');
    fireEvent.change(capField(), { target: { value: cap } });
    save();
    await waitFor(() => {
      expect(server.writes('PUT', SETTINGS)).toHaveLength(1);
    });
  });

  it.each([['101'], ['-1'], ['1.5'], ['']])('refuses a cap of "%s"', async (cap) => {
    renderWatch(<SettingsPanel />);
    await screen.findByText('Digest hour');
    fireEvent.change(capField(), { target: { value: cap } });
    expect(capField().hasAttribute('aria-invalid')).toBe(false);
    save();
    expect(capField().getAttribute('aria-invalid')).toBe('true');
    expect(screen.getByText('Write a whole number from 0 to 100.')).toBeTruthy();
    expect(server.writes('PUT', SETTINGS)).toEqual([]);
  });

  it('points the cap at its hint, and clears the mark once it is valid', async () => {
    renderWatch(<SettingsPanel />);
    await screen.findByText('Digest hour');
    const hint = screen.getByText(/Past the cap/);
    expect(capField().getAttribute('aria-describedby')).toBe(hint.id);
    fireEvent.change(capField(), { target: { value: '999' } });
    save();
    fireEvent.change(capField(), { target: { value: '9' } });
    expect(capField().hasAttribute('aria-invalid')).toBe(false);
  });

  it('says how often it checks, and that no digest went out yet', async () => {
    renderWatch(<SettingsPanel />);
    expect(
      await screen.findByText(/Aura checks every 5 minutes\. No digest sent yet\./),
    ).toBeTruthy();
  });

  it('says it in the singular', async () => {
    server.on('GET /api/mail-watch', () => ({
      body: watchStatus({ settings: { ...watchStatus().settings, interval_minutes: 1 } }),
    }));
    renderWatch(<SettingsPanel />);
    expect(await screen.findByText(/Aura checks every minute\./)).toBeTruthy();
  });

  it('says when the last digest went out', async () => {
    const at = '2026-10-31T17:00:00Z';
    server.on('GET /api/mail-watch', () => ({
      body: watchStatus({ settings: { ...watchStatus().settings, last_digest_at: at } }),
    }));
    renderWatch(<SettingsPanel />);
    const expected = `Last digest: ${dayAndTime(at, 'en')}.`;
    expect((await screen.findByText(/Last digest/)).textContent).toContain(expected);
  });

  it('says why a save failed', async () => {
    server.on(`PUT ${SETTINGS}`, () => ({
      status: 400,
      body: { error: 'the digest hour is 0 to 23' },
    }));
    renderWatch(<SettingsPanel />);
    await screen.findByText('Digest hour');
    save();
    expect((await screen.findByRole('alert')).textContent).toBe('the digest hour is 0 to 23');
  });

  it('shows the saved values in the cache at once', async () => {
    answers();
    const { client } = renderWatch(<SettingsPanel />);
    await screen.findByText('Digest hour');
    fireEvent.change(capField(), { target: { value: '9' } });
    save();
    await screen.findByText('Saved.');
    const cached = client.getQueryData<ReturnType<typeof watchStatus>>(['mail-watch', 'status']);
    expect(cached?.settings.daily_alert_cap).toBe(9);
  });

  it('announces the wait', () => {
    server.on('GET /api/mail-watch', hang);
    renderWatch(<SettingsPanel />);
    expect(screen.getByText('Loading…')).toBeTruthy();
  });

  it('says when it cannot read the settings', async () => {
    server.on('GET /api/mail-watch', () => ({ status: 500, body: { error: 'mail_watch_failed' } }));
    renderWatch(<SettingsPanel />);
    expect((await screen.findByRole('alert')).textContent).toMatch(/went wrong/);
  });
});
