import { afterEach, describe, expect, it, vi } from 'vitest';
import { registerComposerAttach } from '../composer/composerAttach';
import { IDENTITY_SCOPED } from './renderers/assetSourceContext';
import { composerSource } from './composerSource';

// A chat artifact edited in a Craft app goes into the next message as an attachment (the
// operator's decision of 2026-10-27): the original stays, and the agent sees the edit the way
// it sees any file the operator sends.

let close = () => undefined;

afterEach(() => {
  close();
});

function composer() {
  const attach = vi.fn<(file: File) => Promise<void>>(() => Promise.resolve());
  const unregister = registerComposerAttach(attach);
  close = () => {
    unregister();
  };
  return attach;
}

describe('composerSource', () => {
  it('reads like the identity tier and saves into the message', () => {
    const source = composerSource({ file_name: 'generated.png', mime_type: 'image/png' });
    expect(source.assetUrl('a')).toBe(IDENTITY_SCOPED.assetUrl('a'));
    expect(source.streamUrl('a')).toBe(IDENTITY_SCOPED.streamUrl('a'));
    expect(source.savesInto).toBe('message');
  });

  it('attaches an edit saved under the same name with the original type', async () => {
    const attach = composer();
    const source = composerSource({ file_name: 'generated.png', mime_type: 'image/png' });
    await source.save?.('a', new ArrayBuffer(4));
    const file = attach.mock.calls[0]?.[0];
    expect(file?.name).toBe('generated.png');
    expect(file?.type).toBe('image/png');
    expect(file?.size).toBe(4);
  });

  it('attaches a Save As by its own name, typed only where the extension is kept', async () => {
    const attach = composer();
    const source = composerSource({ file_name: 'clip.mp4', mime_type: 'video/mp4' });
    await source.save?.('a', new ArrayBuffer(2), 'edited.mp4');
    await source.save?.('a', new ArrayBuffer(2), 'Untitled.fcproj');
    expect(attach.mock.calls.map(([file]) => [file.name, file.type])).toEqual([
      ['edited.mp4', 'video/mp4'],
      ['Untitled.fcproj', ''],
    ]);
  });
});
