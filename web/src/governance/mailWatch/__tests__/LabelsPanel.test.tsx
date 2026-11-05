import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import '../../../i18n/i18n';
import { chooseOption, shownOption } from '../../../test/chooseOption';
import {
  hang,
  label,
  mailWatchServer,
  renderWatch,
  rowHolding,
  rule,
  settled,
} from '../../../test/mailWatchServer';
import { LabelsPanel } from '../LabelsPanel';

// The labels tab: list, add, rename with an optional tier, and a removal that counts the rules
// going with it, because the server deletes them.

const LABELS = '/api/mail-watch/labels';

let server: ReturnType<typeof mailWatchServer>;
beforeEach(() => {
  server = mailWatchServer();
});
afterEach(() => {
  vi.unstubAllGlobals();
});

function nameField() {
  return screen.getByLabelText('Name');
}

describe('the list', () => {
  it('shows each label, with a tier only where it has one', async () => {
    server.on(`GET ${LABELS}`, () => ({
      body: [
        label({ id: 'a', name: 'Client' }),
        label({ id: 'b', name: 'Ads', tier: 'later', position: 1 }),
      ],
    }));
    renderWatch(<LabelsPanel />);
    await screen.findByText('Client');
    expect(rowHolding('Client').queryByText('Later')).toBeNull();
    expect(rowHolding('Ads').getByText('Later')).toBeTruthy();
  });

  it('says so when there are none', async () => {
    renderWatch(<LabelsPanel />);
    expect(await screen.findByText(/No labels yet/)).toBeTruthy();
  });

  it('announces the wait', () => {
    server.on(`GET ${LABELS}`, hang);
    renderWatch(<LabelsPanel />);
    expect(screen.getByText('Loading…')).toBeTruthy();
  });

  it('says when it cannot read them', async () => {
    server.on(`GET ${LABELS}`, () => ({ status: 500, body: { error: 'mail_watch_failed' } }));
    renderWatch(<LabelsPanel />);
    expect((await screen.findByRole('alert')).textContent).toMatch(/cannot be read/);
  });
});

describe('adding a label', () => {
  it('sends the trimmed name and clears the form', async () => {
    server.on(`POST ${LABELS}`, () => ({ body: label() }));
    renderWatch(<LabelsPanel />);
    await screen.findByText(/No labels yet/);
    fireEvent.change(nameField(), { target: { value: '  Client  ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add label' }));
    await waitFor(() => {
      expect(server.writes('POST', LABELS)[0]?.body).toEqual({
        name: 'Client',
        tier: '',
        description: '',
        discard: false,
      });
    });
    await waitFor(() => {
      expect((nameField() as HTMLInputElement).value).toBe('');
    });
  });

  it('sends a tier when one is chosen', async () => {
    server.on(`POST ${LABELS}`, () => ({ body: label() }));
    renderWatch(<LabelsPanel />);
    await screen.findByText(/No labels yet/);
    fireEvent.change(nameField(), { target: { value: 'Ads' } });
    await chooseOption('Tier', 'Later');
    fireEvent.click(screen.getByRole('button', { name: 'Add label' }));
    await waitFor(() => {
      expect(server.writes('POST', LABELS)[0]?.body).toEqual({
        name: 'Ads',
        tier: 'later',
        description: '',
        discard: false,
      });
    });
  });

  it('refuses an empty name after an attempt, and omits aria-invalid otherwise', async () => {
    renderWatch(<LabelsPanel />);
    await screen.findByText(/No labels yet/);
    expect(nameField().hasAttribute('aria-invalid')).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: 'Add label' }));
    expect(nameField().getAttribute('aria-invalid')).toBe('true');
    await settled();
    expect(server.writes('POST', LABELS)).toEqual([]);
    fireEvent.change(nameField(), { target: { value: 'Client' } });
    expect(nameField().hasAttribute('aria-invalid')).toBe(false);
  });

  it('counts a name in characters, as the database does', async () => {
    server.on(`POST ${LABELS}`, () => ({ body: label() }));
    renderWatch(<LabelsPanel />);
    await screen.findByText(/No labels yet/);
    fireEvent.change(nameField(), { target: { value: '👍'.repeat(60) } });
    fireEvent.click(screen.getByRole('button', { name: 'Add label' }));
    await waitFor(() => {
      expect(server.writes('POST', LABELS)).toHaveLength(1);
    });
    fireEvent.change(nameField(), { target: { value: 'a'.repeat(61) } });
    fireEvent.click(screen.getByRole('button', { name: 'Add label' }));
    expect(screen.getByText(/1 to 60 characters/)).toBeTruthy();
    await settled();
    expect(server.writes('POST', LABELS)).toHaveLength(1);
  });

  it('shows why the server refused', async () => {
    server.on(`POST ${LABELS}`, () => ({
      status: 400,
      body: { error: 'invalid_label_name', message: "a label's name is 1 to 60 characters" },
    }));
    renderWatch(<LabelsPanel />);
    await screen.findByText(/No labels yet/);
    fireEvent.change(nameField(), { target: { value: 'Client' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add label' }));
    expect((await screen.findByRole('alert')).textContent).toMatch(/1 to 60 characters/);
  });
});

describe('renaming a label', () => {
  beforeEach(() => {
    server.on(`GET ${LABELS}`, () => ({
      body: [label({ id: 'a', name: 'Client', tier: 'now' })],
    }));
  });

  async function startRename() {
    fireEvent.click(await screen.findByRole('button', { name: 'Rename Client' }));
    return screen.getByRole('listitem');
  }

  it('opens on the current name and tier, and saves both', async () => {
    server.on(`PUT ${LABELS}/a`, () => ({ body: label({ name: 'Customers' }) }));
    renderWatch(<LabelsPanel />);
    const item = await startRename();
    const field = within(item).getByLabelText('Name') as HTMLInputElement;
    expect(field.value).toBe('Client');
    expect(within(item).getByRole('combobox').textContent).toBe('Now');
    fireEvent.change(field, { target: { value: 'Customers' } });
    fireEvent.click(within(item).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(server.writes('PUT', `${LABELS}/a`)[0]?.body).toEqual({
        name: 'Customers',
        tier: 'now',
        description: '',
        discard: false,
      });
    });
    expect(await screen.findByRole('button', { name: 'Rename Client' })).toBeTruthy();
  });

  it('clears a tier by sending it empty, because a PUT keeps what it leaves out', async () => {
    server.on(`PUT ${LABELS}/a`, () => ({ body: label() }));
    renderWatch(<LabelsPanel />);
    const item = await startRename();
    fireEvent.click(within(item).getByRole('combobox'));
    fireEvent.click(await screen.findByRole('option', { name: 'No tier' }));
    fireEvent.click(within(item).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(server.writes('PUT', `${LABELS}/a`)[0]?.body).toEqual({
        name: 'Client',
        tier: '',
        description: '',
        discard: false,
      });
    });
  });

  it('can be cancelled without a write', async () => {
    renderWatch(<LabelsPanel />);
    const item = await startRename();
    fireEvent.click(within(item).getByRole('button', { name: 'Cancel' }));
    expect(await screen.findByRole('button', { name: 'Rename Client' })).toBeTruthy();
    expect(server.writes('PUT', `${LABELS}/a`)).toEqual([]);
  });

  it('says why a save failed and stays open', async () => {
    server.on(`PUT ${LABELS}/a`, () => ({ status: 404, body: { error: 'not_found' } }));
    renderWatch(<LabelsPanel />);
    const item = await startRename();
    fireEvent.click(within(item).getByRole('button', { name: 'Save' }));
    expect((await within(item).findByRole('alert')).textContent).toBe('That is no longer there.');
    expect(within(item).getByLabelText('Name')).toBeTruthy();
  });

  it('the add form starts with no tier', async () => {
    server.on(`GET ${LABELS}`, () => ({ body: [] }));
    renderWatch(<LabelsPanel />);
    await screen.findByText(/No labels yet/);
    expect(shownOption('Tier')).toBe('No tier');
  });
});

describe('a label for the decision model', () => {
  it('adds a label with a description and discard', async () => {
    server.on(`POST ${LABELS}`, () => ({ body: label() }));
    renderWatch(<LabelsPanel />);
    await screen.findByText(/No labels yet/);
    fireEvent.change(nameField(), { target: { value: 'Ads' } });
    const field = screen.getByLabelText('Description') as HTMLTextAreaElement;
    expect(field.maxLength).toBe(300);
    expect(screen.getByText(/The model reads only this/)).toBeTruthy();
    fireEvent.change(field, { target: { value: '  Offers sent to a list.  ' } });
    fireEvent.click(screen.getByRole('radio', { name: 'Discard' }));
    expect(screen.getByText(/Discarded mail leaves the list/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Add label' }));
    await waitFor(() => {
      expect(server.writes('POST', LABELS)[0]?.body).toEqual({
        name: 'Ads',
        tier: '',
        description: 'Offers sent to a list.',
        discard: true,
      });
    });
  });

  it('edits a description and keeps what the label already says', async () => {
    server.on(`GET ${LABELS}`, () => ({
      body: [label({ id: 'a', name: 'Ads', description: 'Old.', discard: true })],
    }));
    server.on(`PUT ${LABELS}/a`, () => ({ body: label() }));
    renderWatch(<LabelsPanel />);
    const rename = await screen.findByRole('button', { name: 'Rename Ads' });
    expect(rowHolding('Ads').getByText('Discard')).toBeTruthy();
    fireEvent.click(rename);
    const item = screen.getByRole('listitem');
    const field = within(item).getByLabelText('Description') as HTMLTextAreaElement;
    expect(field.value).toBe('Old.');
    expect(within(item).getByRole('radio', { name: 'Discard' }).getAttribute('aria-checked')).toBe(
      'true',
    );
    fireEvent.change(field, { target: { value: 'New.' } });
    fireEvent.click(within(item).getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(server.writes('PUT', `${LABELS}/a`)[0]?.body).toEqual({
        name: 'Ads',
        tier: '',
        description: 'New.',
        discard: true,
      });
    });
  });

  it('marks a label with no description', async () => {
    server.on(`GET ${LABELS}`, () => ({
      body: [
        label({ id: 'a', name: 'Client', description: 'Customers asking for quotes.' }),
        label({ id: 'b', name: 'Ads', position: 1 }),
      ],
    }));
    renderWatch(<LabelsPanel />);
    await screen.findByText('Client');
    expect(rowHolding('Ads').getByText('Without a description the model skips it')).toBeTruthy();
    expect(rowHolding('Client').queryByText('Without a description the model skips it')).toBeNull();
  });

  it('flags a reserved name', async () => {
    server.on(`GET ${LABELS}`, () => ({
      body: [label({ id: 'a', name: 'None', description: 'Anything at all.' })],
    }));
    renderWatch(<LabelsPanel />);
    await screen.findByText('None');
    expect(rowHolding('None').getByText('Reserved name: the model skips it')).toBeTruthy();
  });
});

describe('removing a label', () => {
  beforeEach(() => {
    server.on(`GET ${LABELS}`, () => ({ body: [label({ id: 'a', name: 'Client' })] }));
  });

  async function askToRemove() {
    fireEvent.click(await screen.findByRole('button', { name: 'Remove Client' }));
    return screen.findByRole('dialog', { name: 'Remove the label Client?' });
  }

  it('says no rule uses it, when none does', async () => {
    renderWatch(<LabelsPanel />);
    const dialog = await askToRemove();
    expect(within(dialog).getByText('No rule uses it.')).toBeTruthy();
  });

  it('counts the rules that go with it, because the server deletes them', async () => {
    server.on('GET /api/mail-watch/rules', () => ({
      body: [
        rule({ id: '1', label_id: 'a' }),
        rule({ id: '2', label_id: 'a' }),
        rule({ id: '3', label_id: 'b' }),
      ],
    }));
    renderWatch(<LabelsPanel />);
    await waitFor(() => {
      expect(server.calls.some((call) => call.path.endsWith('/rules'))).toBe(true);
    });
    const dialog = await askToRemove();
    expect(await within(dialog).findByText('The 2 rules that use it go with it.')).toBeTruthy();
  });

  it('says it in the singular', async () => {
    server.on('GET /api/mail-watch/rules', () => ({ body: [rule({ label_id: 'a' })] }));
    renderWatch(<LabelsPanel />);
    await waitFor(() => {
      expect(server.calls.some((call) => call.path.endsWith('/rules'))).toBe(true);
    });
    const dialog = await askToRemove();
    expect(await within(dialog).findByText('The rule that uses it goes with it.')).toBeTruthy();
  });

  it('removes it, and reads the labels and the rules again', async () => {
    server.on(`DELETE ${LABELS}/a`, () => {
      server.on(`GET ${LABELS}`, () => ({ body: [] }));
      return { body: { removed: true } };
    });
    renderWatch(<LabelsPanel />);
    const dialog = await askToRemove();
    const before = server.calls.filter((call) => call.path.endsWith('/rules')).length;
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove label' }));
    expect(await screen.findByText(/No labels yet/)).toBeTruthy();
    expect(server.calls.filter((call) => call.path.endsWith('/rules')).length).toBeGreaterThan(
      before,
    );
  });

  it('keeps it when the person changes their mind', async () => {
    renderWatch(<LabelsPanel />);
    const dialog = await askToRemove();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Keep it' }));
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    expect(server.writes('DELETE', `${LABELS}/a`)).toEqual([]);
  });

  it('says why a removal failed', async () => {
    server.on(`DELETE ${LABELS}/a`, () => ({ status: 500, body: { error: 'mail_watch_failed' } }));
    renderWatch(<LabelsPanel />);
    const dialog = await askToRemove();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove label' }));
    expect((await within(dialog).findByRole('alert')).textContent).toMatch(/went wrong/);
  });
});
