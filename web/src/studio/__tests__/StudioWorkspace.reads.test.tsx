import { fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  mountPage,
  openedOnVideo,
  prompt,
  stubServer,
  viewport,
  NO_INPUTS,
  WITH_INPUTS,
} from './studioPageHarness';

// The read half of the page: what Reuse may do before the library has answered, and what the
// history says while it is loading or after it was refused. Every one of these is a state
// that `data ?? []` used to flatten into "there is nothing".

const realMatchMedia = window.matchMedia;

beforeEach(() => {
  localStorage.clear();
  viewport(true);
});

afterEach(() => {
  window.matchMedia = realMatchMedia;
  vi.unstubAllGlobals();
});

describe('StudioWorkspace reading', () => {
  it('will not reuse a record with inputs until the library has answered', async () => {
    let release = (): void => undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    stubServer({ history: [WITH_INPUTS], libraryGate: gate });
    mountPage();
    await openedOnVideo();

    // Clicking now would resolve nothing and hand back a cheaper request than the one shown.
    expect(screen.getByRole('button', { name: /Reuse/ }).hasAttribute('disabled')).toBe(true);
    release();
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Reuse/ }).hasAttribute('disabled')).toBe(false);
    });
  });

  it('refuses to reuse a record with inputs when the library cannot be read', async () => {
    stubServer({ history: [WITH_INPUTS], libraryFails: true });
    mountPage();
    await openedOnVideo();

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Reuse/ }).getAttribute('title')).toBe(
        'Your images could not be read, so this cannot be reused yet.',
      );
    });
    expect(screen.getByRole('button', { name: /Reuse/ }).hasAttribute('disabled')).toBe(true);
  });

  it('reuses a record with no inputs even while the library is unavailable', async () => {
    stubServer({ history: [NO_INPUTS], libraryFails: true });
    mountPage();
    await openedOnVideo();

    // Nothing to resolve, so the library's state is irrelevant to this record.
    expect(screen.getByRole('button', { name: /Reuse/ }).hasAttribute('disabled')).toBe(false);
  });

  it('says so when a reused record names an image the library no longer lists', async () => {
    // The query SUCCEEDED and the id is not in it: the asset is deleted, which is a different
    // fact from "the library could not be read" and has to be said out loud.
    stubServer({ history: [WITH_INPUTS], library: [] });
    mountPage();
    await openedOnVideo();
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Reuse/ }).hasAttribute('disabled')).toBe(false);
    });
    fireEvent.click(screen.getByRole('button', { name: /Reuse/ }));

    await screen.findByPlaceholderText('Describe the image you want to generate');
    expect(screen.getByText(/no longer available/)).toBeTruthy();
    expect((prompt() as HTMLTextAreaElement).value).toBe('a reference-driven still');
    // The reference is genuinely gone, not silently pretended: no tile carries it.
    expect(screen.queryByRole('button', { name: 'Remove moodboard.png' })).toBeNull();
  });

  it('reuses the inputs it can resolve, with no complaint', async () => {
    stubServer({ history: [WITH_INPUTS] });
    mountPage();
    await openedOnVideo();
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Reuse/ }).hasAttribute('disabled')).toBe(false);
    });
    fireEvent.click(screen.getByRole('button', { name: /Reuse/ }));

    await screen.findByPlaceholderText('Describe the image you want to generate');
    expect(screen.getByRole('button', { name: 'Remove moodboard.png' })).toBeTruthy();
    expect(screen.queryByText(/no longer available/)).toBeNull();
  });

  it('does not call the history empty while it is still being read', async () => {
    let release = (): void => undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    stubServer({ historyGate: gate });
    mountPage();

    expect(await screen.findByText('Reading your generations…')).toBeTruthy();
    expect(screen.queryByText('Nothing generated yet.')).toBeNull();
    release();
    expect(await screen.findByText('Nothing generated yet.')).toBeTruthy();
  });

  it('says why the history could not be read instead of looking empty', async () => {
    stubServer({ historyFails: true });
    mountPage();
    await openedOnVideo();

    // The refusal the strict-envelope read raises reaches the panel as an alert; it is not
    // swallowed into "Nothing generated yet", which would be a claim nobody checked.
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toBe('the history could not be read');
    expect(screen.queryByText('Nothing generated yet.')).toBeNull();
  });

  it('shows the newest generation by default and follows a card that is clicked', async () => {
    stubServer({
      history: [
        {
          id: 'job-new',
          kind: 'image',
          status: 'completed',
          model: 'openai/gpt-image-1-mini',
          prompt: 'the newest',
          used: { aspect_ratio: '1:1' },
          cost_usd: 0.05,
          asset_id: 'asset-new',
          created_at: '2026-09-17T12:00:00Z',
        },
        {
          id: 'job-old',
          kind: 'video',
          status: 'failed',
          model: 'google/veo-3.1-lite',
          prompt: 'the older one',
          used: {},
          created_at: '2026-09-17T09:00:00Z',
          error: { code: 'no_credit', message: 'provider answered 402' },
        },
      ],
    });
    mountPage();
    await openedOnVideo();

    // The newest row is on the stage without anything being clicked.
    expect(screen.getByRole('link', { name: /Download/ }).getAttribute('href')).toBe(
      '/api/assets/asset-new/download',
    );
    fireEvent.click(screen.getByRole('button', { name: /the older one/ }));
    expect(screen.getByRole('alert').textContent).toContain(
      'The OpenRouter account is out of credit.',
    );
    expect(screen.queryByRole('link', { name: /Download/ })).toBeNull();
  });
});

describe('the way back into the video editor', () => {
  const COMPLETED_VIDEO = {
    id: 'job-clip',
    kind: 'video',
    status: 'completed',
    model: 'google/veo-3.1-lite',
    prompt: 'a harbour at dawn',
    used: { aspect_ratio: '16:9' },
    cost_usd: 0.05,
    asset_id: 'asset-clip',
    created_at: '2026-09-17T12:00:00Z',
  };

  it('offers the selected clip to the multi-track editor', async () => {
    stubServer({ history: [COMPLETED_VIDEO] });
    mountPage();
    await openedOnVideo();

    expect(screen.getByRole('button', { name: 'Open in the video editor' })).toBeTruthy();
  });

  it('offers the last project saved here, restored from storage on mount', async () => {
    // The state alone made this a door that closed behind you: it vanished on the reload that
    // often follows a save. This mount is that reload.
    localStorage.setItem('aura.videoStudio.lastSavedProject', 'file-7');
    stubServer({ history: [COMPLETED_VIDEO] });
    mountPage();
    await openedOnVideo();

    expect(
      screen.getByRole('button', { name: 'Reopen the last project you saved here' }),
    ).toBeTruthy();
  });

  it('offers nothing to reopen when nothing has been saved here', async () => {
    stubServer({ history: [COMPLETED_VIDEO] });
    mountPage();
    await openedOnVideo();

    expect(
      screen.queryByRole('button', { name: 'Reopen the last project you saved here' }),
    ).toBeNull();
  });
});
