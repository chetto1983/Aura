import { useState } from 'react';
import { Pin, PinOff, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { BoardFilters, BoardView, CardSource } from './boardApi';
import { orderViews } from './boardModel';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';

interface BoardViewsBarProps {
  readonly filters: BoardFilters;
  readonly onFiltersChange: (filters: BoardFilters) => void;
  readonly tags: readonly string[];
  readonly views: readonly BoardView[];
  readonly activeViewId: string;
  readonly onSelectView: (view: BoardView | null) => void;
  readonly onSaveView: (name: string) => void;
  readonly onTogglePin: (view: BoardView) => void;
  readonly onDeleteView: (view: BoardView) => void;
}

const SOURCES: readonly CardSource[] = ['cockpit', 'chat', 'background'];

/**
 * The board's filters and the views saved from them (PMSync's smart views, consolidation item
 * 3a). A view is a name for a set of filters; choosing one sets them, and touching a filter
 * leaves the view, so what is on screen always matches what the bar says.
 */
export function BoardViewsBar(props: BoardViewsBarProps) {
  const { filters, onFiltersChange, tags, views, activeViewId } = props;
  const { t } = useTranslation();
  const [naming, setNaming] = useState(false);
  const [name, setName] = useState('');
  const filtering = Object.values(filters).some((value) => value !== undefined);

  function set<K extends keyof BoardFilters>(key: K, value: BoardFilters[K]) {
    onFiltersChange({ ...filters, [key]: value });
  }

  return (
    <div className="flex flex-col gap-2 border-b border-border px-3 py-2">
      <div
        role="group"
        aria-label={t('board.views.label')}
        className="flex items-center gap-1.5 overflow-x-auto md:flex-wrap"
      >
        <Button
          type="button"
          size="sm"
          variant={activeViewId === '' && !filtering ? 'secondary' : 'ghost'}
          aria-pressed={activeViewId === '' && !filtering}
          onClick={() => {
            props.onSelectView(null);
          }}
        >
          {t('board.views.all')}
        </Button>
        {orderViews(views).map((view) => (
          <span key={view.id} className="inline-flex items-center rounded-md bg-surface-2">
            <Button
              type="button"
              size="sm"
              variant={view.id === activeViewId ? 'secondary' : 'ghost'}
              aria-pressed={view.id === activeViewId}
              onClick={() => {
                props.onSelectView(view);
              }}
            >
              {view.name}
            </Button>
            <Button
              type="button"
              size="icon"
              variant="ghost"
              aria-label={t(view.pinned ? 'board.views.unpin' : 'board.views.pin', {
                name: view.name,
              })}
              onClick={() => {
                props.onTogglePin(view);
              }}
            >
              {view.pinned ? <PinOff aria-hidden="true" /> : <Pin aria-hidden="true" />}
            </Button>
            <Button
              type="button"
              size="icon"
              variant="ghost"
              aria-label={t('board.views.delete', { name: view.name })}
              onClick={() => {
                props.onDeleteView(view);
              }}
            >
              <X aria-hidden="true" />
            </Button>
          </span>
        ))}
      </div>
      <div className="flex items-center gap-1.5 overflow-x-auto md:flex-wrap">
        <NativeSelect
          size="sm"
          className="w-auto min-w-32 shrink-0"
          aria-label={t('board.filters.source')}
          value={filters.source ?? ''}
          onChange={(event) => {
            set('source', (event.target.value || undefined) as CardSource | undefined);
          }}
        >
          <NativeSelectOption value="">{t('board.filters.anySource')}</NativeSelectOption>
          {SOURCES.map((source) => (
            <NativeSelectOption key={source} value={source}>
              {t(`board.source.${source}`)}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <NativeSelect
          size="sm"
          className="w-auto min-w-32 shrink-0"
          aria-label={t('board.filters.priority')}
          value={filters.priority === undefined ? '' : String(filters.priority)}
          onChange={(event) => {
            set('priority', event.target.value === '' ? undefined : Number(event.target.value));
          }}
        >
          <NativeSelectOption value="">{t('board.filters.anyPriority')}</NativeSelectOption>
          {[3, 2, 1].map((priority) => (
            <NativeSelectOption key={priority} value={String(priority)}>
              {t(`board.priority.${String(priority)}`)}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <NativeSelect
          size="sm"
          className="w-auto min-w-32 shrink-0"
          aria-label={t('board.filters.due')}
          value={filters.due ?? ''}
          onChange={(event) => {
            set('due', (event.target.value || undefined) as BoardFilters['due']);
          }}
        >
          <NativeSelectOption value="">{t('board.filters.anyDue')}</NativeSelectOption>
          <NativeSelectOption value="overdue">{t('board.filters.overdue')}</NativeSelectOption>
          <NativeSelectOption value="week">{t('board.filters.week')}</NativeSelectOption>
        </NativeSelect>
        {tags.length > 0 && (
          <NativeSelect
            size="sm"
            className="w-auto min-w-32 shrink-0"
            aria-label={t('board.filters.tag')}
            value={filters.tag ?? ''}
            onChange={(event) => {
              set('tag', event.target.value || undefined);
            }}
          >
            <NativeSelectOption value="">{t('board.filters.anyTag')}</NativeSelectOption>
            {tags.map((tag) => (
              <NativeSelectOption key={tag} value={tag}>
                {tag}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        )}
        {filtering && !naming && (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={() => {
              setNaming(true);
            }}
          >
            {t('board.views.save')}
          </Button>
        )}
        {naming && (
          <form
            className="flex items-center gap-1.5"
            onSubmit={(event) => {
              event.preventDefault();
              props.onSaveView(name.trim());
              setNaming(false);
              setName('');
            }}
          >
            <Input
              ref={(node) => {
                node?.focus();
              }}
              aria-label={t('board.views.name')}
              placeholder={t('board.views.name')}
              maxLength={80}
              className="w-44"
              value={name}
              onChange={(event) => {
                setName(event.target.value);
              }}
            />
            <Button type="submit" size="sm" disabled={name.trim() === ''}>
              {t('board.views.confirm')}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => {
                setNaming(false);
              }}
            >
              {t('board.views.cancel')}
            </Button>
          </form>
        )}
      </div>
    </div>
  );
}
