import { useItem, type Span } from 'dnd-timeline';
import { useTranslation } from 'react-i18next';
import { audioWindow } from './audioLane';
import { sourceOf, type AudioItem, type AudioTrack, type VideoProject } from './project';
import { Handle, ItemButton } from './Timeline_items';
import { TOUCH_FLOOR, type TrimSpan } from './timelineView';

// Timeline_audio.tsx — the sounds on an audio lane. A sound drags and trims like a clip, but a drop
// is a free position, not a place in a sequence: `moveAudio` re-hangs it on whatever clip is under
// its new start. As for a clip, the handles' pointer drag is dnd-timeline's and reaches the shell
// as `onResizeEnd`; a keystroke is the only edit this file emits.

/** Two edges this close are one boundary, and two handles on it would fight for the press. */
const TOUCHING = 1e-6;

interface AudioItemViewProps {
  readonly item: AudioItem;
  readonly number: number;
  readonly span: Span;
  readonly sourceEnd: number;
  readonly abutsStart: boolean;
  readonly abutsEnd: boolean;
  readonly frame: number;
  readonly selected: boolean;
  readonly onSelect: (id: string) => void;
  readonly onTrim: (itemId: string, args: TrimSpan) => void;
}

function AudioItemView({
  item,
  number,
  span,
  sourceEnd,
  abutsStart,
  abutsEnd,
  frame,
  selected,
  onSelect,
  onTrim,
}: AudioItemViewProps) {
  const { t } = useTranslation();
  const { setNodeRef, setActivatorNodeRef, attributes, listeners, itemStyle, itemContentStyle } =
    useItem({ id: item.id, span, resizeHandleWidth: TOUCH_FLOOR });
  const position = { index: number };
  const label = t('videoStudio.audio.item', position);
  const end = item.sourceStart + item.duration;
  return (
    <div
      ref={setNodeRef}
      style={itemStyle}
      onPointerDown={listeners.onPointerDown}
      onPointerMove={listeners.onPointerMove}
    >
      <div style={itemContentStyle}>
        <ItemButton
          label={label}
          length={span.end - span.start}
          selected={selected}
          kind="audio"
          preview={<span className="video-studio-audio-name">{item.label ?? label}</span>}
          idle="border-border bg-surface-3"
          attributes={attributes}
          activatorRef={setActivatorNodeRef}
          onSelect={() => {
            onSelect(item.id);
          }}
        />
      </div>
      <Handle
        side="start"
        abuts={abutsStart}
        selected={selected}
        label={t('videoStudio.audio.trimStart', position)}
        value={item.sourceStart}
        min={0}
        max={end - frame}
        frame={frame}
        onSet={(at) => {
          onTrim(item.id, { start: at - item.sourceStart, end: item.duration });
        }}
      />
      <Handle
        side="end"
        abuts={abutsEnd}
        selected={selected}
        label={t('videoStudio.audio.trimEnd', position)}
        value={end}
        min={item.sourceStart + frame}
        max={sourceEnd}
        frame={frame}
        onSet={(at) => {
          onTrim(item.id, { start: 0, end: at - item.sourceStart });
        }}
      />
    </div>
  );
}

interface AudioLaneItemsProps {
  readonly project: VideoProject;
  readonly track: AudioTrack;
  /** How many sounds the lanes above hold: the numbering runs across lanes, so a name is unique. */
  readonly before: number;
  readonly selectedId: string | undefined;
  readonly onSelect: (id: string) => void;
  readonly onTrim: (itemId: string, args: TrimSpan) => void;
}

/** One lane's sounds. A handle beside a neighbour stays inside its own sound, for the reason a
 *  clip's does (Timeline_items.tsx, `Handle`). */
export function AudioLaneItems({
  project,
  track,
  before,
  selectedId,
  onSelect,
  onTrim,
}: AudioLaneItemsProps) {
  const placed = track.items.map((item) => ({ item, span: audioWindow(project, item) }));
  const touches = (at: number, edge: 'start' | 'end', self: string) =>
    placed.some(({ item, span }) => item.id !== self && Math.abs(span[edge] - at) < TOUCHING);
  return (
    <>
      {placed.map(({ item, span }, index) => (
        <AudioItemView
          key={item.id}
          item={item}
          number={before + index + 1}
          span={span}
          sourceEnd={Math.max(
            sourceOf(project, item.sourceId)?.duration ?? 0,
            item.sourceStart + item.duration,
          )}
          abutsStart={touches(span.start, 'end', item.id)}
          abutsEnd={touches(span.end, 'start', item.id)}
          frame={1 / project.fps}
          selected={item.id === selectedId}
          onSelect={onSelect}
          onTrim={onTrim}
        />
      ))}
    </>
  );
}
