import { act, fireEvent, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mountPage, openedOnVideo, posts, prompt, stubServer, viewport } from './studioPageHarness';

// The composer half of the page, against a stubbed server: what the catalog decides the bar
// opens on, what each mode keeps across a switch, the exact body posted, and what is said
// when the server refuses. The read-side states live in StudioWorkspace.reads.test.tsx.

const realMatchMedia = window.matchMedia;

beforeEach(() => {
  localStorage.clear();
  viewport(true);
});

afterEach(() => {
  window.matchMedia = realMatchMedia;
  vi.unstubAllGlobals();
});

describe('StudioWorkspace composing', () => {
  it('opens on the headline, the deployment video model and its cheapest estimate', async () => {
    stubServer();
    mountPage();
    await openedOnVideo();

    expect(screen.getByRole('heading', { name: 'Bring your idea to life' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: 'Model' }).textContent).toContain('Veo 3.1 Lite');
    expect(screen.getByRole('button', { name: 'Options' }).textContent).toBe('16:9 · 720p · 4s');
    // 0.0297/s silent at 720p over the shortest declared clip.
    expect(screen.getByText('≈ $0.12')).toBeTruthy();
  });

  it('keeps the prompt across a mode switch and reprices with the image catalog', async () => {
    stubServer();
    mountPage();
    await openedOnVideo();
    fireEvent.change(prompt(), { target: { value: 'a harbour at dawn' } });

    fireEvent.click(screen.getByRole('radio', { name: 'Image' }));
    await screen.findByPlaceholderText('Describe the image you want to generate');

    expect((prompt() as HTMLTextAreaElement).value).toBe('a harbour at dawn');
    expect(screen.getByRole('combobox', { name: 'Model' }).textContent).toContain(
      'GPT Image 1 mini',
    );
    // The image model declares no duration and neither sound nor seed.
    expect(screen.getByRole('button', { name: 'Options' }).textContent).toBe('3:2');
    expect(screen.queryByRole('button', { name: 'Advanced' })).toBeNull();
    expect(screen.getByText('≈ $0.05')).toBeTruthy();
  });

  it('keeps each mode’s own options across a round trip, prompt edit or not', async () => {
    stubServer();
    mountPage();
    await openedOnVideo();

    // Move the video draft off its cheapest defaults.
    fireEvent.click(screen.getByRole('button', { name: 'Options' }));
    fireEvent.click(screen.getByRole('radio', { name: '1080p' }));
    fireEvent.keyDown(document.body, { key: 'Escape' });
    expect(screen.getByRole('button', { name: 'Options' }).textContent).toBe('16:9 · 1080p · 4s');

    fireEvent.click(screen.getByRole('radio', { name: 'Image' }));
    await screen.findByPlaceholderText('Describe the image you want to generate');
    // Editing the prompt in image mode writes the image half — and must not reach the video
    // half, which is the bug this pins: a prompt edit changing unrelated mode state.
    fireEvent.change(prompt(), { target: { value: 'a quiet room' } });

    fireEvent.click(screen.getByRole('radio', { name: 'Video' }));
    await screen.findByPlaceholderText('Describe the video scene you want to generate');
    expect(screen.getByRole('button', { name: 'Options' }).textContent).toBe('16:9 · 1080p · 4s');
    // The prompt is deliberately shared, as A1 says a mode switch keeps it.
    expect((prompt() as HTMLTextAreaElement).value).toBe('a quiet room');
  });

  it('survives the same round trip with nothing typed in the other mode', async () => {
    stubServer();
    mountPage();
    await openedOnVideo();
    fireEvent.click(screen.getByRole('button', { name: 'Options' }));
    fireEvent.click(screen.getByRole('radio', { name: '9:16' }));
    fireEvent.keyDown(document.body, { key: 'Escape' });

    fireEvent.click(screen.getByRole('radio', { name: 'Image' }));
    await screen.findByPlaceholderText('Describe the image you want to generate');
    fireEvent.click(screen.getByRole('radio', { name: 'Video' }));
    await screen.findByPlaceholderText('Describe the video scene you want to generate');

    expect(screen.getByRole('button', { name: 'Options' }).textContent).toBe('9:16 · 720p · 4s');
  });

  it('posts one video with exactly the options the pills read, and refetches the history', async () => {
    const calls = stubServer();
    mountPage();
    await openedOnVideo();
    fireEvent.change(prompt(), { target: { value: 'a harbour at dawn' } });
    fireEvent.keyDown(prompt(), { key: 'Enter', ctrlKey: true });

    await waitFor(() => {
      expect(posts(calls)).toHaveLength(1);
    });
    const post = posts(calls)[0];
    expect(post?.url).toBe('/api/studio/videos');
    expect(post?.body).toEqual({
      model: 'google/veo-3.1-lite',
      prompt: 'a harbour at dawn',
      duration: 4,
      resolution: '720p',
      aspect_ratio: '16:9',
      audio: false,
    });

    // The accepted record makes every history list stale, so the panel re-reads.
    await waitFor(() => {
      expect(
        calls.filter((call) => call.url.startsWith('/api/studio/history')).length,
      ).toBeGreaterThan(1);
    });
  });

  it('holds Generate shut while the request is in flight, so a clip is not bought twice', async () => {
    let release = (): void => undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const calls = stubServer({ createGate: gate });
    mountPage();
    await openedOnVideo();
    fireEvent.change(prompt(), { target: { value: 'a harbour at dawn' } });
    fireEvent.keyDown(prompt(), { key: 'Enter', ctrlKey: true });

    const sending = await screen.findByRole('button', { name: /Sending/ });
    expect(sending.hasAttribute('disabled')).toBe(true);
    // The shortcut is shut too, not merely the button: the same request must not be sent
    // twice while the first one is still open.
    fireEvent.keyDown(prompt(), { key: 'Enter', ctrlKey: true });
    expect(posts(calls)).toHaveLength(1);

    release();
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Generate/ }).hasAttribute('disabled')).toBe(false);
    });
    expect(posts(calls)).toHaveLength(1);
  });

  it('posts once when the shortcut fires twice inside one frame', async () => {
    const calls = stubServer();
    mountPage();
    await openedOnVideo();
    fireEvent.change(prompt(), { target: { value: 'a harbour at dawn' } });

    // Both events in ONE act, so React never re-renders between them and `submitting` is
    // still false on the second — exactly the window a render-lagged guard leaves open, and
    // exactly what a held key or a double tap does in a browser. fireEvent would flush
    // between the two and never open it. Every headerless POST gets its own
    // Idempotency-Key, so a second one here is a second paid generation.
    const box = prompt();
    act(() => {
      for (let press = 0; press < 3; press += 1) {
        box.dispatchEvent(
          new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, bubbles: true }),
        );
      }
    });

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Generate/ })).toBeTruthy();
    });
    expect(posts(calls)).toHaveLength(1);
  });

  it('reopens Generate after a refusal, so a fixable request can be sent again', async () => {
    const calls = stubServer({
      createFailure: { status: 409, code: 'no_key', error: 'no credential' },
    });
    mountPage();
    await openedOnVideo();
    fireEvent.change(prompt(), { target: { value: 'a harbour at dawn' } });
    fireEvent.keyDown(prompt(), { key: 'Enter', ctrlKey: true });
    await screen.findByRole('alert');

    // The in-flight lock must clear on a refusal too, or one 409 bricks the page.
    fireEvent.keyDown(prompt(), { key: 'Enter', ctrlKey: true });
    await waitFor(() => {
      expect(posts(calls)).toHaveLength(2);
    });
  });

  it('posts an image with the reference it was given', async () => {
    const calls = stubServer();
    mountPage();
    await openedOnVideo();
    fireEvent.click(screen.getByRole('radio', { name: 'Image' }));
    await screen.findByPlaceholderText('Describe the image you want to generate');

    // Attach a reference from the identity's own library.
    fireEvent.keyDown(screen.getByRole('button', { name: 'References' }), { key: 'Enter' });
    fireEvent.click(screen.getByRole('menuitem', { name: 'Your images' }));
    fireEvent.click(await screen.findByRole('button', { name: 'moodboard.png' }));

    fireEvent.change(prompt(), { target: { value: 'a quiet room' } });
    fireEvent.keyDown(prompt(), { key: 'Enter', ctrlKey: true });

    await waitFor(() => {
      expect(posts(calls)).toHaveLength(1);
    });
    expect(posts(calls)[0]?.url).toBe('/api/studio/images');
    expect(posts(calls)[0]?.body).toEqual({
      model: 'openai/gpt-image-1-mini',
      prompt: 'a quiet room',
      aspect_ratio: '3:2',
      reference_asset_ids: ['asset-ref'],
    });
  });

  it('shows the localized sentence when the server refuses the generation', async () => {
    stubServer({
      createFailure: { status: 409, code: 'no_key', error: 'openrouter credential missing' },
    });
    mountPage();
    await openedOnVideo();
    fireEvent.change(prompt(), { target: { value: 'a harbour at dawn' } });
    fireEvent.keyDown(prompt(), { key: 'Enter', ctrlKey: true });

    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('This deployment has no OpenRouter key');
    // The server's own wording is not what an operator is shown for a code Aura knows.
    expect(alert.textContent).not.toContain('credential missing');
  });

  it('posts nothing at all while the prompt is empty', async () => {
    const calls = stubServer();
    mountPage();
    await openedOnVideo();

    expect(screen.getByRole('button', { name: /Generate/ }).hasAttribute('disabled')).toBe(true);
    fireEvent.keyDown(prompt(), { key: 'Enter', ctrlKey: true });
    fireEvent.click(screen.getByRole('button', { name: /Generate/ }));
    await waitFor(() => {
      expect(screen.getByText('≈ $0.12')).toBeTruthy();
    });
    expect(posts(calls)).toHaveLength(0);
  });

  it('remembers the chosen model per kind, and survives a localStorage that throws', async () => {
    stubServer();
    const first = mountPage();
    await openedOnVideo();
    fireEvent.click(screen.getByRole('combobox', { name: 'Model' }));
    fireEvent.click(screen.getByRole('option', { name: /Hailuo 2.3/ }));
    await waitFor(() => {
      expect(screen.getByRole('combobox', { name: 'Model' }).textContent).toContain('Hailuo 2.3');
    });
    expect(localStorage.getItem('aura.studio.model.video')).toBe('minimax/hailuo-2.3');
    first.unmount();

    const second = mountPage();
    await openedOnVideo();
    // The remembered model, not the deployment default.
    expect(screen.getByRole('combobox', { name: 'Model' }).textContent).toContain('Hailuo 2.3');
    second.unmount();

    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    mountPage();
    await openedOnVideo();
    fireEvent.click(screen.getByRole('combobox', { name: 'Model' }));
    fireEvent.click(screen.getByRole('option', { name: /Hailuo 2.3/ }));
    await waitFor(() => {
      expect(screen.getByRole('combobox', { name: 'Model' }).textContent).toContain('Hailuo 2.3');
    });
    vi.restoreAllMocks();
  });

  it('explains a deployment the Studio cannot run on, and offers no bar', async () => {
    stubServer({
      modelsFailure: {
        status: 409,
        code: 'local_route',
        error: 'the deployment routes models locally',
      },
    });
    mountPage();

    expect(await screen.findByText(/routes its models locally/)).toBeTruthy();
    expect(screen.queryByRole('textbox', { name: 'Prompt' })).toBeNull();
    expect(screen.queryByRole('button', { name: /Generate/ })).toBeNull();
  });

  it('says a catalog with no model for this kind is empty, rather than showing a bare page', async () => {
    stubServer({ emptyCatalog: true });
    mountPage();

    expect(await screen.findByText('No model is available for this kind.')).toBeTruthy();
    expect(screen.queryByRole('textbox', { name: 'Prompt' })).toBeNull();
  });
});
