import type { ReactNode } from 'react';
import { ModeTabBar } from './ModeTabBar';
import type { SurfaceIntent } from './modes';

export function BottomDock({
  activeMode,
  onModeSelect,
  children,
  modes,
}: {
  readonly activeMode: SurfaceIntent;
  readonly onModeSelect: (mode: SurfaceIntent) => void;
  readonly children: ReactNode;
  // Visible live modes (MUSR-01 capability-gated by AppShell); undefined keeps the full set.
  readonly modes?: readonly SurfaceIntent[];
}) {
  return (
    <div className="shell-bottom-dock grid min-h-0 border-t border-border bg-surface pb-[max(env(safe-area-inset-bottom),0px)] [padding-bottom:calc(max(env(safe-area-inset-bottom),0px)+env(keyboard-inset-height,0px))]">
      {children}
      <ModeTabBar active={activeMode} onSelect={onModeSelect} modes={modes} />
    </div>
  );
}
