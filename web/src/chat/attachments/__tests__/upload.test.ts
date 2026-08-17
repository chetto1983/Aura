import { describe, expect, it } from 'vitest';
import { attachmentPollDelayMs, isReadyAsset, isTerminalAsset } from '../upload';
import type { Asset, AssetStatus } from '../types';

function asset(status: AssetStatus): Asset {
  return {
    id: 'asset-1',
    status,
    modality: 'document',
    file_name: 'note.txt',
    mime_type: 'text/plain',
    declared_size_bytes: 4,
    size_bytes: 4,
  };
}

describe('isReadyAsset', () => {
  // The old rule waited for 'searchable', which internal/assets/context.go measured as never
  // arriving on this deployment (2026-08-13: searchable 0). An attachment that can never be
  // ready is a turn that can never be sent, which is what the operator hit on 2026-08-17.
  it('treats a server-accepted asset as attachable without waiting for indexing', () => {
    expect(isReadyAsset(asset('accepted'))).toBe(true);
    expect(isReadyAsset(asset('processing'))).toBe(true);
  });

  it('still accepts the indexed states, for the day the pipeline sets them again', () => {
    expect(isReadyAsset(asset('searchable'))).toBe(true);
    expect(isReadyAsset(asset('embedding'))).toBe(true);
    expect(isReadyAsset(asset('complete'))).toBe(true);
  });

  it('does not call an asset ready before the server has decided', () => {
    expect(isReadyAsset(asset('created'))).toBe(false);
    expect(isReadyAsset(asset('presigned'))).toBe(false);
    expect(isReadyAsset(asset('uploaded'))).toBe(false);
  });

  it('never calls a refused or failed asset ready', () => {
    for (const status of ['failed', 'refused', 'deleted', 'canceled'] as const) {
      expect(isReadyAsset(asset(status))).toBe(false);
      expect(isTerminalAsset(asset(status))).toBe(true);
    }
  });
});

describe('attachmentPollDelayMs', () => {
  it('backs off as the wait grows', () => {
    expect(attachmentPollDelayMs(0)).toBe(500);
    expect(attachmentPollDelayMs(10_000)).toBe(1_500);
    expect(attachmentPollDelayMs(120_000)).toBe(5_000);
  });
});
