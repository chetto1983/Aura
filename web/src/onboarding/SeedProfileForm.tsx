import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { seedFieldTooLong, seedNameMissing, type SeedField } from './onboardingWizardModel';
import type { OnboardingSeed } from './onboardingApi';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';

// SeedProfileForm (Amendment #95) is the typed seed form that REPLACED the 5-question LLM
// interview. Six optional fields, submitted once, mapped deterministically server-side into
// :Entity / :Fact / :Preference writes — no LLM anywhere on this path, so the name stored is
// byte-identical to the name typed (which is precisely what the interview got wrong).
//
// It is PRESENTATIONAL and controlled: the host owns the value, the CTA, and the submit. That is
// what lets ONE component serve BOTH flows — the provisioning wizard's credentials phase (seeding
// the identity it creates) and first-run setup (seeding the signed-in operator) — instead of two
// forms that drift apart.
//
// Leaving the WHOLE form blank is valid — that is the skip. Filling any field makes Name required,
// because the mapper keys every fact off it and would otherwise write them under the placeholder
// subject "operator" with no PERSON entity to resolve against. Both that and exceeding a server cap
// are surfaced inline. aria-invalid is OMITTED when valid (never aria-invalid="false"). 44px
// targets, visible focus rings, label+id wiring, no placeholder-as-label.

// Hoisted: the IANA zone list is ~400 entries and never changes, so it is built once instead of on
// every keystroke re-render. It backs a <datalist> (autocomplete WITH free-text fallback) rather
// than a <select>, because a 400-option select is unusable and the server accepts any string.
const TIME_ZONES: readonly string[] = Intl.supportedValuesOf('timeZone');

export interface SeedProfileFormProps {
  readonly value: OnboardingSeed;
  readonly onChange: (next: OnboardingSeed) => void;
}

export function SeedProfileForm({ value, onChange }: SeedProfileFormProps) {
  const { t } = useTranslation();
  const nameId = useId();
  const langId = useId();
  const locationId = useId();
  const timezoneId = useId();
  const roleId = useId();
  const companyId = useId();
  const timezoneListId = useId();

  const set = (field: SeedField) => (next: string) => {
    onChange({ ...value, [field]: next });
  };

  /** The i18n key of the field's blocking error, or undefined when it is acceptable. Name carries
   * the one conditional requirement: a seed that says anything must say who (see seedNameMissing). */
  function fieldError(field: SeedField): string | undefined {
    if (seedFieldTooLong(field, value[field] ?? '')) return 'onboarding.seed.tooLong';
    if (field === 'name' && seedNameMissing(value)) return 'onboarding.seed.name.required';
    return undefined;
  }

  function fieldProps(field: SeedField, id: string, describedById: string) {
    const invalid = fieldError(field) !== undefined;
    return {
      id,
      value: value[field] ?? '',
      'aria-describedby': invalid ? `${describedById}-error` : describedById,
      'aria-invalid': invalid || undefined,
      onChange: (e: { target: { value: string } }) => {
        set(field)(e.target.value);
      },
    };
  }

  function help(field: SeedField, describedById: string) {
    const error = fieldError(field);
    if (error !== undefined) {
      return (
        <p
          id={`${describedById}-error`}
          role="alert"
          className="text-[13px] leading-relaxed text-danger"
        >
          {t(error)}
        </p>
      );
    }
    return (
      <p id={describedById} className="text-[13px] leading-relaxed text-text-muted">
        {t(`onboarding.seed.${field}.help`)}
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <h2 className="font-display text-[22px] font-semibold text-text">
          {t('onboarding.seed.heading')}
        </h2>
        <p className="max-w-2xl text-[15.5px] leading-relaxed text-text-muted">
          {t('onboarding.seed.intro')}
        </p>
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor={nameId} className="text-[13px] font-semibold text-text">
          {t('onboarding.seed.name.label')}
        </Label>
        <Input
          {...fieldProps('name', nameId, `${nameId}-help`)}
          type="text"
          autoComplete="off"
          placeholder={t('onboarding.seed.name.placeholder')}
          className="bg-surface-2 text-[15.5px]"
        />
        {help('name', `${nameId}-help`)}
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor={langId} className="text-[13px] font-semibold text-text">
          {t('onboarding.seed.lang.label')}
        </Label>
        <NativeSelect
          {...fieldProps('lang', langId, `${langId}-help`)}
          className="w-full bg-surface-2 text-[15.5px]"
        >
          <NativeSelectOption value="">{t('onboarding.seed.lang.unset')}</NativeSelectOption>
          <NativeSelectOption value="it">{t('language.italian')}</NativeSelectOption>
          <NativeSelectOption value="en">{t('language.english')}</NativeSelectOption>
        </NativeSelect>
        {help('lang', `${langId}-help`)}
      </div>

      <div className="grid gap-6 sm:grid-cols-2">
        <div className="flex flex-col gap-2">
          <Label htmlFor={locationId} className="text-[13px] font-semibold text-text">
            {t('onboarding.seed.location.label')}
          </Label>
          <Input
            {...fieldProps('location', locationId, `${locationId}-help`)}
            type="text"
            autoComplete="off"
            placeholder={t('onboarding.seed.location.placeholder')}
            className="bg-surface-2 text-[15.5px]"
          />
          {help('location', `${locationId}-help`)}
        </div>

        <div className="flex flex-col gap-2">
          <Label htmlFor={timezoneId} className="text-[13px] font-semibold text-text">
            {t('onboarding.seed.timezone.label')}
          </Label>
          <Input
            {...fieldProps('timezone', timezoneId, `${timezoneId}-help`)}
            type="text"
            autoComplete="off"
            list={timezoneListId}
            placeholder={t('onboarding.seed.timezone.placeholder')}
            className="bg-surface-2 text-[15.5px]"
          />
          <datalist id={timezoneListId}>
            {TIME_ZONES.map((zone) => (
              <option key={zone} value={zone} />
            ))}
          </datalist>
          {help('timezone', `${timezoneId}-help`)}
        </div>
      </div>

      <div className="grid gap-6 sm:grid-cols-2">
        <div className="flex flex-col gap-2">
          <Label htmlFor={roleId} className="text-[13px] font-semibold text-text">
            {t('onboarding.seed.role.label')}
          </Label>
          <Input
            {...fieldProps('role', roleId, `${roleId}-help`)}
            type="text"
            autoComplete="off"
            placeholder={t('onboarding.seed.role.placeholder')}
            className="bg-surface-2 text-[15.5px]"
          />
          {help('role', `${roleId}-help`)}
        </div>

        <div className="flex flex-col gap-2">
          <Label htmlFor={companyId} className="text-[13px] font-semibold text-text">
            {t('onboarding.seed.company.label')}
          </Label>
          <Input
            {...fieldProps('company', companyId, `${companyId}-help`)}
            type="text"
            autoComplete="off"
            placeholder={t('onboarding.seed.company.placeholder')}
            className="bg-surface-2 text-[15.5px]"
          />
          {help('company', `${companyId}-help`)}
        </div>
      </div>
    </div>
  );
}
