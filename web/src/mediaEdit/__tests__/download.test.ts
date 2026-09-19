import { afterEach, describe, expect, it, vi } from 'vitest';
import { downloadBlob } from '../download';

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe('downloadBlob', () => {
  it('clicks a download link and revokes its URL afterwards', () => {
    vi.useFakeTimers();
    const create = vi.fn(() => 'blob:edited');
    const revoke = vi.fn();
    // jsdom has no object URLs; assign them rather than replacing the URL class jsdom uses.
    Object.assign(URL, { createObjectURL: create, revokeObjectURL: revoke });
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, 'click')
      .mockImplementation(() => undefined);
    downloadBlob(new Blob(['x']), 'clip-edited.mp4');
    expect(click).toHaveBeenCalledTimes(1);
    const anchor = click.mock.contexts[0] as HTMLAnchorElement;
    expect(anchor.download).toBe('clip-edited.mp4');
    expect(anchor.href).toBe('blob:edited');
    expect(revoke).not.toHaveBeenCalled();
    vi.runAllTimers();
    expect(revoke).toHaveBeenCalledWith('blob:edited');
  });
});
