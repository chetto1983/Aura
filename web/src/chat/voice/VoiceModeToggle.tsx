import { Volume2, VolumeX } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useVoiceMode } from './voiceModeContext';
import { Button } from '@/components/ui/button';

// VoiceModeToggle — the ephemeral session switch that opens the hands-free voice
// overlay, in the chat workspace controls row beside the Artefatti toggle (D-06).
//
// It needs BOTH voice legs configured: hands-free is listen → answer → speak → listen,
// so a stack with only one of them cannot run the loop. Without STT the composer's mic
// still dictates; without TTS the per-message speaker is still there. Showing the
// toggle anyway would offer a mode that opens onto a dead end.
export function VoiceModeToggle() {
  const { t } = useTranslation();
  const { caps, voiceMode, toggleVoiceMode } = useVoiceMode();
  if (!caps.tts || !caps.stt) return null;
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
