import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { getJSON } from '@/api/json';
import { Button } from '@/components/ui/button';

interface ContentPage {
  readonly passages: readonly { readonly ordinal: number; readonly text: string }[];
  readonly offset: number;
  readonly has_more: boolean;
}
export function DocumentContent({
  id,
  identity,
}: {
  readonly id: string;
  readonly identity: string;
}) {
  const { t } = useTranslation();
  const [offset, setOffset] = useState(0);
  const content = useQuery({
    queryKey: ['documents-content', identity, id, offset],
    queryFn: ({ signal }) =>
      getJSON<ContentPage>(
        `/api/documents/${encodeURIComponent(id)}/content?offset=${String(offset)}&limit=25`,
        signal,
      ),
  });
  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-text-muted">{t('documents.detail.contentHint')}</p>
      <Button
        variant="outline"
        onClick={() => {
          void content.refetch();
        }}
      >
        {t('documents.detail.refreshContent')}
      </Button>
      {content.isError ? (
        <p role="alert">{t('documents.detail.contentFailed')}</p>
      ) : content.isPending ? (
        <p role="status">{t('documents.detail.loading')}</p>
      ) : (
        <>
          {content.data.passages.length ? (
            content.data.passages.map((passage) => (
              <p
                key={passage.ordinal}
                className="whitespace-pre-wrap break-words text-sm leading-relaxed"
              >
                {passage.text}
              </p>
            ))
          ) : (
            <p>{t('documents.detail.noContent')}</p>
          )}
          <div className="flex gap-2">
            <Button
              variant="outline"
              disabled={offset === 0}
              onClick={() => {
                setOffset(Math.max(0, offset - 25));
              }}
            >
              {t('documents.detail.previousContent')}
            </Button>
            <Button
              variant="outline"
              disabled={!content.data.has_more}
              onClick={() => {
                setOffset(offset + 25);
              }}
            >
              {t('documents.detail.nextContent')}
            </Button>
          </div>
        </>
      )}
    </div>
  );
}
