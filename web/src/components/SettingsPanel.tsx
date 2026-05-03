import { useEffect, useMemo, useState } from 'react';
import { Settings as SettingsIcon, Save, FlaskConical, Eye, EyeOff, Loader2 } from 'lucide-react';
import { toast } from 'sonner';
import { Trans } from 'react-i18next';
import { api, ApiError } from '@/api';
import { useLocale } from '@/hooks/useLocale';
import type { SettingItem } from '@/types/api';

type Group = 'provider' | 'embeddings' | 'ocr' | 'budget' | 'summarizer' | 'other';

const GROUP_ORDER: Group[] = ['provider', 'embeddings', 'ocr', 'budget', 'summarizer', 'other'];

export function SettingsPanel() {
  const { t } = useLocale();
  const [items, setItems] = useState<SettingItem[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // pending tracks edited values keyed by setting key. We don't mutate
  // `items` directly so the user can revert changes by reloading.
  const [pending, setPending] = useState<Record<string, string>>({});
  const [revealed, setRevealed] = useState<Record<string, boolean>>({});
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);

  useEffect(() => {
    let cancelled = false;
    api.settings()
      .then((res) => {
        if (cancelled) return;
        setItems(res.items);
        setLoaded(true);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err instanceof ApiError ? err.message : String(err));
        setLoaded(true);
      });
    return () => { cancelled = true; };
  }, []);

  const groups = useMemo(() => {
    const byGroup: Record<string, SettingItem[]> = {};
    for (const it of items) {
      const g = (it.group ?? 'other') as string;
      (byGroup[g] = byGroup[g] || []).push(it);
    }
    return byGroup;
  }, [items]);

  const dirtyKeys = Object.keys(pending);
  const hasChanges = dirtyKeys.length > 0;

  function valueOf(key: string): string {
    if (key in pending) return pending[key];
    return items.find((it) => it.key === key)?.value ?? '';
  }
  function setValue(key: string, value: string) {
    setPending((prev) => ({ ...prev, [key]: value }));
  }
  function revertOne(key: string) {
    setPending((prev) => {
      const next = { ...prev };
      delete next[key];
      return next;
    });
  }

  async function save() {
    if (!hasChanges) return;
    setSaving(true);
    try {
      const res = await api.updateSettings(pending);
      if (res.ok) {
        const fresh = await api.settings();
        setItems(fresh.items);
        setPending({});
        const count = res.applied?.length ?? dirtyKeys.length;
        toast.success(
          count === 1
            ? t('settings.toast.saved_one')
            : t('settings.toast.saved_other', { count }),
        );
      } else {
        toast.error(t('settings.toast.partialFail', { errors: (res.errors ?? []).join('; ') }));
      }
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  async function testProvider() {
    setTesting(true);
    const baseURL = valueOf('LLM_BASE_URL');
    const apiKey = valueOf('LLM_API_KEY');
    try {
      const res = await api.testProvider(baseURL, apiKey, '/models');
      if (res.ok) {
        const detail = res.models && res.models.length > 0 ? `${res.models.length} models available` : 'connected';
        toast.success(t('settings.testSuccess', { detail }));
      } else {
        toast.error(t('settings.testFailed', { error: res.error ?? 'failed' }));
      }
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : String(err));
    } finally {
      setTesting(false);
    }
  }

  const groupLabel = (g: Group): string => {
    switch (g) {
      case 'provider': return t('settings.group.provider');
      case 'embeddings': return t('settings.group.embeddings');
      case 'ocr': return t('settings.group.ocr');
      case 'budget': return t('settings.group.budget');
      case 'summarizer': return t('settings.group.summarizer');
      case 'other': return t('settings.group.other');
    }
  };

  const groupHint = (g: Group): string => {
    switch (g) {
      case 'provider': return t('settings.hint.provider');
      case 'embeddings': return t('settings.hint.embeddings');
      case 'ocr': return t('settings.hint.ocr');
      case 'budget': return t('settings.hint.budget');
      case 'summarizer': return t('settings.hint.summarizer');
      case 'other': return t('settings.hint.other');
    }
  };

  if (error) {
    return (
      <div className="p-6">
        <h1 className="text-2xl font-semibold mb-2">{t('settings.title')}</h1>
        <p className="text-sm text-rose-400">{t('settings.errorLoading')}: {error}</p>
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-4xl px-6 py-8 space-y-8">
      <header className="flex items-start justify-between gap-4 flex-wrap pb-6 border-b border-border/60">
        <div className="space-y-1.5 max-w-xl">
          <h1 className="text-2xl font-semibold tracking-tight flex items-center gap-2.5">
            <SettingsIcon size={22} className="text-muted-foreground" /> {t('settings.title')}
          </h1>
          <p className="text-[13px] text-muted-foreground leading-relaxed">
            <Trans
              i18nKey="settings.description"
              components={{ code: <code className="text-[12px] font-mono" /> }}
            />
          </p>
        </div>
        <div className="flex flex-wrap gap-2 items-center">
          <button
            onClick={testProvider}
            disabled={testing || !valueOf('LLM_BASE_URL')}
            aria-describedby={!valueOf('LLM_BASE_URL') ? 'settings-test-disabled' : undefined}
            title={!valueOf('LLM_BASE_URL') ? t('settings.testDisabled') : t('settings.testHint')}
            className="min-h-11 text-[13px] rounded-md px-3 bg-secondary/60 hover:bg-secondary border border-border/80 flex items-center gap-1.5 disabled:opacity-40 transition-colors"
          >
            {testing ? <Loader2 size={14} className="animate-spin" /> : <FlaskConical size={14} />}
            {t('settings.testConnection')}
          </button>
          <button
            onClick={save}
            disabled={!hasChanges || saving}
            aria-describedby={!hasChanges ? 'settings-save-disabled' : undefined}
            title={!hasChanges ? t('settings.saveDisabled') : t('settings.saveHint')}
            className="min-h-11 text-[13px] rounded-md px-3.5 bg-primary text-primary-foreground hover:brightness-105 active:brightness-95 flex items-center gap-1.5 disabled:opacity-40 disabled:cursor-not-allowed transition-[filter,opacity]"
          >
            {saving ? <Loader2 size={14} className="animate-spin" /> : <Save size={14} />}
            {hasChanges ? t('settings.saveCount', { count: dirtyKeys.length }) : t('settings.save')}
          </button>
          <span id="settings-test-disabled" className="sr-only">{t('settings.testDisabled')}</span>
          <span id="settings-save-disabled" className="sr-only">{t('settings.saveDisabled')}</span>
        </div>
      </header>

      {!loaded && <p className="text-[13px] text-muted-foreground">{t('common.loading')}</p>}

      {loaded && GROUP_ORDER.map((group) => {
        const groupItems = groups[group];
        if (!groupItems || groupItems.length === 0) return null;
        return (
          <section key={group} className="rounded-lg border border-border/80 bg-card overflow-hidden">
            <div className="px-5 py-4 border-b border-border/60 bg-card">
              <h2 className="text-[11px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">
                {groupLabel(group)}
              </h2>
              <p className="text-[12.5px] text-muted-foreground/90 mt-1">{groupHint(group)}</p>
            </div>
            <div className="divide-y divide-border/40">
              {groupItems.map((it) => (
                <SettingRow
                  key={it.key}
                  item={it}
                  value={valueOf(it.key)}
                  dirty={it.key in pending}
                  revealed={!!revealed[it.key]}
                  onChange={(v) => setValue(it.key, v)}
                  onRevert={() => revertOne(it.key)}
                  onToggleReveal={() =>
                    setRevealed((prev) => ({ ...prev, [it.key]: !prev[it.key] }))
                  }
                />
              ))}
            </div>
          </section>
        );
      })}
    </div>
  );
}

function SettingRow({
  item, value, dirty, revealed, onChange, onRevert, onToggleReveal,
}: {
  item: SettingItem;
  value: string;
  dirty: boolean;
  revealed: boolean;
  onChange: (v: string) => void;
  onRevert: () => void;
  onToggleReveal: () => void;
}) {
  const { t } = useLocale();
  const sourceBadge = (() => {
    if (dirty) return { label: t('settings.badge.edited'), cls: 'bg-amber-500/12 text-amber-700 dark:text-amber-300 border-amber-500/40' };
    switch (item.source) {
      case 'db':
        return { label: t('settings.badge.saved'), cls: 'bg-primary/12 text-cyan-700 dark:text-cyan-300 border-primary/40' };
      case 'env':
        return { label: t('settings.badge.env'), cls: 'bg-sky-500/12 text-sky-700 dark:text-sky-300 border-sky-500/40' };
      default:
        return { label: t('settings.badge.unset'), cls: 'bg-muted/50 text-foreground border-border' };
    }
  })();
  return (
    <div className="grid grid-cols-1 md:grid-cols-[260px_1fr_auto] gap-x-5 gap-y-1.5 md:items-center px-5 py-3.5">
      <label className="min-w-0" htmlFor={item.key}>
        <div className="text-[13px] font-medium text-foreground flex items-center gap-1.5 flex-wrap">
          <span className="truncate">{item.label ?? item.key}</span>
          <span className={`text-[9.5px] font-medium uppercase tracking-[0.06em] px-1.5 py-px rounded-[4px] border whitespace-nowrap ${sourceBadge.cls}`}>
            {sourceBadge.label}
          </span>
        </div>
        <div className="text-[10.5px] font-mono text-muted-foreground mt-0.5 truncate">{item.key}</div>
        {item.hint && <div className="text-[12px] text-muted-foreground mt-1.5 leading-snug">{item.hint}</div>}
      </label>
      <div className="flex gap-1.5 min-w-0 items-center">
        <Control
          item={item}
          value={value}
          revealed={revealed}
          onChange={onChange}
          onToggleReveal={onToggleReveal}
        />
      </div>
      <div className="flex items-center justify-end">
        {dirty ? (
          <button
            type="button"
            onClick={onRevert}
            className="text-[11px] uppercase tracking-[0.06em] text-amber-500 dark:text-amber-300 hover:text-amber-600 dark:hover:text-amber-200 px-2 py-1 rounded transition-colors"
            title={t('settings.action.discardHint')}
          >
            {t('settings.action.revert')}
          </button>
        ) : null}
      </div>
    </div>
  );
}

function Control({
  item, value, revealed, onChange, onToggleReveal,
}: {
  item: SettingItem;
  value: string;
  revealed: boolean;
  onChange: (v: string) => void;
  onToggleReveal: () => void;
}) {
  const { t } = useLocale();
  const kind = item.kind ?? 'text';

  if (kind === 'bool') {
    const on = value === 'true' || value === '1';
    // index.css has a global `button { background: none; border: none; }`
    // reset that defeats Tailwind utilities AND any inline borderColor
    // (because border-style: none stays applied). Set everything via
    // inline shorthand so all three border sub-properties + background
    // + box-shadow override the reset cleanly.
    const trackOn = 'var(--primary, #06b6d4)';
    const trackOff = '#a1a1aa'; // zinc-400 — readable on both light card AND dark card
    const borderOn = 'var(--primary, #0891b2)';
    const borderOff = '#71717a'; // zinc-500
    return (
      <button
        id={item.key}
        type="button"
        role="switch"
        aria-checked={on}
        data-state={on ? 'checked' : 'unchecked'}
        onClick={() => onChange(on ? 'false' : 'true')}
        title={on ? t('settings.action.disable') : t('settings.action.enable')}
        style={{
          height: 32,
          width: 52,
          borderRadius: 9999,
          background: on ? trackOn : trackOff,
          border: `1px solid ${on ? borderOn : borderOff}`,
          boxShadow: 'inset 0 1px 2px rgba(0,0,0,0.35)',
          transition: 'background 120ms ease, border-color 120ms ease',
          position: 'relative',
          padding: 0,
          cursor: 'pointer',
          flexShrink: 0,
        }}
      >
        <span
          aria-hidden
          style={{
            position: 'absolute',
            top: 2,
            left: 2,
            width: 26,
            height: 26,
            borderRadius: 9999,
            background: '#ffffff',
            boxShadow: '0 1px 3px rgba(0,0,0,0.45), 0 0 0 1px rgba(0,0,0,0.18)',
            transform: `translateX(${on ? 20 : 0}px)`,
            transition: 'transform 120ms ease',
          }}
        />
        <span className="sr-only">{on ? t('settings.status.enabled') : t('settings.status.disabled')}</span>
      </button>
    );
  }

  // 2026 input styling: 36px height, 6px radius, 3px tinted focus halo,
  // hover lifts the border alpha. Tailwind `border-border` was too
  // light in light mode (hairline against white card looked invisible),
  // so pin to a stronger token via inline style on the input itself.
  const fieldCls = 'min-h-11 w-full text-[13px] font-mono rounded-md bg-background px-3 transition-[border-color,box-shadow] duration-[120ms] focus:outline-none';
  const fieldStyle: React.CSSProperties = {
    border: '1px solid var(--border, oklch(0.85 0.01 240))',
    boxShadow: 'inset 0 0 0 1px transparent',
  };

  if (kind === 'enum') {
    return (
      <select
        id={item.key}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        style={fieldStyle}
        className={`${fieldCls} pr-8 cursor-pointer`}
      >
        {!item.options?.includes(value) && value !== '' && <option value={value}>{value}</option>}
        {item.options?.map((opt) => (
          <option key={opt} value={opt}>{opt}</option>
        ))}
      </select>
    );
  }

  if (kind === 'int' || kind === 'float') {
    return (
      <input
        id={item.key}
        type="number"
        step={kind === 'float' ? 'any' : '1'}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        style={fieldStyle}
        className={fieldCls}
      />
    );
  }

  // text / url / secret
  const inputType = item.is_secret && !revealed ? 'password' : (kind === 'url' ? 'url' : 'text');
  return (
    <>
      <input
        id={item.key}
        type={inputType}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        autoComplete="off"
        spellCheck={false}
        style={fieldStyle}
        className={fieldCls}
      />
      {item.is_secret && (
        <button
          type="button"
          onClick={onToggleReveal}
          title={revealed ? t('settings.action.hide') : t('settings.action.reveal')}
          style={{ border: '1px solid var(--border, oklch(0.85 0.01 240))', background: 'var(--secondary, oklch(0.92 0.01 240))' }}
          className="h-11 w-11 shrink-0 inline-flex items-center justify-center rounded-md hover:brightness-95 transition text-muted-foreground hover:text-foreground"
        >
          {revealed ? <EyeOff size={14} /> : <Eye size={14} />}
        </button>
      )}
    </>
  );
}
