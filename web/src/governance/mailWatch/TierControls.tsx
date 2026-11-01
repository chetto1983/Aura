import type { KeyboardEvent, ReactNode } from 'react';
import { Moon, Sun, Zap, type LucideIcon } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { MAIL_WATCH_TIERS, type MailWatchTier } from '../mailWatchApi';
import type { TierChoice } from './mailWatchLogic';
import { Badge, type BadgeProps } from '@/components/ui/badge';
import { Select } from '@/components/ui/select';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { cn } from '@/lib/utils';

// TierControls.tsx is how a tier looks, wherever it appears. Each tier carries its own glyph as
// well as its colour, so it reads at a glance and survives without colour: amber lightning for
// what needs the person now, blue sun for the day's digest, a quiet moon for what can wait.

interface TierLook {
  readonly icon: LucideIcon;
  readonly badge: BadgeProps['variant'];
  /** The pressed state in the tier control. */
  readonly on: string;
  readonly hue: string;
  readonly rail: string;
}

const TIER_LOOK: Readonly<Record<MailWatchTier, TierLook>> = {
  now: {
    icon: Zap,
    badge: 'warning',
    on: 'data-[state=on]:bg-warning/15 data-[state=on]:text-warning',
    hue: 'text-warning',
    rail: 'bg-warning',
  },
  today: {
    icon: Sun,
    badge: 'info',
    on: 'data-[state=on]:bg-info/15 data-[state=on]:text-info',
    hue: 'text-info',
    rail: 'bg-info',
  },
  later: {
    icon: Moon,
    badge: 'secondary',
    on: 'data-[state=on]:bg-surface-2 data-[state=on]:text-text',
    hue: 'text-text-muted',
    rail: 'bg-border-strong',
  },
};

export function TierBadge({ tier }: { readonly tier: MailWatchTier }) {
  const { t } = useTranslation();
  const look = TIER_LOOK[tier];
  return (
    <Badge variant={look.badge}>
      <look.icon aria-hidden="true" />
      {t(`governance.mailWatch.tier.${tier}`)}
    </Badge>
  );
}

/** A tier's glyph beside any text, for a heading or a count: the colour stays on the glyph. */
export function TierMark({
  tier,
  children,
}: {
  readonly tier: MailWatchTier;
  readonly children: ReactNode;
}) {
  const look = TIER_LOOK[tier];
  return (
    <span className="inline-flex items-center gap-1.5">
      <look.icon aria-hidden="true" className={cn('size-3.5', look.hue)} />
      {children}
    </span>
  );
}

/** The bar on a row's edge. Its colour eases to the new tier, so a correction is seen landing. */
export function TierRail({ tier }: { readonly tier: MailWatchTier }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'absolute inset-y-2 left-1 w-[3px] rounded-full transition-colors duration-(--motion-base) motion-reduce:transition-none',
        TIER_LOOK[tier].rail,
      )}
    />
  );
}

interface TierControlProps {
  readonly value: MailWatchTier;
  /** Whether the control is in the tab order: only the active row's is. */
  readonly tabbable: boolean;
  readonly onChange: (tier: MailWatchTier) => void;
  readonly onFocus: () => void;
  readonly onKeyDown: (event: KeyboardEvent<HTMLElement>) => void;
}

/** The three tiers as one choice. Radix reports '' when the pressed tier is pressed again, which
 * changes nothing here: a mail always has a tier. Outside the active row the roving focus leaves
 * the tab order at both levels, the group and its items. */
export function TierControl({ value, tabbable, onChange, onFocus, onKeyDown }: TierControlProps) {
  const { t } = useTranslation();
  const tabOrder = tabbable ? {} : { tabIndex: -1 };
  return (
    <ToggleGroup
      type="single"
      role="radiogroup"
      spacing={1}
      value={value}
      aria-label={t('governance.mailWatch.tier.aria')}
      onValueChange={(next: string) => {
        if (next !== '' && next !== value) onChange(next as MailWatchTier);
      }}
      onFocus={onFocus}
      onKeyDown={onKeyDown}
      className="shrink-0 gap-0.5 rounded-[var(--radius-sm)] border border-border bg-surface p-0.5"
      {...tabOrder}
    >
      {MAIL_WATCH_TIERS.map((tier) => {
        const look = TIER_LOOK[tier];
        const name = t(`governance.mailWatch.tier.${tier}`);
        return (
          <ToggleGroupItem
            key={tier}
            value={tier}
            aria-label={name}
            title={name}
            className={cn(
              'group/tier h-11 min-w-11 gap-1.5 rounded-[6px] px-2 text-[13px] font-semibold text-text-muted transition-colors hover:bg-surface-2 hover:text-text @lg/day:h-9',
              look.on,
            )}
            {...tabOrder}
          >
            <look.icon aria-hidden="true" className="size-4" />
            <span className="hidden group-data-[state=on]/tier:inline">{name}</span>
          </ToggleGroupItem>
        );
      })}
    </ToggleGroup>
  );
}

interface TierSelectProps {
  readonly id: string;
  readonly value: TierChoice;
  readonly onChange: (tier: TierChoice) => void;
}

/** An optional tier, for a rule or a label. */
export function TierSelect({ id, value, onChange }: TierSelectProps) {
  const { t } = useTranslation();
  return (
    <Select
      id={id}
      value={value}
      onValueChange={(next) => {
        onChange(next as TierChoice);
      }}
      options={[
        { value: '', label: t('governance.mailWatch.tier.none') },
        ...MAIL_WATCH_TIERS.map((tier) => ({
          value: tier,
          label: t(`governance.mailWatch.tier.${tier}`),
        })),
      ]}
    />
  );
}
