import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import {
  Filemanager,
  Willow,
  WillowDark,
  type IApi,
  type IEntity,
} from '@svar-ui/react-filemanager';
import { Locale } from '@svar-ui/react-core';
import { useTranslation } from 'react-i18next';
import '@svar-ui/react-filemanager/all.css';
import { createFileManagerProvider, directURL, parseDates } from './filesApi';
import { filesWords } from './filesLocale';

interface FilesWorkspaceProps {
  readonly mobileMenu?: ReactNode;
}

/**
 * The corpus browser IS the component: SVAR React File Manager (MIT), driven by its OWN
 * RestDataProvider against a mount that speaks its REST dialect. Every write -- create,
 * rename, move, copy, delete, upload -- is issued by the provider, so there is no
 * request-building code here to drift from the contract.
 *
 * It replaces a hand-written library workspace that read the document catalog. The catalog
 * only ever had rows for documents uploaded through it, so a file reconciled from the
 * bucket was invisible here no matter that it was fully indexed and answerable.
 */
export default function FilesWorkspace({ mobileMenu }: FilesWorkspaceProps) {
  const { t, i18n } = useTranslation();
  const [data, setData] = useState<IEntity[]>([]);
  const [error, setError] = useState('');
  const apiRef = useRef<IApi | null>(null);

  // One provider for the component's lifetime: it is an event-bus link, and rebuilding it
  // per render would re-register handlers on every keystroke.
  const provider = useMemo(() => createFileManagerProvider(), []);

  useEffect(() => {
    let cancelled = false;
    provider
      .loadFiles('')
      .then((files: IEntity[]) => {
        if (!cancelled) setData(parseDates(files));
      })
      .catch(() => {
        if (!cancelled) setError(t('files.loadFailed'));
      });
    return () => {
      cancelled = true;
    };
  }, [provider, t]);

  useEffect(() => {
    // The store may have had to pick a different name than the one typed -- a collision, or
    // a character it will not keep. The provider says so; without this the UI keeps showing
    // the name the user asked for while the bucket holds another.
    provider.on('file-renamed', (ev) => {
      // The bus types every action as the union of all payloads, so this one is narrowed at
      // the point of use rather than by widening the handler's signature.
      const { id, newId } = ev as { id: string; newId: string };
      // skipProvider stops the correction from being sent straight back to the server as a
      // second rename; there is nothing to await, the store update is local.
      void apiRef.current?.exec('rename-file', {
        id,
        name: newId.slice(newId.lastIndexOf('/') + 1),
        skipProvider: true,
      });
    });
  }, [provider]);

  const init = useCallback(
    (api: IApi) => {
      apiRef.current = api;
      // Putting the provider last on the bus is what turns every local action into its REST
      // call. This one line replaces a handler per verb.
      api.setNext(provider);
      // Open renders in a tab, download saves. The backend distinguishes the two and makes
      // inline safe with a sandbox CSP rather than by refusing to render at all.
      api.on('open-file', ({ id }: { id: string }) => {
        window.open(directURL(id, false), '_blank', 'noopener,noreferrer');
      });
      api.on('download-file', ({ id }: { id: string }) => {
        window.location.assign(directURL(id, true));
      });
    },
    [provider],
  );

  const requestData = useCallback(
    (ev: { id: string }) => {
      provider
        .loadFiles(ev.id)
        // Returned, not fired and forgotten: exec resolves asynchronously, so a failure to
        // hand the folder over has to reach the same catch as a failure to fetch it.
        .then((files: IEntity[]) =>
          apiRef.current?.exec('provide-data', { id: ev.id, data: parseDates(files) }),
        )
        .catch(() => {
          // The folder stays collapsed and the banner says why; resolving with an empty
          // list would claim the folder is empty, which is a different and wrong answer.
          setError(t('files.loadFailed'));
        });
    },
    [provider, t],
  );

  const Theme =
    document.documentElement.getAttribute('data-theme') === 'light' ? Willow : WillowDark;

  return (
    <section aria-label={t('files.title')} className="flex h-full min-h-0 min-w-0 flex-col bg-bg">
      {/* Only on mobile, where the nav button has nowhere else to live. On desktop the
          widget's own toolbar is the header, so a second one would just repeat the mode
          tab that opened this surface. */}
      <header className="flex items-center gap-2 border-b border-border px-3 py-2 md:hidden">
        {mobileMenu}
        <h1 className="text-[15px] font-semibold text-text">{t('files.title')}</h1>
      </header>
      {error !== '' && (
        <p role="alert" className="px-3 py-2 text-sm text-danger">
          {error}
        </p>
      )}
      {/* The theme wrapper is a plain div with no height of its own, so the manager inside
          it collapses to its content unless the wrapper is stretched — the demo layout
          gives its container the same explicit full height. */}
      <div className="min-h-0 flex-1 [&>*]:h-full">
        <Theme>
          <Locale words={filesWords(i18n.language)}>
            <Filemanager data={data} init={init} onRequestData={requestData} />
          </Locale>
        </Theme>
      </div>
    </section>
  );
}
