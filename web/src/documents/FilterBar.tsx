import type { ReactNode } from 'react';
import { ChevronDown, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import {
  EMPTY_FILTER,
  FILE_TYPES,
  FILTER_FIELDS,
  type CustomField,
  type DocumentsFilter,
  type Facets,
  type FilterField,
  type Label,
} from './documentsApi';
import { displayDate, filterChips, type LabelNames } from './documentsModel';
import { FieldMenu } from './FieldMenu';
import { CustomFilters } from './CustomFilters';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Select } from '@/components/ui/select';

interface FilterBarProps {
  readonly compact?: boolean;
  readonly children?: ReactNode;
  readonly filter: DocumentsFilter;
  readonly onChange: (filter: DocumentsFilter) => void;
  readonly facets: Facets;
  readonly lists: Readonly<Record<FilterField, readonly Label[]>>;
  readonly names: LabelNames;
  readonly customFields: readonly CustomField[];
  readonly shownFields: readonly string[];
  readonly onShownFieldsChange: (ids: readonly string[]) => void;
}

const FIELD_LABEL: Readonly<Record<FilterField, string>> = {
  type: 'documents.columns.type',
  counterparty: 'documents.columns.counterparty',
  tags: 'documents.columns.tags',
};

/**
 * The bar above the list (spec decision 4, mockup 1): the search box, one dropdown per list with
 * its counts, the date, the custom-field columns, the active filters as chips, and Azzera filtri.
 * It sits outside the grid, so typing in it never reaches the grid's keyboard handling.
 */
export function FilterBar(props: FilterBarProps) {
  const { filter, onChange, facets, lists, names, customFields, shownFields, onShownFieldsChange } =
    props;
  const { t, i18n } = useTranslation();
  const chips = filterChips(filter, names, t('documents.filters.unassigned'), (from, to) =>
    [
      from && `${t('documents.filters.from')} ${displayDate(from, i18n.language)}`,
      to && `${t('documents.filters.to')} ${displayDate(to, i18n.language)}`,
    ]
      .filter(Boolean)
      .join(' '),
  );
  const filtering =
    chips.length > 0 ||
    filter.search.trim() !== '' ||
    filter.fileType !== 'documents' ||
    filter.custom.length > 0 ||
    !filter.includeUnreviewed ||
    filter.needsReview;

  const advanced = (
    <>
      <Select
        size="sm"
        className="min-h-11 w-auto md:min-h-8"
        aria-label={t('documents.workspace.searchMode')}
        value={filter.searchMode}
        options={['content', 'name'].map((value) => ({
          value,
          label: t(`documents.workspace.${value}`),
        }))}
        onValueChange={(value) => {
          onChange({ ...filter, searchMode: value === 'name' ? 'name' : 'content' });
        }}
      />
      <CustomFilters
        fields={customFields}
        filter={filter}
        onChange={onChange}
        showChips={!props.compact}
      />
      {FILTER_FIELDS.map((field) => (
        <FieldMenu
          key={field}
          field={field}
          label={t(FIELD_LABEL[field])}
          labels={lists[field]}
          facets={facets}
          filter={filter}
          onChange={onChange}
        />
      ))}
      <DateMenu filter={filter} onChange={onChange} />
      <label className="flex items-center gap-2 text-xs text-text-muted">
        <Checkbox
          checked={filter.includeUnreviewed}
          onCheckedChange={(value) => {
            onChange({ ...filter, includeUnreviewed: value === true });
          }}
        />
        {t('documents.workspace.includeUnreviewed')}
      </label>
      {customFields.length > 0 && (
        <Popover>
          <PopoverTrigger asChild>
            <Button type="button" variant="outline" size="sm">
              {t('documents.columns.fields')}
              <ChevronDown aria-hidden="true" data-icon="inline-end" />
            </Button>
          </PopoverTrigger>
          <PopoverContent
            aria-label={t('documents.columns.fields')}
            align="start"
            className="flex w-64 flex-col gap-1 p-3"
          >
            {customFields.map((cf) => {
              const on = shownFields.includes(cf.id);
              return (
                <label
                  key={cf.id}
                  htmlFor={`cf-${cf.id}`}
                  className="flex min-h-11 items-center gap-2"
                >
                  <Checkbox
                    id={`cf-${cf.id}`}
                    checked={on}
                    onCheckedChange={() => {
                      onShownFieldsChange(
                        on ? shownFields.filter((id) => id !== cf.id) : [...shownFields, cf.id],
                      );
                    }}
                  />
                  {cf.name}
                </label>
              );
            })}
          </PopoverContent>
        </Popover>
      )}
      {props.children}
    </>
  );

  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
      <Input
        type="search"
        aria-label={t('documents.workspace.search')}
        placeholder={t('documents.workspace.search')}
        className="min-w-48 flex-[1_1_16rem]"
        value={filter.search}
        onChange={(e) => {
          onChange({ ...filter, search: e.target.value });
        }}
      />
      <Select
        size="sm"
        className="min-h-11 w-auto md:min-h-8"
        aria-label={t('documents.workspace.fileType')}
        value={filter.fileType}
        options={FILE_TYPES.map((value) => ({
          value,
          label: t(`documents.workspace.fileTypes.${value}`),
        }))}
        onValueChange={(value) => {
          onChange({
            ...filter,
            fileType: FILE_TYPES.find((type) => type === value) ?? 'documents',
          });
        }}
      />
      {props.compact ? (
        <Popover>
          <PopoverTrigger asChild>
            <Button variant="outline" className="min-h-11">
              {t('documents.workspace.filters')}
            </Button>
          </PopoverTrigger>
          <PopoverContent
            aria-label={t('documents.workspace.filters')}
            align="end"
            className="flex max-h-[70dvh] w-[min(360px,calc(100vw-2rem))] flex-wrap gap-3 overflow-y-auto p-4 [&_button]:min-h-11"
          >
            {advanced}
          </PopoverContent>
        </Popover>
      ) : (
        advanced
      )}
      {props.compact && (
        <CustomFilters
          fields={customFields}
          filter={filter}
          onChange={onChange}
          showControl={false}
        />
      )}
      {chips.map((chip) => {
        const field =
          chip.field === 'date' ? t('documents.filters.date') : t(FIELD_LABEL[chip.field]);
        const text = t(chip.excluded ? 'documents.filters.chipNot' : 'documents.filters.chip', {
          field,
          value: chip.label,
        });
        return (
          <Button
            key={chip.key}
            type="button"
            size="sm"
            variant="outline"
            title={t('documents.filters.removeChip', { field, value: chip.label })}
            className="border-info/60 bg-info/10 font-semibold"
            onClick={() => {
              onChange(chip.remove(filter));
            }}
          >
            {text}
            <X aria-hidden="true" data-icon="inline-end" />
          </Button>
        );
      })}
      {filtering && (
        <Button
          type="button"
          size="sm"
          variant="ghost"
          className="ml-auto text-info"
          onClick={() => {
            onChange({ ...EMPTY_FILTER, inbox: filter.inbox });
          }}
        >
          {t('documents.filters.reset')}
        </Button>
      )}
    </div>
  );
}

function isoDay(date: Date): string {
  return date.toISOString().slice(0, 10);
}

function yearRange(year: number): readonly [string, string] {
  return [`${String(year)}-01-01`, `${String(year)}-12-31`];
}

/** The date filter: a range, and the presets an office reaches for. */
function DateMenu({
  filter,
  onChange,
}: {
  readonly filter: DocumentsFilter;
  readonly onChange: (f: DocumentsFilter) => void;
}) {
  const { t } = useTranslation();
  // Each preset reads the clock when it is chosen, not when the menu renders.
  const presets: readonly {
    readonly label: string;
    readonly range: (now: Date) => readonly [string, string];
  }[] = [
    { label: t('documents.filters.thisYear'), range: (now) => yearRange(now.getUTCFullYear()) },
    { label: t('documents.filters.lastYear'), range: (now) => yearRange(now.getUTCFullYear() - 1) },
    {
      label: t('documents.filters.last30'),
      range: (now) => [isoDay(new Date(now.getTime() - 30 * 86_400_000)), isoDay(now)],
    },
  ];
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button type="button" variant="outline" size="sm" aria-label={t('documents.filters.date')}>
          {t('documents.filters.date')}
          <ChevronDown aria-hidden="true" data-icon="inline-end" />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        aria-label={t('documents.filters.date')}
        align="start"
        className="flex w-72 flex-col gap-2 p-3"
      >
        <div className="flex flex-wrap gap-1">
          {presets.map((preset) => (
            <Button
              key={preset.label}
              type="button"
              size="sm"
              variant="outline"
              onClick={() => {
                const [dateFrom, dateTo] = preset.range(new Date());
                onChange({ ...filter, dateFrom, dateTo });
              }}
            >
              {preset.label}
            </Button>
          ))}
        </div>
        <label className="flex items-center gap-2 text-sm">
          <span className="w-8">{t('documents.filters.from')}</span>
          <Input
            type="date"
            value={filter.dateFrom}
            onChange={(e) => {
              onChange({ ...filter, dateFrom: e.target.value });
            }}
          />
        </label>
        <label className="flex items-center gap-2 text-sm">
          <span className="w-8">{t('documents.filters.to')}</span>
          <Input
            type="date"
            value={filter.dateTo}
            onChange={(e) => {
              onChange({ ...filter, dateTo: e.target.value });
            }}
          />
        </label>
      </PopoverContent>
    </Popover>
  );
}
