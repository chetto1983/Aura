import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import type { DisplayChildReport } from '../displays/types';
import { statusDotClass } from '../displays/swarmRow';
import type { WorkerStatus } from './workerStream';

export interface WorkerPickerProps {
  readonly workers: readonly DisplayChildReport[];
  readonly statuses: ReadonlyMap<string, WorkerStatus>;
  readonly watchedChildId: string;
  readonly onSelect: (childId: string) => void;
}

function selectedIndex(workers: readonly DisplayChildReport[], watchedChildId: string): number {
  const index = workers.findIndex((worker) => worker.child_id === watchedChildId);
  return index < 0 ? 0 : index;
}

export function WorkerPicker({ workers, statuses, watchedChildId, onSelect }: WorkerPickerProps) {
  const { t } = useTranslation();
  const [focusedChildId, setFocusedChildId] = useState(watchedChildId);
  const focusIndex = selectedIndex(workers, focusedChildId);
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const selectedRef = useRef<HTMLButtonElement | null>(null);
  useEffect(() => {
    selectedRef.current?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }, [watchedChildId]);

  if (workers.length === 0) return null;

  const focusAt = (index: number) => {
    const normalized = (index + workers.length) % workers.length;
    setFocusedChildId(workers[normalized]?.child_id ?? watchedChildId);
    refs.current[normalized]?.focus();
  };

  const onKeyDown = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    switch (event.key) {
      case 'ArrowLeft':
      case 'ArrowUp':
        event.preventDefault();
        focusAt(index - 1);
        break;
      case 'ArrowRight':
      case 'ArrowDown':
        event.preventDefault();
        focusAt(index + 1);
        break;
      case 'Home':
        event.preventDefault();
        focusAt(0);
        break;
      case 'End':
        event.preventDefault();
        focusAt(workers.length - 1);
        break;
      case 'Enter':
      case ' ':
        event.preventDefault();
        onSelect(workers[index]?.child_id ?? watchedChildId);
        break;
      default:
        break;
    }
  };

  return (
    <div
      role="tablist"
      aria-label={t('swarm.picker.label')}
      className="mb-3 flex shrink-0 gap-1 overflow-x-auto border-b border-border pb-2"
    >
      {workers.map((worker, index) => {
        const active = worker.child_id === watchedChildId;
        const status = statuses.get(worker.child_id)?.status ?? worker.status;
        const goal = worker.goal ?? worker.child_id;
        return (
          <button
            key={worker.child_id}
            ref={(element) => {
              refs.current[index] = element;
              if (active) selectedRef.current = element;
            }}
            type="button"
            role="tab"
            aria-label={`${goal} · ${worker.child_id}`}
            aria-selected={active}
            tabIndex={index === focusIndex ? 0 : -1}
            title={goal}
            data-active={active}
            onFocus={() => {
              setFocusedChildId(worker.child_id);
            }}
            onClick={() => {
              onSelect(worker.child_id);
            }}
            onKeyDown={(event) => {
              onKeyDown(event, index);
            }}
            className="relative flex min-h-[44px] min-w-[44px] max-w-56 shrink-0 items-center gap-2 border-b-2 border-transparent px-3 py-2 text-left text-[0.75rem] text-text-muted outline-none hover:text-text focus-visible:ring-2 focus-visible:ring-accent data-[active=true]:border-accent data-[active=true]:text-text"
          >
            <span
              aria-hidden="true"
              className={`size-2 shrink-0 rounded-sm ${statusDotClass(status)}`}
            />
            <span className="flex min-w-0 flex-col gap-1">
              <span className="max-w-48 truncate">{goal}</span>
              <span
                aria-hidden="true"
                className="max-w-48 truncate font-mono text-xs text-text-faint"
              >
                {worker.child_id}
              </span>
            </span>
          </button>
        );
      })}
    </div>
  );
}
