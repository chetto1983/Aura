import { useId, useMemo, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Plus, X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { ariaInvalid } from '../a11y/aria';
import { Spinner } from '../components/Spinner';
import { installMcpServer, type McpInstallRequest } from './governanceApi';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';

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
        <Button
          type="button"
          variant="ghost"
          size="icon"
          onClick={onClose}
          aria-label={t('governance.closeAria')}
          className="text-text-muted hover:text-text"
        >
          <X data-icon aria-hidden="true" className="size-4" />
        </Button>
      </header>

      {/* Mode toggle — Recipe | Custom (stdio). */}
      <div
        role="group"
        aria-label={t('governance.mcp.install.modeLabel')}
        className="flex gap-1 rounded-md bg-surface-2 p-1"
      >
        {(['recipe', 'custom'] as const).map((m) => (
          <Button
            key={m}
            type="button"
            variant={mode === m ? 'default' : 'ghost'}
            aria-pressed={mode === m}
            onClick={() => {
              setMode(m);
            }}
            className="flex-1"
          >
            {t(`governance.mcp.install.mode.${m}`)}
          </Button>
        ))}
      </div>

      <div className="flex flex-col gap-1">
        <Label htmlFor={nameId} className="text-[13px] font-semibold text-text">
          {t('governance.mcp.install.nameLabel')}
        </Label>
        <Input
          id={nameId}
          type="text"
          value={name}
          onChange={(event) => {
            setName(event.target.value);
          }}
          placeholder={mode === 'recipe' ? recipe : undefined}
          aria-invalid={ariaInvalid(duplicate)}
          aria-describedby={duplicate ? `${nameId}-err` : undefined}
          className="font-mono text-[13px]"
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
      <Card className="gap-2 bg-surface-3 p-4">
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
      </Card>

      {mode === 'custom' ? (
        <p role="note" className="text-[13px] text-text-muted">
          {t('governance.mcp.install.customBlockedNote')}
        </p>
      ) : null}

      {mutation.isError ? (
        <Alert variant="destructive">
          <AlertDescription>{t('governance.error')}</AlertDescription>
        </Alert>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          disabled={!canInstall}
          aria-busy={mutation.isPending}
          onClick={submit}
        >
          {mutation.isPending ? <Spinner /> : null}
          {t('governance.mcp.install.submit')}
        </Button>
        <Button type="button" variant="outline" disabled={mutation.isPending} onClick={onClose}>
          {t('governance.mcp.install.discard')}
        </Button>
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
        <Label htmlFor={selectId} className="text-[13px] font-semibold text-text">
          {t('governance.mcp.install.recipeLabel')}
        </Label>
        <NativeSelect
          id={selectId}
          value={recipe}
          onChange={(event) => {
            onRecipeChange(event.target.value);
          }}
          className="w-full bg-surface-3 text-[13px]"
        >
          {RECIPES.map((r) => (
            <NativeSelectOption key={r.name} value={r.name}>
              {r.name}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      </div>
      {requiredEnv.map((key) => {
        const fieldId = `recipe-env-${key}`;
        return (
          <div key={key} className="flex flex-col gap-1">
            <Label
              htmlFor={fieldId}
              className="break-all font-mono text-[13px] font-semibold text-text"
            >
              {key}
            </Label>
            <Input
              id={fieldId}
              type="text"
              value={recipeEnv[key] ?? ''}
              onChange={(event) => {
                onEnvChange(key, event.target.value);
              }}
              className="font-mono text-[13px]"
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
        <Label htmlFor={commandId} className="text-[13px] font-semibold text-text">
          {t('governance.mcp.install.commandLabel')}
        </Label>
        <Input
          id={commandId}
          type="text"
          value={command}
          onChange={(event) => {
            onCommandChange(event.target.value);
          }}
          className="font-mono text-[13px]"
        />
      </div>
      <div className="flex flex-col gap-1">
        <span className="text-[13px] font-semibold text-text">
          {t('governance.mcp.install.argsLabel')}
        </span>
        {args.map((arg, index) => (
          <Input
            key={`arg-${String(index)}`}
            type="text"
            aria-label={t('governance.mcp.install.argAria', { index: index + 1 })}
            value={arg}
            onChange={(event) => {
              const next = [...args];
              next[index] = event.target.value;
              onArgsChange(next);
            }}
            className="font-mono text-[13px]"
          />
        ))}
        <Button
          type="button"
          variant="outline"
          onClick={() => {
            onArgsChange([...args, '']);
          }}
          className="self-start"
        >
          <Plus data-icon aria-hidden="true" className="size-4" />
          {t('governance.mcp.install.addArg')}
        </Button>
      </div>
    </div>
  );
}
