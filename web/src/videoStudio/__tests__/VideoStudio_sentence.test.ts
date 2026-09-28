import { describe, expect, it } from 'vitest';
import { CommandRefusal } from '../commands';
import { failure, says } from '../VideoStudio_sentence';

// What the workspace's status line says about an error: a refusal in its own words, anything else
// in the words of whoever was attempting it, with the message it came with.

describe('failure', () => {
  it('lets a refusal speak for itself', () => {
    expect(failure(new CommandRefusal('videoStudio.refusal.trimPastSource'), 'fallback')).toEqual(
      says('videoStudio.refusal.trimPastSource'),
    );
  });

  it('words any other error by the attempt, and carries its message', () => {
    expect(failure(new Error('network down'), 'videoStudio.save.failed')).toEqual({
      key: 'videoStudio.save.failed',
      values: { reason: 'network down' },
    });
    expect(failure('plain', 'videoStudio.save.failed').values).toEqual({ reason: 'plain' });
  });
});
