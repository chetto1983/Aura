import { useCallback, useEffect, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { checkTelegramAvailability, putSetting } from '../settings/settingsApi';
import { Button } from '@/components/ui/button';

// TelegramTokenStep is the onboarding Telegram-integration step (ONBD): the operator pastes
// the bot token (@BotFather), Aura validates it LIVE via getMe (POST /api/settings/telegram/
// check with the token in the body), then persists it (PUT /api/settings/TELEGRAM_BOT_TOKEN).
// The save hot-starts the bot channel that consumes the pairing scan and says whether it came
// up (channel_active). It used to end in "restart Aura", a dead end on an appliance with no
// monitor and no SSH; a channel that did not start now shows the server's reason and a Retry.
//
// Multi-user (CR-01): there is ONE bot per instance. On mount the step checks the STORED
// token; if the bot is already configured it auto-surfaces "already configured" and the
// operator just continues — a provisioned sub-user never re-enters the global token (and the
// PUT is governance.write-gated server-side regardless). onDone advances to profile completion.

const TELEGRAM_BOT_TOKEN_KEY = 'TELEGRAM_BOT_TOKEN';

type Phase =
  | 'checking'
  | 'needsToken'
  | 'verifying'
  | 'saving'
  | 'active'
  | 'notStarted'
  | 'configured'
  | 'error';

export interface TelegramTokenStepProps {
  readonly onDone: () => void;
}

export function TelegramTokenStep({ onDone }: TelegramTokenStepProps) {
  const { t } = useTranslation();
  const [phase, setPhase] = useState<Phase>('checking');
  const [token, setToken] = useState('');
  const [bot, setBot] = useState('');
  const [rejected, setRejected] = useState(false);
  const [channelError, setChannelError] = useState('');

  useEffect(() => {
    let cancelled = false;
    async function probeStored() {
      try {
        const res = await checkTelegramAvailability();
        if (cancelled) return;
        if (res.available && res.botUsername !== undefined && res.botUsername !== '') {
          setBot(res.botUsername);
          setPhase('configured');
          return;
        }
      } catch {
        // fall through to manual token entry
      }
      if (!cancelled) setPhase('needsToken');
    }
    void probeStored();
    return () => {
      cancelled = true;
    };
  }, []);

  // Retry comes back here without a second getMe: the token was accepted, the channel was not.
  const save = useCallback(async (value: string) => {
    setPhase('saving');
    try {
      const res = await putSetting(TELEGRAM_BOT_TOKEN_KEY, value);
      setChannelError(res.channel_error ?? '');
      setPhase(res.channel_active === true ? 'active' : 'notStarted');
    } catch {
      setPhase('error');
    }
  }, []);

  const verifyAndSave = useCallback(async () => {
    const trimmed = token.trim();
    if (trimmed === '') return;
    setRejected(false);
    setPhase('verifying');
    try {
      const res = await checkTelegramAvailability(trimmed);
      if (!res.available || res.botUsername === undefined || res.botUsername === '') {
        setRejected(true);
        setPhase('needsToken');
        return;
      }
      setBot(res.botUsername);
    } catch {
      setRejected(true);
      setPhase('needsToken');
      return;
    }
    await save(trimmed);
  }, [save, token]);

  if (phase === 'checking') {
    return (
      <div role="status" className="flex items-center gap-3 py-6 text-text-muted">
        <Loader2 aria-hidden="true" className="h-5 w-5 animate-spin" />
        <span className="text-[15.5px]">{t('onboarding.profile.telegram.checking')}</span>
      </div>
    );
  }

  if (phase === 'configured') {
    return (
      <div className="flex flex-col items-start gap-5">
        <p role="status" className="text-[15.5px] leading-relaxed text-success">
          {t('onboarding.profile.telegram.alreadyConfigured', { bot })}
        </p>
        <Button type="button" onClick={onDone} className="px-6">
          {t('onboarding.profile.telegram.continue')}
        </Button>
      </div>
    );
  }

  if (phase === 'active') {
    return (
      <div className="flex flex-col items-start gap-4">
        <p role="status" className="text-[15.5px] font-semibold text-success">
          {t('onboarding.profile.telegram.channelActive', { bot })}
        </p>
        <Button type="button" onClick={onDone} className="px-6">
          {t('onboarding.profile.telegram.continue')}
        </Button>
      </div>
    );
  }

  if (phase === 'notStarted') {
    return (
      <div className="flex max-w-xl flex-col items-start gap-4">
        <div role="alert" className="flex flex-col gap-2">
          <p className="text-[15.5px] font-semibold text-warning">
            {t('onboarding.profile.telegram.channelInactive', { bot })}
          </p>
          {channelError === '' ? null : (
            <p className="break-words font-mono text-[13px] leading-relaxed text-text-muted">
              {channelError}
            </p>
          )}
        </div>
        <Button type="button" onClick={() => void save(token.trim())} className="px-6">
          {t('onboarding.retry')}
        </Button>
      </div>
    );
  }

  const busy = phase === 'verifying' || phase === 'saving';

  return (
    <form
      className="flex max-w-xl flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault();
        void verifyAndSave();
      }}
    >
      <p className="text-[15.5px] leading-relaxed text-text-muted">
        {t('onboarding.profile.telegram.intro')}
      </p>
      <label className="flex flex-col gap-2">
        <span className="text-sm font-semibold text-text">
          {t('onboarding.profile.telegram.tokenLabel')}
        </span>
        <input
          type="password"
          autoComplete="off"
          spellCheck={false}
          value={token}
          disabled={busy}
          onChange={(e) => {
            setToken(e.target.value);
          }}
          placeholder={t('onboarding.profile.telegram.tokenPlaceholder')}
          className="min-h-[44px] rounded-md border border-border bg-surface px-3 py-2 text-[15.5px] text-text outline-none focus-visible:ring-2 focus-visible:ring-ring"
        />
      </label>

      {rejected ? (
        <p role="alert" className="text-[13px] text-danger">
          {t('onboarding.profile.telegram.invalid')}
        </p>
      ) : null}
      {phase === 'error' ? (
        <p role="alert" className="text-[13px] text-danger">
          {t('onboarding.profile.telegram.errorSave')}
        </p>
      ) : null}

      <Button type="submit" disabled={busy || token.trim() === ''} className="self-start px-6">
        {busy ? (
          <span className="flex items-center gap-2">
            <Loader2 aria-hidden="true" className="h-4 w-4 animate-spin" />
            {t(
              phase === 'saving'
                ? 'onboarding.profile.telegram.saving'
                : 'onboarding.profile.telegram.verifying',
            )}
          </span>
        ) : (
          t('onboarding.profile.telegram.verify')
        )}
      </Button>
    </form>
  );
}
