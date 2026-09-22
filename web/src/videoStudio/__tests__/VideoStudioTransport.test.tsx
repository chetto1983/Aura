import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import i18n from '../../i18n/i18n';
import { VideoStudioTransport } from '../VideoStudioTransport';

function mount(playing = false) {
  const onPlayToggle = vi.fn();
  const onSeek = vi.fn();
  render(
    <VideoStudioTransport
      playing={playing}
      time={1.5}
      duration={4}
      onPlayToggle={onPlayToggle}
      onSeek={onSeek}
    />,
  );
  return { onPlayToggle, onSeek };
}

describe('VideoStudioTransport', () => {
  it('plays and exposes the current project time', () => {
    const { onPlayToggle } = mount();
    expect(screen.getByText('00:01.5', { exact: false })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: i18n.t('videoStudio.transport.play') }));
    expect(onPlayToggle).toHaveBeenCalledOnce();
  });

  it('seeks to either edge and names the paused state', () => {
    const { onPlayToggle, onSeek } = mount(true);
    fireEvent.click(screen.getByRole('button', { name: i18n.t('videoStudio.transport.start') }));
    fireEvent.click(screen.getByRole('button', { name: i18n.t('videoStudio.transport.end') }));
    fireEvent.click(screen.getByRole('button', { name: i18n.t('videoStudio.transport.pause') }));
    expect(onSeek.mock.calls).toEqual([[0], [4]]);
    expect(onPlayToggle).toHaveBeenCalledOnce();
  });
});
