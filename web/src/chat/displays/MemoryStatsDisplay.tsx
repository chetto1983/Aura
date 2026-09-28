import { useTranslation } from 'react-i18next';
import type { DisplayPayload } from './types';
import { ToolStats } from '@/components/tool-ui-stats';

export function MemoryStatsDisplay({ payload }: { readonly payload: DisplayPayload }) {
  const { t, i18n } = useTranslation();
  const items = payload.stats?.items ?? [];
  return (
    <ToolStats
      id={payload.tool_call_id}
      title={t('display.memory.graph')}
      locale={i18n.language}
      stats={items.map((item) => ({
        key: item.label,
        label: t(`display.memory.metric.${item.label}`),
        value: item.value,
      }))}
    />
  );
}
