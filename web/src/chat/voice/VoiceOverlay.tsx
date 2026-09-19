import { useTranslation } from 'react-i18next';
import { Square, X } from 'lucide-react';
import { VoiceOrb } from './VoiceOrb';
import { useVoiceMode } from './voiceModeContext';
import { useVoiceSession } from './useVoiceSession';
import type { VoiceErrorKey, VoicePhase } from './voiceSessionMachine';
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';

// VoiceOverlay — the hands-free voice surface: orb, one spoken-state line, the words
// that actually went to the model, and two controls. Mounted inside the runtime
// subtree (it drives the composer), rendered only while voice mode is on.
//
// What it is NOT: a second conversation. Every utterance it sends is an ordinary turn
// in the thread behind it, so closing the overlay mid-session leaves a normal chat
// with the spoken turns in it — nothing to reconcile, nothing that lives only here.

const PHASE_KEYS: Record<VoicePhase, string> = {
  idle: 'chat.voice.phase.opening',
  listening: 'chat.voice.phase.listening',
  transcribing: 'chat.voice.phase.transcribing',
  thinking: 'chat.voice.phase.thinking',
  speaking: 'chat.voice.phase.speaking',
  error: 'chat.voice.phase.error',
};

const ERROR_KEYS: Record<VoiceErrorKey, string> = {
  mic: 'chat.voice.error.mic',
  stt: 'chat.voice.error.stt',
  tts: 'chat.voice.error.tts',
};

export function VoiceOverlay() {
  const { t } = useTranslation();
  const { caps, voiceMode, toggleVoiceMode } = useVoiceMode();
  // Hands-free needs BOTH legs: without TTS there is no spoken reply to loop on, and
  // the composer's dictation already covers the listen-only case.
  const open = voiceMode && caps.stt && caps.tts;
  const session = useVoiceSession(open);

  if (!open) return null;

  const status =
    session.phase === 'error' && session.errorKey !== undefined
      ? t(ERROR_KEYS[session.errorKey])
      : t(PHASE_KEYS[session.phase]);

  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next) toggleVoiceMode();
      }}
    >
      <DialogContent
        showCloseButton={false}
        data-testid="voice-overlay"
        aria-describedby={undefined}
        className="max-w-xl gap-6 rounded-[var(--radius-xl)] px-6 py-8 sm:px-10"
      >
        <DialogTitle className="text-center text-[15px] font-medium tracking-[0.14em] text-text-faint uppercase">
          {t('chat.voice.title')}
        </DialogTitle>

        <div className="flex flex-col items-center gap-5">
          <VoiceOrb phase={session.phase} level={session.level} />
          <p
            role="status"
            aria-live="polite"
            data-testid="voice-status"
            className="min-h-6 text-center font-display text-lg text-text"
          >
            {status}
          </p>
        </div>

        {/* The last exchange, in the words that crossed the wire: the transcript is
            literally what was sent as the turn, so a mis-hearing is visible instead of
            being something the person has to infer from a strange answer. */}
        <div className="flex min-h-16 flex-col gap-2 text-[0.9375rem] leading-relaxed">
          {session.transcript !== '' ? (
            <p data-testid="voice-transcript" className="text-text">
              <span className="text-text-faint">{t('chat.voice.you')} </span>
              {session.transcript}
            </p>
          ) : null}
          {session.reply !== '' ? (
            <p data-testid="voice-reply" className="line-clamp-4 text-text-muted">
              {session.reply}
            </p>
          ) : null}
        </div>

        <div className="flex items-center justify-center gap-2">
          {session.phase === 'listening' ? (
            <Button type="button" variant="secondary" onClick={session.endUtterance}>
              <Square data-icon aria-hidden="true" className="size-3.5 fill-current" />
              {t('chat.voice.send')}
            </Button>
          ) : null}
          {session.phase === 'error' ? (
            <Button type="button" onClick={session.retry}>
              {t('chat.voice.retry')}
            </Button>
          ) : null}
          <Button type="button" variant="ghost" onClick={toggleVoiceMode}>
            <X data-icon aria-hidden="true" className="size-4" />
            {t('chat.voice.close')}
          </Button>
        </div>

        <p className="text-center text-[0.75rem] text-text-faint">{t('chat.voice.hint')}</p>
      </DialogContent>
    </Dialog>
  );
}
