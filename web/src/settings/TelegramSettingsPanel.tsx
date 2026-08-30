import { useEffect, useMemo, useState } from 'react';
import { QrCode, RefreshCw, RotateCcw, Save, Send, ShieldCheck } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import {
  checkTelegramAvailability,
  createTelegramLink,
  deleteSetting,
  fetchSettings,
  fetchTelegramLinkStatus,
  putSetting,
  type SettingItem,
  type TelegramAvailability,
  type TelegramLink,
  type TelegramLinkStatus,
} from './settingsApi';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { SecretInput } from '@/components/ui/secret-input';

const TELEGRAM_BOT_TOKEN = 'TELEGRAM_BOT_TOKEN';

function emptyTelegramSetting(): SettingItem {
  return {
    key: TELEGRAM_BOT_TOKEN,
    label: 'Telegram bot token',
    kind: 'string',
    secret: true,
    value: '',
    has_value: false,
    overridden: false,
    applied: 'boot',
  };
}

function svgDataUri(svg: string): string {
  return `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`;
}

export function TelegramSettingsPanel() {
  const { t } = useTranslation();
  const [setting, setSetting] = useState<SettingItem>(emptyTelegramSetting);
  const [token, setToken] = useState('');
  const [loadStatus, setLoadStatus] = useState<'loading' | 'ready' | 'error'>('loading');
  const [busy, setBusy] = useState<'save' | 'reset' | 'check' | 'link' | 'status' | undefined>();
  const [saved, setSaved] = useState(false);
  const [availability, setAvailability] = useState<TelegramAvailability | undefined>();
  const [link, setLink] = useState<TelegramLink | undefined>();
  const [linkStatus, setLinkStatus] = useState<TelegramLinkStatus | undefined>();
  const [actionError, setActionError] = useState(false);

  async function reload() {
    setLoadStatus('loading');
    try {
      const settings = await fetchSettings();
      setSetting(
        settings.settings.find((item) => item.key === TELEGRAM_BOT_TOKEN) ?? emptyTelegramSetting(),
      );
      setLoadStatus('ready');
    } catch {
      setLoadStatus('error');
    }
  }

  useEffect(() => {
    let cancelled = false;
    void fetchSettings()
      .then((settings) => {
        if (cancelled) return;
        setSetting(
          settings.settings.find((item) => item.key === TELEGRAM_BOT_TOKEN) ??
            emptyTelegramSetting(),
        );
        setLoadStatus('ready');
      })
      .catch(() => {
        if (cancelled) return;
        setLoadStatus('error');
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const tokenConfigured = setting.has_value;
  const statusLabel = tokenConfigured
    ? t('settings.status.configured')
    : t('settings.status.notConfigured');
  const availabilityText = useMemo(() => {
    if (availability === undefined) return undefined;
    if (!availability.configured) return t('settings.telegram.notConfigured');
    if (availability.available) {
      return t('settings.telegram.available', { username: availability.botUsername ?? 'bot' });
    }
    return t('settings.telegram.unavailable');
  }, [availability, t]);

  async function runAction(action: NonNullable<typeof busy>, fn: () => Promise<void>) {
    if (busy !== undefined) return;
    setBusy(action);
    setActionError(false);
    try {
      await fn();
    } catch {
      setActionError(true);
    } finally {
      setBusy(undefined);
    }
  }

  async function saveToken() {
    const value = token.trim();
    if (value === '') return;
    await runAction('save', async () => {
      await putSetting(TELEGRAM_BOT_TOKEN, value);
      await reload();
      setToken('');
      setSaved(true);
    });
  }

  async function resetToken() {
    await runAction('reset', async () => {
      await deleteSetting(TELEGRAM_BOT_TOKEN);
      await reload();
      setAvailability(undefined);
      setSaved(false);
    });
  }

  async function checkToken() {
    await runAction('check', async () => {
      setAvailability(await checkTelegramAvailability(token.trim() || undefined));
    });
  }

  async function createLink() {
    await runAction('link', async () => {
      const next = await createTelegramLink();
      setLink(next);
      setLinkStatus({ linked: false });
    });
  }

  async function checkLinkStatus() {
    if (link?.sessionToken === undefined) return;
    await runAction('status', async () => {
      setLinkStatus(await fetchTelegramLinkStatus(link.sessionToken));
    });
  }

  if (loadStatus === 'loading') {
    return (
      <section className="grid min-h-28 place-items-center text-sm text-text-muted">
        {t('settings.telegram.loading')}
      </section>
    );
  }

  if (loadStatus === 'error') {
    return (
      <Alert variant="destructive">
        <AlertDescription>
          <span>{t('settings.telegram.error')}</span>
          <Button type="button" variant="outline" onClick={() => void reload()} className="mt-3">
            <RefreshCw aria-hidden="true" />
            {t('settings.actions.retry')}
          </Button>
        </AlertDescription>
      </Alert>
    );
  }

  return (
    <section aria-labelledby="settings-telegram" className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <h2 id="settings-telegram" className="text-[20px] font-semibold text-text">
          {t('settings.telegram.heading')}
        </h2>
        <p className="max-w-3xl text-[15.5px] leading-relaxed text-text-muted">
          {t('settings.telegram.body')}
        </p>
      </div>

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(280px,360px)]">
        <div className="flex min-h-48 flex-col gap-3 rounded-md border border-border bg-surface px-3 py-3">
          <div className="flex items-start justify-between gap-2">
            <Label htmlFor="setting-telegram-token" className="text-[13px]">
              {t('settings.fields.telegramBotToken')}
            </Label>
            <Badge variant={tokenConfigured ? 'success' : 'secondary'}>{statusLabel}</Badge>
          </div>
          <SecretInput
            id="setting-telegram-token"
            value={token}
            placeholder="123456:ABC..."
            showLabel={t('secret.show', { label: t('settings.fields.telegramBotToken') })}
            hideLabel={t('secret.hide', { label: t('settings.fields.telegramBotToken') })}
            onChange={(event) => {
              setToken(event.target.value);
              setSaved(false);
            }}
            className="font-mono text-[13px]"
          />
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant="outline"
              disabled={busy !== undefined}
              onClick={() => void checkToken()}
            >
              {busy === 'check' ? <Spinner /> : <ShieldCheck aria-hidden="true" />}
              {t('settings.telegram.actions.check')}
            </Button>
            <Button
              type="button"
              disabled={busy !== undefined || token.trim() === ''}
              onClick={() => void saveToken()}
            >
              {busy === 'save' ? <Spinner /> : <Save aria-hidden="true" />}
              {t('settings.telegram.actions.save')}
            </Button>
            {setting.overridden ? (
              <Button
                type="button"
                variant="ghost"
                disabled={busy !== undefined}
                onClick={() => void resetToken()}
              >
                {busy === 'reset' ? <Spinner /> : <RotateCcw aria-hidden="true" />}
                {t('settings.actions.reset')}
              </Button>
            ) : null}
          </div>
          {availabilityText !== undefined ? (
            <p role="status" className="text-[13px] text-text-muted">
              {availabilityText}
            </p>
          ) : null}
          {availability?.requiresRestart === true ? (
            <p className="text-[13px] text-warning">{t('settings.telegram.requiresRestart')}</p>
          ) : null}
          {saved ? (
            <p role="status" className="text-[13px] text-success">
              {t('settings.telegram.saved')}
            </p>
          ) : null}
        </div>

        <div className="flex min-h-48 flex-col gap-3 rounded-md border border-border bg-surface px-3 py-3">
          <div className="flex items-start justify-between gap-2">
            <h3 className="text-[13px] font-medium text-text">
              {t('settings.telegram.qrHeading')}
            </h3>
            <QrCode aria-hidden="true" className="size-4 text-text-muted" />
          </div>
          <p className="text-[13px] leading-relaxed text-text-muted">
            {t('settings.telegram.qrBody')}
          </p>
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant="outline"
              disabled={busy !== undefined}
              onClick={() => void createLink()}
            >
              {busy === 'link' ? <Spinner /> : <QrCode aria-hidden="true" />}
              {t('settings.telegram.actions.createQr')}
            </Button>
            {link?.sessionToken !== undefined ? (
              <Button
                type="button"
                variant="ghost"
                disabled={busy !== undefined}
                onClick={() => void checkLinkStatus()}
              >
                {busy === 'status' ? <Spinner /> : <RefreshCw aria-hidden="true" />}
                {t('settings.telegram.actions.checkLink')}
              </Button>
            ) : null}
          </div>

          {link?.deepLink !== undefined ? (
            <a
              href={link.deepLink}
              target="_blank"
              rel="noreferrer noopener"
              className="inline-flex min-h-[40px] items-center justify-center gap-2 rounded-md bg-accent px-3 py-2 text-[14px] font-semibold text-on-accent outline-none transition-opacity hover:opacity-90 focus-visible:ring-2 focus-visible:ring-ring"
            >
              <Send aria-hidden="true" className="size-4" />
              {t('onboarding.telegram.deepLinkCta')}
            </a>
          ) : null}

          {link?.qrSvg !== undefined ? (
            <div className="flex flex-col items-center gap-2">
              <div className="rounded-md border-2 border-accent bg-surface p-3">
                <img
                  src={svgDataUri(link.qrSvg)}
                  alt={t('settings.telegram.qrAlt')}
                  className="h-40 w-40"
                />
              </div>
              <p className="text-[12px] text-text-muted">{t('settings.telegram.qrCaption')}</p>
            </div>
          ) : null}

          {linkStatus !== undefined ? (
            <p
              role="status"
              className={
                linkStatus.linked ? 'text-[13px] text-success' : 'text-[13px] text-text-muted'
              }
            >
              {t(linkStatus.linked ? 'settings.telegram.linked' : 'settings.telegram.waiting')}
            </p>
          ) : null}
        </div>
      </div>

      {actionError ? (
        <Alert variant="destructive">
          <AlertDescription>{t('settings.telegram.actionError')}</AlertDescription>
        </Alert>
      ) : null}
    </section>
  );
}
