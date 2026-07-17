import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../i18n/i18n';
import { VoiceModeContext, type VoiceModeState } from '../chat/voice/voiceModeContext';
import { ChatWorkspaceControls } from './ChatWorkspaceControls';

// ChatWorkspaceControls.test.tsx — a 37F plan 14 deviation test. The plan's own acceptance
// criteria describe the cluster order as "asserted by their line order in AppShell.tsx", written
// against the pre-3f687e456 floating-overlay cluster that lived directly in AppShell.tsx. That
// cluster is now this component (see ShareShell.tsx's placement note and 37F-14-SUMMARY.md), so
// the order assertion moves here instead — same locked intent
// ([VoiceModeToggle, ShareToggle, ArtifactsToggle]), corrected DOM location.

const VOICE_ENABLED: VoiceModeState = {
  caps: { tts: true, stt: false },
  voiceMode: false,
  turnWasDictated: false,
  toggleVoiceMode: () => undefined,
  markTurnDictated: () => undefined,
  clearTurnDictated: () => undefined,
};

function renderControls(onShareOpen = vi.fn(), onArtifactsToggle = vi.fn()) {
  return render(
    <VoiceModeContext.Provider value={VOICE_ENABLED}>
      <ChatWorkspaceControls
        artifactsActive={false}
        onArtifactsToggle={onArtifactsToggle}
        onShareOpen={onShareOpen}
      />
    </VoiceModeContext.Provider>,
  );
}

describe('ChatWorkspaceControls', () => {
  it('mounts VoiceModeToggle, ShareToggle, ArtifactsToggle in that DOM order', () => {
    const { container } = renderControls();
    const row = container.querySelector('[data-chat-workspace-controls]');
    expect(row).toBeTruthy();
    const buttons = [...(row?.querySelectorAll('button') ?? [])];
    expect(buttons.length).toBe(3);
    expect(buttons[0]?.getAttribute('aria-label')).toBe('Voice mode off');
    expect(buttons[1]?.getAttribute('aria-label')).toBe('Share conversation');
    expect(buttons[2]?.getAttribute('aria-label')).toBe('Toggle the artifacts panel');
  });

  it('wires onShareOpen to the ShareToggle click', () => {
    const onShareOpen = vi.fn();
    renderControls(onShareOpen);
    fireEvent.click(screen.getByRole('button', { name: 'Share conversation' }));
    expect(onShareOpen).toHaveBeenCalledTimes(1);
  });

  it('renders ShareToggle neutral (no share-list data source wired yet)', () => {
    renderControls();
    const button = screen.getByRole('button', { name: 'Share conversation' });
    expect(button.getAttribute('data-shared')).toBe('false');
  });
});
