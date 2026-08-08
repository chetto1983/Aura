import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import {
  Filemanager,
  Willow,
  WillowDark,
  getMenuOptions,
  type IApi,
  type IFile,
} from '@svar-ui/react-filemanager';
import { Locale } from '@svar-ui/react-core';
import { useTranslation } from 'react-i18next';
import '@svar-ui/react-filemanager/all.css';
import { downloadURL, loadFolder, uploadFile, type FileEntry } from './filesApi';
import { filesWords } from './filesLocale';

interface FilesWorkspaceProps {
  readonly mobileMenu?: ReactNode;
}

/**
 * The corpus browser IS the component: SVAR React File Manager (MIT), driven the way its
 * own backend demo drives it -- a root listing in `data`, folders filled on demand through
 * request-data/provide-data, downloads and uploads routed at the api seam.
 *
 * It replaces a hand-written library workspace that read the document catalog. The catalog
 * only ever had rows for documents uploaded through it, so a file reconciled from the
 * bucket was invisible here no matter that it was fully indexed and answerable.
 */
export default function FilesWorkspace({ mobileMenu }: FilesWorkspaceProps) {
  const { t, i18n } = useTranslation();
  const [data, setData] = useState<readonly FileEntry[]>([]);
  const [error, setError] = useState('');
  const apiRef = useRef<IApi | null>(null);

  useEffect(() => {
    let cancelled = false;
    loadFolder()
      .then((entries) => {
        if (!cancelled) setData(entries);
      })
      .catch(() => {
        if (!cancelled) setError(t('files.loadFailed'));
      });
    return () => {
      cancelled = true;
    };
  }, [t]);

  const init = useCallback(
    (api: IApi) => {
      apiRef.current = api;
      // Both actions download. Serving user-supplied bytes inline from the cockpit's own
      // origin is a stored-XSS hazard the backend refuses, so "open" cannot mean "render
      // here" -- it resolves to the same attachment rather than a second, weaker path.
      const download = ({ id }: { id: string }) => {
        window.location.assign(downloadURL(id));
      };
      api.on('download-file', download);
      api.on('open-file', download);

      // An upload arrives as create-file carrying the raw File; the same action without one
      // is a new empty file or folder, which this backend does not offer and the menu below
      // does not show. The widget has already added the row optimistically, so the PUT only
      // has to make the bucket agree with what the user is looking at.
      api.on('create-file', ({ file, parent }: { file: IFile; parent: string }) => {
        if (!file.file) return;
        return uploadFile(parent, file.file).catch(() => {
          setError(t('files.uploadFailed', { name: file.name }));
        });
      });
    },
    [t],
  );

  const requestData = useCallback(
    (ev: { id: string }) => {
      loadFolder(ev.id)
        // Returned, not fired and forgotten: exec resolves asynchronously, so a failure to
        // hand the folder over has to reach the same catch as a failure to fetch it.
        .then((entries) => apiRef.current?.exec('provide-data', { id: ev.id, data: entries }))
        .catch(() => {
          // The folder stays collapsed and the banner says why; resolving with an empty
          // list would claim the folder is empty, which is a different and wrong answer.
          setError(t('files.loadFailed'));
        });
    },
    [t],
  );

  // The menu is the backend's capability list, not a taste decision. The mount serves a
  // listing, a byte stream and an upload, so those are the three things offered; rename,
  // copy, move, delete and new-folder would every one of them 404. `readonly` would have
  // been the one-word version of this but it hides upload too.
  const menuOptions = useCallback((mode: string) => {
    if (mode === 'add') return getMenuOptions(mode).filter((option) => option.id === 'upload');
    if (mode === 'file') return getMenuOptions(mode).filter((option) => option.id === 'download');
    return false as const;
  }, []);

  const Theme = document.documentElement.getAttribute('data-theme') === 'light' ? Willow : WillowDark;

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
            <Filemanager
              data={data as FileEntry[]}
              init={init}
              onRequestData={requestData}
              menuOptions={menuOptions}
            />
          </Locale>
        </Theme>
      </div>
    </section>
  );
}
