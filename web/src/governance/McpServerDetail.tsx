import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { McpEnvEditForm } from './McpEnvEditForm';
import { McpLifecycleCluster } from './McpLifecycleCluster';
import { WhatsAppConnect } from './WhatsAppConnect';
import { CalendarConnect } from './CalendarConnect';
import { isWhatsAppServer, type McpProbeResult, type McpServerRow } from './governanceApi';
import { isCalendarServer } from './pimApi';

// McpServerDetail — the detail pane for a selected MCP server (the NodeInspector <dl>/<dt>/<dd>
// idiom, NodeInspector.tsx:72-98). It renders the static row fields (trust/runtime/startup/auth)
// as React-escaped text, the redacted env-KEY chips (value NEVER in the DOM — T-28-03-01), and
// the LIVE probe outcome (doctor detail + tool count) once the per-row probe resolves. Every
// data-shaped value (env key, content hash, probe detail) is mono so it reads as data, not prose.
// The close button restores focus to the originating row (the BoardLayout focus-restore wiring).

export interface McpServerDetailProps {
  readonly server: McpServerRow;
  readonly probe: McpProbeResult | undefined;
  readonly probeLoading: boolean;
  readonly onClose: () => void;
}

function Field({ label, value }: { readonly label: string; readonly value: string }) {
  return (
    <div className="flex flex-col">
      <dt className="text-[13px] font-semibold text-text-muted">{label}</dt>
      <dd className="break-words text-[15.5px] text-text">{value}</dd>
    </div>
  );
}

export function McpServerDetail({ server, probe, probeLoading, onClose }: McpServerDetailProps) {
  const { t } = useTranslation();
  const none = t('governance.mcp.detail.none');
  const [editingEnv, setEditingEnv] = useState(false);
  const [secretPreserved, setSecretPreserved] = useState(false);

  return (
    <div className="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-4">
      <header className="flex items-start justify-between gap-2">
        {/* Backend-supplied name — React-escaped text, mono (it is an identifier). */}
        <h3 className="break-words font-display text-[20px] font-semibold text-text">
          {server.name}
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

      <dl className="flex flex-col gap-3">
        <Field label={t('governance.mcp.detail.source')} value={server.source || none} />
        <Field label={t('governance.mcp.detail.trust')} value={server.trust} />
        <Field label={t('governance.mcp.detail.riskPolicy')} value={server.riskPolicy || none} />
        <Field label={t('governance.mcp.detail.runtime')} value={server.runtime} />
        <Field label={t('governance.mcp.detail.startup')} value={server.startupState} />
        <Field label={t('governance.mcp.detail.authStatus')} value={server.authStatus} />
        <Field
          label={t('governance.mcp.detail.profiles')}
          value={server.profiles.length > 0 ? server.profiles.join(', ') : none}
        />
        <Field
          label={t('governance.mcp.detail.networkAllowlist')}
          value={server.networkAllowlist.length > 0 ? server.networkAllowlist.join(', ') : none}
        />
      </dl>

      {/* Lifecycle cluster — enable/disable + trust-approve + remove (inline, not a kebab). */}
      <McpLifecycleCluster server={server} onRemoved={onClose} />

      {/* Cockpit "Connect" — the WhatsApp device-linking section, only for the WhatsApp server. */}
      {isWhatsAppServer(server) ? <WhatsAppConnect /> : null}

      {/* Cockpit "Connect" — the Google Calendar account-linking section, only for the calendar
          (aura-pim-mcp) server. */}
      {isCalendarServer(server) ? <CalendarConnect /> : null}

      {editingEnv ? (
        <McpEnvEditForm
          serverName={server.name}
          envKeys={server.envKeys}
          onSaved={(preserved) => {
            setSecretPreserved(preserved);
          }}
          onClose={() => {
            setEditingEnv(false);
          }}
        />
      ) : (
        <section className="flex flex-col gap-1">
          <div className="flex items-center justify-between gap-2">
            <h4 className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
              {t('governance.mcp.detail.envKeys')}
            </h4>
            <button
              type="button"
              onClick={() => {
                setSecretPreserved(false);
                setEditingEnv(true);
              }}
              className="min-h-[44px] rounded-md border border-border-strong bg-surface-2 px-3 py-2 text-[13px] font-semibold text-text outline-none hover:border-border-strong focus-visible:ring-2 focus-visible:ring-ring"
            >
              {t('governance.mcp.detail.editEnv')}
            </button>
          </div>
          {secretPreserved ? (
            <p role="status" className="text-[13px] text-success">
              {t('governance.mcp.env.secretPreserved')}
            </p>
          ) : null}
          {server.envKeys.length > 0 ? (
            <ul className="flex flex-wrap gap-1">
              {server.envKeys.map((chip) => (
                <li
                  key={chip.key}
                  className="flex items-center gap-1 rounded-sm bg-surface-3 px-2 py-1 font-mono text-[13px] text-text-muted"
                >
                  {/* The KEY name (React-escaped); the VALUE never reaches the DOM. */}
                  <span className="break-all text-text">{chip.key}</span>
                  {chip.redacted ? (
                    <span className="text-text-muted">· {t('governance.mcp.redacted')}</span>
                  ) : null}
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-[15.5px] text-text-muted">{t('governance.mcp.detail.noEnvKeys')}</p>
          )}
        </section>
      )}

      {/* Live probe outcome — the heavier doctor result, resolved independently per row. */}
      <section className="flex flex-col gap-1">
        <h4 className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
          {t('governance.mcp.detail.probeDetail')}
        </h4>
        {probeLoading ? (
          <p role="status" className="text-[15.5px] text-warning">
            {t('governance.mcp.status.checking')}
          </p>
        ) : probe !== undefined ? (
          <dl className="flex flex-col gap-2">
            <div className="flex flex-col">
              <dt className="text-[13px] font-semibold text-text-muted">
                {t('governance.mcp.detail.toolCount')}
              </dt>
              <dd className="text-[15.5px] text-text">{probe.tool_count}</dd>
            </div>
            <div className="flex flex-col">
              <dt className="text-[13px] font-semibold text-text-muted">
                {t('governance.mcp.detail.probeDetail')}
              </dt>
              <dd
                className={`break-words font-mono text-[13px] tabular-nums ${probe.ok ? 'text-success' : 'text-danger'}`}
              >
                {probe.detail}
              </dd>
            </div>
            {probe.err !== undefined && probe.err !== '' ? (
              <div className="flex flex-col">
                <dt className="text-[13px] font-semibold text-text-muted">
                  {t('governance.mcp.detail.lastError')}
                </dt>
                <dd className="break-words font-mono text-[13px] text-danger">{probe.err}</dd>
              </div>
            ) : null}
            {/* Fail-soft per-row probe warning — this row only; the board keeps rendering. */}
            {!probe.ok ? (
              <p role="note" className="text-[13px] text-warning">
                {t('governance.mcp.lifecycle.probeWarning')}
              </p>
            ) : null}
          </dl>
        ) : null}
      </section>

      {server.lastError !== undefined && server.lastError !== '' ? (
        <section className="flex flex-col gap-1">
          <h4 className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
            {t('governance.mcp.detail.lastError')}
          </h4>
          <p className="break-words font-mono text-[13px] text-danger">{server.lastError}</p>
        </section>
      ) : null}
    </div>
  );
}
