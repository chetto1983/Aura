import { useEffect, useId, useState } from 'react';
import { RefreshCw, Save, UserRound } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import {
  emptyProfile,
  fetchProfile,
  formatList,
  parseList,
  saveProfile,
  type ProfileDoc,
} from './profileApi';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';

// ProfilePanel is where the operator profile becomes editable.
//
// Until now it was written exactly once, by the onboarding seed form, and the only fields it
// asked for were six. Everything else the agent reads about the operator — expertise, current
// projects, preferred tone, and the vetoes that ride the always-block as hard rules — had no
// way in and, worse, no way to change: a timezone typed once was wrong the first time you
// travelled, and a veto you no longer want could not be withdrawn.
//
// The lists are one comma-separated line each. Seven list editors would bury the four fields
// that carry most of the meaning, and every entry here is a short phrase.
//
// It sits ABOVE the admin gate in the settings page on purpose: every operator has a profile,
// only an admin has runtime settings.

// The IANA zone list is ~400 entries and never changes — built once, and backing a <datalist>
// so the field autocompletes while still accepting anything the server will take.
const TIME_ZONES: readonly string[] = Intl.supportedValuesOf('timeZone');

type ListField = 'expertise' | 'stack' | 'projects' | 'goals' | 'interests' | 'people' | 'vetoes';
type TextField =
  | 'name'
  | 'role'
  | 'company'
  | 'location'
  | 'timezone'
  | 'lang'
  | 'tonePreference'
  | 'responseLength';

const TEXT_FIELDS: readonly TextField[] = [
  'name',
  'role',
  'company',
  'location',
  'timezone',
  'lang',
  'tonePreference',
  'responseLength',
];

const LIST_FIELDS: readonly ListField[] = [
  'expertise',
  'stack',
  'projects',
  'goals',
  'interests',
  'people',
];

export function ProfilePanel() {
  const { t } = useTranslation();
  const headingId = useId();
  const timezoneListId = useId();
  const [profile, setProfile] = useState<ProfileDoc>(emptyProfile);
  const [status, setStatus] = useState<'loading' | 'ready' | 'error'>('loading');
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [saveError, setSaveError] = useState<string | undefined>();

  async function reload() {
    setStatus('loading');
    try {
      setProfile(await fetchProfile());
      setStatus('ready');
    } catch {
      setStatus('error');
    }
  }

  useEffect(() => {
    let cancelled = false;
    void fetchProfile()
      .then((loaded) => {
        if (cancelled) return;
        setProfile(loaded);
        setStatus('ready');
      })
      .catch(() => {
        if (!cancelled) setStatus('error');
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // One mutator: every edit clears the "saved" note, so the note can never outlive the state
  // it describes.
  function update(patch: Partial<ProfileDoc>) {
    setSaved(false);
    setProfile((current) => ({ ...current, ...patch }));
  }

  function setList(field: ListField, raw: string) {
    update({ [field]: parseList(raw) });
  }

  async function submit() {
    setSaving(true);
    setSaveError(undefined);
    try {
      // The server's normalized answer wins: it trims and drops blanks, so echoing it back
      // into the form shows what was actually stored rather than what was typed.
      setProfile(await saveProfile(profile));
      setSaved(true);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  if (status === 'loading') {
    return (
      <div role="status" className="flex items-center gap-3 py-6 text-sm text-text-muted">
        <Spinner />
        {t('profile.loading')}
      </div>
    );
  }

  if (status === 'error') {
    return (
      <Alert variant="destructive">
        <AlertDescription>
          <span>{t('profile.error')}</span>
          <Button type="button" variant="outline" onClick={() => void reload()} className="mt-3">
            <RefreshCw aria-hidden="true" />
            {t('settings.actions.retry')}
          </Button>
        </AlertDescription>
      </Alert>
    );
  }

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <h2 id={headingId} className="flex items-center gap-2 text-[20px] font-semibold text-text">
          <UserRound aria-hidden="true" className="size-5 text-accent-text" />
          {t('profile.heading')}
        </h2>
        <p className="max-w-3xl text-[15.5px] leading-relaxed text-text-muted">
          {t('profile.body')}
        </p>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        {TEXT_FIELDS.map((field) => (
          <ProfileTextField
            key={field}
            field={field}
            value={profile[field]}
            listId={field === 'timezone' ? timezoneListId : undefined}
            onChange={(next) => {
              update({ [field]: next });
            }}
          />
        ))}
      </div>
      <datalist id={timezoneListId}>
        {TIME_ZONES.map((zone) => (
          <option key={zone} value={zone} />
        ))}
      </datalist>

      <div className="grid gap-4 sm:grid-cols-2">
        {LIST_FIELDS.map((field) => (
          <ProfileListField
            key={field}
            field={field}
            value={formatList(profile[field])}
            onChange={(next) => {
              setList(field, next);
            }}
          />
        ))}
      </div>

      <ProfileTextArea
        label={t('profile.fields.customInstructions')}
        hint={t('profile.hints.customInstructions')}
        value={profile.customInstructions}
        onChange={(next) => {
          update({ customInstructions: next });
        }}
      />

      <ProfileTextArea
        label={t('profile.fields.vetoes')}
        hint={t('profile.hints.vetoes')}
        value={formatList(profile.vetoes)}
        onChange={(next) => {
          setList('vetoes', next);
        }}
      />

      <div className="flex flex-wrap items-center gap-3">
        <Button type="button" onClick={() => void submit()} disabled={saving}>
          {saving ? <Spinner /> : <Save aria-hidden="true" />}
          {t('profile.actions.save')}
        </Button>
        {saved ? (
          <span role="status" className="text-[13px] text-text-muted">
            {t('profile.saved')}
          </span>
        ) : null}
      </div>

      {saveError !== undefined ? (
        <Alert variant="destructive">
          <AlertDescription>{t('profile.saveError', { message: saveError })}</AlertDescription>
        </Alert>
      ) : null}
    </section>
  );
}

interface FieldProps {
  readonly value: string;
  readonly onChange: (next: string) => void;
}

function ProfileTextField({
  field,
  value,
  listId,
  onChange,
}: FieldProps & { readonly field: TextField; readonly listId: string | undefined }) {
  const { t } = useTranslation();
  const id = useId();
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{t(`profile.fields.${field}`)}</Label>
      <Input
        id={id}
        value={value}
        list={listId}
        onChange={(e) => {
          onChange(e.target.value);
        }}
      />
    </div>
  );
}

function ProfileListField({ field, value, onChange }: FieldProps & { readonly field: ListField }) {
  const { t } = useTranslation();
  const id = useId();
  const hintId = `${id}-hint`;
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{t(`profile.fields.${field}`)}</Label>
      <Input
        id={id}
        value={value}
        aria-describedby={hintId}
        onChange={(e) => {
          onChange(e.target.value);
        }}
      />
      <p id={hintId} className="text-[13px] leading-relaxed text-text-muted">
        {t('profile.hints.list')}
      </p>
    </div>
  );
}

function ProfileTextArea({
  label,
  hint,
  value,
  onChange,
}: FieldProps & { readonly label: string; readonly hint: string }) {
  const id = useId();
  const hintId = `${id}-hint`;
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={id}>{label}</Label>
      <Textarea
        id={id}
        rows={3}
        value={value}
        aria-describedby={hintId}
        onChange={(e) => {
          onChange(e.target.value);
        }}
      />
      <p id={hintId} className="text-[13px] leading-relaxed text-text-muted">
        {hint}
      </p>
    </div>
  );
}
