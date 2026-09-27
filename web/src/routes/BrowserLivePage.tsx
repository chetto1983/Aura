import { useTranslation } from 'react-i18next';
import { useParams } from 'react-router';
import { BrowserLiveView } from '../browserLive/BrowserLiveView';
import { Button } from '@/components/ui/button';

/** The live view on a page of its own, for another channel's user or a second screen. */
export function BrowserLivePage() {
  const { t } = useTranslation();
  const { session = '' } = useParams();
  return (
    <main className="h-dvh">
      <BrowserLiveView
        key={session}
        session={session}
        className="h-full"
        actions={
          <Button
            size="sm"
            onClick={() => {
              window.close();
            }}
          >
            {t('browserLive.done')}
          </Button>
        }
      />
    </main>
  );
}
