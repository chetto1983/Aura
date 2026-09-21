import {
  ChevronLeft,
  Clapperboard,
  Maximize2,
  Plus,
  Redo2,
  Save,
  Scissors,
  SlidersHorizontal,
  Trash2,
  Type,
  Undo2,
} from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAssetSource } from '../chat/artifacts/renderers/assetSourceContext';
import { MediaEditorLayer } from '../mediaEdit/MediaEditorLayer';
import { addOverlay, CommandRefusal, freeOverlayTrack, removeItem, splitAt } from './commands';
import { createHistory, type Edit, type History } from './history';
import { Inspector } from './Inspector';
import type { ClipTab } from './Inspector_clip';
import { JunctionTransitionInspector } from './Inspector_transition';
import {
  clipAt,
  clipStart,
  clipTimelineDuration,
  projectDuration,
  type ClipJunction,
  type VideoProject,
} from './project';
import { projectFileName, rememberSavedProject, saveProject } from './projectStore';
import { Stage } from './Stage';
import { Timeline } from './Timeline';
import { VideoStudioTransport } from './VideoStudioTransport';
import { MobileVideoTools } from './VideoStudio_mobile';
import { ExportPanel } from './VideoStudio_export';
import {
  openedProject,
  probeSource,
  REFUSAL_MISSING_ASSET,
  sourceEdit,
  SOURCE_ACCEPT,
  uploadSource,
  type StudioOpen,
} from './VideoStudio_sources';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/ui/confirm-dialog';

/** A string the operator will read, kept unresolved so the language can still change under it. */
interface Sentence {
  readonly key: string;
  readonly values: Record<string, unknown>;
}

function says(key: string, values: Record<string, unknown> = {}): Sentence {
  return { key, values };
}

const TITLE_SECONDS = 3;

interface VideoStudioProps {
  readonly open: StudioOpen;
  readonly onClose: () => void;
  readonly onSaved?: ((assetId: string) => void) | undefined;
}

function overlayCount(project: VideoProject): number {
  return project.overlays.reduce((total, lane) => total + lane.items.length, 0);
}

function addedOverlay(before: VideoProject, after: VideoProject): string | undefined {
  const had = new Set(before.overlays.flatMap((lane) => lane.items.map((item) => item.id)));
  return after.overlays.flatMap((lane) => lane.items).find((item) => !had.has(item.id))?.id;
}

function holds(project: VideoProject, id: string | undefined): boolean {
  if (id === undefined) return false;
  return (
    project.video.some((clip) => clip.id === id) ||
    project.overlays.some((lane) => lane.items.some((item) => item.id === id))
  );
}

function unplayableClips(project: VideoProject, missing: readonly string[]): readonly string[] {
  return project.video.filter((clip) => missing.includes(clip.sourceId)).map((clip) => clip.id);
}

/**
 * The sentence an error becomes. A refusal speaks for itself and is shown unchanged, whichever
 * of the seven it is; anything else — a caller out of step with the model, a dropped connection
 * — is worded by whoever was attempting it and carries the message it came with. Neither is
 * silenced, and neither is dressed as the other.
 */
function failure(error: unknown, fallbackKey: string): Sentence {
  if (error instanceof CommandRefusal) return says(error.reasonKey);
  return says(fallbackKey, { reason: error instanceof Error ? error.message : String(error) });
}

export default function VideoStudio({ open, onClose, onSaved }: VideoStudioProps) {
  const { t } = useTranslation();
  const assetSource = useAssetSource();
  const [history, setHistory] = useState<History>();
  const fileInput = useRef<HTMLInputElement>(null);
  const [project, setProject] = useState<VideoProject>();
  const [selectedId, setSelectedId] = useState<string>();
  const [selectedJunction, setSelectedJunction] = useState<ClipJunction>();
  const [playhead, setPlayhead] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [inspectorTab, setInspectorTab] = useState<ClipTab>('transform');
  const [mobileInspectorOpen, setMobileInspectorOpen] = useState(false);
  const [problem, setProblem] = useState<Sentence>();
  const [status, setStatus] = useState<Sentence>();
  const [pending, setPending] = useState<{ readonly edit: Edit; readonly lost: number }>();
  const propertiesRef = useRef<HTMLElement>(null);
  const canvasRef = useRef<HTMLElement>(null);
  const [missing, setMissing] = useState<readonly string[]>([]);

  useEffect(() => {
    let live = true;
    openedProject(open, assetSource).then(
      (loaded) => {
        if (!live) return;
        setHistory(createHistory(loaded.project));
        setProject(loaded.project);
        setSelectedId(loaded.project.video[0]?.id);
        setMissing(loaded.missing);
        if (loaded.missing.length > 0) setProblem(says(REFUSAL_MISSING_ASSET));
      },
      (error: unknown) => {
        if (!live) return;
        setProblem(failure(error, 'videoStudio.open.failed'));
      },
    );
    return () => {
      live = false;
    };
  }, [open, assetSource]);

  function reselect(next: VideoProject, added?: string) {
    setSelectedId((current) => added ?? (holds(next, current) ? current : undefined));
    setSelectedJunction((current) => {
      if (current === undefined) return undefined;
      const toIndex = next.video.findIndex((clip) => clip.id === current.toClipId);
      return next.video[toIndex - 1]?.id === current.fromClipId ? current : undefined;
    });
  }

  function commit(edit: Edit) {
    if (history === undefined) return;
    try {
      const before = history.current;
      const next = history.apply(edit);
      setProject(next);
      reselect(next, addedOverlay(before, next));
      // The frame can only change by a project taking its first source's, and it has to be said:
      // a silent re-frame is the same class of surprise as the silent crop it prevents.
      if (before.size.width !== next.size.width || before.size.height !== next.size.height) {
        setStatus(
          says('videoStudio.frameAdopted', {
            width: next.size.width,
            height: next.size.height,
          }),
        );
      }
    } catch (error) {
      setProblem(failure(error, 'videoStudio.problem'));
    }
  }

  /** Every edit enters here: it is measured against the project it would replace, and only an
   *  edit that costs no overlay reaches the history without a question. */
  function run(edit: Edit) {
    if (history === undefined) return;
    setProblem(undefined);
    try {
      const lost = overlayCount(history.current) - overlayCount(edit(history.current));
      if (lost > 0) {
        setPending({ edit, lost });
        return;
      }
    } catch (error) {
      setProblem(failure(error, 'videoStudio.problem'));
      return;
    }
    commit(edit);
  }

  function step(move: (stack: History) => VideoProject) {
    if (history === undefined) return;
    setProblem(undefined);
    const next = move(history);
    setProject(next);
    reselect(next);
  }

  async function addFile(file: File) {
    setProblem(undefined);
    setStatus(says('videoStudio.source.reading'));
    try {
      // Probed first, uploaded second: a clip this browser cannot decode never costs a transfer.
      const probed = await probeSource(file);
      setStatus(says('videoStudio.source.uploading', { name: file.name }));
      const assetId = await uploadSource(file);
      // Cleared BEFORE the commit, never after: the commit may replace this line with the frame
      // the project has just taken from this source, and clearing afterwards would eat it.
      setStatus(undefined);
      run(sourceEdit(probed, assetId));
    } catch (error) {
      setStatus(undefined);
      setProblem(failure(error, 'videoStudio.source.failed'));
    }
  }

  function addTitle() {
    const current = history?.current;
    if (current === undefined) return;
    const clip = clipAt(current, playhead);
    if (clip === undefined) return;
    const speed = Math.abs(clip.speed ?? 1);
    const timelineOffset = playhead - (clipStart(current, clip.id) ?? 0);
    const offset = timelineOffset * speed;
    const duration = Math.min(TITLE_SECONDS, clipTimelineDuration(clip) - timelineOffset);
    const props = { text: t('videoStudio.newTitle') };
    const anchor = { clipId: clip.id, offset };
    // The lane is chosen, not opened: a title joins the first lane that is free over its window,
    // and only a title with nowhere to go gets a lane of its own. `undefined` IS that case, and
    // it is the value `addOverlay` reads as "open one".
    const trackId = freeOverlayTrack(current, anchor, duration);
    run((current) => addOverlay(current, { trackId, kind: 'text', anchor, duration, props }));
  }

  async function save() {
    const current = history?.current;
    if (current === undefined) return;
    setProblem(undefined);
    setStatus(says('videoStudio.save.saving'));
    try {
      const assetId = await saveProject(current, name);
      rememberSavedProject(assetId);
      setStatus(says('videoStudio.save.saved'));
      onSaved?.(assetId);
    } catch (error) {
      setStatus(undefined);
      setProblem(failure(error, 'videoStudio.save.failed'));
    }
  }

  const name =
    project === undefined || project.name === '' ? t('videoStudio.untitled') : project.name;
  const duration = project === undefined ? 0 : projectDuration(project);
  // A title hangs on a clip, so there has to be one under the playhead to hang it on.
  const underPlayhead = project === undefined ? undefined : clipAt(project, playhead);
  const unplayable = project === undefined ? [] : unplayableClips(project, missing);

  /** Why the export cannot run, worded once and in one place: a clip whose bytes are gone, or a
   *  lane with nothing on it. `undefined` is the only value that lets the button run. */
  function exportRefusal(shown: VideoProject): string | undefined {
    if (unplayable.length > 0) return t(REFUSAL_MISSING_ASSET);
    return projectDuration(shown) <= 0 ? t('videoStudio.export.empty') : undefined;
  }

  useEffect(() => {
    if (!playing || duration <= 0) return undefined;
    let last = performance.now();
    const timer = window.setInterval(() => {
      const now = performance.now();
      const elapsed = (now - last) / 1000;
      last = now;
      setPlayhead((current) => {
        const next = Math.min(duration, current + elapsed);
        if (next >= duration) setPlaying(false);
        return next;
      });
    }, 50);
    return () => {
      window.clearInterval(timer);
    };
  }, [duration, playing]);

  function seek(time: number) {
    setPlaying(false);
    setPlayhead(Math.min(Math.max(time, 0), duration));
  }

  function showInspector(tab: ClipTab) {
    setSelectedJunction(undefined);
    setInspectorTab(tab);
    setMobileInspectorOpen((open) => (open && inspectorTab === tab ? false : true));
  }

  return (
    <MediaEditorLayer label={t('videoStudio.title')} onEscape={onClose}>
      <div className="video-studio-shell">
        <header className="video-studio-topbar">
          <span className="video-studio-mark" aria-hidden="true">
            <Clapperboard />
          </span>
          <h2 className="video-studio-project-name">{name}</h2>
          <div className="video-studio-top-actions">
            <button
              type="button"
              className="video-studio-icon-button"
              disabled={history?.canUndo !== true}
              aria-label={t('videoStudio.command.undo')}
              onClick={() => {
                step((stack) => stack.undo());
              }}
            >
              <Undo2 aria-hidden="true" />
            </button>
            <button
              type="button"
              className="video-studio-icon-button"
              disabled={history?.canRedo !== true}
              aria-label={t('videoStudio.command.redo')}
              onClick={() => {
                step((stack) => stack.redo());
              }}
            >
              <Redo2 aria-hidden="true" />
            </button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="video-studio-save"
              disabled={project === undefined}
              onClick={() => void save()}
            >
              <Save aria-hidden="true" />
              <span className="video-studio-save-label">{t('videoStudio.save.action')}</span>
            </Button>
            {project === undefined ? null : (
              <ExportPanel
                project={project}
                fileName={projectFileName(project, 'mp4', name)}
                urls={assetSource}
                refusal={exportRefusal(project)}
              />
            )}
            <button
              type="button"
              className="video-studio-close video-studio-icon-button"
              aria-label={t('videoStudio.close')}
              onClick={onClose}
            >
              <ChevronLeft aria-hidden="true" />
            </button>
          </div>
        </header>

        {project === undefined ? (
          // Nothing to show yet — or nothing to show at all, once the alert below says why. A
          // surface still claiming to be opening under a refusal is a surface telling two stories.
          problem !== undefined ? null : (
            <p role="status" className="flex-1 p-6 text-center text-sm text-text-muted">
              {t('videoStudio.open.loading')}
            </p>
          )
        ) : (
          <>
            <div
              role="toolbar"
              aria-label={t('videoStudio.commands')}
              className="video-studio-rail"
            >
              <button
                type="button"
                className="video-studio-rail-button"
                data-primary="true"
                onClick={() => fileInput.current?.click()}
              >
                <Plus aria-hidden="true" />
                <span>{t('videoStudio.command.addSource')}</span>
              </button>
              <button
                type="button"
                className="video-studio-rail-button"
                disabled={underPlayhead === undefined}
                onClick={addTitle}
              >
                <Type aria-hidden="true" />
                <span>{t('videoStudio.command.addText')}</span>
              </button>
              <button
                type="button"
                className="video-studio-rail-button"
                onClick={() => {
                  run((current) => splitAt(current, { time: playhead }));
                }}
              >
                <Scissors aria-hidden="true" />
                <span>{t('videoStudio.command.split')}</span>
              </button>
              <button
                type="button"
                className="video-studio-rail-button"
                onClick={() => propertiesRef.current?.focus()}
              >
                <SlidersHorizontal aria-hidden="true" />
                <span>{t('videoStudio.inspector.label')}</span>
              </button>
              <button
                type="button"
                className="video-studio-rail-button"
                disabled={selectedId === undefined}
                onClick={() => {
                  if (selectedId !== undefined)
                    run((current) => removeItem(current, { itemId: selectedId }));
                }}
              >
                <Trash2 aria-hidden="true" />
                <span>{t('videoStudio.command.remove')}</span>
              </button>
              <input
                ref={fileInput}
                type="file"
                accept={SOURCE_ACCEPT}
                className="sr-only"
                aria-label={t('videoStudio.source.pick')}
                onChange={(event) => {
                  const file = event.target.files?.[0];
                  // The same file picked twice in a row fires no change event unless the input is
                  // cleared, and a retry after a refusal is exactly that case.
                  event.target.value = '';
                  if (file !== undefined) void addFile(file);
                }}
              />
            </div>

            <div className="video-studio-workspace">
              <section ref={canvasRef} className="video-studio-canvas">
                <div className="video-studio-stage-frame">
                  {unplayable.length > 0 ? (
                    // No renderer at all: handing VideoFlow a source that 404s gets a disabled layer
                    // and a black picture, which reads as a preview and is not one.
                    <p
                      role="status"
                      className="flex aspect-video w-full items-center justify-center rounded-[var(--radius-md)] bg-surface-1 p-4 text-center text-sm text-text-muted"
                    >
                      {t('videoStudio.unplayable', { count: unplayable.length })}
                    </p>
                  ) : (
                    <Stage
                      project={project}
                      time={playhead}
                      selectedId={selectedId}
                      onSelect={(id) => {
                        setSelectedJunction(undefined);
                        setSelectedId(id);
                      }}
                      onCommand={run}
                    />
                  )}
                </div>
                <VideoStudioTransport
                  playing={playing}
                  time={playhead}
                  duration={duration}
                  onPlayToggle={() => {
                    if (playhead >= duration) setPlayhead(0);
                    setPlaying((current) => !current);
                  }}
                  onSeek={seek}
                />
                <button
                  type="button"
                  className="video-studio-fullscreen"
                  aria-label={t('videoStudio.stage.fullscreen')}
                  onClick={() => {
                    const canvas = canvasRef.current;
                    if (canvas === null) return;
                    const action =
                      document.fullscreenElement === canvas
                        ? document.exitFullscreen()
                        : canvas.requestFullscreen();
                    void action.catch(() => {
                      setProblem(says('videoStudio.stage.fullscreenFailed'));
                    });
                  }}
                >
                  <Maximize2 aria-hidden="true" />
                </button>
              </section>
              <aside
                ref={propertiesRef}
                tabIndex={-1}
                data-mobile-open={mobileInspectorOpen ? 'true' : 'false'}
                className="video-studio-properties"
              >
                {selectedJunction === undefined ? (
                  <Inspector
                    project={project}
                    selectedId={selectedId}
                    onCommand={run}
                    activeClipTab={inspectorTab}
                    onClipTabChange={setInspectorTab}
                  />
                ) : (
                  <JunctionTransitionInspector
                    project={project}
                    junction={selectedJunction}
                    onCommand={run}
                  />
                )}
              </aside>
            </div>

            <section
              className="video-studio-timeline-shell"
              data-mobile-obscured={mobileInspectorOpen ? 'true' : 'false'}
            >
              <div className="video-studio-timeline-heading">
                <span>{t('videoStudio.timeline.label')}</span>
                <span>{t('videoStudio.timeline.duration', { time: duration.toFixed(1) })}</span>
              </div>
              {project.video.length === 0 ? (
                <p role="status" className="p-4 text-sm text-text-muted">
                  {t('videoStudio.empty')}
                </p>
              ) : (
                <Timeline
                  project={project}
                  selectedId={selectedId}
                  selectedJunction={selectedJunction}
                  playhead={playhead}
                  onSelect={(id) => {
                    setSelectedJunction(undefined);
                    setSelectedId(id);
                  }}
                  onSelectJunction={(junction) => {
                    setSelectedJunction(junction);
                    setSelectedId(junction.toClipId);
                    setMobileInspectorOpen(true);
                  }}
                  onCommand={run}
                  onScrub={seek}
                />
              )}
            </section>
            <MobileVideoTools
              selectedId={selectedId}
              inspectorTab={inspectorTab}
              inspectorOpen={mobileInspectorOpen && selectedJunction === undefined}
              onBack={() => {
                if (mobileInspectorOpen) setMobileInspectorOpen(false);
                else {
                  setSelectedJunction(undefined);
                  setSelectedId(undefined);
                }
              }}
              onSplit={() => {
                setSelectedJunction(undefined);
                run((current) => splitAt(current, { time: playhead }));
              }}
              onRemove={() => {
                setSelectedJunction(undefined);
                if (selectedId !== undefined)
                  run((current) => removeItem(current, { itemId: selectedId }));
              }}
              onAddClip={() => fileInput.current?.click()}
              onAddTitle={addTitle}
              onOpenInspector={showInspector}
            />
          </>
        )}

        <div className="video-studio-status">
          {status === undefined ? null : (
            <p role="status" className="text-xs text-text-muted">
              {t(status.key, status.values)}
            </p>
          )}
          {problem === undefined ? null : (
            <p role="alert" className="text-xs text-danger">
              {t(problem.key, problem.values)}
            </p>
          )}
        </div>

        <ConfirmDialog
          open={pending !== undefined}
          onOpenChange={(next) => {
            if (!next) setPending(undefined);
          }}
          title={t('videoStudio.confirm.title', { count: pending?.lost ?? 0 })}
          description={t('videoStudio.confirm.body', { count: pending?.lost ?? 0 })}
          cancelLabel={t('videoStudio.confirm.cancel')}
          confirmLabel={t('videoStudio.confirm.proceed')}
          onConfirm={() => {
            if (pending !== undefined) commit(pending.edit);
            setPending(undefined);
          }}
        />
      </div>
    </MediaEditorLayer>
  );
}
