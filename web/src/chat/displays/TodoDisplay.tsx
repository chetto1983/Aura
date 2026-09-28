import { useTranslation } from 'react-i18next';
import type { DisplayPayload } from './types';
import { TodoList, type TodoItem } from '@/components/todo-list';

export interface TodoDisplayProps {
  readonly payload: DisplayPayload;
}

export function TodoDisplay({ payload }: TodoDisplayProps) {
  const { t } = useTranslation();
  const items: TodoItem[] = (payload.todo?.items ?? []).map((item, index) => ({
    id: `${payload.tool_call_id}:${String(index)}`,
    text: item.content,
    status:
      item.status === 'in_progress' ? 'active' : item.status === 'completed' ? 'done' : 'pending',
    ...(item.status === 'in_progress' && item.active_form ? { description: item.active_form } : {}),
  }));

  return (
    <TodoList
      title={t('display.todo.title')}
      items={items}
      statusLabels={{
        pending: t('display.todo.pending'),
        active: t('display.todo.active'),
        done: t('display.todo.done'),
      }}
      className="max-w-none rounded-[var(--radius-md)] border border-border bg-surface px-3 py-3"
    />
  );
}
