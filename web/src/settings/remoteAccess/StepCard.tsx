import { useId, type ReactNode } from 'react';
import { ExternalLink } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../../components/Spinner';
import { Button } from '@/components/ui/button';

export function StepCard({
  heading,
  children,
}: {
  readonly heading: string;
  readonly children: ReactNode;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-3 rounded-md border border-border bg-surface p-4">
      {/* The wizard moves focus here on a step change: it is what brings the card into view. */}
      <h3 tabIndex={-1} className="font-semibold text-text outline-none">
        {heading}
      </h3>
      {children}
    </div>
  );
}

interface StepLinkProps {
  readonly href: string;
  readonly variant?: 'default' | 'outline';
  /** Opens in a new tab, for pages that are not the cockpit's. */
  readonly external?: boolean;
  readonly children: ReactNode;
}

/** A link drawn as the buttons beside it. A real button takes the body font from atmosphere.css;
 * an anchor keeps the Button classes, and without this it reads smaller and bolder. */
export function StepLink({ href, variant = 'outline', external = false, children }: StepLinkProps) {
  return (
    <Button asChild variant={variant} className="w-fit text-[15.5px] font-normal">
      <a href={href} {...(external ? { target: '_blank', rel: 'noreferrer noopener' } : {})}>
        <ExternalLink aria-hidden="true" />
        {children}
      </a>
    </Button>
  );
}

interface StepFootProps {
  readonly onBack?: (() => void) | undefined;
  readonly onNext?: (() => void) | undefined;
  /** Why Avanti is disabled; null when it can be pressed. */
  readonly why?: string | null;
  readonly busy?: boolean;
}

export function StepFoot({ onBack, onNext, why = null, busy = false }: StepFootProps) {
  const { t } = useTranslation();
  const whyId = useId();
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-3">
      {onBack ? (
        <Button
          type="button"
          variant="outline"
          onClick={onBack}
          className="text-text-muted hover:text-text"
        >
          {t('remoteAccess.nav.back')}
        </Button>
      ) : null}
      {onNext ? (
        <div className="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-3">
          {why === null ? null : (
            <p id={whyId} className="min-w-0 text-[13px] leading-relaxed text-text-muted">
              {why}
            </p>
          )}
          <Button
            type="button"
            className="px-6"
            disabled={why !== null || busy}
            aria-busy={busy ? 'true' : undefined}
            aria-describedby={why === null ? undefined : whyId}
            onClick={onNext}
          >
            {busy ? <Spinner /> : null}
            {t('remoteAccess.nav.next')}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
