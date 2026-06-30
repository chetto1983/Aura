import { Filter, Grid2X2, List } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { DocumentScope } from './documentApi';
import type { DocumentTab } from './documentViewModel';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';

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
  return (
    <div className="border-b border-border bg-bg px-4 py-3 sm:px-6">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-3">
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
          <Tabs value={tab} onValueChange={(value) => { onTabChange(value as DocumentTab); }}>
            <TabsList className="max-w-full overflow-x-auto">
              {tabs.map((item) => (
                <TabsTrigger key={item} value={item}>
                  {t(`documents.tabs.${item}`)}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          <div className="flex items-center gap-1">
            <Button
              type="button"
              size="icon"
              variant={viewMode === 'list' ? 'default' : 'outline'}
              aria-label={t('documents.view.list')}
              onClick={() => { onViewModeChange('list'); }}
            >
              <List aria-hidden="true" />
            </Button>
            <Button
              type="button"
              size="icon"
              variant={viewMode === 'grid' ? 'default' : 'outline'}
              aria-label={t('documents.view.grid')}
              onClick={() => { onViewModeChange('grid'); }}
            >
              <Grid2X2 aria-hidden="true" />
            </Button>
          </div>
        </div>
        <div className="grid gap-3 sm:grid-cols-[minmax(0,16rem)_minmax(0,12rem)_auto]">
          <div className="grid gap-1.5">
            <Label htmlFor="documents-tag">{t('documents.filters.tag')}</Label>
            <Input
              id="documents-tag"
              value={tag}
              onChange={(event) => { onTagChange(event.target.value); }}
            />
          </div>
          <div className="grid gap-1.5">
            <Label htmlFor="documents-scope">{t('documents.filters.scope')}</Label>
            <NativeSelect
              id="documents-scope"
              value={scope}
              onChange={(event) => { onScopeChange(event.target.value as ScopeFilter); }}
            >
              <NativeSelectOption value="all">{t('documents.scope.all')}</NativeSelectOption>
              <NativeSelectOption value="library">{t('documents.scope.library')}</NativeSelectOption>
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
