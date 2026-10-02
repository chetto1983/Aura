import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import { useChatGPTPlanConnection } from './useChatGPTPlanConnection';
import { Button } from '@/components/ui/button';

interface ChatGPTPlanConnectionProps {
  readonly onConnectionChange?: (ready: boolean) => void;
}

export function ChatGPTPlanConnection({ onConnectionChange }: ChatGPTPlanConnectionProps) {
  const { t } = useTranslation();
  const {
    connection,
    busy,
    error,
    authURL,
    ended,
    pending,
    ready,
    activeLogin,
    login,
    disconnect,
    retry,
    cancel,
  } = useChatGPTPlanConnection(onConnectionChange);
  return (
    <div className="flex flex-col gap-3 rounded-md border border-border bg-surface p-4">
      <div role="status" className="text-[13px] text-text-muted">
        {busy && activeLogin && authURL === undefined
          ? t('settings.chatgpt.opening')
          : connection === undefined
            ? t('settings.chatgpt.loading')
            : pending
              ? t('settings.chatgpt.pending')
              : ready
                ? t('settings.chatgpt.connected')
                : connection.connected
                  ? t('settings.chatgpt.missingPlan')
                  : t('settings.chatgpt.disconnected')}
        {connection?.connected && connection.email !== '' ? (
          <span className="mt-1 block text-text">{connection.email}</span>
        ) : null}
      </div>
      {ended === 'cancelled' ? (
        <p role="status" className="text-[13px] text-text-muted">
          {busy ? t('settings.chatgpt.cancelling') : t('settings.chatgpt.cancelled')}
        </p>
      ) : null}
      {error !== undefined || ended === 'timeout' ? (
        <div role="alert" className="text-[13px] text-danger">
          {ended === 'timeout' ? t('settings.chatgpt.timedOut') : error}
        </div>
      ) : null}
      <div className="flex flex-wrap items-center gap-2">
        {!ready ? (
          <Button
            type="button"
            variant="outline"
            disabled={busy || (connection === undefined && error === undefined)}
            aria-busy={busy}
            onClick={() => void login()}
            className="min-h-11 border-black/20 bg-white text-black hover:bg-neutral-100 dark:border-white/30 dark:bg-black dark:text-white dark:hover:bg-neutral-900"
          >
            {busy ? <Spinner /> : null}
            {t('settings.chatgpt.login')}
          </Button>
        ) : null}
        {activeLogin ? (
          <Button type="button" variant="outline" onClick={cancel}>
            {t('settings.chatgpt.cancel')}
          </Button>
        ) : null}
        {connection?.connected ? (
          <Button
            type="button"
            variant="outline"
            disabled={busy}
            aria-busy={busy}
            onClick={() => void login()}
          >
            {t('settings.chatgpt.reconnect')}
          </Button>
        ) : null}
        {connection?.connected ? (
          <Button type="button" variant="outline" disabled={busy} onClick={() => void disconnect()}>
            {t('settings.chatgpt.disconnect')}
          </Button>
        ) : null}
        {error !== undefined || ended === 'timeout' ? (
          <Button type="button" variant="outline" disabled={busy} onClick={retry}>
            {t('settings.actions.retry')}
          </Button>
        ) : null}
      </div>
      {authURL !== undefined && pending ? (
        <a
          href={authURL}
          target="_blank"
          rel="noopener noreferrer"
          className="text-[13px] text-text underline underline-offset-4"
        >
          {t('settings.chatgpt.openLogin')}
        </a>
      ) : null}
    </div>
  );
}
