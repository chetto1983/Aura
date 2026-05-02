import { useEffect, useMemo, useState } from 'react';
import { Settings as SettingsIcon, Save, FlaskConical, Eye, EyeOff, Loader2 } from 'lucide-react';
import { toast } from 'sonner';
import { api, ApiError } from '@/api';
import type { SettingItem } from '@/types/api';

type Group = 'provider' | 'embeddings' | 'ocr' | 'budget' | 'summarizer' | 'other';

const GROUP_ORDER: Group[] = ['provider', 'embeddings', 'ocr', 'budget', 'summarizer', 'other'];
const GROUP_LABEL: Record<Group, string> = {
  provider: 'LLM provider',
  embeddings: 'Wiki search (embeddings)',
  ocr: 'PDF OCR',
  budget: 'Budget & context',
  summarizer: 'Summarizer',
  other: 'Other',
};
const GROUP_HINT: Record<Group, string> = {
  provider: 'The model the bot uses for chat. Test the connection before saving.',
  embeddings: 'Optional. Powers wiki search. Mistral free tier is enough for personal use.',
  ocr: 'Optional. Lets the bot ingest PDFs you send through Telegram.',
  budget: 'Spend caps and conversation sizing. Defaults are conservative.',
  summarizer: 'Auto-distills chat into wiki memory. Off by default; review mode is the safe pick.',
  other: 'Misc settings.',
};

export function SettingsPanel() {
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
        toast.success(`Saved ${res.applied?.length ?? dirtyKeys.length} setting${(res.applied?.length ?? 0) === 1 ? '' : 's'}.`);
      } else {
        toast.error(`Save partially failed: ${(res.errors ?? []).join('; ')}`);
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
        toast.success(`✓ ${detail}`);
      } else {
        toast.error(`✗ ${res.error ?? 'failed'}`);
      }
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : String(err));
    } finally {
      setTesting(false);
    }
  }

  if (error) {
    return (
      <div className="p-6">
        <h1 className="text-2xl font-semibold mb-2">Settings</h1>
        <p className="text-sm text-rose-400">Failed to load settings: {error}</p>
      </div>
    );
  }

  return (
    <div className="p-6 space-y-6">
      <header className="flex items-start justify-between gap-4 flex-wrap">
        <div>
          <h1 className="text-2xl font-semibold flex items-center gap-2">
            <SettingsIcon size={20} /> Settings
          </h1>
          <p className="text-xs text-muted-foreground mt-1">
            Tunable values applied on top of <code>.env</code>. Edits persist in <code>aura.db</code> and take effect on the next conversation turn — no restart needed for most fields. Bootstrap settings (Telegram token, dashboard port, file paths) stay in <code>.env</code>.
          </p>
        </div>
        <div className="flex gap-2 items-center">
          <button
            onClick={testProvider}
            disabled={testing || !valueOf('LLM_BASE_URL')}
            className="text-sm rounded-md px-3 py-1.5 bg-secondary hover:bg-secondary/80 border border-border flex items-center gap-1.5 disabled:opacity-50"
          >
            {testing ? <Loader2 size={14} className="animate-spin" /> : <FlaskConical size={14} />}
            Test connection
          </button>
          <button
            onClick={save}
            disabled={!hasChanges || saving}
            className="text-sm rounded-md px-3 py-1.5 bg-primary text-primary-foreground hover:brightness-110 flex items-center gap-1.5 disabled:opacity-50"
          >
            {saving ? <Loader2 size={14} className="animate-spin" /> : <Save size={14} />}
            Save {hasChanges ? `(${dirtyKeys.length})` : ''}
          </button>
        </div>
      </header>

      {!loaded && <p className="text-sm text-muted-foreground">Loading...</p>}

      {loaded && GROUP_ORDER.map((group) => {
        const groupItems = groups[group];
        if (!groupItems || groupItems.length === 0) return null;
        return (
          <section key={group} className="rounded-lg border border-border bg-card p-4">
            <h2 className="text-sm font-semibold mb-1">{GROUP_LABEL[group]}</h2>
            <p className="text-xs text-muted-foreground mb-4">{GROUP_HINT[group]}</p>
            <div className="space-y-3">
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
  const sourceBadge = (() => {
    if (dirty) return { label: 'edited', cls: 'bg-amber-500/15 text-amber-300 border-amber-500/30' };
    switch (item.source) {
      case 'db':
        return { label: 'saved', cls: 'bg-primary/15 text-primary border-primary/30' };
      case 'env':
        return { label: 'from .env', cls: 'bg-sky-500/15 text-sky-300 border-sky-500/30' };
      default:
        return { label: 'default', cls: 'bg-muted/40 text-muted-foreground border-border' };
    }
  })();
  return (
    <div className="grid grid-cols-1 md:grid-cols-[200px_1fr_auto] gap-2 md:items-center">
      <label className="text-xs text-muted-foreground" htmlFor={item.key}>
        <div className="font-medium text-foreground/90 flex items-center gap-1.5 flex-wrap">
          {item.label ?? item.key}
          <span className={`text-[9px] uppercase tracking-wider px-1.5 py-0.5 rounded border ${sourceBadge.cls}`}>
            {sourceBadge.label}
          </span>
        </div>
        <div className="text-[10px] font-mono opacity-60">{item.key}</div>
        {item.hint && <div className="text-[11px] text-muted-foreground/80 mt-1">{item.hint}</div>}
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
      {dirty ? (
        <button
          type="button"
          onClick={onRevert}
          className="text-xs text-amber-400 hover:text-amber-300 px-2 py-1"
          title="Discard this change"
        >
          revert
        </button>
      ) : (
        <span className="text-xs text-muted-foreground/40 px-2">·</span>
      )}
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
        title={on ? 'Click to disable' : 'Click to enable'}
        style={{
          height: 24,
          width: 44,
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
            width: 18,
            height: 18,
            borderRadius: 9999,
            background: '#ffffff',
            boxShadow: '0 1px 3px rgba(0,0,0,0.45), 0 0 0 1px rgba(0,0,0,0.18)',
            transform: `translateX(${on ? 20 : 0}px)`,
            transition: 'transform 120ms ease',
          }}
        />
        <span className="sr-only">{on ? 'enabled' : 'disabled'}</span>
      </button>
    );
  }

  if (kind === 'enum') {
    return (
      <select
        id={item.key}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="w-full text-sm rounded-md bg-background border border-border px-2.5 py-1.5 focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/20"
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
        className="w-full text-sm font-mono rounded-md bg-background border border-border px-2.5 py-1.5 focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/20"
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
        className="w-full text-sm font-mono rounded-md bg-background border border-border px-2.5 py-1.5 focus:outline-none focus:border-primary focus:ring-2 focus:ring-primary/20"
      />
      {item.is_secret && (
        <button
          type="button"
          onClick={onToggleReveal}
          title={revealed ? 'Hide' : 'Reveal'}
          className="rounded-md px-2 bg-secondary hover:bg-secondary/80 border border-border"
        >
          {revealed ? <EyeOff size={14} /> : <Eye size={14} />}
        </button>
      )}
    </>
  );
}
