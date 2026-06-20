import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { fetchTelegramStatus } from './onboardingApi';

// TelegramLinkStep (ONBD-01b / D-08) — shown AFTER /provision returns the deep-link + the server-
// rendered QR SVG. It renders:
//   - an "Open in Telegram" button (the t.me/<bot>?start=<onboarding-token> deep-link), and
//   - the server-rendered QR as an <img> (data: URI, NOT dangerouslySetInnerHTML — T-28-06-05; an
//     <img>-rendered SVG cannot execute script), with the "Or scan to link Telegram" caption.
// It polls GET /telegram-status (TanStack refetchInterval) until {linked:true}, then flips to the
// "Telegram linked" success state and stops polling. The bot token is NEVER in the DOM — only the
// deep-link URL + the QR (T-28-06-02). An expired session (404) surfaces the expired copy.
//
// The QR accent frame is the reserved accent (UI-SPEC §Color). When no deep-link was minted (the
// operator skipped Telegram, or no bot is configured) the step renders the no-link note.

export interface TelegramLinkStepProps {
  readonly sessionToken: string;
  readonly deepLink: string | undefined;
  readonly qrSvg: string | undefined;
  /** Polling is disabled in tests/SSR or once the wizard moves on. */
  readonly polling: boolean;
}

function svgDataUri(svg: string): string {
  return `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`;
}

export function TelegramLinkStep({
  sessionToken,
  deepLink,
  qrSvg,
  polling,
}: TelegramLinkStepProps) {
  const { t } = useTranslation();

  const status = useQuery({
    queryKey: ['onboarding', 'telegram-status', sessionToken],
    queryFn: () => fetchTelegramStatus(sessionToken),
    enabled: polling && deepLink !== undefined,
    retry: false,
    // Poll every 2s while not yet linked; stop once linked (R6).
    refetchInterval: (query) => (query.state.data?.linked === true ? false : 2000),
  });

  const linked = status.data?.linked === true;
  // A 404 (expired/unknown session) → the expired copy; any other failure also lands here.
  const expired = status.isError;

  if (deepLink === undefined) {
    return (
      <p className="text-[15.5px] leading-relaxed text-text-muted">{t('onboarding.telegram.none')}</p>
    );
  }

  if (linked) {
    return (
      <div role="status" className="flex flex-col items-center gap-3 py-6 text-center">
        <span aria-hidden="true" className="text-4xl leading-none text-success">
          ◍
        </span>
        <p className="text-[15.5px] font-semibold text-success">{t('onboarding.telegram.linked')}</p>
      </div>
    );
  }

  return (
    <div className="flex flex-col items-center gap-5">
      <a
        href={deepLink}
        target="_blank"
        rel="noreferrer noopener"
        className="flex min-h-[44px] w-full items-center justify-center rounded-md bg-accent px-4 py-2 text-[15.5px] font-semibold text-on-accent outline-none transition-opacity hover:opacity-90 focus-visible:ring-2 focus-visible:ring-ring"
      >
        {t('onboarding.telegram.deepLinkCta')}
      </a>

      {qrSvg !== undefined ? (
        <div className="flex flex-col items-center gap-2">
          <div className="rounded-lg border-2 border-accent bg-surface p-3">
            <img src={svgDataUri(qrSvg)} alt={t('onboarding.telegram.qrAlt')} className="h-44 w-44" />
          </div>
          <p className="text-[13px] text-text-muted">{t('onboarding.telegram.qrCaption')}</p>
        </div>
      ) : null}

      {expired ? (
        <p role="alert" className="text-[13px] text-warning">
          {t('onboarding.telegram.expired')}
        </p>
      ) : (
        <p role="status" className="text-[13px] text-text-muted">
          {t('onboarding.telegram.waiting')}
        </p>
      )}
    </div>
  );
}
