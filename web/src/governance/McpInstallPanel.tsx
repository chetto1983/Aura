import { useId, useMemo, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { ariaInvalid } from '../a11y/aria';
import { installMcpServer, type McpInstallRequest } from './governanceApi';

// McpInstallPanel (MCPW-01) — the install form in the BoardLayout detail slot. A 2-segment
// Recipe | Custom (stdio) toggle (Recipe default, lower-risk/guided); Recipe renders a
// dropdown of the built-in catalog recipes and their RequiredEnv as a guided form; Custom
// renders command + repeatable args + env key/value rows. A live read-only PREVIEW shows the
// CLI-equivalent (`aura mcp install …` / `aura mcp add … -- …`) and the `Will write to:`
// managed-config destination BEFORE save (MCPW-01 AC). A duplicate name → an inline aria-invalid
// field error + Install disabled, no request fires (idempotency edge). Abandon = Discard install.
//
// The catalog is mirrored as a frontend descriptor (the read DTO does not expose it); the
// recipe names + RequiredEnv match internal/mcp/manager/catalog.go BuiltInCatalog().

interface RecipeDescriptor {
  readonly name: string;
  readonly requiredEnv: readonly string[];
}

// BuiltInCatalog() recipes (catalog.go) — none currently declare RequiredEnv, but the guided
// form renders any future RequiredEnv as labelled inputs (secret-typed vars are masked).
const RECIPES: readonly RecipeDescriptor[] = [
  { name: 'calculator', requiredEnv: [] },
  { name: 'calendar', requiredEnv: [] },
  { name: 'memory', requiredEnv: [] },
  { name: 'whatsapp', requiredEnv: [] },
];

/** The default managed-config destination shown in the preview before save (the real path /
 * override source is confirmed in the install response). */
const DEFAULT_DESTINATION = '~/.aura/mcp/servers.json';

type Mode = 'recipe' | 'custom';

export interface McpInstallPanelProps {
  /** The existing server names — a name collision blocks Install with an inline error. */
  readonly existingNames: readonly string[];
  readonly onClose: () => void;
}

export function McpInstallPanel({ existingNames, onClose }: McpInstallPanelProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const headingId = useId();
  const nameId = useId();

  const [mode, setMode] = useState<Mode>('recipe');
  const [recipe, setRecipe] = useState<string>(RECIPES[0]?.name ?? '');
  const [name, setName] = useState('');
  const [command, setCommand] = useState('');
  const [args, setArgs] = useState<string[]>(['']);
  const [recipeEnv, setRecipeEnv] = useState<Record<string, string>>({});

  const existing = useMemo(() => new Set(existingNames), [existingNames]);

  // The effective install name: in recipe mode the name defaults to the recipe name unless the
  // operator overrode it; in custom mode it is the typed name.
  const effectiveName = name.trim() !== '' ? name.trim() : mode === 'recipe' ? recipe : '';
  const duplicate = effectiveName !== '' && existing.has(effectiveName);

  const selectedRecipe = RECIPES.find((r) => r.name === recipe);
  const requiredEnv = mode === 'recipe' ? (selectedRecipe?.requiredEnv ?? []) : [];

  const mutation = useMutation({
    mutationFn: (req: McpInstallRequest) => installMcpServer(req),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['governance', 'mcp'] });
      onClose();
    },
  });

  const cliEquivalent =
    mode === 'recipe'
      ? `aura mcp install ${recipe}`
      : `aura mcp add ${effectiveName || '<name>'}${command.trim() !== '' ? ` -- ${command.trim()}` : ''}`;

  function buildRequest(): McpInstallRequest {
    if (mode === 'recipe') {
      const env = requiredEnv
        .filter((key) => (recipeEnv[key] ?? '') !== '')
        .map((key) => `${key}=${recipeEnv[key] ?? ''}`);
      return {
        name: effectiveName,
        recipe,
        ...(env.length > 0 ? { env } : {}),
      };
    }
    return {
      name: effectiveName,
      command: command.trim(),
      args: args.map((a) => a.trim()).filter((a) => a !== ''),
    };
  }

  const canInstall =
    effectiveName !== '' &&
    !duplicate &&
    !mutation.isPending &&
    (mode === 'recipe' ? recipe !== '' : command.trim() !== '');

  function submit() {
    if (!canInstall) return;
    mutation.mutate(buildRequest());
  }

  return (
    <section
      aria-labelledby={headingId}
      className="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-4"
    >
      <header className="flex items-start justify-between gap-2">
        <h3 id={headingId} className="font-display text-[20px] font-semibold text-text">
          {t('governance.mcp.install.heading')}
        </h3>
        <button
          type="button"
          onClick={onClose}
          aria-label={t('governance.closeAria')}
          className="min-h-[44px] min-w-[44px] rounded-md text-text-muted hover:text-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          ✕
        </button>
      </header>

      {/* Mode toggle — Recipe | Custom (stdio). */}
      <div
        role="group"
        aria-label={t('governance.mcp.install.modeLabel')}
        className="flex gap-1 rounded-md bg-surface-2 p-1"
      >
        {(['recipe', 'custom'] as const).map((m) => (
          <button
            key={m}
            type="button"
            aria-pressed={mode === m}
            onClick={() => {
              setMode(m);
            }}
            className={`min-h-[44px] flex-1 rounded-md px-3 py-2 text-[13px] font-semibold transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${
              mode === m
                ? 'bg-accent text-on-accent'
                : 'text-text-muted hover:bg-surface-3 hover:text-text'
            }`}
          >
            {t(`governance.mcp.install.mode.${m}`)}
          </button>
        ))}
      </div>

      <div className="flex flex-col gap-1">
        <label htmlFor={nameId} className="text-[13px] font-semibold text-text">
          {t('governance.mcp.install.nameLabel')}
        </label>
        <input
          id={nameId}
          type="text"
          value={name}
          onChange={(event) => {
            setName(event.target.value);
          }}
          placeholder={mode === 'recipe' ? recipe : undefined}
          aria-invalid={ariaInvalid(duplicate)}
          aria-describedby={duplicate ? `${nameId}-err` : undefined}
          className="w-full rounded-md border border-border bg-surface-3 px-3 py-2 font-mono text-[13px] text-text outline-none focus-visible:ring-2 focus-visible:ring-ring"
        />
        {duplicate ? (
          <p id={`${nameId}-err`} role="alert" className="text-[13px] text-danger">
            {t('governance.mcp.install.duplicateName', { name: effectiveName })}
          </p>
        ) : null}
      </div>

      {mode === 'recipe' ? (
        <RecipeFields
          recipe={recipe}
          onRecipeChange={setRecipe}
          requiredEnv={requiredEnv}
          recipeEnv={recipeEnv}
          onEnvChange={(key, value) => {
            setRecipeEnv((prev) => ({ ...prev, [key]: value }));
          }}
        />
      ) : (
        <CustomFields
          command={command}
          onCommandChange={setCommand}
          args={args}
          onArgsChange={setArgs}
        />
      )}

      {/* Live read-only preview — CLI-equivalent + the destination, BEFORE save. */}
      <div className="flex flex-col gap-2 rounded-md bg-surface-3 p-4">
        <div className="flex flex-col gap-1">
          <span className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
            {t('governance.mcp.install.cliLabel')}
          </span>
          <code className="break-all font-mono text-[13px] text-text">{cliEquivalent}</code>
        </div>
        <div className="flex flex-col gap-1">
          <span className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
            {t('governance.mcp.install.willWriteTo')}
          </span>
          <code className="break-all font-mono text-[13px] text-text">{DEFAULT_DESTINATION}</code>
        </div>
      </div>

      {mode === 'custom' ? (
        <p role="note" className="text-[13px] text-text-muted">
          {t('governance.mcp.install.customBlockedNote')}
        </p>
      ) : null}

      {mutation.isError ? (
        <p role="alert" className="text-[13px] text-danger">
          {t('governance.error')}
        </p>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          disabled={!canInstall}
          onClick={submit}
          className="min-h-[44px] rounded-md bg-accent px-4 py-2 text-[13px] font-semibold text-on-accent outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
        >
          {t('governance.mcp.install.submit')}
        </button>
        <button
          type="button"
          disabled={mutation.isPending}
          onClick={onClose}
          className="min-h-[44px] rounded-md border border-border-strong bg-surface-2 px-4 py-2 text-[13px] font-semibold text-text outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
        >
          {t('governance.mcp.install.discard')}
        </button>
      </div>
    </section>
  );
}

function RecipeFields({
  recipe,
  onRecipeChange,
  requiredEnv,
  recipeEnv,
  onEnvChange,
}: {
  readonly recipe: string;
  readonly onRecipeChange: (next: string) => void;
  readonly requiredEnv: readonly string[];
  readonly recipeEnv: Record<string, string>;
  readonly onEnvChange: (key: string, value: string) => void;
}) {
  const { t } = useTranslation();
  const selectId = useId();
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-1">
        <label htmlFor={selectId} className="text-[13px] font-semibold text-text">
          {t('governance.mcp.install.recipeLabel')}
        </label>
        <select
          id={selectId}
          value={recipe}
          onChange={(event) => {
            onRecipeChange(event.target.value);
          }}
          className="w-full rounded-md border border-border bg-surface-3 px-3 py-2 text-[13px] text-text outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          {RECIPES.map((r) => (
            <option key={r.name} value={r.name}>
              {r.name}
            </option>
          ))}
        </select>
      </div>
      {requiredEnv.map((key) => {
        const fieldId = `recipe-env-${key}`;
        return (
          <div key={key} className="flex flex-col gap-1">
            <label
              htmlFor={fieldId}
              className="break-all font-mono text-[13px] font-semibold text-text"
            >
              {key}
            </label>
            <input
              id={fieldId}
              type="text"
              value={recipeEnv[key] ?? ''}
              onChange={(event) => {
                onEnvChange(key, event.target.value);
              }}
              className="w-full rounded-md border border-border bg-surface-3 px-3 py-2 font-mono text-[13px] text-text outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
          </div>
        );
      })}
    </div>
  );
}

function CustomFields({
  command,
  onCommandChange,
  args,
  onArgsChange,
}: {
  readonly command: string;
  readonly onCommandChange: (next: string) => void;
  readonly args: readonly string[];
  readonly onArgsChange: (next: string[]) => void;
}) {
  const { t } = useTranslation();
  const commandId = useId();
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-1">
        <label htmlFor={commandId} className="text-[13px] font-semibold text-text">
          {t('governance.mcp.install.commandLabel')}
        </label>
        <input
          id={commandId}
          type="text"
          value={command}
          onChange={(event) => {
            onCommandChange(event.target.value);
          }}
          className="w-full rounded-md border border-border bg-surface-3 px-3 py-2 font-mono text-[13px] text-text outline-none focus-visible:ring-2 focus-visible:ring-ring"
        />
      </div>
      <div className="flex flex-col gap-1">
        <span className="text-[13px] font-semibold text-text">
          {t('governance.mcp.install.argsLabel')}
        </span>
        {args.map((arg, index) => (
          <input
            key={`arg-${String(index)}`}
            type="text"
            aria-label={t('governance.mcp.install.argAria', { index: index + 1 })}
            value={arg}
            onChange={(event) => {
              const next = [...args];
              next[index] = event.target.value;
              onArgsChange(next);
            }}
            className="w-full rounded-md border border-border bg-surface-3 px-3 py-2 font-mono text-[13px] text-text outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
        ))}
        <button
          type="button"
          onClick={() => {
            onArgsChange([...args, '']);
          }}
          className="min-h-[44px] self-start rounded-md border border-border-strong bg-surface-2 px-3 py-2 text-[13px] font-semibold text-text outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring"
        >
          {t('governance.mcp.install.addArg')}
        </button>
      </div>
    </div>
  );
}
