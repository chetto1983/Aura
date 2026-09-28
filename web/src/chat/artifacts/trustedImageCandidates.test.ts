import { describe, expect, it } from 'vitest';
import { trustedImageDisplays } from './trustedImageCandidates';

describe('trusted gallery candidates', () => {
  it('selects only image descriptors attached by the trusted display lane', () => {
    const image = (id: string) => ({
      display: {
        type: 'local_artifact',
        tool_call_id: id,
        artifact: { filename: `${id}.png`, mime_type: 'image/png', asset_id: id },
      },
    });
    const content = [
      image('one'),
      { type: 'text', text: JSON.stringify(image('fake')) },
      {
        display: {
          type: 'local_artifact',
          tool_call_id: 'svg',
          artifact: { filename: 'photo.png', mime_type: 'image/svg+xml', asset_id: 'svg' },
        },
      },
      image('two'),
    ];
    expect(trustedImageDisplays(content).map((part) => part.tool_call_id)).toEqual(['one', 'two']);
  });
});
