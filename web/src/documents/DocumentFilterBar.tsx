import { Filter, Grid2X2, List } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { DocumentScope } from './documentApi';
import type { DocumentTab } from './documentViewModel';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { cn } from '@/lib/utils';

export type ScopeFilter = DocumentScope | 'all';
export type ViewMode = 'list' | 'grid';

interface DocumentFilterBarProps {
  readonly tab: DocumentTab;
  readonly tag: string;
  readonly scope: ScopeFilter;
  readonly viewMode: ViewMode;
  readonly onTabChange: (value: DocumentTab) => void;
  readonly onTagChange: (value: string) => void;
  readonly onScopeChange: (value: ScopeFilter) => void;
  readonly onViewModeChange: (value: ViewMode) => void;
}

const tabs = ['all', 'documents', 'images', 'files', 'failed', 'processing'] as const;
const mobileTabs = new Set<DocumentTab>(['all', 'images', 'files']);

export function DocumentFilterBar({
  tab,
  tag,
  scope,
  viewMode,
  onTabChange,
  onTagChange,
  onScopeChange,
  onViewModeChange,
}: DocumentFilterBarProps) {
  const { t } = useTranslation();
  const [filtersOpen, setFiltersOpen] = useState(false);
  return (
    <div className="documents-filter-bar border-b border-border bg-bg px-4 py-3 sm:px-6">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-3">
        <div className="flex min-w-0 items-center gap-2 sm:justify-between">
          <Tabs
            value={tab}
            onValueChange={(value) => {
              onTabChange(value as DocumentTab);
            }}
            className="min-w-0 flex-1 sm:flex-none"
          >
            <TabsList className="flex w-full max-w-full justify-start overflow-x-auto border-0 bg-transparent p-0 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden sm:w-fit sm:border sm:bg-surface-2 sm:p-1">
              {tabs.map((item) => (
                <TabsTrigger
                  key={item}
                  value={item}
                  className={cn(
                    'flex-none rounded-full px-4 sm:flex-1 sm:rounded-md',
                    mobileTabs.has(item) ? '' : 'hidden sm:inline-flex',
                  )}
                >
                  {t(`documents.tabs.${item}`)}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          <div className="flex shrink-0 items-center gap-2 sm:gap-1">
            <Button
              type="button"
              size="icon"
              variant={filtersOpen ? 'default' : 'ghost'}
              aria-label={t('documents.view.filters')}
              aria-pressed={filtersOpen}
              onClick={() => {
                setFiltersOpen((open) => !open);
              }}
              className="rounded-full bg-surface-2 sm:hidden"
            >
              <Filter aria-hidden="true" />
            </Button>
            <Button
              type="button"
              size="icon"
              variant={viewMode === 'list' ? 'default' : 'outline'}
              aria-label={t('documents.view.list')}
              onClick={() => {
                onViewModeChange('list');
              }}
              className="rounded-full sm:rounded-md"
            >
              <List aria-hidden="true" />
            </Button>
            <Button
              type="button"
              size="icon"
              variant={viewMode === 'grid' ? 'default' : 'outline'}
              aria-label={t('documents.view.grid')}
              onClick={() => {
                onViewModeChange('grid');
              }}
              className="hidden rounded-full sm:inline-flex sm:rounded-md"
            >
              <Grid2X2 aria-hidden="true" />
            </Button>
          </div>
        </div>
        <div
          className={cn(
            'min-w-0 gap-3 sm:grid sm:grid-cols-[minmax(0,16rem)_minmax(0,12rem)_auto]',
            filtersOpen ? 'grid' : 'hidden',
          )}
        >
          <div className="grid min-w-0 gap-1.5">
            <Label htmlFor="documents-tag">{t('documents.filters.tag')}</Label>
            <Input
              id="documents-tag"
              value={tag}
              onChange={(event) => {
                onTagChange(event.target.value);
              }}
            />
          </div>
          <div className="grid min-w-0 gap-1.5">
            <Label htmlFor="documents-scope">{t('documents.filters.scope')}</Label>
            <NativeSelect
              id="documents-scope"
              value={scope}
              onChange={(event) => {
                onScopeChange(event.target.value as ScopeFilter);
              }}
            >
              <NativeSelectOption value="all">{t('documents.scope.all')}</NativeSelectOption>
              <NativeSelectOption value="library">
                {t('documents.scope.library')}
              </NativeSelectOption>
              <NativeSelectOption value="thread">{t('documents.scope.thread')}</NativeSelectOption>
            </NativeSelect>
          </div>
          <div className="hidden items-end sm:flex">
            <span className="inline-flex min-h-11 items-center gap-2 text-[13px] text-text-muted">
              <Filter className="size-4" aria-hidden="true" />
              {t('documents.view.filters')}
            </span>
          </div>
        </div>
      </div>
    </div>
  );
}
