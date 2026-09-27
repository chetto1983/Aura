import {
  ChevronLeft,
  Clock3,
  Crop,
  Gauge,
  Music,
  Plus,
  Scissors,
  SlidersHorizontal,
  Sparkles,
  Trash2,
  Type,
  Volume2,
} from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { ClipTab } from './Inspector_clip';
import { Button } from '@/components/ui/button';

interface MobileVideoToolsProps {
  readonly selectedId: string | undefined;
  readonly inspectorTab: ClipTab;
  readonly inspectorOpen: boolean;
  readonly onBack: () => void;
  readonly onSplit: () => void;
  readonly onRemove: () => void;
  readonly onAddClip: () => void;
  readonly onAddAudio: () => void;
  readonly soundSelected: boolean;
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

/** A sound has no frame, no look and no motion: only these of the clip's tools apply to it. */
const SOUND_TABS: readonly ClipTab[] = ['speed', 'audio', 'time'];

export function MobileVideoTools({
  selectedId,
  inspectorTab,
  inspectorOpen,
  onBack,
  onSplit,
  onRemove,
  onAddClip,
  onAddAudio,
  soundSelected,
  onAddTitle,
  onOpenInspector,
}: MobileVideoToolsProps) {
  const { t } = useTranslation();
  const hasSelection = selectedId !== undefined;
  const tools = soundSelected
    ? INSPECTOR_TOOLS.filter(({ tab }) => SOUND_TABS.includes(tab))
    : INSPECTOR_TOOLS;

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
          onClick={onAddAudio}
        >
          <Music aria-hidden="true" />
          <span>{t('videoStudio.audio.add')}</span>
        </Button>
        <Button
          type="button"
          variant="ghost"
          className="video-studio-mobile-tool"
          onClick={onAddTitle}
        >
          <Type aria-hidden="true" />
          <span>{t('videoStudio.command.addText')}</span>
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
      {tools.slice(0, 3).map(({ tab, icon: Icon, labelKey }) => (
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
      {tools.slice(3).map(({ tab, icon: Icon, labelKey }) => (
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
