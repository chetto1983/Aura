import { useTranslation } from 'react-i18next';

/** The Studio surface. The composer, the stage and the history panel land here next; this
 *  renders the title only, so the mode the nav already offers leads somewhere rather than
 *  being a tab with nothing behind it. */
export default function StudioWorkspace() {
  const { t } = useTranslation();
  return (
    <section
      aria-label={t('studio.title')}
      className="flex h-full min-h-0 flex-col overflow-hidden bg-bg px-4 py-4"
    >
      <h1 className="text-[15px] font-semibold text-text">{t('studio.title')}</h1>
    </section>
  );
}
