import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import { ProfilePanel } from '../ProfilePanel';
import type { ProfileDoc } from '../profileApi';

// What matters here is the one thing onboarding could never do: change an answer, and remove
// one. Everything the panel writes rides the agent's always-block on every turn, so a veto
// that cannot be withdrawn is a rule the operator is stuck with.

function storedProfile(): ProfileDoc {
  return {
    name: 'Davide',
    role: 'programmatore',
    company: 'Aura',
    location: 'Piacenza',
    timezone: 'Europe/Rome',
    lang: 'it',
    tonePreference: 'diretto',
    responseLength: 'breve',
    customInstructions: 'cita le fonti',
    expertise: ['Go', 'Postgres'],
    stack: [],
    projects: [],
    goals: [],
    interests: [],
    people: [],
    vetoes: ['non scrivere email al mio posto'],
  };
}

function stubProfileFetch(initial: ProfileDoc | { readonly status: number }) {
  const put = vi.fn();
  vi.stubGlobal(
    'fetch',
    vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
      const method = (init?.method ?? 'GET').toUpperCase();
      if (method === 'PUT') {
        const body = JSON.parse(typeof init?.body === 'string' ? init.body : '{}') as ProfileDoc;
        put(body);
        return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }));
      }
      if ('status' in initial) {
        return Promise.resolve(new Response('{}', { status: initial.status }));
      }
      return Promise.resolve(new Response(JSON.stringify(initial), { status: 200 }));
    }),
  );
  return put;
}

// firstPut reads the body of the first PUT. The index access is guarded here once rather
// than asserted at four call sites.
function firstPut(put: ReturnType<typeof vi.fn>): ProfileDoc {
  const call = put.mock.calls[0];
  if (call === undefined) throw new Error('no PUT was sent');
  return call[0] as ProfileDoc;
}

describe('ProfilePanel', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders the stored profile so it can be revised', async () => {
    stubProfileFetch(storedProfile());
    render(<ProfilePanel />);

    expect(await screen.findByDisplayValue('Davide')).toBeTruthy();
    expect(screen.getByDisplayValue('Europe/Rome')).toBeTruthy();
    // The lists arrive as one comma-separated line each.
    expect(screen.getByDisplayValue('Go, Postgres')).toBeTruthy();
    expect(screen.getByDisplayValue('non scrivere email al mio posto')).toBeTruthy();
  });

  it('sends a cleared field as empty, so an answer can actually be removed', async () => {
    const put = stubProfileFetch(storedProfile());
    render(<ProfilePanel />);

    const company = await screen.findByDisplayValue('Aura');
    fireEvent.change(company, { target: { value: '' } });
    const vetoes = screen.getByDisplayValue('non scrivere email al mio posto');
    fireEvent.change(vetoes, { target: { value: '' } });

    fireEvent.click(screen.getByRole('button', { name: 'Save profile' }));

    await waitFor(() => {
      expect(put).toHaveBeenCalledTimes(1);
    });
    const sent = firstPut(put);
    expect(sent.company).toBe('');
    expect(sent.vetoes).toEqual([]);
    expect(sent.name).toBe('Davide');
    expect(await screen.findByText('Profile saved.')).toBeTruthy();
  });

  it('splits a comma-separated list and drops the blanks', async () => {
    const put = stubProfileFetch(storedProfile());
    render(<ProfilePanel />);

    const expertise = await screen.findByDisplayValue('Go, Postgres');
    fireEvent.change(expertise, { target: { value: 'Go, , ArcadeDB ,' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save profile' }));

    await waitFor(() => {
      expect(put).toHaveBeenCalledTimes(1);
    });
    expect(firstPut(put).expertise).toEqual(['Go', 'ArcadeDB']);
  });

  it('offers a retry when the profile cannot be loaded', async () => {
    stubProfileFetch({ status: 502 });
    render(<ProfilePanel />);

    expect(await screen.findByRole('button', { name: 'Retry' })).toBeTruthy();
  });

  it('surfaces the server message when a save is refused', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        if ((init?.method ?? 'GET').toUpperCase() === 'PUT') {
          return Promise.resolve(
            new Response(JSON.stringify({ error: 'profile: field too long' }), { status: 400 }),
          );
        }
        return Promise.resolve(new Response(JSON.stringify(storedProfile()), { status: 200 }));
      }),
    );
    render(<ProfilePanel />);

    fireEvent.click(await screen.findByRole('button', { name: 'Save profile' }));

    expect(await screen.findByText(/profile: field too long/)).toBeTruthy();
  });
});
