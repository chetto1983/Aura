import { X } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAssetSource } from '../chat/artifacts/renderers/assetSourceContext';
import { MediaEditorLayer } from '../mediaEdit/MediaEditorLayer';
import { addOverlay, CommandRefusal, removeItem, splitAt } from './commands';
import { createHistory, type Edit, type History } from './history';
import { Inspector } from './Inspector';
import { clipAt, clipStart, type VideoProject } from './project';
import { projectFileName, saveProject } from './projectStore';
import { Stage } from './Stage';
import { Timeline } from './Timeline';
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

// VideoStudio.tsx — the workspace: one history, one selection, one playhead, and the three
// panels wired to them. It owns the two things none of the parts can own.
//
// The first is the SENTENCE. A command refuses by key and never by prose, so this is the only
// file that turns `CommandRefusal.reasonKey` into something an operator reads — which is why a
// sentence is held here as a key plus its values and translated at RENDER time: switching
// language re-words the alert already on screen instead of leaving the old one behind.
//
// The second is the WARNING. Removing a clip takes the overlays anchored to it, and so does a
// trim that leaves one no window — silently, in both cases, because the commands are pure and a
// pure function has nobody to ask. Every edit is therefore run once against the current project
// BEFORE it is committed: if it would leave fewer overlays than it found, the operator is told
// how many and asked. The edit is pure, so running it twice costs arithmetic and changes nothing.

/** A string the operator will read, kept unresolved so the language can still change under it. */
interface Sentence {
  readonly key: string;
  readonly values: Record<string, unknown>;
}

function says(key: string, values: Record<string, unknown> = {}): Sentence {
  return { key, values };
}

/** What a new title lasts, unless the clip it hangs on has less left than that. */
const TITLE_SECONDS = 3;

interface VideoStudioProps {
  readonly open: StudioOpen;
  readonly onClose: () => void;
  /** Where the saved project landed, so the surface that opened the editor can offer it back. */
  readonly onSaved?: ((assetId: string) => void) | undefined;
}

function overlayCount(project: VideoProject): number {
  return project.overlays.reduce((total, lane) => total + lane.items.length, 0);
}

/** The overlay an edit added, found by difference: `addOverlay` mints an id it cannot return —
 *  a command is `(project, args) => project` and stays that shape, because in cycle 3 it is a
 *  tool with those same arguments. Diffing is what the workspace pays for that. */
function addedOverlay(before: VideoProject, after: VideoProject): string | undefined {
  const had = new Set(before.overlays.flatMap((lane) => lane.items.map((item) => item.id)));
  return after.overlays.flatMap((lane) => lane.items).find((item) => !had.has(item.id))?.id;
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
  // The history is state rather than a ref although it is never replaced: `canUndo` and
  // `canRedo` are read while rendering the two buttons, and a ref read during render is a value
  // React has not promised is current.
  const [history, setHistory] = useState<History>();
  const fileInput = useRef<HTMLInputElement>(null);
  const [project, setProject] = useState<VideoProject>();
  const [selectedId, setSelectedId] = useState<string>();
  const [playhead, setPlayhead] = useState(0);
  const [problem, setProblem] = useState<Sentence>();
  const [status, setStatus] = useState<Sentence>();
  const [pending, setPending] = useState<{ readonly edit: Edit; readonly lost: number }>();

  useEffect(() => {
    let live = true;
    openedProject(open, assetSource).then(
      (loaded) => {
        if (!live) return;
        setHistory(createHistory(loaded.project));
        setProject(loaded.project);
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

  function commit(edit: Edit) {
    if (history === undefined) return;
    try {
      const before = history.current;
      const next = history.apply(edit);
      setProject(next);
      const added = addedOverlay(before, next);
      if (added !== undefined) setSelectedId(added);
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
    setProject(move(history));
  }

  async function addFile(file: File) {
    setProblem(undefined);
    setStatus(says('videoStudio.source.reading'));
    try {
      // Probed first, uploaded second: a clip this browser cannot decode never costs a transfer.
      const probed = await probeSource(file);
      setStatus(says('videoStudio.source.uploading', { name: file.name }));
      run(sourceEdit(probed, await uploadSource(file)));
      setStatus(undefined);
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
    const offset = playhead - (clipStart(current, clip.id) ?? 0);
    const duration = Math.min(TITLE_SECONDS, clip.duration - offset);
    const props = { text: t('videoStudio.newTitle') };
    run((current) =>
      addOverlay(current, { kind: 'text', anchor: { clipId: clip.id, offset }, duration, props }),
    );
  }

  async function save() {
    const current = history?.current;
    if (current === undefined) return;
    setProblem(undefined);
    setStatus(says('videoStudio.save.saving'));
    try {
      const assetId = await saveProject(current);
      setStatus(says('videoStudio.save.saved'));
      onSaved?.(assetId);
    } catch (error) {
      setStatus(undefined);
      setProblem(failure(error, 'videoStudio.save.failed'));
    }
  }

  const name =
    project === undefined || project.name === '' ? t('videoStudio.untitled') : project.name;
  // A title hangs on a clip, so there has to be one under the playhead to hang it on.
  const underPlayhead = project === undefined ? undefined : clipAt(project, playhead);

  return (
    <MediaEditorLayer label={t('videoStudio.title')} onEscape={onClose}>
      <header className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2">
        <h2 className="min-w-0 flex-1 truncate font-mono text-sm">{name}</h2>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          disabled={project === undefined}
          onClick={() => void save()}
        >
          {t('videoStudio.save.action')}
        </Button>
        {project === undefined ? null : (
          <ExportPanel
            project={project}
            fileName={projectFileName(project, 'mp4')}
            urls={assetSource}
          />
        )}
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label={t('videoStudio.close')}
          onClick={onClose}
        >
          <X aria-hidden="true" className="size-4" />
        </Button>
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
            className="flex flex-wrap items-center gap-2 px-4 py-2"
          >
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => fileInput.current?.click()}
            >
              {t('videoStudio.command.addSource')}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => {
                run((current) => splitAt(current, { time: playhead }));
              }}
            >
              {t('videoStudio.command.split')}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              disabled={underPlayhead === undefined}
              onClick={addTitle}
            >
              {t('videoStudio.command.addText')}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              disabled={selectedId === undefined}
              onClick={() => {
                if (selectedId !== undefined)
                  run((current) => removeItem(current, { itemId: selectedId }));
              }}
            >
              {t('videoStudio.command.remove')}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              disabled={history?.canUndo !== true}
              onClick={() => {
                step((stack) => stack.undo());
              }}
            >
              {t('videoStudio.command.undo')}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              disabled={history?.canRedo !== true}
              onClick={() => {
                step((stack) => stack.redo());
              }}
            >
              {t('videoStudio.command.redo')}
            </Button>
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

          <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-4 lg:flex-row">
            <div className="min-w-0 flex-1">
              <Stage
                project={project}
                time={playhead}
                selectedId={selectedId}
                onSelect={setSelectedId}
                onCommand={run}
              />
            </div>
            <div className="w-full lg:w-72 lg:flex-none">
              <Inspector project={project} selectedId={selectedId} onCommand={run} />
            </div>
          </div>

          <div className="px-4 py-3">
            {project.video.length === 0 ? (
              <p role="status" className="text-sm text-text-muted">
                {t('videoStudio.empty')}
              </p>
            ) : (
              <Timeline
                project={project}
                selectedId={selectedId}
                playhead={playhead}
                onSelect={setSelectedId}
                onCommand={run}
                onScrub={setPlayhead}
              />
            )}
          </div>
        </>
      )}

      <div className="px-4 pb-3">
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
    </MediaEditorLayer>
  );
}
