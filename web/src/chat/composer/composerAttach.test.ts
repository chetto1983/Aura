import { describe, expect, it, vi } from 'vitest';
import { attachToComposer, registerComposerAttach } from './composerAttach';

// The Artifacts panel's preview is mounted by the shell, beside the chat's runtime rather than
// inside it: this channel is how a file edited there still reaches the chat composer.

describe('composerAttach', () => {
  it('refuses when no chat composer is open', async () => {
    await expect(attachToComposer(new File(['x'], 'a.png'))).rejects.toThrow(/composer/);
  });

  it('hands the file to the open composer, and refuses again once it closes', async () => {
    const attach = vi.fn(() => Promise.resolve());
    const close = registerComposerAttach(attach);
    const file = new File(['x'], 'a.png');
    await attachToComposer(file);
    expect(attach).toHaveBeenCalledWith(file);
    close();
    await expect(attachToComposer(file)).rejects.toThrow(/composer/);
  });

  it('keeps the newer composer when an older one closes late', async () => {
    const closeOlder = registerComposerAttach(vi.fn(() => Promise.resolve()));
    const newer = vi.fn(() => Promise.resolve());
    const closeNewer = registerComposerAttach(newer);
    closeOlder();
    await attachToComposer(new File(['x'], 'a.png'));
    expect(newer).toHaveBeenCalledOnce();
    closeNewer();
  });
});
