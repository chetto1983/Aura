import { useEffect, useRef, useState } from 'react';
import type { TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';

// durationFormat — the shared elapsed-time formatter + live ticker hook, extracted
// from ToolActivityCard so tool rows, the tool-group header, and the reasoning
// pill all render durations identically (REUSABLE CODE; compact-chat spec §5.3,
// docs/superpowers/specs/2026-07-23-cockpit-compact-chat-ui-spec.md).

/** Format elapsed time through the active locale and bilingual duration templates. */
export function formatElapsed(ms: number, language: string, t: TFunction): string {
  const seconds = Math.max(0, ms / 1000);
  const number = new Intl.NumberFormat(language, {
    maximumFractionDigits: seconds < 10 ? 2 : 0,
  });
  if (seconds < 60) {
    return t('chat.tool.duration.seconds', { value: number.format(seconds) });
  }
  return t('chat.tool.duration.minutes', {
    minutes: String(Math.floor(seconds / 60)),
    seconds: String(Math.floor(seconds % 60)),
  });
}

/**
 * Live elapsed readout. `running` controls ticker life and assistive semantics
 * (generalized from the ToolStatus param so non-tool parts can share it).
 * `finishedAt` is the authoritative endpoint when present; otherwise the logical
 * settlement instant is captured once. The interval is cleared on settlement and
 * unmount. Returns null without a start timestamp.
 */
export function useElapsed(
  startedAt: number | undefined,
  finishedAt: number | undefined,
  running: boolean,
): string | null {
  const { i18n, t } = useTranslation();
  const isRunning = startedAt !== undefined && running;
  const previousRunning = useRef(running);
  // Date.now initializes state once during mount rendering, then is read on running
  // ticks and the logical-settlement edge.
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!isRunning) return;
    const id = setInterval(() => {
      setNow(Date.now());
    }, 1000);
    return () => {
      clearInterval(id);
    };
  }, [isRunning, startedAt]);

  useEffect(() => {
    const settled = previousRunning.current && !running;
    previousRunning.current = running;
    if (!settled || finishedAt !== undefined || startedAt === undefined) return;

    const settledAt = Date.now();
    let active = true;
    // Capture synchronously, then notify outside the effect body to preserve hook purity.
    queueMicrotask(() => {
      if (active) setNow(settledAt);
    });
    return () => {
      active = false;
    };
  }, [finishedAt, startedAt, running]);

  if (startedAt === undefined) return null;
  const end = finishedAt ?? now;
  return formatElapsed(end - startedAt, i18n.resolvedLanguage ?? i18n.language, t);
}
