import type { ReactNode } from 'react';
import { FileText, MessageSquareText, Network, Settings, ShieldCheck } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { MODES, type SurfaceIntent } from './modes';
import { Button } from '@/components/ui/button';

const modeIcons = {
  chat: MessageSquareText,
  graph: Network,
  governance: ShieldCheck,
  documents: FileText,
  settings: Settings,
} as const satisfies Record<SurfaceIntent, typeof MessageSquareText>;

interface MobileAppSidebarProps {
  readonly activeMode: SurfaceIntent;
  readonly onModeSelect: (mode: SurfaceIntent) => void;
  readonly children: ReactNode;
  // Visible live modes (MUSR-01 capability-gated by AppShell); defaults to the full live set.
  readonly modes?: readonly SurfaceIntent[];
}

export function MobileAppSidebar({
  activeMode,
  onModeSelect,
  children,
  modes = MODES,
}: MobileAppSidebarProps) {
  const { t } = useTranslation();
  return (
    <div className="flex h-full min-h-0 flex-col gap-3 bg-bg px-3 py-3 text-text">
      <nav aria-label={t('shell.mobileModes')} className="grid gap-1">
        {modes.map((mode) => {
          const Icon = modeIcons[mode];
          return (
            <Button
              key={mode}
              type="button"
              variant="ghost"
              aria-current={mode === activeMode ? 'page' : undefined}
              onClick={() => {
                onModeSelect(mode);
              }}
              className="h-11 min-h-11 justify-start rounded-md px-2 text-[14px] font-medium text-text-muted hover:bg-surface-2 hover:text-text aria-[current=page]:bg-surface-2 aria-[current=page]:text-text"
            >
              <Icon aria-hidden="true" />
              {t(`shell.modes.${mode}`)}
            </Button>
          );
        })}
      </nav>

      <div className="h-px bg-border" />

      <div className="min-h-0 flex-1 overflow-hidden">{children}</div>
    </div>
  );
}
