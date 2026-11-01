import { useState, type ReactNode } from 'react';
import { Pin, PinOff, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { BoardFilters, BoardView, CardSource } from './boardApi';
import { orderViews } from './boardModel';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';

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
const ALL_FILTERS = 'all';

interface FilterFieldProps {
  readonly label: string;
  readonly active: boolean;
  readonly children: ReactNode;
}

function FilterField({ label, active, children }: FilterFieldProps) {
  return (
    <div
      className={`flex shrink-0 items-center gap-1 rounded-md border pl-2.5 transition-colors has-[button:focus-visible]:ring-2 has-[button:focus-visible]:ring-ring ${
        active ? 'border-info/60 bg-info/8' : 'border-border bg-surface'
      }`}
    >
      <span className="text-[11px] font-medium text-text-muted">{label}</span>
      {children}
    </div>
  );
}

interface FilterSelectProps {
  readonly label: string;
  readonly value: string | undefined;
  readonly active: boolean;
  readonly options: readonly { readonly value: string; readonly label: string }[];
  readonly onValueChange: (value: string | undefined) => void;
}

function FilterSelect({ label, value, active, options, onValueChange }: FilterSelectProps) {
  return (
    <FilterField label={label} active={active}>
      <Select
        aria-label={label}
        value={value ?? ALL_FILTERS}
        onValueChange={(next) => {
          onValueChange(next === ALL_FILTERS ? undefined : next);
        }}
        options={options}
        size="sm"
        className="w-auto max-w-44 gap-2 border-0 bg-transparent font-medium focus-visible:ring-0"
      />
    </FilterField>
  );
}

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
    <div className="flex min-w-0 flex-col gap-2 border-b border-border px-3 pb-2 md:flex-row md:flex-wrap md:items-center md:gap-x-3">
      <div
        role="group"
        aria-label={t('board.views.label')}
        className="flex min-w-0 items-center gap-1.5 overflow-x-auto md:max-w-full md:flex-wrap"
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
      <div className="flex min-w-0 items-center gap-1.5 overflow-x-auto pb-1 md:max-w-full md:flex-wrap md:pb-0">
        <FilterSelect
          label={t('board.filters.source')}
          value={filters.source}
          active={filters.source !== undefined}
          options={[
            { value: ALL_FILTERS, label: t('board.filters.anySource') },
            ...SOURCES.map((source) => ({ value: source, label: t(`board.source.${source}`) })),
          ]}
          onValueChange={(value) => {
            set('source', value as CardSource | undefined);
          }}
        />
        <FilterSelect
          label={t('board.filters.priority')}
          value={filters.priority === undefined ? undefined : String(filters.priority)}
          active={filters.priority !== undefined}
          options={[
            { value: ALL_FILTERS, label: t('board.filters.anyPriority') },
            ...[3, 2, 1].map((priority) => ({
              value: String(priority),
              label: t(`board.priority.${String(priority)}`),
            })),
          ]}
          onValueChange={(value) => {
            set('priority', value === undefined ? undefined : Number(value));
          }}
        />
        <FilterSelect
          label={t('board.filters.due')}
          value={filters.due}
          active={filters.due !== undefined}
          options={[
            { value: ALL_FILTERS, label: t('board.filters.anyDue') },
            { value: 'overdue', label: t('board.filters.overdue') },
            { value: 'week', label: t('board.filters.week') },
          ]}
          onValueChange={(value) => {
            set('due', value as BoardFilters['due']);
          }}
        />
        {tags.length > 0 && (
          <FilterSelect
            label={t('board.filters.tag')}
            value={filters.tag === undefined ? undefined : `tag:${filters.tag}`}
            active={filters.tag !== undefined}
            options={[
              { value: ALL_FILTERS, label: t('board.filters.anyTag') },
              ...tags.map((tag) => ({ value: `tag:${tag}`, label: tag })),
            ]}
            onValueChange={(value) => {
              set('tag', value?.slice(4));
            }}
          />
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
