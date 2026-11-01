import { useRef, useState } from 'react';
import { X } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { DecisionsView } from './DecisionsView';
import { LabelsPanel } from './LabelsPanel';
import { RulesPanel } from './RulesPanel';
import { SettingsPanel } from './SettingsPanel';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

// MailWatchDialog is the watch's workspace, and it is a dialog rather than a section of the MCP
// detail pane for three reasons. The pane is a narrow column on a desktop and a 78svh bottom sheet
// on a phone, already holding the server's fields, and a day of mail needs height it cannot give.
// A dialog keeps the focus inside it, so J, K and the tier keys have one clear owner. And on a
// phone it can take the whole screen, as a sheet inside a sheet could not. Below `sm` it does, and
// rules, labels and settings share it so that the rule preview can read the day on screen.

export interface MailWatchDialogAccount {
  readonly id: string;
  readonly name: string;
}

export type MailWatchTab = 'day' | 'rules' | 'labels' | 'settings';

const GENERAL_TABS: readonly MailWatchTab[] = ['rules', 'labels', 'settings'];

interface MailWatchDialogProps {
  readonly open: boolean;
  readonly onOpenChange: (open: boolean) => void;
  /** The account of the day tab. Without one the dialog holds only what accounts share. */
  readonly account?: MailWatchDialogAccount;
}

export function MailWatchDialog({ open, onOpenChange, account }: MailWatchDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        // Radix closes on Escape in the capture phase. The governance board's mobile sheet listens
        // on the document too, and without this one Escape would close both.
        onKeyDown={(event) => {
          if (event.key === 'Escape') event.stopPropagation();
        }}
        className="h-[min(88svh,60rem)] w-[calc(100vw-2rem)] max-w-3xl gap-0 overflow-hidden p-0 max-sm:left-0 max-sm:top-0 max-sm:h-svh max-sm:w-screen max-sm:max-w-none max-sm:translate-x-0 max-sm:translate-y-0 max-sm:rounded-none max-sm:border-0"
      >
        <MailWatchWorkspace {...(account === undefined ? {} : { account })} />
      </DialogContent>
    </Dialog>
  );
}

/** Rendered inside the dialog's content, so its state goes when the dialog closes. */
function MailWatchWorkspace({ account }: { readonly account?: MailWatchDialogAccount }) {
  const { t } = useTranslation();
  const tabs: readonly MailWatchTab[] =
    account === undefined ? GENERAL_TABS : ['day', ...GENERAL_TABS];
  const [day, setDay] = useState<string | null>(null);
  const focusClaimed = useRef(false);

  function claimFocus(): boolean {
    const first = !focusClaimed.current;
    focusClaimed.current = true;
    return first;
  }

  return (
    <Tabs defaultValue={tabs[0]} className="flex min-h-0 flex-1 flex-col gap-0">
      <div className="flex flex-col gap-3 border-b border-border px-4 pb-3 pt-4">
        <div className="flex items-start justify-between gap-2">
          <div className="min-w-0">
            <DialogTitle className="break-words">
              {account?.name ?? t('governance.mailWatch.title')}
            </DialogTitle>
            <DialogDescription>
              {t(
                account === undefined
                  ? 'governance.mailWatch.dialog.descriptionGeneral'
                  : 'governance.mailWatch.dialog.description',
              )}
            </DialogDescription>
          </div>
          <DialogClose asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={t('governance.closeAria')}
              className="-mr-2 -mt-2 text-text-muted hover:text-text"
            >
              <X aria-hidden="true" />
            </Button>
          </DialogClose>
        </div>
        <TabsList className="w-full">
          {tabs.map((tab) => (
            <TabsTrigger key={tab} value={tab} className="px-2">
              {t(`governance.mailWatch.dialog.tabs.${tab}`)}
            </TabsTrigger>
          ))}
        </TabsList>
      </div>
      {account === undefined ? null : (
        <TabsContent value="day" className="min-h-0 overflow-y-auto overscroll-contain">
          <DecisionsView
            accountId={account.id}
            day={day}
            onDayChange={setDay}
            claimFocus={claimFocus}
          />
        </TabsContent>
      )}
      <TabsContent value="rules" className="min-h-0 overflow-y-auto overscroll-contain">
        <RulesPanel day={day} />
      </TabsContent>
      <TabsContent value="labels" className="min-h-0 overflow-y-auto overscroll-contain">
        <LabelsPanel />
      </TabsContent>
      <TabsContent value="settings" className="min-h-0 overflow-y-auto overscroll-contain">
        <SettingsPanel />
      </TabsContent>
    </Tabs>
  );
}
