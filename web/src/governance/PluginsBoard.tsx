import { useId, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { HttpError } from '../api/json';
import { BoardStateView, boardStatus } from './governanceView';
import { PluginInstallPanel } from './PluginInstallPanel';
import { PluginRemoveDialog } from './PluginRemoveDialog';
import { fetchPlugins, removePlugin, shortCommit, type PluginRecord } from './pluginApi';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';

// PluginsBoard — install a plugin and see what each installed one brought. It is one column,
// not the master + detail of the other boards: a row holds everything a plugin has, so there
// is no detail to open, and the install field stays in view as the spec asks.
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

  // A removal that fails part-way still removed something, so every board it touches refreshes
  // however it ends.
  const remove = useMutation({
    mutationFn: ({ name, deleteData }: RemoveRequest) => removePlugin(name, deleteData),
    onSettled: () => {
      setPendingRemove(undefined);
      for (const board of ['plugins', 'skills', 'mcp']) {
        void queryClient.invalidateQueries({ queryKey: ['governance', board] });
      }
    },
  });

  const rows: readonly PluginRecord[] = plugins.data ?? [];
  const status = boardStatus({
    isLoading: plugins.isLoading,
    isError: plugins.isError,
    error: plugins.error,
    isEmpty: rows.length === 0,
  });
  const leftBehind = [...(remove.data?.skills ?? []), ...(remove.data?.connectors ?? [])].filter(
    (step) => step.outcome !== 'ok',
  );

  return (
    <div className="h-full min-h-0 overflow-y-auto">
      <div className="mx-auto flex w-full max-w-4xl flex-col gap-8 px-4 py-6 sm:px-6">
        <PluginInstallPanel installedNames={rows.map((r) => r.name)} onOpenServer={onOpenServer} />

        <section aria-labelledby={headingId} className="flex flex-col gap-3">
          <h3 id={headingId} className="font-display text-[20px] font-semibold text-text">
            {t('governance.plugins.installedHeading')}
          </h3>

          {remove.isSuccess ? (
            <Alert role="status" variant={remove.data.error === '' ? 'default' : 'destructive'}>
              <AlertDescription className="gap-1">
                <p>
                  {remove.data.error === ''
                    ? t('governance.plugins.removed', { name: remove.data.name })
                    : t('governance.plugins.removeStopped', {
                        name: remove.data.name,
                        reason: remove.data.error,
                      })}
                </p>
                {leftBehind.map((step) => (
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
          {remove.isError ? (
            <Alert variant="destructive">
              <AlertDescription>
                {remove.error instanceof HttpError && remove.error.reason !== ''
                  ? remove.error.reason
                  : t('governance.plugins.removeFailed', { name: remove.variables.name })}
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
                  removeDisabled={remove.isPending}
                  onRemove={() => {
                    remove.reset();
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

function PluginRow({
  plugin,
  removeDisabled,
  onRemove,
}: {
  readonly plugin: PluginRecord;
  readonly removeDisabled: boolean;
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
        <Button
          type="button"
          variant="outline"
          disabled={removeDisabled}
          aria-label={t('governance.plugins.removeAria', { name: plugin.name })}
          onClick={onRemove}
        >
          <Trash2 data-icon="inline-start" aria-hidden="true" focusable="false" />
          {t('governance.plugins.remove.confirm')}
        </Button>
      </Card>
    </li>
  );
}
