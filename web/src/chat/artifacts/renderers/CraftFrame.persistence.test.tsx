import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import '@/i18n/i18n';
import CraftFrame from './CraftFrame';
import { AssetSourceContext, IDENTITY_SCOPED, type AssetSource } from './assetSourceContext';
import { PreviewCloseGuardContext } from './previewCloseGuardContext';

afterEach(() => vi.unstubAllGlobals());
function mount(save: NonNullable<AssetSource['save']>, source: Partial<AssetSource> = {}) {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => new Response(new Uint8Array([1, 2]))),
  );
  const setUnsaved = vi.fn();
  const view = render(
    <AssetSourceContext.Provider value={{ ...IDENTITY_SCOPED, ...source, save }}>
      <PreviewCloseGuardContext.Provider value={{ setUnsaved, held: false }}>
        <CraftFrame app="wordcraft" assetId="a" fileName="memo.doc" mimeType="" />
      </PreviewCloseGuardContext.Provider>
    </AssetSourceContext.Provider>,
  );
  const frame = view.container.querySelector('iframe');
  if (!frame) throw new Error('Missing native frame');
  const emit = (data: unknown) => {
    act(() => {
      window.dispatchEvent(
        new MessageEvent('message', { source: frame.contentWindow, origin: location.origin, data }),
      );
    });
  };
  const send = (bytes = new ArrayBuffer(3)) => {
    emit({ type: 'wordcraft:save', name: 'memo.docx', bytes });
    emit({ type: 'wordcraft:dirty', dirty: false });
  };
  return { ...view, setUnsaved, send };
}

it('guards pending and failed storage after native dirty=false and retries retained bytes', async () => {
  let reject!: (error: Error) => void;
  const save = vi
    .fn<NonNullable<AssetSource['save']>>()
    .mockImplementationOnce(
      () =>
        new Promise((_, fail) => {
          reject = fail;
        }),
    )
    .mockResolvedValue(undefined);
  const { send, setUnsaved } = mount(save);
  const bytes = new ArrayBuffer(3);
  send(bytes);
  await waitFor(() => {
    expect(setUnsaved).toHaveBeenLastCalledWith(true);
  });
  await act(async () => {
    reject(new Error('offline'));
    await Promise.resolve();
  });
  expect(setUnsaved).toHaveBeenLastCalledWith(true);
  fireEvent.click(await screen.findByRole('button', { name: /retry|riprova/i }));
  await waitFor(() => {
    expect(setUnsaved).toHaveBeenLastCalledWith(false);
  });
  expect(save.mock.calls[1]?.slice(0, 3)).toEqual(['a', bytes, 'memo.docx']);
  expect(screen.getByRole('status').textContent).toContain('memo.docx');
});

it('serializes repeated native saves and stops the queued continuation after unmount', async () => {
  let resolve!: () => void;
  const save = vi
    .fn<NonNullable<AssetSource['save']>>()
    .mockImplementationOnce(
      () =>
        new Promise<void>((done) => {
          resolve = done;
        }),
    )
    .mockResolvedValue(undefined);
  const { send, unmount } = mount(save);
  send();
  send(new ArrayBuffer(5));
  await waitFor(() => {
    expect(save).toHaveBeenCalledTimes(1);
  });
  const signal = save.mock.calls[0]?.[3];
  unmount();
  expect(signal?.aborted).toBe(true);
  await act(async () => {
    resolve();
    await Promise.resolve();
  });
  expect(save).toHaveBeenCalledTimes(1);
});

it('persists repeated saves in order and keeps the guard until the last acknowledgment', async () => {
  let finishFirst!: () => void;
  let finishSecond!: () => void;
  const first = new Promise<void>((resolve) => {
    finishFirst = resolve;
  });
  const second = new Promise<void>((resolve) => {
    finishSecond = resolve;
  });
  const save = vi
    .fn<NonNullable<AssetSource['save']>>()
    .mockReturnValueOnce(first)
    .mockReturnValueOnce(second);
  const { send, setUnsaved } = mount(save);
  const older = new ArrayBuffer(3);
  const newer = new ArrayBuffer(5);
  send(older);
  send(newer);
  expect(save).toHaveBeenCalledTimes(1);
  await act(async () => {
    finishFirst();
    await first;
  });
  expect(save.mock.calls.map((call) => call[1])).toEqual([older, newer]);
  expect(setUnsaved).toHaveBeenLastCalledWith(true);
  await act(async () => {
    finishSecond();
    await second;
  });
  await waitFor(() => {
    expect(setUnsaved).toHaveBeenLastCalledWith(false);
  });
});

it('says an edit saved into the chat went into the message', async () => {
  const save = vi.fn<NonNullable<AssetSource['save']>>().mockResolvedValue(undefined);
  const { send } = mount(save, { savesInto: 'message' });
  send();
  await waitFor(() => {
    expect(screen.getByRole('status').textContent).toMatch(
      /attached to your message as memo\.docx|allegato al messaggio come memo\.docx/i,
    );
  });
});
