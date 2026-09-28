import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../../i18n/i18n';
import { AssetSourceContext, type AssetSource } from './assetSourceContext';
import AudioPreview from './AudioPreview';

const props = { assetId: 'a/b?c', mimeType: 'audio/mpeg', fileName: 'recording.mp3' };

describe('AudioPreview', () => {
  it('uses the owned stream route without autoplay and shows a useful error', () => {
    const { container } = render(<AudioPreview {...props} />);
    const audio = container.querySelector('audio');
    if (audio === null) throw new Error('audio player did not render');
    expect(audio?.src).toBe(`${location.origin}/api/assets/a%2Fb%3Fc/stream`);
    expect(audio?.hasAttribute('autoplay')).toBe(false);
    expect(screen.getByRole('button', { name: 'Play recording.mp3' })).toBeTruthy();
    fireEvent.error(audio);
    expect(screen.getByRole('alert').textContent).toContain("Couldn't load this preview.");
  });

  it('uses the share-scoped stream route without fetching the asset body', () => {
    const source: AssetSource = {
      assetUrl: (id) => `/s/token/asset/${encodeURIComponent(id)}`,
      streamUrl: (id) => `/s/token/asset/${encodeURIComponent(id)}/stream`,
      credentials: 'omit',
    };
    const { container } = render(
      <AssetSourceContext.Provider value={source}>
        <AudioPreview {...props} />
      </AssetSourceContext.Provider>,
    );
    expect(container.querySelector('audio')?.src).toBe(
      `${location.origin}/s/token/asset/a%2Fb%3Fc/stream`,
    );
  });

  it('seeks twice using loaded media duration without starting playback', () => {
    const { container } = render(<AudioPreview {...props} />);
    const audio = container.querySelector('audio');
    if (audio === null) throw new Error('audio player did not render');
    Object.defineProperty(audio, 'duration', { configurable: true, value: 120 });
    fireEvent.loadedMetadata(audio);
    const seek = screen.getByRole('slider', { name: 'Seek' }) as HTMLInputElement;
    fireEvent.change(seek, { target: { value: '15' } });
    expect(audio.currentTime).toBe(15);
    fireEvent.change(seek, { target: { value: '65' } });
    expect(audio.currentTime).toBe(65);
    expect(seek.value).toBe('65');
    expect(audio.paused).toBe(true);
  });

  it('refuses a stream URL outside the active origin', () => {
    const source: AssetSource = {
      assetUrl: () => '/safe/download',
      streamUrl: () => 'https://elsewhere.example/recording.mp3',
      credentials: 'omit',
    };
    const { container } = render(
      <AssetSourceContext.Provider value={source}>
        <AudioPreview {...props} />
      </AssetSourceContext.Provider>,
    );
    expect(container.querySelector('audio')).toBeNull();
    expect(screen.getByRole('alert')).toBeTruthy();
  });
});
