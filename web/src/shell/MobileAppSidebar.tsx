import type { ReactNode } from 'react';
import { FileText, MessageSquareText, Network, Plus, Settings, ShieldCheck } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { LIVE_MODES, type SurfaceIntent } from './modes';
import { Button } from '@/components/ui/button';

const modeIcons = {
  chat: MessageSquareText,
  graph: Network,
  governance: ShieldCheck,
  documents: FileText,
  settings: Settings,
} as const satisfies Record<(typeof LIVE_MODES)[number], typeof MessageSquareText>;

interface MobileAppSidebarProps {
  readonly activeMode: SurfaceIntent;
  readonly onModeSelect: (mode: SurfaceIntent) => void;
  readonly onCreateIdentity: () => void;
  readonly children: ReactNode;
}

export function MobileAppSidebar({
  activeMode,
  onModeSelect,
  onCreateIdentity,
  children,
}: MobileAppSidebarProps) {
  const { t } = useTranslation();
  return (
    <div className="flex h-full min-h-0 flex-col gap-3 bg-bg px-3 py-3 text-text">
      <nav aria-label={t('shell.mobileModes')} className="grid gap-1">
        {LIVE_MODES.map((mode) => {
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

      <Button
        type="button"
        variant="ghost"
        onClick={onCreateIdentity}
        className="h-11 min-h-11 justify-start rounded-md px-2 text-[14px] font-medium text-text-muted hover:bg-surface-2 hover:text-text"
      >
        <Plus aria-hidden="true" />
        {t('onboarding.open')}
      </Button>

      <div className="h-px bg-border" />

      <div className="min-h-0 flex-1 overflow-hidden">{children}</div>
    </div>
  );
}
