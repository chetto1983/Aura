import { useEffect, useState } from 'react';
import { Loader2, RotateCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Spinner } from '../components/Spinner';
import { browserRestartDeps, requestRestart, watchRestart } from './restartAura';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';

type RestartStatus = 'idle' | 'restarting' | 'timedOut' | 'unsupported' | 'failed';

// RestartAuraControl carries out the restart the banner asks for, for an operator with no
// shell on the box. Once the daemon accepts, the page is blocked: every other control would
// be talking to a process that is about to exit.
export function RestartAuraControl() {
  const { t } = useTranslation();
  const [status, setStatus] = useState<RestartStatus>('idle');
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState('');

  useEffect(() => {
    if (status !== 'restarting') return undefined;
    return watchRestart(browserRestartDeps, () => {
      setStatus('timedOut');
    });
  }, [status]);

  async function restart() {
    if (pending) return;
    setPending(true);
    try {
      const outcome = await requestRestart();
      setStatus(outcome === 'accepted' ? 'restarting' : 'unsupported');
    } catch (err) {
      setFailure(err instanceof Error ? err.message : String(err));
      setStatus('failed');
    } finally {
      setPending(false);
    }
  }

  const timedOut = status === 'timedOut';

  return (
    <div className="mt-3 flex flex-col items-start gap-2">
      <Button
        type="button"
        variant="outline"
        disabled={pending}
        aria-busy={pending}
        onClick={() => void restart()}
      >
        {pending ? <Spinner /> : <RotateCw aria-hidden="true" />}
        {t('settings.restart.action')}
      </Button>
      {status === 'unsupported' ? (
        <p role="alert" className="text-danger">
          {t('settings.restart.unsupported')}
        </p>
      ) : null}
      {status === 'failed' ? (
        <p role="alert" className="text-danger">
          {t('settings.restart.failed', { message: failure })}
        </p>
      ) : null}

      <Dialog
        open={status === 'restarting' || timedOut}
        onOpenChange={(open) => {
          if (!open) setStatus('idle');
        }}
      >
        <DialogContent
          role="alertdialog"
          aria-modal="true"
          showCloseButton={false}
          onEscapeKeyDown={(event) => {
            if (!timedOut) event.preventDefault();
          }}
          onInteractOutside={(event) => {
            event.preventDefault();
          }}
          className="w-[calc(100vw-2rem)] max-w-sm items-center py-8 text-center"
        >
          <DialogHeader aria-live="assertive" className="items-center gap-3 text-center">
            {timedOut ? null : (
              <Loader2 aria-hidden="true" className="size-8 animate-spin text-text-muted" />
            )}
            <DialogTitle>
              {timedOut ? t('settings.restart.timedOutTitle') : t('settings.restart.restarting')}
            </DialogTitle>
            <DialogDescription>
              {timedOut ? t('settings.restart.timedOutBody') : t('settings.restart.restartingBody')}
            </DialogDescription>
          </DialogHeader>
          {timedOut ? (
            <DialogFooter className="w-full justify-center">
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  setStatus('idle');
                }}
              >
                {t('settings.restart.close')}
              </Button>
              <Button
                type="button"
                disabled={pending}
                aria-busy={pending}
                onClick={() => void restart()}
              >
                {pending ? <Spinner /> : null}
                {t('settings.restart.tryAgain')}
              </Button>
            </DialogFooter>
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  );
}
