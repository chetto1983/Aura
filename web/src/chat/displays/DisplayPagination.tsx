import { Children, useId, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';

// DisplayPagination (D-PAGINATION): in-card pagination that lives INSIDE each
// display card. Ports the elysia DisplayPagination LOGIC (Children.toArray +
// slice + page/per-page state, default 3 items/page) and REWRITES the chrome
// with native <button>/<select> on the committed dark-operator tokens — no
// framer-motion, no react-icons, no shadcn Select (those deps are absent and
// UI-SPEC-forbidden).
//
// A11y (UI-SPEC Interaction & Accessibility Contract): prev/next/per-page are
// real native controls with aria-labels + 44px (min-h-11/min-w-11) touch
// targets; the "X–Y of N" micro-label reads the window in context; accent is
// reserved for the ACTIVE page number only (Color rule); the new range is
// announced politely on change (aria-live).

const PER_PAGE_OPTIONS = [3, 6, 9] as const;

export interface DisplayPaginationProps {
  /** The items to paginate (one child per item). */
  readonly children: ReactNode;
  /** Items per page (default 3, D-PAGINATION). */
  readonly itemsPerPage?: number;
  /** Optional class for the items container. */
  readonly className?: string;
}

export function DisplayPagination({
  children,
  itemsPerPage = 3,
  className = '',
}: DisplayPaginationProps) {
  const { t } = useTranslation();
  const selectId = useId();
  const [page, setPage] = useState(0);
  const [perPage, setPerPage] = useState(itemsPerPage);

  const items = Children.toArray(children);
  const total = items.length;
  if (total === 0) return null;

  const totalPages = Math.max(1, Math.ceil(total / perPage));
  // Clamp the page if perPage grew past the current window (defensive — keeps the
  // slice in range even if a re-render shifts the bounds).
  const current = Math.min(page, totalPages - 1);
  const start = current * perPage;
  const visible = items.slice(start, start + perPage);
  const from = start + 1;
  const to = Math.min(start + perPage, total);

  const goPrev = () => {
    setPage((p) => Math.max(0, p - 1));
  };
  const goNext = () => {
    setPage((p) => Math.min(totalPages - 1, p + 1));
  };
  const changePerPage = (value: number) => {
    setPerPage(value);
    setPage(0);
  };

  return (
    <div className="flex w-full flex-col gap-2">
      <div className={`flex flex-col gap-2 ${className}`}>{visible}</div>

      {/* Footer: per-page select + "X–Y of N" count + prev/next. The count is a
          polite live region so the new window is announced on navigation. */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <label
            htmlFor={selectId}
            className="text-[0.75rem] font-medium text-text-muted"
          >
            {t('display.pagination.perPage')}
          </label>
          <select
            id={selectId}
            value={perPage}
            onChange={(e) => {
              changePerPage(Number(e.target.value));
            }}
            className="min-h-11 rounded-[var(--radius-md)] border border-border bg-surface-2 px-2 text-xs text-text outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          >
            {PER_PAGE_OPTIONS.map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
        </div>

        <span
          aria-live="polite"
          className="text-[0.75rem] tabular-nums text-text-faint"
        >
          {t('display.pagination.count', { from, to, total })}
        </span>

        {totalPages > 1 ? (
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={goPrev}
              disabled={current === 0}
              aria-label={t('display.pagination.previous')}
              className="flex min-h-11 min-w-11 items-center justify-center rounded-[var(--radius-md)] text-text-muted hover:text-text disabled:opacity-30 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
              >
                <path d="m15 18-6-6 6-6" />
              </svg>
            </button>
            {/* The active page number is the ONLY accented element (Color rule). */}
            <span className="text-[0.75rem] tabular-nums text-accent-text">
              {current + 1}
            </span>
            <button
              type="button"
              onClick={goNext}
              disabled={current >= totalPages - 1}
              aria-label={t('display.pagination.next')}
              className="flex min-h-11 min-w-11 items-center justify-center rounded-[var(--radius-md)] text-text-muted hover:text-text disabled:opacity-30 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
              >
                <path d="m9 18 6-6-6-6" />
              </svg>
            </button>
          </div>
        ) : null}
      </div>
    </div>
  );
}
