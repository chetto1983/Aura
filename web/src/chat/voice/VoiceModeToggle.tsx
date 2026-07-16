import { Volume2, VolumeX } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useVoiceMode } from './voiceModeContext';
import { Button } from '@/components/ui/button';

// VoiceModeToggle — the ephemeral session voice-mode switch that lives in the chat
// workspace controls row beside the Artefatti toggle (D-06). Rendered only
// when TTS is configured (caps.tts, WEBVOICE-03); flips the in-memory voiceMode with
// aria-pressed reflecting state. Extracted from AppShell to keep it under the 600-LOC
// cap; matches the ArtifactsToggle shape (BLUE accent on active).
export function VoiceModeToggle() {
  const { t } = useTranslation();
  const { caps, voiceMode, toggleVoiceMode } = useVoiceMode();
  if (!caps.tts) return null;
  const label = t(voiceMode ? 'chat.voiceMode.on' : 'chat.voiceMode.off');
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      aria-pressed={voiceMode}
      aria-label={label}
      title={label}
      data-active={voiceMode}
      data-required-touch-target
      onClick={toggleVoiceMode}
      className="min-h-[44px] min-w-[44px] rounded-full bg-surface/70 text-text-muted backdrop-blur hover:bg-surface-2 hover:text-text data-[active=true]:bg-surface-2 data-[active=true]:text-accent-text"
    >
      {voiceMode ? (
        <Volume2 aria-hidden="true" focusable="false" />
      ) : (
        <VolumeX aria-hidden="true" focusable="false" />
      )}
    </Button>
  );
}
