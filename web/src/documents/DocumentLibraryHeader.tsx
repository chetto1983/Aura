import type { ReactNode } from 'react';
import { RefreshCw, Search, Upload } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

interface DocumentLibraryHeaderProps {
  readonly query: string;
  readonly refreshing: boolean;
  readonly mobileMenu?: ReactNode;
  readonly onQueryChange: (value: string) => void;
  readonly onSearch: () => void;
  readonly onRefresh: () => void;
  readonly onUpload: () => void;
}

export function DocumentLibraryHeader({
  query,
  refreshing,
  mobileMenu,
  onQueryChange,
  onSearch,
  onRefresh,
  onUpload,
}: DocumentLibraryHeaderProps) {
  const { t } = useTranslation();
  return (
    <header className="documents-library-header shrink-0 border-b border-border bg-bg px-4 py-4 sm:px-6 sm:py-5">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-4">
        <div className="flex min-w-0 items-center gap-3 sm:hidden">
          {mobileMenu}
          <h1 className="min-w-0 truncate text-[21px] font-semibold text-text">
            {t('documents.mobileTitle')}
          </h1>
        </div>
        <form
          role="search"
          className="min-w-0 sm:hidden"
          onSubmit={(event) => {
            event.preventDefault();
            onSearch();
          }}
        >
          <div className="relative min-w-0 flex-1">
            <Search
              className="pointer-events-none absolute left-4 top-1/2 size-4 -translate-y-1/2 text-text-faint"
              aria-hidden="true"
            />
            <Input
              type="search"
              aria-label={t('documents.filters.search')}
              value={query}
              onChange={(event) => {
                onQueryChange(event.target.value);
              }}
              placeholder={t('documents.filters.searchShort')}
              className="min-h-[44px] rounded-full bg-surface-2 pl-11"
            />
          </div>
          <button type="submit" className="sr-only">
            {t('documents.actions.search')}
          </button>
        </form>
        <div className="hidden min-w-0 gap-3 sm:flex sm:items-center sm:justify-between">
          <h1 className="min-w-0 truncate font-display text-[24px] font-semibold text-text sm:text-[28px]">
            {t('documents.title')}
          </h1>
          <div className="grid min-w-0 grid-cols-2 gap-2 sm:flex sm:w-auto sm:items-center">
            <Button type="button" variant="outline" disabled={refreshing} onClick={onRefresh}>
              <RefreshCw aria-hidden="true" />
              {t('documents.actions.refresh')}
            </Button>
            <Button type="button" onClick={onUpload}>
              <Upload aria-hidden="true" />
              {t('documents.actions.upload')}
            </Button>
          </div>
        </div>
        <form
          role="search"
          className="hidden min-w-0 gap-2 sm:flex sm:max-w-xl"
          onSubmit={(event) => {
            event.preventDefault();
            onSearch();
          }}
        >
          <div className="relative min-w-0 flex-1">
            <Search
              className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-text-faint"
              aria-hidden="true"
            />
            <Input
              type="search"
              aria-label={t('documents.filters.search')}
              value={query}
              onChange={(event) => {
                onQueryChange(event.target.value);
              }}
              placeholder={t('documents.filters.search')}
              className="pl-9"
            />
          </div>
          <Button type="submit" aria-label={t('documents.actions.search')} className="shrink-0">
            <Search aria-hidden="true" />
            <span className="hidden sm:inline">{t('documents.actions.search')}</span>
          </Button>
        </form>
      </div>
    </header>
  );
}
