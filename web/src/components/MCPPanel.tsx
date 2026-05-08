import { useCallback, useEffect, useState, type ReactNode } from 'react';
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Database,
  ExternalLink,
  Globe,
  Loader2,
  LockKeyhole,
  Mail,
  Play,
  Plug,
  RefreshCw,
  RotateCw,
  Save,
  Server,
  ShieldAlert,
  ShieldCheck,
  Wrench,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { toast } from 'sonner';
import { api } from '@/api';
import { useApi } from '@/hooks/useApi';
import { useLocale } from '@/hooks/useLocale';
import { Skeleton } from '@/components/ui/skeleton';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { ErrorCard } from '@/components/common/ErrorCard';
import type {
  ConnectorProbeResponse,
  ConnectorProviderSummary,
  ConnectorRiskBadge,
  DatabaseSetupProvider,
  DatabaseSetupRequest,
  DatabaseSetupStatus,
  MailSetupProvider,
  MailSetupRequest,
  MailSetupStatus,
  MCPInvokeResponse,
  MCPServerSummary,
  MCPToolInfo,
} from '@/types/api';

// MCPPanel is now split into operator-facing connector configuration and the
// original raw MCP diagnostic surface. Raw tool invocation stays available, but
// provider setup starts from approved Aura connector profiles.
const MAIL_SETUP_PROVIDER_ID = 'mail-mcp';
const DATABASE_SETUP_PROVIDER_ID = 'database';
const MCP_FIELD_CLASS = 'min-h-11 w-full rounded-md border bg-background px-3 text-xs focus:outline-none focus:ring-2 focus:ring-primary/30';

function ariaExpanded(expanded: boolean): { 'aria-expanded': 'true' | 'false' } {
  return { 'aria-expanded': expanded ? 'true' : 'false' };
}

export function MCPPanel() {
  const { t } = useLocale();
  const fetchProviders = useCallback(() => api.mcpProviders(), []);
  const fetchServers = useCallback(() => api.mcpServers(), []);
  const providers = useApi(fetchProviders);
  const servers = useApi(fetchServers);
  const totalTools = (servers.data ?? []).reduce((acc, s) => acc + s.tool_count, 0);

  if ((providers.loading && !providers.data) || (servers.loading && !servers.data)) {
    return <MCPSkeleton />;
  }

  return (
    <div className="p-6 space-y-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold">{t('mcp.title')}</h1>
          <p className="mt-1 max-w-3xl text-xs text-muted-foreground">
            {t('mcp.subtitle')}
          </p>
        </div>
        <button
          type="button"
          onClick={() => {
            void providers.refetch();
            void servers.refetch();
          }}
          aria-label={t('mcp.refresh')}
          title={t('mcp.refreshHint')}
          className="inline-flex min-h-11 items-center gap-2 rounded-md border px-3 py-2 text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <RefreshCw size={14} />
          <span>{t('mcp.refresh')}</span>
          {t('mcp.providerCount', { count: providers.data?.length ?? 0 })} ·{' '}
          {t('mcp.serverCount', { count: servers.data?.length ?? 0 })} ·{' '}
          {t('mcp.toolCount', { count: totalTools })}
        </button>
      </header>

      <Tabs defaultValue="connectors" className="space-y-4">
        <TabsList className="h-auto flex-wrap justify-start">
          <TabsTrigger value="connectors" className="min-h-9 px-3">
            <Plug size={14} />
            {t('mcp.tab.connectors')}
          </TabsTrigger>
          <TabsTrigger value="installed" className="min-h-9 px-3">
            <CheckCircle2 size={14} />
            {t('mcp.tab.installed')}
          </TabsTrigger>
          <TabsTrigger value="health" className="min-h-9 px-3">
            <Activity size={14} />
            {t('mcp.tab.health')}
          </TabsTrigger>
          <TabsTrigger value="review" className="min-h-9 px-3">
            <ShieldCheck size={14} />
            {t('mcp.tab.review')}
          </TabsTrigger>
          <TabsTrigger value="raw" className="min-h-9 px-3">
            <Wrench size={14} />
            {t('mcp.tab.raw')}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="connectors">
          <ConnectorsView
            providers={providers.data ?? []}
            error={providers.error ?? null}
            onRetry={providers.refetch}
          />
        </TabsContent>

        <TabsContent value="installed">
          <InstalledView providers={providers.data ?? []} />
        </TabsContent>

        <TabsContent value="health">
          <HealthView providers={providers.data ?? []} />
        </TabsContent>

        <TabsContent value="review">
          <EmptyTab icon={ShieldCheck} title={t('mcp.review.emptyTitle')} />
        </TabsContent>

        <TabsContent value="raw">
          <RawMCPView
            data={servers.data ?? []}
            error={servers.error ?? null}
            onRetry={servers.refetch}
          />
        </TabsContent>
      </Tabs>
    </div>
  );
}

function ConnectorsView({
  providers,
  error,
  onRetry,
}: {
  providers: ConnectorProviderSummary[];
  error: Error | null;
  onRetry: () => void;
}) {
  const { t } = useLocale();
  if (error && providers.length === 0) {
    return <ErrorCard error={error} title={t('mcp.providers.errorTitle')} onRetry={onRetry} />;
  }
  if (providers.length === 0) {
    return (
      <div className="rounded-lg border border-dashed py-12 text-center">
        <div className="flex flex-col items-center gap-2 text-muted-foreground">
          <Plug size={32} className="opacity-40" />
          <p className="text-sm font-medium">{t('mcp.providers.emptyTitle')}</p>
        </div>
      </div>
    );
  }
  return (
    <div className="grid gap-3 xl:grid-cols-2">
      {providers.map((provider) => (
        <ConnectorCard key={provider.id} provider={provider} />
      ))}
    </div>
  );
}

function ConnectorCard({ provider }: { provider: ConnectorProviderSummary }) {
  const { t } = useLocale();
  const Icon = provider.kind === 'database' ? Database : Mail;
  const enabledCount = provider.capabilities.filter((c) => c.enabled).length;
  const [probe, setProbe] = useState<ConnectorProbeResponse | null>(null);
  const [probing, setProbing] = useState(false);

  const runProbe = async () => {
    setProbing(true);
    try {
      const result = await api.probeMCPProvider(provider.id);
      setProbe(result);
      if (result.ok) {
        toast.success(t('mcp.providers.probeOk', { provider: provider.name }));
      } else {
        toast.error(result.error || t('mcp.providers.probeFailed'));
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : t('mcp.providers.probeFailed');
      setProbe({ ok: false, provider_id: provider.id, error: message });
      toast.error(message);
    } finally {
      setProbing(false);
    }
  };

  return (
    <article className="rounded-lg border bg-card p-4">
      <div className="flex min-w-0 items-start justify-between gap-3">
        <div className="flex min-w-0 items-start gap-3">
          <div className="grid size-10 shrink-0 place-items-center rounded-md bg-primary/10 text-primary">
            <Icon size={18} />
          </div>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="truncate text-base font-semibold">{provider.name}</h2>
              <StatusPill status={provider.status} />
            </div>
            <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
              {provider.description}
            </p>
          </div>
        </div>
        {provider.repository_url && (
          <a
            href={provider.repository_url}
            target="_blank"
            rel="noreferrer"
            className="inline-flex size-9 shrink-0 items-center justify-center rounded-md border text-muted-foreground hover:bg-muted hover:text-foreground"
            title={t('mcp.providers.openRepository')}
            aria-label={t('mcp.providers.openRepository')}
          >
            <ExternalLink size={14} />
          </a>
        )}
      </div>

      <div className="mt-3 flex flex-wrap gap-1.5">
        <MetaPill label={provider.runtime_type} />
        <MetaPill label={t(`mcp.providers.profile.${provider.profile}`)} />
        <MetaPill label={t(`mcp.providers.kind.${provider.kind}`)} />
      </div>

      <div className="mt-3 flex flex-wrap gap-1.5">
        {provider.risk_badges.map((badge) => (
          <RiskPill key={badge.id} badge={badge} />
        ))}
      </div>

      <div className="mt-4 grid gap-3 lg:grid-cols-2">
        <section className="space-y-2">
          <h3 className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
            {t('mcp.providers.capabilities')}
          </h3>
          <div className="space-y-1.5">
            {provider.capabilities.map((capability) => (
              <div key={capability.id} className="flex items-center justify-between gap-2 rounded-md border px-2 py-1.5 text-xs">
                <span className="min-w-0 truncate">{capability.label}</span>
                <span className="shrink-0 text-[10px] uppercase tracking-wide text-muted-foreground">
                  {capability.review_required ? t('mcp.providers.review') : t('mcp.providers.ready')}
                </span>
              </div>
            ))}
          </div>
        </section>

        <section className="space-y-2">
          <h3 className="text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
            {t('mcp.providers.allowlist')}
          </h3>
          <ToolList values={provider.approved_tools ?? []} tone="approved" />
          <ToolList values={provider.blocked_tools ?? []} tone="blocked" />
        </section>
      </div>

      {provider.required_secrets && provider.required_secrets.length > 0 && (
        <div className="mt-3 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
          <LockKeyhole size={13} />
          {provider.required_secrets.map((secret) => (
            <code key={secret} className="rounded bg-muted px-1.5 py-0.5 text-[11px]">
              {secret}
            </code>
          ))}
        </div>
      )}

      {provider.id === MAIL_SETUP_PROVIDER_ID && <MailSetupWizard />}
      {provider.id === DATABASE_SETUP_PROVIDER_ID && <DatabaseSetupWizard />}

      {probe && (
        <div
          className={`mt-3 rounded-md border px-3 py-2 text-xs ${
            probe.ok
              ? 'border-emerald-200 bg-emerald-50 text-emerald-900 dark:border-emerald-900/50 dark:bg-emerald-950/30 dark:text-emerald-200'
              : 'border-destructive/30 bg-destructive/5 text-destructive'
          }`}
        >
          <div className="flex flex-wrap items-center gap-2 font-medium">
            {probe.ok ? <CheckCircle2 size={13} /> : <AlertTriangle size={13} />}
            <span>{probe.ok ? t('mcp.providers.probeReady') : t('mcp.providers.probeNotReady')}</span>
          </div>
          {probe.capabilities_ready && probe.capabilities_ready.length > 0 && (
            <p className="mt-1 text-[11px]">
              {t('mcp.providers.readyCapabilities')}: {probe.capabilities_ready.join(', ')}
            </p>
          )}
          {probe.missing_capabilities && probe.missing_capabilities.length > 0 && (
            <p className="mt-1 text-[11px]">
              {t('mcp.providers.missingCapabilities')}: {probe.missing_capabilities.join(', ')}
            </p>
          )}
          {probe.blocked_tools_advertised && probe.blocked_tools_advertised.length > 0 && (
            <p className="mt-1 text-[11px]">
              {t('mcp.providers.blockedAdvertised')}: {probe.blocked_tools_advertised.join(', ')}
            </p>
          )}
          {probe.error && <p className="mt-1 text-[11px]">{probe.error}</p>}
        </div>
      )}

      <div className="mt-4 flex flex-wrap justify-between gap-2 border-t pt-3">
        <span className="text-xs text-muted-foreground">
          {t('mcp.providers.enabledCount', { enabled: enabledCount, total: provider.capabilities.length })}
        </span>
        <div className="flex gap-2">
          <button
            type="button"
            onClick={() => void runProbe()}
            disabled={probing}
            className="inline-flex min-h-10 items-center gap-1.5 rounded-md border px-3 py-2 text-xs text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-60"
            title={t('mcp.providers.probeHint')}
          >
            {probing ? <Loader2 size={13} className="animate-spin" /> : <Activity size={13} />}
            {t('mcp.providers.probe')}
          </button>
          <button
            type="button"
            disabled
            className="inline-flex min-h-10 items-center gap-1.5 rounded-md bg-primary px-3 py-2 text-xs text-primary-foreground opacity-60"
            title={t('mcp.providers.enableNext')}
          >
            <CheckCircle2 size={13} />
            {t('mcp.providers.enable')}
          </button>
        </div>
      </div>
    </article>
  );
}

type MailSetupForm = {
  provider: MailSetupProvider;
  account_id: string;
  email: string;
  imap_host: string;
  imap_port: string;
  imap_secure: boolean;
  smtp_host: string;
  smtp_port: string;
  smtp_secure: boolean;
  app_password: string;
  enable_smtp: boolean;
};

const MAIL_PROVIDER_DEFAULTS: Record<MailSetupProvider, Pick<MailSetupForm, 'imap_host' | 'imap_port' | 'imap_secure' | 'smtp_host' | 'smtp_port' | 'smtp_secure'>> = {
  gmail: {
    imap_host: 'imap.gmail.com',
    imap_port: '993',
    imap_secure: true,
    smtp_host: 'smtp.gmail.com',
    smtp_port: '465',
    smtp_secure: true,
  },
  outlook: {
    imap_host: 'outlook.office365.com',
    imap_port: '993',
    imap_secure: true,
    smtp_host: 'smtp.office365.com',
    smtp_port: '587',
    smtp_secure: true,
  },
  imap: {
    imap_host: '',
    imap_port: '993',
    imap_secure: true,
    smtp_host: '',
    smtp_port: '587',
    smtp_secure: true,
  },
};

function MailSetupWizard() {
  const { t } = useLocale();
  const [status, setStatus] = useState<MailSetupStatus | null>(null);
  const [form, setForm] = useState<MailSetupForm>(() => mailStatusToForm(null));
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [restartReady, setRestartReady] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    api.mailSetupStatus()
      .then((nextStatus) => {
        if (cancelled) return;
        setStatus(nextStatus);
        setForm(mailStatusToForm(nextStatus));
        setRestartReady(Boolean(nextStatus.restart_required));
        setError(null);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => { cancelled = true; };
  }, []);

  const update = <K extends keyof MailSetupForm>(key: K, value: MailSetupForm[K]) => {
    setForm((prev) => ({ ...prev, [key]: value }));
  };

  const changeProvider = (provider: MailSetupProvider) => {
    const defaults = MAIL_PROVIDER_DEFAULTS[provider];
    setForm((prev) => ({
      ...prev,
      provider,
      imap_host: defaults.imap_host,
      imap_port: defaults.imap_port,
      imap_secure: defaults.imap_secure,
      smtp_host: defaults.smtp_host,
      smtp_port: defaults.smtp_port,
      smtp_secure: defaults.smtp_secure,
    }));
  };

  const save = async () => {
    const request = mailFormToRequest(form);
    if (!request.account_id || !request.email) {
      toast.error(t('mcp.mail.validationRequired'));
      return;
    }
    if (!request.imap_host || !request.imap_port) {
      toast.error(t('mcp.mail.validationImap'));
      return;
    }
    if (request.enable_smtp && (!request.smtp_host || !request.smtp_port)) {
      toast.error(t('mcp.mail.validationSmtp'));
      return;
    }
    setSaving(true);
    try {
      const result = await api.saveMailSetup(request);
      if (!result.ok || !result.status) {
        toast.error(t('mcp.mail.saveFailed'));
        return;
      }
      const nextStatus = result.status;
      setStatus(nextStatus);
      setForm(mailStatusToForm(nextStatus));
      setRestartReady(Boolean(nextStatus.restart_required || nextStatus.needs_restart));
      setError(null);
      toast.success(t('mcp.mail.saveOk'));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const restartAura = async () => {
    setRestarting(true);
    try {
      await api.restart();
      toast.success(t('mcp.mail.restartOk'));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err));
      setRestarting(false);
    }
  };

  return (
    <section className="mt-4 rounded-lg border bg-muted/10 p-3">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <h3 className="text-sm font-semibold">{t('mcp.mail.title')}</h3>
          <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
            {t('mcp.mail.description')}
          </p>
        </div>
        <StatusPill status={status?.configured ? 'configured' : 'not_configured'} />
      </div>

      {loading ? (
        <div className="mt-3 flex items-center gap-2 text-xs text-muted-foreground">
          <Loader2 size={13} className="animate-spin" />
          {t('common.loading')}
        </div>
      ) : (
        <div className="mt-3 space-y-3">
          {error && (
            <div className="flex items-center gap-2 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
              <AlertTriangle size={13} />
              {error}
            </div>
          )}

          <div className="grid gap-3 md:grid-cols-3">
            <Field label={t('mcp.mail.providerLabel')} htmlFor="mail-provider">
              <select
                id="mail-provider"
                aria-label={t('mcp.mail.providerLabel')}
                title={t('mcp.mail.providerLabel')}
                value={form.provider}
                onChange={(e) => changeProvider(e.target.value as MailSetupProvider)}
                className={MCP_FIELD_CLASS}
              >
                <option value="gmail">{t('mcp.mail.provider.gmail')}</option>
                <option value="outlook">{t('mcp.mail.provider.outlook')}</option>
                <option value="imap">{t('mcp.mail.provider.imap')}</option>
              </select>
            </Field>
            <Field label={t('mcp.mail.emailLabel')} htmlFor="mail-email">
              <input
                id="mail-email"
                type="email"
                aria-label={t('mcp.mail.emailLabel')}
                title={t('mcp.mail.emailLabel')}
                value={form.email}
                onChange={(e) => {
                  update('email', e.target.value);
                  if (!form.account_id || form.account_id === form.email) {
                    update('account_id', e.target.value);
                  }
                }}
                autoComplete="email"
                className={MCP_FIELD_CLASS}
                placeholder={t('mcp.mail.emailPlaceholder')}
              />
            </Field>
            <Field label={t('mcp.mail.accountLabel')} htmlFor="mail-account">
              <input
                id="mail-account"
                type="text"
                aria-label={t('mcp.mail.accountLabel')}
                title={t('mcp.mail.accountLabel')}
                value={form.account_id}
                onChange={(e) => update('account_id', e.target.value)}
                autoComplete="off"
                className={`${MCP_FIELD_CLASS} font-mono`}
                placeholder={t('mcp.mail.accountPlaceholder')}
              />
            </Field>
          </div>

          <div className="grid gap-3 md:grid-cols-[1fr_120px_auto]">
            <Field label={t('mcp.mail.imapHostLabel')} htmlFor="mail-imap-host">
              <input
                id="mail-imap-host"
                type="text"
                aria-label={t('mcp.mail.imapHostLabel')}
                title={t('mcp.mail.imapHostLabel')}
                value={form.imap_host}
                onChange={(e) => update('imap_host', e.target.value)}
                autoComplete="off"
                className={`${MCP_FIELD_CLASS} font-mono`}
              />
            </Field>
            <Field label={t('mcp.mail.imapPortLabel')} htmlFor="mail-imap-port">
              <input
                id="mail-imap-port"
                type="number"
                aria-label={t('mcp.mail.imapPortLabel')}
                title={t('mcp.mail.imapPortLabel')}
                min={1}
                max={65535}
                value={form.imap_port}
                onChange={(e) => update('imap_port', e.target.value)}
                className={`${MCP_FIELD_CLASS} font-mono`}
              />
            </Field>
            <CheckField
              id="mail-imap-secure"
              checked={form.imap_secure}
              onChange={(value) => update('imap_secure', value)}
              label={t('mcp.mail.secureLabel')}
            />
          </div>

          <div className="rounded-md border bg-background/60 p-3">
            <label className="flex items-center gap-2 text-xs font-medium">
              <input
                type="checkbox"
                aria-label={t('mcp.mail.smtpToggle')}
                title={t('mcp.mail.smtpToggle')}
                checked={form.enable_smtp}
                onChange={(e) => update('enable_smtp', e.target.checked)}
                className="size-4"
              />
              {t('mcp.mail.smtpToggle')}
            </label>
            {form.enable_smtp && (
              <div className="mt-3 grid gap-3 md:grid-cols-[1fr_120px_auto]">
                <Field label={t('mcp.mail.smtpHostLabel')} htmlFor="mail-smtp-host">
                  <input
                    id="mail-smtp-host"
                    type="text"
                    aria-label={t('mcp.mail.smtpHostLabel')}
                    title={t('mcp.mail.smtpHostLabel')}
                    value={form.smtp_host}
                    onChange={(e) => update('smtp_host', e.target.value)}
                    autoComplete="off"
                    className={`${MCP_FIELD_CLASS} font-mono`}
                  />
                </Field>
                <Field label={t('mcp.mail.smtpPortLabel')} htmlFor="mail-smtp-port">
                  <input
                    id="mail-smtp-port"
                    type="number"
                    aria-label={t('mcp.mail.smtpPortLabel')}
                    title={t('mcp.mail.smtpPortLabel')}
                    min={1}
                    max={65535}
                    value={form.smtp_port}
                    onChange={(e) => update('smtp_port', e.target.value)}
                    className={`${MCP_FIELD_CLASS} font-mono`}
                  />
                </Field>
                <CheckField
                  id="mail-smtp-secure"
                  checked={form.smtp_secure}
                  onChange={(value) => update('smtp_secure', value)}
                  label={t('mcp.mail.secureLabel')}
                />
              </div>
            )}
          </div>

          <Field label={t('mcp.mail.secretLabel')} htmlFor="mail-app-password">
            <input
              id="mail-app-password"
              type="password"
              aria-label={t('mcp.mail.secretLabel')}
              title={t('mcp.mail.secretLabel')}
              value={form.app_password}
              onChange={(e) => update('app_password', e.target.value)}
              autoComplete="new-password"
              spellCheck={false}
              className={MCP_FIELD_CLASS}
              placeholder={status?.secret_configured ? t('mcp.mail.secretConfigured') : t('mcp.mail.secretPlaceholder')}
            />
            <p className="mt-1 text-[11px] text-muted-foreground">
              {status?.secret_configured ? t('mcp.mail.secretKeepHint') : t('mcp.mail.secretHint')}
            </p>
          </Field>

          {restartReady && (
            <div className="rounded-md border border-orange-500/30 bg-orange-500/10 px-3 py-2 text-xs text-orange-800 dark:text-orange-100">
              <div className="font-medium">{t('mcp.mail.restartTitle')}</div>
              <div className="mt-1">{t('mcp.mail.restartDescription')}</div>
            </div>
          )}

          {status?.configured && !status.binary_present && (
            <div className="rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-800 dark:text-amber-100">
              <div className="font-medium">{t('mcp.mail.binaryMissingTitle')}</div>
              <div className="mt-1">{t('mcp.mail.binaryMissingDescription', { command: status.command || '/usr/local/bin/mail-mcp' })}</div>
            </div>
          )}

          <div className="flex flex-wrap items-center justify-between gap-2 border-t pt-3">
            <span className="text-xs text-muted-foreground">
              {status?.configured ? t('mcp.mail.configuredAs', { email: status.email || status.configured_email || status.account_id || '' }) : t('mcp.mail.notConfigured')}
            </span>
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                onClick={() => void save()}
                disabled={saving}
                className="inline-flex min-h-10 items-center gap-1.5 rounded-md bg-primary px-3 py-2 text-xs text-primary-foreground hover:bg-primary/90 disabled:opacity-60"
              >
                {saving ? <Loader2 size={13} className="animate-spin" /> : <Save size={13} />}
                {saving ? t('mcp.mail.saving') : t('mcp.mail.save')}
              </button>
              {restartReady && (
                <button
                  type="button"
                  onClick={() => void restartAura()}
                  disabled={restarting || saving}
                  className="inline-flex min-h-10 items-center gap-1.5 rounded-md border border-orange-500/50 bg-orange-500/10 px-3 py-2 text-xs text-orange-700 hover:bg-orange-500/15 disabled:opacity-60 dark:text-orange-200"
                >
                  {restarting ? <Loader2 size={13} className="animate-spin" /> : <RotateCw size={13} />}
                  {restarting ? t('mcp.mail.restarting') : t('mcp.mail.restartButton')}
                </button>
              )}
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

function Field({
  label,
  htmlFor,
  children,
}: {
  label: string;
  htmlFor: string;
  children: ReactNode;
}) {
  return (
    <label className="block min-w-0 text-xs font-medium" htmlFor={htmlFor}>
      <span className="mb-1 block text-muted-foreground">{label}</span>
      {children}
    </label>
  );
}

function CheckField({
  id,
  checked,
  onChange,
  label,
}: {
  id: string;
  checked: boolean;
  onChange: (value: boolean) => void;
  label: string;
}) {
  return (
    <label className="flex min-h-11 items-end gap-2 pb-2 text-xs font-medium text-muted-foreground" htmlFor={id}>
      <input
        id={id}
        type="checkbox"
        aria-label={label}
        title={label}
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
        className="mb-0.5 size-4"
      />
      {label}
    </label>
  );
}

function mailStatusToForm(status: MailSetupStatus | null): MailSetupForm {
  const provider = status?.provider ?? 'gmail';
  const defaults = MAIL_PROVIDER_DEFAULTS[provider];
  return {
    provider,
    account_id: status?.account_id ?? status?.email ?? '',
    email: status?.email ?? '',
    imap_host: status?.imap_host ?? defaults.imap_host,
    imap_port: String(status?.imap_port ?? defaults.imap_port),
    imap_secure: status?.imap_secure ?? defaults.imap_secure,
    smtp_host: status?.smtp_host ?? defaults.smtp_host,
    smtp_port: String(status?.smtp_port ?? defaults.smtp_port),
    smtp_secure: status?.smtp_secure ?? defaults.smtp_secure,
    app_password: '',
    enable_smtp: status?.enable_smtp ?? false,
  };
}

function mailFormToRequest(form: MailSetupForm): MailSetupRequest {
  const request: MailSetupRequest = {
    provider: form.provider,
    account_id: form.account_id.trim(),
    email: form.email.trim(),
    imap_host: form.imap_host.trim(),
    imap_port: Number(form.imap_port),
    imap_secure: form.imap_secure,
    enable_smtp: form.enable_smtp,
  };
  if (form.enable_smtp) {
    request.smtp_host = form.smtp_host.trim();
    request.smtp_port = Number(form.smtp_port);
    request.smtp_secure = form.smtp_secure;
  }
  if (form.app_password.trim() !== '') {
    request.app_password = form.app_password;
  }
  return request;
}

type DatabaseSetupForm = {
  provider: DatabaseSetupProvider;
  sqlite_path: string;
  host: string;
  port: string;
  database: string;
  user: string;
  password: string;
  ssl: boolean;
};

const DATABASE_DEFAULT_PORTS: Record<DatabaseSetupProvider, string> = {
  sqlite: '',
  postgresql: '5432',
  mysql: '3306',
  sqlserver: '1433',
};

function DatabaseSetupWizard() {
  const { t } = useLocale();
  const [status, setStatus] = useState<DatabaseSetupStatus | null>(null);
  const [form, setForm] = useState<DatabaseSetupForm>(() => databaseStatusToForm(null));
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [restartReady, setRestartReady] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    api.databaseSetupStatus()
      .then((nextStatus) => {
        if (cancelled) return;
        setStatus(nextStatus);
        setForm(databaseStatusToForm(nextStatus));
        setRestartReady(Boolean(nextStatus.restart_required));
        setError(null);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => { cancelled = true; };
  }, []);

  const update = <K extends keyof DatabaseSetupForm>(key: K, value: DatabaseSetupForm[K]) => {
    setForm((prev) => ({ ...prev, [key]: value }));
  };

  const changeProvider = (provider: DatabaseSetupProvider) => {
    setForm((prev) => ({
      ...prev,
      provider,
      port: DATABASE_DEFAULT_PORTS[provider],
      ssl: provider === 'postgresql',
    }));
  };

  const save = async () => {
    const request = databaseFormToRequest(form);
    if (request.provider === 'sqlite' && !request.sqlite_path) {
      toast.error(t('mcp.database.validationSqlite'));
      return;
    }
    if (request.provider !== 'sqlite' && (!request.host || !request.database)) {
      toast.error(t('mcp.database.validationNetwork'));
      return;
    }
    setSaving(true);
    try {
      const result = await api.saveDatabaseSetup(request);
      if (!result.ok || !result.status) {
        toast.error(t('mcp.database.saveFailed'));
        return;
      }
      const nextStatus = result.status;
      setStatus(nextStatus);
      setForm(databaseStatusToForm(nextStatus));
      setRestartReady(Boolean(nextStatus.restart_required || nextStatus.needs_restart));
      setError(null);
      toast.success(t('mcp.database.saveOk'));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const restartAura = async () => {
    setRestarting(true);
    try {
      await api.restart();
      toast.success(t('mcp.database.restartOk'));
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err));
      setRestarting(false);
    }
  };

  const networkDatabase = form.provider !== 'sqlite';

  return (
    <section className="mt-4 rounded-lg border bg-muted/10 p-3">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <h3 className="text-sm font-semibold">{t('mcp.database.title')}</h3>
          <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
            {t('mcp.database.description')}
          </p>
        </div>
        <StatusPill status={status?.configured ? 'configured' : 'not_configured'} />
      </div>

      {loading ? (
        <div className="mt-3 flex items-center gap-2 text-xs text-muted-foreground">
          <Loader2 size={13} className="animate-spin" />
          {t('common.loading')}
        </div>
      ) : (
        <div className="mt-3 space-y-3">
          {error && (
            <div className="flex items-center gap-2 rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
              <AlertTriangle size={13} />
              {error}
            </div>
          )}

          <div className="grid gap-3 md:grid-cols-3">
            <Field label={t('mcp.database.providerLabel')} htmlFor="database-provider">
              <select
                id="database-provider"
                aria-label={t('mcp.database.providerLabel')}
                title={t('mcp.database.providerLabel')}
                value={form.provider}
                onChange={(e) => changeProvider(e.target.value as DatabaseSetupProvider)}
                className={MCP_FIELD_CLASS}
              >
                <option value="sqlite">{t('mcp.database.provider.sqlite')}</option>
                <option value="postgresql">{t('mcp.database.provider.postgresql')}</option>
                <option value="mysql">{t('mcp.database.provider.mysql')}</option>
                <option value="sqlserver">{t('mcp.database.provider.sqlserver')}</option>
              </select>
            </Field>

            {networkDatabase ? (
              <>
                <Field label={t('mcp.database.hostLabel')} htmlFor="database-host">
                  <input
                    id="database-host"
                    type="text"
                    aria-label={t('mcp.database.hostLabel')}
                    title={t('mcp.database.hostLabel')}
                    value={form.host}
                    onChange={(e) => update('host', e.target.value)}
                    autoComplete="off"
                    className={`${MCP_FIELD_CLASS} font-mono`}
                    placeholder="localhost"
                  />
                </Field>
                <Field label={t('mcp.database.portLabel')} htmlFor="database-port">
                  <input
                    id="database-port"
                    type="number"
                    aria-label={t('mcp.database.portLabel')}
                    title={t('mcp.database.portLabel')}
                    min={1}
                    max={65535}
                    value={form.port}
                    onChange={(e) => update('port', e.target.value)}
                    className={`${MCP_FIELD_CLASS} font-mono`}
                  />
                </Field>
              </>
            ) : (
              <div className="md:col-span-2">
                <Field label={t('mcp.database.sqlitePathLabel')} htmlFor="database-sqlite-path">
                  <input
                    id="database-sqlite-path"
                    type="text"
                    aria-label={t('mcp.database.sqlitePathLabel')}
                    title={t('mcp.database.sqlitePathLabel')}
                    value={form.sqlite_path}
                    onChange={(e) => update('sqlite_path', e.target.value)}
                    autoComplete="off"
                    className={`${MCP_FIELD_CLASS} font-mono`}
                    placeholder="/workspace/data/customer.db"
                  />
                </Field>
              </div>
            )}
          </div>

          {networkDatabase && (
            <>
              <div className="grid gap-3 md:grid-cols-3">
                <Field label={t('mcp.database.databaseLabel')} htmlFor="database-name">
                  <input
                    id="database-name"
                    type="text"
                    aria-label={t('mcp.database.databaseLabel')}
                    title={t('mcp.database.databaseLabel')}
                    value={form.database}
                    onChange={(e) => update('database', e.target.value)}
                    autoComplete="off"
                    className={`${MCP_FIELD_CLASS} font-mono`}
                  />
                </Field>
                <Field label={t('mcp.database.userLabel')} htmlFor="database-user">
                  <input
                    id="database-user"
                    type="text"
                    aria-label={t('mcp.database.userLabel')}
                    title={t('mcp.database.userLabel')}
                    value={form.user}
                    onChange={(e) => update('user', e.target.value)}
                    autoComplete="username"
                    className={`${MCP_FIELD_CLASS} font-mono`}
                  />
                </Field>
                <CheckField
                  id="database-ssl"
                  checked={form.ssl}
                  onChange={(value) => update('ssl', value)}
                  label={t('mcp.database.sslLabel')}
                />
              </div>
              <Field label={t('mcp.database.passwordLabel')} htmlFor="database-password">
                <input
                  id="database-password"
                  type="password"
                  aria-label={t('mcp.database.passwordLabel')}
                  title={t('mcp.database.passwordLabel')}
                  value={form.password}
                  onChange={(e) => update('password', e.target.value)}
                  autoComplete="new-password"
                  spellCheck={false}
                  className={MCP_FIELD_CLASS}
                  placeholder={status?.password_configured ? t('mcp.database.passwordConfigured') : t('mcp.database.passwordPlaceholder')}
                />
                <p className="mt-1 text-[11px] text-muted-foreground">
                  {status?.password_configured ? t('mcp.database.passwordKeepHint') : t('mcp.database.passwordHint')}
                </p>
              </Field>
            </>
          )}

          {restartReady && (
            <div className="rounded-md border border-orange-500/30 bg-orange-500/10 px-3 py-2 text-xs text-orange-800 dark:text-orange-100">
              <div className="font-medium">{t('mcp.database.restartTitle')}</div>
              <div className="mt-1">{t('mcp.database.restartDescription')}</div>
            </div>
          )}

          {status?.configured && !status.binary_present && (
            <div className="rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-800 dark:text-amber-100">
              <div className="font-medium">{t('mcp.database.binaryMissingTitle')}</div>
              <div className="mt-1">{t('mcp.database.binaryMissingDescription', { command: status.command || '/usr/local/bin/ea-database-server' })}</div>
            </div>
          )}

          <div className="flex flex-wrap items-center justify-between gap-2 border-t pt-3">
            <span className="text-xs text-muted-foreground">
              {status?.configured ? t('mcp.database.configuredAs', { target: databaseConfiguredTarget(status) }) : t('mcp.database.notConfigured')}
            </span>
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                onClick={() => void save()}
                disabled={saving}
                className="inline-flex min-h-10 items-center gap-1.5 rounded-md bg-primary px-3 py-2 text-xs text-primary-foreground hover:bg-primary/90 disabled:opacity-60"
              >
                {saving ? <Loader2 size={13} className="animate-spin" /> : <Save size={13} />}
                {saving ? t('mcp.database.saving') : t('mcp.database.save')}
              </button>
              {restartReady && (
                <button
                  type="button"
                  onClick={() => void restartAura()}
                  disabled={restarting || saving}
                  className="inline-flex min-h-10 items-center gap-1.5 rounded-md border border-orange-500/50 bg-orange-500/10 px-3 py-2 text-xs text-orange-700 hover:bg-orange-500/15 disabled:opacity-60 dark:text-orange-200"
                >
                  {restarting ? <Loader2 size={13} className="animate-spin" /> : <RotateCw size={13} />}
                  {restarting ? t('mcp.database.restarting') : t('mcp.database.restartButton')}
                </button>
              )}
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

function databaseStatusToForm(status: DatabaseSetupStatus | null): DatabaseSetupForm {
  const provider = status?.provider ?? 'sqlite';
  return {
    provider,
    sqlite_path: status?.sqlite_path ?? '',
    host: status?.host ?? '',
    port: String(status?.port ?? DATABASE_DEFAULT_PORTS[provider]),
    database: status?.database ?? '',
    user: status?.user ?? '',
    password: '',
    ssl: status?.ssl ?? provider === 'postgresql',
  };
}

function databaseFormToRequest(form: DatabaseSetupForm): DatabaseSetupRequest {
  const request: DatabaseSetupRequest = { provider: form.provider };
  if (form.provider === 'sqlite') {
    request.sqlite_path = form.sqlite_path.trim();
    return request;
  }
  request.host = form.host.trim();
  request.port = Number(form.port);
  request.database = form.database.trim();
  request.user = form.user.trim();
  request.ssl = form.ssl;
  if (form.password.trim() !== '') {
    request.password = form.password;
  }
  return request;
}

function databaseConfiguredTarget(status: DatabaseSetupStatus): string {
  if (status.provider === 'sqlite') return status.sqlite_path || 'sqlite';
  return `${status.host || 'host'}/${status.database || 'database'}`;
}

function InstalledView({ providers }: { providers: ConnectorProviderSummary[] }) {
  const { t } = useLocale();
  const installed = providers.filter((p) => p.status === 'enabled' || p.status === 'ready' || p.status === 'configured');
  if (installed.length === 0) {
    return <EmptyTab icon={CheckCircle2} title={t('mcp.installed.emptyTitle')} />;
  }
  return (
    <div className="space-y-3">
      {installed.map((provider) => (
        <ConnectorCard key={provider.id} provider={provider} />
      ))}
    </div>
  );
}

function HealthView({ providers }: { providers: ConnectorProviderSummary[] }) {
  const { t } = useLocale();
  return (
    <div className="rounded-lg border bg-card">
      <div className="grid gap-0 divide-y md:grid-cols-3 md:divide-x md:divide-y-0">
        <HealthMetric label={t('mcp.health.providers')} value={providers.length.toString()} />
        <HealthMetric label={t('mcp.health.ready')} value={providers.filter((p) => p.status === 'ready').length.toString()} />
        <HealthMetric label={t('mcp.health.enabled')} value={providers.filter((p) => p.status === 'enabled').length.toString()} />
      </div>
      <div className="border-t p-4 text-sm text-muted-foreground">
        {t('mcp.health.pendingProbe')}
      </div>
    </div>
  );
}

function RawMCPView({
  data,
  error,
  onRetry,
}: {
  data: MCPServerSummary[];
  error: Error | null;
  onRetry: () => void;
}) {
  const { t } = useLocale();
  const [openServers, setOpenServers] = useState<Set<string>>(new Set());

  const toggleServer = useCallback((name: string) => {
    setOpenServers((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }, []);

  if (error && data.length === 0) {
    return <ErrorCard error={error} title={t('mcp.errorTitle')} onRetry={onRetry} />;
  }

  if (data.length === 0) {
    return (
      <div className="rounded-lg border border-dashed py-12 text-center">
        <div className="flex flex-col items-center gap-2 text-muted-foreground">
          <Plug size={32} className="opacity-40" />
          <p className="text-sm font-medium">{t('mcp.emptyTitle')}</p>
          <p className="max-w-md text-xs" dangerouslySetInnerHTML={{ __html: t('mcp.emptyHint') }} />
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {data.map((srv) => (
        <ServerCard
          key={srv.name}
          server={srv}
          isOpen={openServers.has(srv.name)}
          onToggle={() => toggleServer(srv.name)}
        />
      ))}
    </div>
  );
}

function ServerCard({
  server,
  isOpen,
  onToggle,
}: {
  server: MCPServerSummary;
  isOpen: boolean;
  onToggle: () => void;
}) {
  const { t } = useLocale();
  const TransportIcon = server.transport === 'stdio' ? Server : Globe;
  return (
    <div className="overflow-hidden rounded-lg border bg-card">
      <button
        type="button"
        onClick={onToggle}
        {...ariaExpanded(isOpen)}
        className="flex w-full items-center gap-3 px-4 py-3 text-left hover:bg-muted/30"
      >
        {isOpen ? <ChevronDown size={16} className="shrink-0" /> : <ChevronRight size={16} className="shrink-0" />}
        <TransportIcon size={18} className="shrink-0 text-primary" />
        <div className="min-w-0 flex-1">
          <div className="flex items-baseline gap-2">
            <span className="font-mono text-sm font-medium">{server.name}</span>
            <span className="text-xs uppercase tracking-wide text-muted-foreground">{server.transport}</span>
          </div>
        </div>
        <span className="text-xs text-muted-foreground">
          {t('mcp.toolCount', { count: server.tool_count })}
        </span>
      </button>
      {isOpen && (
        <div className="divide-y border-t bg-muted/10">
          {server.tools.length === 0 ? (
            <div className="px-12 py-3 text-xs text-muted-foreground">
              {t('mcp.noTools')}
            </div>
          ) : (
            server.tools.map((tool) => (
              <ToolRow key={tool.name} server={server.name} tool={tool} />
            ))
          )}
        </div>
      )}
    </div>
  );
}

function ToolRow({ server, tool }: { server: string; tool: MCPToolInfo }) {
  const { t } = useLocale();
  const [showSchema, setShowSchema] = useState(false);
  const [showRun, setShowRun] = useState(false);
  const [args, setArgs] = useState<string>(() => seedArgsFromSchema(tool.input_schema));
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<MCPInvokeResponse | null>(null);
  const [parseErr, setParseErr] = useState<string | null>(null);

  const hasSchema = tool.input_schema && Object.keys(tool.input_schema).length > 0;
  const toolFQN = `mcp_${server}_${tool.name}`;

  const handleRun = useCallback(async () => {
    setParseErr(null);
    let parsed: Record<string, unknown> = {};
    const trimmed = args.trim();
    if (trimmed !== '') {
      try {
        const v = JSON.parse(trimmed);
        if (v === null || typeof v !== 'object' || Array.isArray(v)) {
          setParseErr(t('mcp.argsObjectError'));
          return;
        }
        parsed = v as Record<string, unknown>;
      } catch (e) {
        setParseErr(e instanceof Error ? e.message : 'invalid JSON');
        return;
      }
    }
    setRunning(true);
    setResult(null);
    const toastId = toast.loading(t('mcp.invoking', { tool: toolFQN }));
    try {
      const resp = await api.invokeMCPTool(server, tool.name, parsed);
      setResult(resp);
      if (resp.ok) {
        toast.success(t('mcp.returned', { tool: toolFQN }), { id: toastId });
      } else if (resp.is_error) {
        toast.error(t('mcp.isError'), { id: toastId, description: resp.error?.slice(0, 200) });
      } else {
        toast.error(t('mcp.transportFailed'), { id: toastId, description: resp.error?.slice(0, 200) });
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      toast.error(t('mcp.invokeFailed', { msg }), { id: toastId });
      setResult({ ok: false, error: msg });
    } finally {
      setRunning(false);
    }
  }, [args, server, tool.name, t, toolFQN]);

  return (
    <div className="px-12 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-mono text-xs font-medium">{toolFQN}</span>
        {hasSchema && (
          <button
            type="button"
            onClick={() => setShowSchema((v) => !v)}
            {...ariaExpanded(showSchema)}
            className="min-h-[28px] px-1 text-[10px] text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
          >
            {showSchema ? t('mcp.hideSchema') : t('mcp.showSchema')}
          </button>
        )}
        <button
          type="button"
          onClick={() => setShowRun((v) => !v)}
          {...ariaExpanded(showRun)}
          className="ml-auto inline-flex min-h-[36px] items-center justify-center gap-1 rounded-md border border-primary/30 bg-primary/5 px-3 py-1.5 text-[11px] text-primary hover:bg-primary/10"
        >
          <Play size={11} />
          {showRun ? t('mcp.hideRun') : t('mcp.run')}
        </button>
      </div>
      {tool.description && (
        <p className="mt-1 text-xs text-muted-foreground">{tool.description}</p>
      )}
      {showSchema && hasSchema && (
        <pre className="mt-2 overflow-x-auto rounded-md border bg-background p-2 font-mono text-[10px] leading-relaxed">
          {JSON.stringify(tool.input_schema, null, 2)}
        </pre>
      )}
      {showRun && (
        <div className="mt-2 space-y-2 rounded-md border bg-muted/10 p-3">
          <label
            className="text-[10px] uppercase tracking-wide text-muted-foreground"
            htmlFor={`mcp-args-${server}-${tool.name}`}
          >
            {t('mcp.argsLabel')}
          </label>
          <textarea
            id={`mcp-args-${server}-${tool.name}`}
            aria-label={t('mcp.argsLabel')}
            title={t('mcp.argsLabel')}
            value={args}
            onChange={(e) => setArgs(e.target.value)}
            spellCheck={false}
            rows={Math.min(10, Math.max(3, args.split('\n').length))}
            className="min-h-24 w-full rounded-md border bg-background p-2 font-mono text-[11px] focus:outline-none focus:ring-2 focus:ring-primary/30"
          />
          {parseErr && (
            <div className="flex items-center gap-1.5 text-[11px] text-destructive">
              <AlertTriangle size={12} />
              {parseErr}
            </div>
          )}
          <div className="flex justify-end">
            <button
              type="button"
              onClick={() => void handleRun()}
              disabled={running}
              className="inline-flex min-h-11 items-center justify-center gap-1.5 rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            >
              {running ? <Loader2 size={12} className="animate-spin" /> : <Play size={12} />}
              {running ? t('mcp.calling') : t('mcp.invoke')}
            </button>
          </div>
          {result && <ToolResult result={result} />}
        </div>
      )}
    </div>
  );
}

function ToolResult({ result }: { result: MCPInvokeResponse }) {
  const { t } = useLocale();
  const tone =
    result.ok ? 'border-emerald-500/30 bg-emerald-500/5' :
    result.is_error ? 'border-amber-500/30 bg-amber-500/5' :
    'border-destructive/30 bg-destructive/5';
  const Icon = result.ok ? CheckCircle2 : AlertTriangle;
  const iconColor = result.ok ? 'text-emerald-600 dark:text-emerald-400' : result.is_error ? 'text-amber-600 dark:text-amber-400' : 'text-destructive';
  const title = result.ok ? t('mcp.success') : result.is_error ? t('mcp.isErrorTitle') : t('mcp.transportTimeout');
  const body = result.ok ? result.output : (result.error || result.output || t('mcp.noDetail'));
  return (
    <div className={`rounded-md border ${tone} p-2`}>
      <div className={`flex items-center gap-1.5 text-[11px] font-medium ${iconColor}`}>
        <Icon size={12} />
        {title}
      </div>
      {body && (
        <pre className="mt-1.5 max-h-64 overflow-y-auto whitespace-pre-wrap font-mono text-[10px] leading-relaxed text-muted-foreground">
          {body}
        </pre>
      )}
    </div>
  );
}

function StatusPill({ status }: { status: ConnectorProviderSummary['status'] }) {
  const { t } = useLocale();
  const cls =
    status === 'enabled' ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300' :
    status === 'ready' ? 'border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300' :
    status === 'failed' ? 'border-rose-500/30 bg-rose-500/10 text-rose-700 dark:text-rose-300' :
    'border-border bg-muted/40 text-muted-foreground';
  return (
    <span className={`rounded-md border px-1.5 py-0.5 text-[10px] uppercase tracking-wide ${cls}`}>
      {t(`mcp.providers.status.${status}`)}
    </span>
  );
}

function MetaPill({ label }: { label: string }) {
  return (
    <span className="rounded-md border bg-muted/30 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">
      {label}
    </span>
  );
}

function RiskPill({ badge }: { badge: ConnectorRiskBadge }) {
  const cls =
    badge.level === 'high' ? 'border-rose-500/30 bg-rose-500/10 text-rose-700 dark:text-rose-300' :
    badge.level === 'medium' ? 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300' :
    'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300';
  return (
    <span className={`inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[10px] uppercase tracking-wide ${cls}`}>
      <ShieldAlert size={11} />
      {badge.label}
    </span>
  );
}

function ToolList({ values, tone }: { values: string[]; tone: 'approved' | 'blocked' }) {
  const { t } = useLocale();
  if (values.length === 0) return null;
  const cls = tone === 'approved'
    ? 'border-emerald-500/20 bg-emerald-500/5 text-emerald-700 dark:text-emerald-300'
    : 'border-rose-500/20 bg-rose-500/5 text-rose-700 dark:text-rose-300';
  return (
    <div className="space-y-1">
      <div className="text-[10px] uppercase tracking-wide text-muted-foreground">
        {tone === 'approved' ? t('mcp.providers.approvedTools') : t('mcp.providers.blockedTools')}
      </div>
      <div className="flex flex-wrap gap-1">
        {values.slice(0, 8).map((value) => (
          <code key={value} className={`rounded border px-1.5 py-0.5 text-[10px] ${cls}`}>
            {value}
          </code>
        ))}
        {values.length > 8 && (
          <span className="text-[10px] text-muted-foreground">+{values.length - 8}</span>
        )}
      </div>
    </div>
  );
}

function HealthMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="p-4">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 font-mono text-2xl font-semibold tabular-nums">{value}</div>
    </div>
  );
}

function EmptyTab({ icon: Icon, title }: { icon: LucideIcon; title: string }) {
  return (
    <div className="rounded-lg border border-dashed py-12 text-center">
      <div className="flex flex-col items-center gap-2 text-muted-foreground">
        <Icon size={32} className="opacity-40" />
        <p className="text-sm font-medium">{title}</p>
      </div>
    </div>
  );
}

function seedArgsFromSchema(schema: Record<string, unknown> | undefined): string {
  if (!schema) return '{}';
  const props = schema.properties as Record<string, { type?: string }> | undefined;
  if (!props || Object.keys(props).length === 0) return '{}';
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(props)) {
    switch (v?.type) {
      case 'integer':
      case 'number':
        out[k] = 0;
        break;
      case 'boolean':
        out[k] = false;
        break;
      case 'array':
        out[k] = [];
        break;
      case 'object':
        out[k] = {};
        break;
      default:
        out[k] = '';
    }
  }
  return JSON.stringify(out, null, 2);
}

function MCPSkeleton() {
  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-4 w-32" />
      </div>
      <Skeleton className="h-9 w-96 max-w-full" />
      <div className="grid gap-3 xl:grid-cols-2">
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-64 w-full rounded-lg" />
        ))}
      </div>
    </div>
  );
}
