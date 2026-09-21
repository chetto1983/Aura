import type { ReactNode } from 'react';
import { RotateCcw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import type { SettingItem } from './settingsApi';
import type { SettingDef, SettingsKey } from './modelSettingsDefs';
import { settingRow, type LoadedState } from './modelSettingsState';
import { ModelPicker } from './ModelPicker';
import type { ModelRow } from './mediaModelCatalog';
import type { ModelCatalogState } from './useModelCatalog';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { SecretInput } from '@/components/ui/secret-input';
import { cn } from '@/lib/utils';

/** How a field is framed. `card` tiles a pane of many settings; `inline` drops the chrome for
 *  the one or two fields that belong to a control that already has its own card and heading. */
export type SettingFieldVariant = 'card' | 'inline';

function SettingsGrid({
  variant,
  children,
}: {
  readonly variant: SettingFieldVariant;
  readonly children: ReactNode;
}) {
  return (
    <div
      className={
        variant === 'inline'
          ? 'flex max-w-xl flex-col gap-5'
          : 'grid gap-4 md:grid-cols-2 xl:grid-cols-3'
      }
    >
      {children}
    </div>
  );
}

/** One model field's catalogue and the formatter that writes its rows. */
export interface PickerBinding {
  readonly catalog: ModelCatalogState<ModelRow>;
  readonly formatRow: (row: ModelRow, freeLabel: string) => string;
  readonly onValueChange?: (value: string) => void;
  /** Optional choice that stores an empty model id (for example, use the local sidecar). */
  readonly emptyOption?: {
    readonly label: string;
    readonly description?: string;
  };
}

export type PickerBindings = Partial<Readonly<Record<SettingsKey, PickerBinding>>>;

export function SettingsFields({
  defs,
  loaded,
  onReset,
  onValueChange,
  pickers,
  invalidKeys,
  describedBy,
  resetting,
  variant = 'card',
}: {
  readonly defs: readonly SettingDef[];
  readonly loaded: LoadedState;
  readonly resetting: string | undefined;
  readonly variant?: SettingFieldVariant;
  readonly onValueChange: (key: SettingsKey, value: string) => void;
  readonly onReset: (key: SettingsKey) => void;
  /** The catalogue each model field picks from; a field without one stays free text. */
  readonly pickers?: PickerBindings | undefined;
  /** Invalid fields expose aria-invalid; valid fields deliberately omit it. */
  readonly invalidKeys?: ReadonlySet<SettingsKey> | undefined;
  readonly describedBy?: Partial<Record<SettingsKey, string>> | undefined;
}) {
  return (
    <SettingsGrid variant={variant}>
      {defs.map((def) => (
        <SettingField
          key={def.key}
          def={def}
          variant={variant}
          item={settingRow(loaded, def)}
          value={loaded.values[def.key] ?? ''}
          picker={pickers?.[def.key]}
          invalid={invalidKeys?.has(def.key) === true}
          describedBy={describedBy?.[def.key]}
          onChange={(value) => {
            onValueChange(def.key, value);
          }}
          onReset={() => {
            onReset(def.key);
          }}
          resetting={resetting === def.key}
        />
      ))}
    </SettingsGrid>
  );
}

function SettingField({
  def,
  item,
  onChange,
  onReset,
  picker,
  invalid,
  describedBy,
  resetting,
  value,
  variant,
}: {
  readonly def: SettingDef;
  readonly variant: SettingFieldVariant;
  readonly item: SettingItem;
  readonly value: string;
  readonly onChange: (value: string) => void;
  readonly onReset: () => void;
  readonly resetting: boolean;
  readonly picker?: PickerBinding | undefined;
  readonly invalid: boolean;
  readonly describedBy: string | undefined;
}) {
  const { t } = useTranslation();
  const inputId = `setting-${def.key}`;
  const label = t(def.labelKey);
  const isBooleanActive = def.kind === 'bool' && value === 'true';
  const isConfigured = def.kind === 'bool' ? isBooleanActive : item.has_value;
  const status =
    def.kind === 'bool'
      ? t(isBooleanActive ? 'settings.status.active' : 'settings.status.inactive')
      : item.has_value
        ? t('settings.status.configured')
        : t('settings.status.notConfigured');

  const inline = variant === 'inline';

  return (
    <div
      // The field's own hook for tests and for the applied-state banner: the frame it wears
      // is a layout choice, and a query that reaches for `.min-h-32` breaks the day one row
      // stops being a tile.
      data-setting={def.key}
      className={cn(
        'flex flex-col gap-2',
        !inline && 'min-h-32 rounded-md border border-border bg-surface px-3 py-3',
      )}
    >
      <div
        className={cn(
          'flex min-w-0 gap-2',
          // A tile is a column: label left, status right. A full-width row has no right edge
          // worth reaching for, so the badge sits where the reading stops instead.
          inline ? 'flex-wrap items-center' : 'items-start justify-between',
        )}
      >
        <Label htmlFor={inputId} className="min-w-0 break-words text-[13px]">
          {label}
        </Label>
        <Badge variant={isConfigured ? 'success' : 'secondary'}>{status}</Badge>
      </div>
      {def.kind === 'bool' ? (
        <label className="flex min-h-[44px] items-center gap-3 rounded-md border border-input bg-surface-3 px-3 text-[13px] text-text">
          <input
            id={inputId}
            type="checkbox"
            checked={value === 'true'}
            onChange={(event) => {
              onChange(event.target.checked ? 'true' : 'false');
            }}
            className="size-4 accent-accent"
          />
          {t('settings.fields.enabled')}
        </label>
      ) : picker !== undefined ? (
        <ModelPicker
          id={inputId}
          value={value}
          catalog={picker.catalog}
          formatRow={picker.formatRow}
          {...(picker.emptyOption === undefined ? {} : { emptyOption: picker.emptyOption })}
          onChange={picker.onValueChange ?? onChange}
        />
      ) : def.secret ? (
        <SecretInput
          id={inputId}
          value={value}
          placeholder={def.placeholder ?? t('settings.secretPlaceholder')}
          showLabel={t('secret.show', { label })}
          hideLabel={t('secret.hide', { label })}
          onChange={(event) => {
            onChange(event.target.value);
          }}
          className="font-mono text-[13px]"
        />
      ) : (
        <Input
          id={inputId}
          type={def.kind === 'int' ? 'number' : 'text'}
          inputMode={def.kind === 'int' ? 'numeric' : undefined}
          value={value}
          placeholder={def.placeholder}
          {...(invalid ? { 'aria-invalid': true } : {})}
          {...(describedBy === undefined ? {} : { 'aria-describedby': describedBy })}
          onChange={(event) => {
            onChange(event.target.value);
          }}
          className="font-mono text-[13px]"
        />
      )}
      {def.helpKey === undefined ? null : (
        <p className="text-[12px] leading-snug text-text-muted">{t(def.helpKey)}</p>
      )}
      <div className="mt-auto flex items-center justify-between gap-2">
        <div className={cn('flex min-w-0 gap-1', inline ? 'items-baseline gap-2' : 'flex-col')}>
          <code className="min-w-0 break-all text-[12px] text-text-faint">{def.key}</code>
          {inline ? (
            <span aria-hidden="true" className="text-[12px] text-text-faint">
              ·
            </span>
          ) : null}
          {/* Amendment #188: each field says how it reaches the running daemon, so a
              pending restart is attributed to THIS row instead of a pane-level banner. */}
          <span
            data-applied={item.applied}
            className={cn(
              'text-[12px]',
              item.applied === 'restart' ? 'font-medium text-warning' : 'text-text-faint',
            )}
          >
            {t(`settings.applied.${item.applied}`)}
          </span>
        </div>
        {item.overridden ? (
          <Button type="button" variant="ghost" size="sm" disabled={resetting} onClick={onReset}>
            {resetting ? <Spinner /> : <RotateCcw aria-hidden="true" />}
            {t('settings.actions.reset')}
          </Button>
        ) : null}
      </div>
    </div>
  );
}
