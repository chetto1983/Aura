import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';

const STARTERS = ['research', 'file', 'artifact', 'automation'] as const;

export function EmptyThreadStarters({
  onRequestDraftPrompt,
}: {
  readonly onRequestDraftPrompt: (text: string) => void;
}) {
  const { t } = useTranslation();

  return (
    <div
      aria-label={t('chat.empty.suggestionsLabel')}
      className="grid w-full max-w-2xl grid-cols-1 gap-2 sm:grid-cols-2"
    >
      {STARTERS.map((starter) => (
        <Button
          key={starter}
          type="button"
          variant="outline"
          className="h-auto min-h-11 min-w-0 justify-start whitespace-normal px-3 py-2 text-start"
          onClick={() => {
            onRequestDraftPrompt(t(`chat.empty.thread.starters.${starter}.body`));
          }}
        >
          {t(`chat.empty.thread.starters.${starter}.label`)}
        </Button>
      ))}
    </div>
  );
}
