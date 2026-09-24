import { useTranslation } from 'react-i18next';
import { useUpdateCenter } from './updateCenterContext';
import { UpdateDecisionDialog } from './UpdateDecisionDialog';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';

// Everyone sees the restart coming (banner) and the restart itself (overlay); only an admin is
// asked to decide. `suppressed` keeps the whole layer off the onboarding screens.
export function SystemUpdateLayer({ suppressed }: { readonly suppressed: boolean }) {
  const center = useUpdateCenter();
  if (suppressed || center?.status === undefined) return null;
  if (center.restarting) return <UpdateOverlay />;
  if (center.status.state === 'requested') return <UpdateBanner />;
  if (!center.dialogOpen) return null;
  return (
    <UpdateDecisionDialog
      status={center.status}
      now={center.now}
      outcome={center.deferOutcome}
      onDismiss={center.dismissDialog}
      onDecided={center.settleDecision}
      onOutcomeClosed={center.closeOutcome}
    />
  );
}

function UpdateBanner() {
  const { t } = useTranslation();
  return (
    <div className="pointer-events-none fixed inset-x-0 top-[4.75rem] z-[80] flex justify-center px-4">
      <p role="status" className="update-banner">
        <span className="update-banner__beacon" aria-hidden="true" />
        {t('update.banner')}
      </p>
    </div>
  );
}

// Held open with no onOpenChange, so neither Escape nor a click can dismiss it: the page has
// nothing to talk to until the new build answers and useSystemUpdate reloads it. The shared
// Dialog is a centred card; its placement is undone here with utilities, which tailwind-merge
// swaps for the card's own, because the CSS minifier drops a `translate: none` override.
function UpdateOverlay() {
  const { t } = useTranslation();
  return (
    <Dialog open>
      <DialogContent
        role="alertdialog"
        aria-modal="true"
        showCloseButton={false}
        className="update-overlay left-0 top-0 z-[120] h-dvh w-screen max-w-none translate-x-0 translate-y-0 gap-9 rounded-none border-0 p-6"
      >
        <div className="update-orbit" aria-hidden="true">
          <span className="update-orbit__halo" />
          <span className="update-orbit__track" />
          <span className="update-orbit__arc" />
          <span className="update-orbit__core">
            <img src="/logo.png" alt="" width={56} height={56} />
          </span>
        </div>
        <div className="flex max-w-md flex-col items-center gap-2 text-center">
          <DialogTitle className="text-[clamp(1.6rem,4vw,2.25rem)] font-medium leading-tight">
            {t('update.overlay.title')}
          </DialogTitle>
          <DialogDescription className="text-[15px] text-text-muted">
            {t('update.overlay.body')}
          </DialogDescription>
        </div>
        <span className="update-progress" aria-hidden="true" />
      </DialogContent>
    </Dialog>
  );
}
