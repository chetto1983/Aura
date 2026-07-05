import { useTranslation } from 'react-i18next';
import { FileText, MessageSquareText, Network, Settings, ShieldCheck } from 'lucide-react';
import { LIVE_MODES, type SurfaceIntent } from './modes';
import { Button } from '@/components/ui/button';

const modeIcons = {
  chat: MessageSquareText,
  graph: Network,
  governance: ShieldCheck,
  documents: FileText,
  settings: Settings,
} satisfies Record<(typeof LIVE_MODES)[number], typeof MessageSquareText>;

export function ModeTabBar({
  active,
  onSelect,
  modes = LIVE_MODES,
}: {
  readonly active: SurfaceIntent;
  readonly onSelect: (mode: SurfaceIntent) => void;
  // Visible live modes (MUSR-01 capability-gated by AppShell); defaults to the full live set.
  readonly modes?: readonly (typeof LIVE_MODES)[number][] | undefined;
}) {
  const { t } = useTranslation();
  return (
    <nav
      aria-label={t('shell.mobileModes')}
      className="shell-mode-tabbar flex min-w-0 overflow-hidden border-t border-border bg-surface md:hidden"
    >
      {modes.map((mode) => {
        const Icon = modeIcons[mode];
        const fullLabel = t(`shell.modes.${mode}`);
        const compactLabel = t(`shell.modesCompact.${mode}`);
        return (
          <Button
            key={mode}
            type="button"
            variant="ghost"
            aria-label={fullLabel}
            aria-current={mode === active ? 'page' : undefined}
            title={fullLabel}
            onClick={() => {
              onSelect(mode);
            }}
            className="h-auto min-h-[48px] min-w-[44px] flex-1 flex-col gap-1 rounded-none px-1 py-1.5 text-[11px] font-medium leading-none text-text-muted aria-[current=page]:bg-surface-3 aria-[current=page]:text-accent-text focus-visible:outline-2 focus-visible:outline-inset focus-visible:outline-accent"
          >
            <Icon data-mode-icon aria-hidden="true" className="size-4" />
            <span data-mode-label className="max-w-full text-center leading-none">
              {compactLabel}
            </span>
          </Button>
        );
      })}
    </nav>
  );
}
