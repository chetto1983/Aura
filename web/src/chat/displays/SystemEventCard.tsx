import { useTranslation } from 'react-i18next';
import type { DisplaySystem } from './types';
import { DisplayCardShell } from './DisplayCardShell';

// SystemEventCard (DISP-04 / D-07): a web-safety / swarm-status card that renders
// ONLY the classified safe-reason label from the backend web/errors.go enum — never
// free-form error text, never a raw URL, never SSRF internals (T-26-12). The reason
// code maps to a friendly i18n label; an unmapped/unknown code falls to a generic
// safe fallback. Severity drives a left-rule color + a status icon + the status word
// (color is NEVER the only signal — WCAG 1.4.1). The card is role="status" (post-hoc
// evidence), not "alert" (not an interrupt).

// The FULL set of web/errors.go codes (8). UI-SPEC mapped only 5; RESEARCH flagged
// the drift — all 8 are covered here, each with an en+it label in systemEvent.reason.*.
const KNOWN_REASONS = new Set([
  'web_search_unavailable',
  'blocked_url',
  'unsupported_scheme',
  'unsupported_content_type',
  'response_too_large',
  'timeout',
  'http_error',
  'extraction_failed',
]);

type Severity = 'error' | 'warning' | 'info';

const RULE: Record<Severity, 'danger' | 'warning' | 'info'> = {
  error: 'danger',
  warning: 'warning',
  info: 'info',
};

const ICON_CLASS: Record<Severity, string> = {
  error: 'text-danger',
  warning: 'text-warning',
  info: 'text-info',
};

/** A severity glyph (icon, not color alone): ✕ danger · ! warning · i info. */
function SeverityIcon({ severity }: { readonly severity: Severity }) {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className={`shrink-0 ${ICON_CLASS[severity]}`}
    >
      <circle cx="12" cy="12" r="10" />
      {severity === 'error' ? (
        <>
          <path d="m15 9-6 6" />
          <path d="m9 9 6 6" />
        </>
      ) : (
        <>
          <path d="M12 8v4" />
          <path d="M12 16h.01" />
        </>
      )}
    </svg>
  );
}

export interface SystemEventCardProps {
  readonly payload: { readonly system?: DisplaySystem };
}

export function SystemEventCard({ payload }: SystemEventCardProps) {
  const { t } = useTranslation();
  const system = payload.system;
  const label = t('display.type.system_event');

  if (system === undefined) {
    return (
      <DisplayCardShell label={label} rule="info">
        <div role="status" className="flex items-center gap-2">
          <SeverityIcon severity="info" />
          <span className="text-sm text-text-muted">{t('systemEvent.reason.unknown')}</span>
        </div>
      </DisplayCardShell>
    );
  }

  // The wire type pins severity to error|warning; the card adds `info` only for the
  // missing-slot fallback above.
  const severity: Severity = system.severity;
  // The reason ALWAYS resolves through the enum map; an unknown code → the generic
  // safe label. We never fall back to system.message free text (T-26-12).
  const reasonKey =
    system.reason !== undefined && KNOWN_REASONS.has(system.reason)
      ? `systemEvent.reason.${system.reason}`
      : 'systemEvent.reason.unknown';

  return (
    <DisplayCardShell
      label={label}
      meta={t(`systemEvent.severity.${severity}`)}
      rule={RULE[severity]}
    >
      {/* role="status" (not alert) — post-hoc evidence, announced politely. Status is
          conveyed by color (rule + icon tint) + icon + text, never color alone. */}
      <div role="status" className="flex items-center gap-2">
        <SeverityIcon severity={severity} />
        <span className="text-sm text-text-muted">{t(reasonKey)}</span>
      </div>
    </DisplayCardShell>
  );
}
