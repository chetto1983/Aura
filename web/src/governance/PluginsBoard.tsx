import { useId, useState } from 'react';
import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
} from '@tanstack/react-query';
import { RefreshCw, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { HttpError } from '../api/json';
import { BoardStateView, boardStatus } from './governanceView';
import { PluginInstallPanel } from './PluginInstallPanel';
import { PluginRemoveDialog } from './PluginRemoveDialog';
import {
  fetchPlugins,
  removePlugin,
  shortCommit,
  upgradePlugin,
  type PluginRecord,
  type PluginRemoveReport,
  type PluginStep,
  type PluginUpgradeReport,
} from './pluginApi';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';

// PluginsBoard — install a plugin and see what each installed one brought. It is one column,
// not the master + detail of the other boards: a row holds everything a plugin has, so there
// is no detail to open, and the install field stays in view as the spec asks.
//
// A row updates its plugin as Codex's `plugin marketplace upgrade` does: one click, no review,
// and the old version stays when a new connector does not start.
//
// The list refetches on window focus: the agent installs plugins from chat too, and a board
// left open on another tab would otherwise show a snapshot from whenever it mounted.

export interface PluginsBoardProps {
  /** Opens a connector on the MCP board, where its authorization lives. */
  readonly onOpenServer: (name: string) => void;
}

interface RemoveRequest {
  readonly name: string;
  readonly deleteData: boolean;
}

/** What the last write told the operator, and what it left behind. */
interface Outcome {
  readonly message: string;
  readonly failed: boolean;
  readonly leftBehind: readonly PluginStep[];
}

export function PluginsBoard({ onOpenServer }: PluginsBoardProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const headingId = useId();
  const [pendingRemove, setPendingRemove] = useState<string | undefined>(undefined);

  const plugins = useQuery({
    queryKey: ['governance', 'plugins', 'list'],
    queryFn: fetchPlugins,
    retry: false,
    refetchOnWindowFocus: true,
  });

  // A write that fails part-way still changed something, so every board it touches refreshes
  // however it ends.
  const refreshBoards = () => {
    for (const board of ['plugins', 'skills', 'mcp']) {
      void queryClient.invalidateQueries({ queryKey: ['governance', board] });
    }
  };
  const remove = useMutation({
    mutationFn: ({ name, deleteData }: RemoveRequest) => removePlugin(name, deleteData),
    onSettled: () => {
      setPendingRemove(undefined);
      refreshBoards();
    },
  });
  const upgrade = useMutation({
    mutationFn: (name: string) => upgradePlugin(name),
    onSettled: refreshBoards,
  });

  const rows: readonly PluginRecord[] = plugins.data ?? [];
  const status = boardStatus({
    isLoading: plugins.isLoading,
    isError: plugins.isError,
    error: plugins.error,
    isEmpty: rows.length === 0,
  });
  const busy = remove.isPending || upgrade.isPending;
  const outcome = upgradeOutcome(t, upgrade) ?? removeOutcome(t, remove);

  return (
    <div className="h-full min-h-0 overflow-y-auto">
      <div className="mx-auto flex w-full max-w-4xl flex-col gap-8 px-4 py-6 sm:px-6">
        <PluginInstallPanel installedNames={rows.map((r) => r.name)} onOpenServer={onOpenServer} />

        <section aria-labelledby={headingId} className="flex flex-col gap-3">
          <h3 id={headingId} className="font-display text-[20px] font-semibold text-text">
            {t('governance.plugins.installedHeading')}
          </h3>

          {outcome !== undefined ? (
            <Alert role="status" variant={outcome.failed ? 'destructive' : 'default'}>
              <AlertDescription className="gap-1">
                <p>{outcome.message}</p>
                {outcome.leftBehind.map((step) => (
                  <p key={step.name} className="text-text-muted">
                    {t('governance.plugins.review.skipped', {
                      name: step.name,
                      reason: step.reason,
                    })}
                  </p>
                ))}
              </AlertDescription>
            </Alert>
          ) : null}

          <BoardStateView
            status={status}
            emptyHeading={t('governance.plugins.emptyHeading')}
            emptyBody={t('governance.plugins.emptyBody')}
            onRetry={() => {
              void plugins.refetch();
            }}
          >
            <ul
              aria-label={t('governance.plugins.installedHeading')}
              className="flex flex-col gap-2"
            >
              {rows.map((plugin) => (
                <PluginRow
                  key={plugin.name}
                  plugin={plugin}
                  busy={busy}
                  onUpgrade={() => {
                    remove.reset();
                    upgrade.mutate(plugin.name);
                  }}
                  onRemove={() => {
                    remove.reset();
                    upgrade.reset();
                    setPendingRemove(plugin.name);
                  }}
                />
              ))}
            </ul>
          </BoardStateView>
        </section>
      </div>

      <PluginRemoveDialog
        key={pendingRemove ?? ''}
        name={pendingRemove}
        pending={remove.isPending}
        onCancel={() => {
          setPendingRemove(undefined);
        }}
        onConfirm={(deleteData) => {
          if (pendingRemove !== undefined) remove.mutate({ name: pendingRemove, deleteData });
        }}
      />
    </div>
  );
}

function notOK(steps: readonly PluginStep[]): readonly PluginStep[] {
  return steps.filter((step) => step.outcome !== 'ok');
}

function refusal(error: unknown, fallback: string): string {
  return error instanceof HttpError && error.reason !== '' ? error.reason : fallback;
}

function upgradeOutcome(
  t: TFunction,
  upgrade: UseMutationResult<PluginUpgradeReport, Error, string>,
): Outcome | undefined {
  const name = upgrade.variables ?? '';
  if (upgrade.isPending) {
    return { message: t('governance.plugins.upgrading', { name }), failed: false, leftBehind: [] };
  }
  if (upgrade.isError) {
    return {
      message: refusal(upgrade.error, t('governance.plugins.upgradeFailed', { name })),
      failed: true,
      leftBehind: [],
    };
  }
  if (!upgrade.isSuccess) return undefined;
  const report = upgrade.data;
  const leftBehind = notOK([...report.skills, ...report.connectors]);
  if (report.error !== '') {
    return {
      message: t('governance.plugins.upgradeStopped', { name: report.name, reason: report.error }),
      failed: true,
      leftBehind,
    };
  }
  if (report.upToDate) {
    return {
      message: t('governance.plugins.upToDate', {
        name: report.name,
        version: report.version,
        commit: shortCommit(report.commit),
      }),
      failed: false,
      leftBehind: [],
    };
  }
  return {
    message: t('governance.plugins.upgraded', {
      name: report.name,
      from: report.fromVersion,
      version: report.version,
    }),
    failed: false,
    leftBehind,
  };
}

function removeOutcome(
  t: TFunction,
  remove: UseMutationResult<PluginRemoveReport, Error, RemoveRequest>,
): Outcome | undefined {
  if (remove.isError) {
    return {
      message: refusal(
        remove.error,
        t('governance.plugins.removeFailed', { name: remove.variables.name }),
      ),
      failed: true,
      leftBehind: [],
    };
  }
  if (!remove.isSuccess) return undefined;
  const report = remove.data;
  return {
    message:
      report.error === ''
        ? t('governance.plugins.removed', { name: report.name })
        : t('governance.plugins.removeStopped', { name: report.name, reason: report.error }),
    failed: report.error !== '',
    leftBehind: notOK([...report.skills, ...report.connectors]),
  };
}

function PluginRow({
  plugin,
  busy,
  onUpgrade,
  onRemove,
}: {
  readonly plugin: PluginRecord;
  readonly busy: boolean;
  readonly onUpgrade: () => void;
  readonly onRemove: () => void;
}) {
  const { t } = useTranslation();
  const names = (list: readonly string[]) =>
    list.length === 0 ? t('governance.plugins.none') : list.join(', ');

  return (
    <li>
      <Card className="flex-row flex-wrap items-start justify-between gap-3 p-4">
        <div className="flex min-w-0 flex-col gap-1">
          <div className="flex flex-wrap items-baseline gap-2">
            <h4 className="break-all font-mono text-[15.5px] font-semibold text-text">
              {plugin.name}
            </h4>
            {plugin.version !== '' ? (
              <span className="font-mono text-[13px] text-text-muted">{plugin.version}</span>
            ) : null}
            <code className="font-mono text-[13px] text-text-faint" title={plugin.commit}>
              {shortCommit(plugin.commit)}
            </code>
            {plugin.phase !== 'installed' ? (
              <Badge variant="warning">
                {t(`governance.plugins.phase.${plugin.phase}`, { defaultValue: plugin.phase })}
              </Badge>
            ) : null}
          </div>
          <p className="break-words text-[13px] text-text-muted">
            {t('governance.plugins.skills', { names: names(plugin.skills.map((s) => s.name)) })}
          </p>
          <p className="break-words text-[13px] text-text-muted">
            {t('governance.plugins.connectors', { names: names(plugin.connectors) })}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="outline"
            disabled={busy}
            aria-label={t('governance.plugins.upgradeAria', { name: plugin.name })}
            onClick={onUpgrade}
          >
            <RefreshCw data-icon="inline-start" aria-hidden="true" focusable="false" />
            {t('governance.plugins.upgrade')}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={busy}
            aria-label={t('governance.plugins.removeAria', { name: plugin.name })}
            onClick={onRemove}
          >
            <Trash2 data-icon="inline-start" aria-hidden="true" focusable="false" />
            {t('governance.plugins.remove.confirm')}
          </Button>
        </div>
      </Card>
    </li>
  );
}
