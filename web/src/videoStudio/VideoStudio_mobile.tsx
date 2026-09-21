import {
  ChevronLeft,
  Clock3,
  Crop,
  Gauge,
  Plus,
  Scissors,
  SlidersHorizontal,
  Sparkles,
  Trash2,
  Type,
  Volume2,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import type { ClipTab } from './Inspector_clip';

interface MobileVideoToolsProps {
  readonly selectedId: string | undefined;
  readonly inspectorTab: ClipTab;
  readonly inspectorOpen: boolean;
  readonly onBack: () => void;
  readonly onSplit: () => void;
  readonly onRemove: () => void;
  readonly onAddClip: () => void;
  readonly onAddTitle: () => void;
  readonly onOpenInspector: (tab: ClipTab) => void;
}

interface InspectorTool {
  readonly tab: ClipTab;
  readonly icon: typeof Crop;
  readonly labelKey: string;
}

const INSPECTOR_TOOLS: readonly InspectorTool[] = [
  { tab: 'adjust', icon: SlidersHorizontal, labelKey: 'videoStudio.mobile.adjust' },
  { tab: 'animation', icon: Sparkles, labelKey: 'videoStudio.mobile.animations' },
  { tab: 'speed', icon: Gauge, labelKey: 'videoStudio.mobile.speed' },
  { tab: 'audio', icon: Volume2, labelKey: 'videoStudio.mobile.audio' },
  { tab: 'transform', icon: Crop, labelKey: 'videoStudio.mobile.transform' },
  { tab: 'time', icon: Clock3, labelKey: 'videoStudio.mobile.time' },
];

export function MobileVideoTools({
  selectedId,
  inspectorTab,
  inspectorOpen,
  onBack,
  onSplit,
  onRemove,
  onAddClip,
  onAddTitle,
  onOpenInspector,
}: MobileVideoToolsProps) {
  const { t } = useTranslation();
  const hasSelection = selectedId !== undefined;

  if (!hasSelection) {
    return (
      <nav className="video-studio-mobile-tools" aria-label={t('videoStudio.mobileTools')}>
        <Button
          type="button"
          variant="ghost"
          className="video-studio-mobile-tool"
          onClick={onAddClip}
        >
          <Plus aria-hidden="true" />
          <span>{t('videoStudio.command.addSource')}</span>
        </Button>
        <Button
          type="button"
          variant="ghost"
          className="video-studio-mobile-tool"
          onClick={onAddTitle}
        >
          <Type aria-hidden="true" />
          <span>{t('videoStudio.command.addTitle')}</span>
        </Button>
      </nav>
    );
  }

  return (
    <nav className="video-studio-mobile-tools" aria-label={t('videoStudio.mobileTools')}>
      <Button
        type="button"
        variant="ghost"
        className="video-studio-mobile-back"
        aria-label={t('videoStudio.mobile.back')}
        onClick={onBack}
      >
        <ChevronLeft aria-hidden="true" />
      </Button>
      <Button type="button" variant="ghost" className="video-studio-mobile-tool" onClick={onSplit}>
        <Scissors aria-hidden="true" />
        <span>{t('videoStudio.mobile.split')}</span>
      </Button>
      {INSPECTOR_TOOLS.slice(0, 3).map(({ tab, icon: Icon, labelKey }) => (
        <Button
          key={tab}
          type="button"
          variant="ghost"
          className="video-studio-mobile-tool"
          aria-pressed={inspectorOpen && inspectorTab === tab}
          onClick={() => {
            onOpenInspector(tab);
          }}
        >
          <Icon aria-hidden="true" />
          <span>{t(labelKey)}</span>
        </Button>
      ))}
      <Button type="button" variant="ghost" className="video-studio-mobile-tool" onClick={onRemove}>
        <Trash2 aria-hidden="true" />
        <span>{t('videoStudio.mobile.delete')}</span>
      </Button>
      {INSPECTOR_TOOLS.slice(3).map(({ tab, icon: Icon, labelKey }) => (
        <Button
          key={tab}
          type="button"
          variant="ghost"
          className="video-studio-mobile-tool"
          aria-pressed={inspectorOpen && inspectorTab === tab}
          onClick={() => {
            onOpenInspector(tab);
          }}
        >
          <Icon aria-hidden="true" />
          <span>{t(labelKey)}</span>
        </Button>
      ))}
    </nav>
  );
}
