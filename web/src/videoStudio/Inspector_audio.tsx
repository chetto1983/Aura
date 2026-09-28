import { AudioLines } from 'lucide-react';
import { useId, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { formatTimecode } from '../mediaEdit/timecode';
import { TimeField } from '../mediaEdit/TimeField';
import { audioLength, audioWindow } from './audioLane';
import { setClipPresentation, setMuted } from './commands';
import {
  extractAudio,
  moveAudio,
  setAudioProperties,
  trimAudio,
  type SetAudioPropertiesArgs,
} from './commands_audio';
import { DuckingControls, NoiseReductionSwitch } from './Inspector_audioClean';
import type { AudioItem, VideoItem, VideoProject } from './project';
import { Button } from '@/components/ui/button';
import { Slider } from '@/components/ui/slider';
import { Switch } from '@/components/ui/switch';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

// Inspector_audio.tsx — what a sound shows in the inspector, and the audio controls a clip shares
// with it: one volume slider, one mute, one speed control for both, so the two never drift. Noise
// reduction and ducking live in Inspector_audioClean.tsx: they analyse before they edit.

type Commit = (edit: (current: VideoProject) => VideoProject) => void;
export type AudioTab = 'audio' | 'speed' | 'time';
const AUDIO_TABS: readonly AudioTab[] = ['audio', 'speed', 'time'];
const MAX_FADE = 5;

/** The Audio tab's panel, a clip's or a sound's. It stays mounted, hidden, while another tab shows:
 *  a cleaning or a listen started in it runs on through a tab switch rather than being cut off
 *  without a word, and says how it went when the tab comes back. */
export function AudioTabContent({
  tab,
  children,
}: {
  readonly tab: string;
  readonly children: ReactNode;
}) {
  return (
    <TabsContent value="audio" forceMount hidden={tab !== 'audio'}>
      {children}
    </TabsContent>
  );
}

/** Volume, 0–200 %: a clip's and a sound's. */
export function VolumeSlider({
  volume,
  onCommit,
}: {
  readonly volume: number;
  readonly onCommit: (volume: number) => void;
}) {
  const { t } = useTranslation();
  return (
    <label className="video-studio-adjustment">
      <span>{t('videoStudio.inspector.volume')}</span>
      <span className="font-mono tabular-nums">{Math.round(volume * 100)}%</span>
      <Slider
        key={volume}
        aria-label={t('videoStudio.inspector.volume')}
        min={0}
        max={200}
        step={1}
        defaultValue={[Math.round(volume * 100)]}
        onValueCommit={([next = 100]) => {
          onCommit(next / 100);
        }}
      />
    </label>
  );
}

function MuteSwitch({
  muted,
  onChange,
}: {
  readonly muted: boolean;
  readonly onChange: (muted: boolean) => void;
}) {
  const { t } = useTranslation();
  const id = useId();
  return (
    <div className="flex items-center gap-2 text-xs text-text-muted">
      <Switch
        id={id}
        checked={muted}
        aria-label={t('videoStudio.inspector.mute')}
        onCheckedChange={onChange}
      />
      <label htmlFor={id}>{t('videoStudio.inspector.mute')}</label>
    </div>
  );
}

/** The playback rate, 0.25–4×, with four presets: a clip's and a sound's. */
export function SpeedSlider({
  speed,
  onCommit,
}: {
  readonly speed: number;
  readonly onCommit: (speed: number) => void;
}) {
  const { t } = useTranslation();
  return (
    <>
      <label className="video-studio-adjustment">
        <span>{t('videoStudio.inspector.playbackRate')}</span>
        <span className="font-mono tabular-nums">{speed.toFixed(2)}×</span>
        <Slider
          key={speed}
          aria-label={t('videoStudio.inspector.playbackRate')}
          min={25}
          max={400}
          step={5}
          defaultValue={[Math.round(speed * 100)]}
          onValueCommit={([next = 100]) => {
            onCommit(next / 100);
          }}
        />
      </label>
      <div className="grid grid-cols-4 gap-1">
        {[0.5, 1, 1.5, 2].map((value) => (
          <Button
            key={value}
            type="button"
            variant={speed === value ? 'default' : 'ghost'}
            size="sm"
            onClick={() => {
              onCommit(value);
            }}
          >
            {value}×
          </Button>
        ))}
      </div>
    </>
  );
}

/** A clip's Audio tab. */
export function ClipAudioControls({
  project,
  clip,
  onCommand,
}: {
  readonly project: VideoProject;
  readonly clip: VideoItem;
  readonly onCommand: Commit;
}) {
  const { t } = useTranslation();
  return (
    <div className="video-studio-tab-panel">
      <VolumeSlider
        volume={clip.volume ?? 1}
        onCommit={(volume) => {
          onCommand((current) => setClipPresentation(current, { clipId: clip.id, volume }));
        }}
      />
      <MuteSwitch
        muted={clip.muted}
        onChange={(muted) => {
          onCommand((current) => setMuted(current, { clipId: clip.id, muted }));
        }}
      />
      <NoiseReductionSwitch
        key={clip.id}
        project={project}
        target={{ kind: 'clip', id: clip.id, sourceId: clip.sourceId, on: clip.denoise === true }}
        onCommand={onCommand}
      />
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={() => {
          onCommand((current) => extractAudio(current, { clipId: clip.id }));
        }}
      >
        <AudioLines aria-hidden="true" />
        {t('videoStudio.audio.extract')}
      </Button>
    </div>
  );
}

function FadeSlider({
  label,
  value,
  max,
  onCommit,
}: {
  readonly label: string;
  readonly value: number;
  readonly max: number;
  readonly onCommit: (seconds: number) => void;
}) {
  return (
    <label className="video-studio-adjustment">
      <span>{label}</span>
      <span className="font-mono tabular-nums">{value.toFixed(1)}s</span>
      <Slider
        key={value}
        aria-label={label}
        min={0}
        max={max}
        step={0.1}
        defaultValue={[value]}
        onValueCommit={([next = value]) => {
          onCommit(next);
        }}
      />
    </label>
  );
}

interface AudioItemInspectorProps {
  readonly project: VideoProject;
  readonly item: AudioItem;
  readonly onCommand: Commit;
  /** The workspace's tab, shared with the clip inspector; one a sound lacks falls back to Audio. */
  readonly activeTab?: string;
  readonly onTabChange?: (tab: AudioTab) => void;
}

export function AudioItemInspector({
  project,
  item,
  onCommand,
  activeTab,
  onTabChange,
}: AudioItemInspectorProps) {
  const { t } = useTranslation();
  const [internalTab, setInternalTab] = useState<AudioTab>('audio');
  const asked = activeTab ?? internalTab;
  const tab = AUDIO_TABS.find((candidate) => candidate === asked) ?? 'audio';
  const set = (values: Omit<SetAudioPropertiesArgs, 'itemId'>) => {
    onCommand((current) => setAudioProperties(current, { itemId: item.id, ...values }));
  };
  const trim = (start: number, end: number) => {
    onCommand((current) => trimAudio(current, { itemId: item.id, start, end }));
  };
  const starts = audioWindow(project, item).start;
  const fadeRoom = Math.min(MAX_FADE, audioLength(item));
  const end = item.sourceStart + item.duration;
  return (
    <Tabs
      value={tab}
      onValueChange={(value) => {
        const next = value as AudioTab;
        setInternalTab(next);
        onTabChange?.(next);
      }}
      className="min-w-0 gap-3"
    >
      <TabsList className="video-studio-tool-tabs">
        {AUDIO_TABS.map((name) => (
          <TabsTrigger key={name} value={name}>
            {t(`videoStudio.inspector.tabs.${name}`)}
          </TabsTrigger>
        ))}
      </TabsList>
      <AudioTabContent tab={tab}>
        <div className="video-studio-tab-panel">
          <VolumeSlider
            volume={item.volume}
            onCommit={(volume) => {
              set({ volume });
            }}
          />
          <MuteSwitch
            muted={item.muted}
            onChange={(muted) => {
              set({ muted });
            }}
          />
          <NoiseReductionSwitch
            key={item.id}
            project={project}
            target={{
              kind: 'sound',
              id: item.id,
              sourceId: item.sourceId,
              label: item.label,
              on: item.denoise === true,
            }}
            onCommand={onCommand}
          />
          <DuckingControls
            key={`duck-${item.id}`}
            project={project}
            item={item}
            onCommand={onCommand}
          />
          <FadeSlider
            label={t('videoStudio.audio.fadeIn')}
            value={item.fadeIn ?? 0}
            max={fadeRoom}
            onCommit={(fadeIn) => {
              set({ fadeIn });
            }}
          />
          <FadeSlider
            label={t('videoStudio.audio.fadeOut')}
            value={item.fadeOut ?? 0}
            max={fadeRoom}
            onCommit={(fadeOut) => {
              set({ fadeOut });
            }}
          />
        </div>
      </AudioTabContent>
      <TabsContent value="speed">
        <div className="video-studio-tab-panel">
          <SpeedSlider
            speed={item.speed ?? 1}
            onCommit={(speed) => {
              set({ speed });
            }}
          />
        </div>
      </TabsContent>
      <TabsContent value="time">
        <div className="video-studio-tab-panel">
          <TimeField
            key={`at-${item.id}-${formatTimecode(starts)}`}
            label={t('videoStudio.audio.startsAt')}
            value={starts}
            onCommit={(start) => {
              onCommand((current) => moveAudio(current, { itemId: item.id, start }));
            }}
          />
          <TimeField
            key={`in-${item.id}-${formatTimecode(item.sourceStart)}`}
            label={t('videoStudio.audio.in')}
            value={item.sourceStart}
            onCommit={(at) => {
              trim(at - item.sourceStart, item.duration);
            }}
          />
          <TimeField
            key={`out-${item.id}-${formatTimecode(end)}`}
            label={t('videoStudio.audio.out')}
            value={end}
            onCommit={(at) => {
              trim(0, at - item.sourceStart);
            }}
          />
        </div>
      </TabsContent>
    </Tabs>
  );
}
