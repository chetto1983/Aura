import { Files } from 'lucide-react';
import { useTranslation } from 'react-i18next';

export default function DocumentsWorkspace() {
  const { t } = useTranslation();
  return (
    <section aria-label={t('documents.title')} className="flex h-full min-h-0 flex-col bg-bg">
      <div className="shrink-0 border-b border-border bg-surface px-4 py-4 sm:px-6">
        <div className="flex min-w-0 items-center gap-3">
          <div className="grid h-10 w-10 shrink-0 place-items-center rounded-[var(--radius-md)] border border-border bg-surface-2 text-accent-text">
            <Files data-icon="icon" aria-hidden="true" focusable="false" />
          </div>
          <h1 className="min-w-0 truncate font-display text-xl font-semibold text-text">
            {t('documents.title')}
          </h1>
        </div>
      </div>
    </section>
  );
}
