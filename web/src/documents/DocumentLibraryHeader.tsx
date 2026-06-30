import { RefreshCw, Search, Upload } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

interface DocumentLibraryHeaderProps {
  readonly query: string;
  readonly refreshing: boolean;
  readonly onQueryChange: (value: string) => void;
  readonly onSearch: () => void;
  readonly onRefresh: () => void;
  readonly onUpload: () => void;
}

export function DocumentLibraryHeader({
  query,
  refreshing,
  onQueryChange,
  onSearch,
  onRefresh,
  onUpload,
}: DocumentLibraryHeaderProps) {
  const { t } = useTranslation();
  return (
    <header className="shrink-0 border-b border-border bg-bg px-4 py-5 sm:px-6">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-4">
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
          <h1 className="min-w-0 truncate font-display text-[28px] font-semibold text-text">
            {t('documents.title')}
          </h1>
          <div className="flex items-center gap-2">
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
          className="flex min-w-0 flex-col gap-2 sm:max-w-xl sm:flex-row"
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
              className="pl-9"
            />
          </div>
          <Button type="submit">
            <Search aria-hidden="true" />
            {t('documents.actions.search')}
          </Button>
        </form>
      </div>
    </header>
  );
}
