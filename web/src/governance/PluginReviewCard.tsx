import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { connectorTarget, shortCommit, type PluginReview, type PluginSkip } from './pluginApi';
import { Badge } from '@/components/ui/badge';
import { Card } from '@/components/ui/card';

// PluginReviewCard shows what an install would write, before the operator confirms it: each
// skill, each connector with where it runs, what it runs or dials, and whether it may ask for an
// authorization, and every skipped entry with the reason. The digest at the foot names exactly this review — the
// install refuses if what it would write no longer matches it.

export function PluginReviewCard({ review }: { readonly review: PluginReview }) {
  const { t } = useTranslation();
  const skipped: readonly PluginSkip[] = [...review.skippedSkills, ...review.skippedConnectors];

  return (
    <Card className="gap-4 border-accent/40 bg-surface-2 p-4">
      <header className="flex flex-col gap-1">
        <h4 className="flex flex-wrap items-baseline gap-2 font-display text-[18px] font-semibold text-text">
          <span className="break-all font-mono">{review.name}</span>
          {review.version !== '' ? (
            <span className="font-mono text-[14px] text-text-muted">{review.version}</span>
          ) : null}
        </h4>
        <p className="flex flex-wrap items-center gap-2 text-[13px] text-text-muted">
          {review.author !== '' ? (
            <span>{t('governance.plugins.review.by', { author: review.author })}</span>
          ) : null}
          <Badge variant="secondary" className="font-mono">
            {review.format}
          </Badge>
          <span>
            {t('governance.plugins.review.commit')}{' '}
            <code className="font-mono text-text" title={review.commit}>
              {shortCommit(review.commit)}
            </code>
          </span>
        </p>
      </header>

      <Section heading={t('governance.plugins.review.skillsHeading')}>
        <ul
          aria-label={t('governance.plugins.review.skillsHeading')}
          className="flex flex-wrap gap-1"
        >
          {review.skills.length === 0 ? (
            <li className="text-[13px] text-text-muted">{t('governance.plugins.none')}</li>
          ) : (
            review.skills.map((s) => (
              <li key={s.name}>
                <Badge variant="secondary" className="font-mono">
                  {s.name}
                </Badge>
              </li>
            ))
          )}
        </ul>
      </Section>

      <Section heading={t('governance.plugins.review.connectorsHeading')}>
        {review.mcpDisabled !== '' ? (
          <p className="text-[13px] text-warning">
            {t('governance.plugins.review.mcpDisabled', { reason: review.mcpDisabled })}
          </p>
        ) : (
          <ul
            aria-label={t('governance.plugins.review.connectorsHeading')}
            className="flex flex-col gap-1"
          >
            {review.connectors.length === 0 ? (
              <li className="text-[13px] text-text-muted">{t('governance.plugins.none')}</li>
            ) : (
              review.connectors.map((c) => (
                <li key={c.name} className="flex flex-wrap items-center gap-2 text-[13px]">
                  <span className="break-all font-mono text-text">{c.name}</span>
                  {c.placement === 'box' ? (
                    <Badge variant="info">{t('governance.plugins.review.placement.box')}</Badge>
                  ) : (
                    <Badge variant="secondary">
                      {t('governance.plugins.review.placement.remote')}
                    </Badge>
                  )}
                  {c.oauth ? (
                    <Badge variant="warning">{t('governance.plugins.review.oauth')}</Badge>
                  ) : null}
                  <code className="basis-full break-all font-mono text-[12px] text-text-muted">
                    {connectorTarget(c)}
                  </code>
                </li>
              ))
            )}
          </ul>
        )}
      </Section>

      {skipped.length > 0 ? (
        <Section heading={t('governance.plugins.review.skippedHeading')}>
          <ul
            aria-label={t('governance.plugins.review.skippedHeading')}
            className="flex flex-col gap-1 text-[13px] text-text-muted"
          >
            {skipped.map((s) => (
              <li key={s.name} className="break-words">
                {t('governance.plugins.review.skipped', { name: s.name, reason: s.reason })}
              </li>
            ))}
          </ul>
        </Section>
      ) : null}

      {review.notes.length > 0 ? (
        <Section heading={t('governance.plugins.review.notesHeading')}>
          <ul className="flex list-disc flex-col gap-1 pl-5 text-[13px] text-text-muted">
            {review.notes.map((note) => (
              <li key={note}>{note}</li>
            ))}
          </ul>
        </Section>
      ) : null}

      <p className="flex flex-col gap-0.5 text-[12px] text-text-faint">
        <span className="font-semibold uppercase tracking-wide">
          {t('governance.plugins.review.digest')}
        </span>
        <code className="break-all font-mono">{review.digest}</code>
      </p>
    </Card>
  );
}

function Section({
  heading,
  children,
}: {
  readonly heading: string;
  readonly children: ReactNode;
}) {
  return (
    <section className="flex flex-col gap-1.5">
      <h5 className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
        {heading}
      </h5>
      {children}
    </section>
  );
}
