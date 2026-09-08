import { useEffect, useRef, useState } from 'react';
import {
  useBulkConversationAction,
  type BulkConversationAction,
  type Conversation,
} from './useConversations';

export function useConversationSelection(
  conversations: readonly Conversation[],
  onDeleted?: (id: string) => void,
) {
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [deleteTargets, setDeleteTargets] = useState<readonly string[] | null>(null);
  const mutation = useBulkConversationAction();
  const busy = useRef(false);
  const deletedCallback = useRef(onDeleted);
  useEffect(() => {
    deletedCallback.current = onDeleted;
  }, [onDeleted]);
  const ids = conversations
    .filter((conversation) => selected.has(conversation.ID))
    .map((conversation) => conversation.ID);

  function reset() {
    if (busy.current) return;
    setSelected(new Set());
    setSelecting(false);
    setDeleteTargets(null);
    mutation.reset();
  }

  function toggle(id: string) {
    if (busy.current) return;
    setSelected((previous) => {
      const next = new Set(previous);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  async function apply(action: BulkConversationAction, targets: readonly string[] = ids) {
    if (busy.current || targets.length === 0) return;
    busy.current = true;
    setDeleteTargets(null);
    try {
      const result = await mutation.mutateAsync({ action, ids: targets });
      setSelected(new Set(result.failed));
      setSelecting(result.failed.length > 0);
      if (action === 'delete') result.succeeded.forEach((id) => deletedCallback.current?.(id));
    } finally {
      busy.current = false;
    }
  }

  return {
    selecting,
    ids,
    selected,
    deleteTargets,
    pending: mutation.isPending,
    result: mutation.data,
    reset,
    toggle,
    apply,
    start: () => {
      mutation.reset();
      setSelecting(true);
    },
    toggleAll: () => {
      if (!busy.current)
        setSelected(
          new Set(
            ids.length === conversations.length
              ? []
              : conversations.map((conversation) => conversation.ID),
          ),
        );
    },
    requestDelete: () => {
      if (ids.length > 0 && !busy.current) setDeleteTargets(ids);
    },
    cancelDelete: () => {
      setDeleteTargets(null);
    },
  };
}
