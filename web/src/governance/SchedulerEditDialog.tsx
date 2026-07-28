import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useEditTask } from './useSchedulerMutations';
import type { SchedulerEditRequest, SchedulerTask } from './governanceApi';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select';
import { Textarea } from '@/components/ui/textarea';

// SchedulerEditDialog reschedules + re-payloads a user task (GOV-03 edit). It prefills the
// schedule grammar from the row (the payload is server-side, so its field is blank with a
// "leave blank to keep current" hint), validates client-side only for empties, and lets the
// backend cron engine do the authoritative grammar validation (a 400 surfaces as the inline
// error). On success the mutation invalidates the board and the dialog closes.

export interface SchedulerEditDialogProps {
  readonly task: SchedulerTask;
  readonly open: boolean;
  readonly onClose: () => void;
}

// payloadForKind wraps the free-text payload in the shape the task kind expects: a reminder
// carries {text}, an agent_job carries {goal}. Backups take no payload.
function payloadForKind(kind: string, text: string): unknown {
  const trimmed = text.trim();
  if (trimmed === '') return undefined;
  if (kind === 'agent_job') return { goal: trimmed };
  if (kind === 'reminder') return { text: trimmed };
  return undefined;
}

export function SchedulerEditDialog({ task, open, onClose }: SchedulerEditDialogProps) {
  const { t } = useTranslation();
  const fieldId = useId();
  const edit = useEditTask();

  const [scheduleKind, setScheduleKind] = useState(task.ScheduleKind || 'cron');
  const [cron, setCron] = useState(task.CronExpr);
  const [everyMinutes, setEveryMinutes] = useState(String(task.EveryMinutes || 5));
  const [at, setAt] = useState(task.RunAt.startsWith('0001') ? '' : task.RunAt);
  const [tz, setTz] = useState(task.TZ);
  const [payload, setPayload] = useState('');
  const [notify, setNotify] = useState(task.NotifyRoute);

  function submit() {
    const body = payloadForKind(task.Kind, payload);
    const req: SchedulerEditRequest = {
      schedule_kind: scheduleKind,
      tz,
      notify,
      ...(scheduleKind === 'cron' ? { cron: cron.trim() } : {}),
      ...(scheduleKind === 'every' ? { every_minutes: Number(everyMinutes) } : {}),
      ...(scheduleKind === 'at' ? { at: at.trim() } : {}),
      ...(body !== undefined ? { payload: body } : {}),
    };
    edit.mutate(
      { id: task.ID, req },
      {
        onSuccess: () => {
          edit.reset();
          onClose();
        },
      },
    );
  }

  const showPayload = task.Kind === 'reminder' || task.Kind === 'agent_job';

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          edit.reset();
          onClose();
        }
      }}
    >
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="font-mono uppercase tracking-wide">
            {t('governance.scheduler.edit.title')}
          </DialogTitle>
          <DialogDescription className="font-mono text-[12px]">{task.Kind}</DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={`${fieldId}-kind`}>{t('governance.scheduler.edit.scheduleKind')}</Label>
            <NativeSelect
              id={`${fieldId}-kind`}
              value={scheduleKind}
              onChange={(e) => {
                setScheduleKind(e.currentTarget.value);
              }}
            >
              <NativeSelectOption value="at">
                {t('governance.scheduler.edit.kindAt')}
              </NativeSelectOption>
              <NativeSelectOption value="every">
                {t('governance.scheduler.edit.kindEvery')}
              </NativeSelectOption>
              <NativeSelectOption value="cron">
                {t('governance.scheduler.edit.kindCron')}
              </NativeSelectOption>
            </NativeSelect>
          </div>

          {scheduleKind === 'cron' && (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor={`${fieldId}-cron`}>{t('governance.scheduler.edit.cron')}</Label>
              <Input
                id={`${fieldId}-cron`}
                className="font-mono"
                value={cron}
                placeholder={t('governance.scheduler.edit.cronHint')}
                onChange={(e) => {
                  setCron(e.currentTarget.value);
                }}
              />
            </div>
          )}
          {scheduleKind === 'every' && (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor={`${fieldId}-every`}>
                {t('governance.scheduler.edit.everyMinutes')}
              </Label>
              <Input
                id={`${fieldId}-every`}
                className="font-mono"
                type="number"
                min={5}
                value={everyMinutes}
                onChange={(e) => {
                  setEveryMinutes(e.currentTarget.value);
                }}
              />
            </div>
          )}
          {scheduleKind === 'at' && (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor={`${fieldId}-at`}>{t('governance.scheduler.edit.at')}</Label>
              <Input
                id={`${fieldId}-at`}
                className="font-mono"
                value={at}
                placeholder="2030-01-01T09:30:00Z"
                onChange={(e) => {
                  setAt(e.currentTarget.value);
                }}
              />
            </div>
          )}

          <div className="flex flex-col gap-1.5">
            <Label htmlFor={`${fieldId}-tz`}>{t('governance.scheduler.edit.tz')}</Label>
            <Input
              id={`${fieldId}-tz`}
              className="font-mono"
              value={tz}
              placeholder="Europe/Rome"
              onChange={(e) => {
                setTz(e.currentTarget.value);
              }}
            />
          </div>

          {showPayload && (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor={`${fieldId}-payload`}>{t('governance.scheduler.edit.payload')}</Label>
              <Textarea
                id={`${fieldId}-payload`}
                rows={2}
                value={payload}
                placeholder={t('governance.scheduler.edit.payloadHint')}
                onChange={(e) => {
                  setPayload(e.currentTarget.value);
                }}
              />
            </div>
          )}

          <div className="flex flex-col gap-1.5">
            <Label htmlFor={`${fieldId}-notify`}>{t('governance.scheduler.edit.notify')}</Label>
            <NativeSelect
              id={`${fieldId}-notify`}
              value={notify}
              onChange={(e) => {
                setNotify(e.currentTarget.value);
              }}
            >
              <NativeSelectOption value="">
                {t('governance.scheduler.edit.notifyDefault')}
              </NativeSelectOption>
              <NativeSelectOption value="telegram">telegram</NativeSelectOption>
              <NativeSelectOption value="whatsapp">whatsapp</NativeSelectOption>
              <NativeSelectOption value="email">email</NativeSelectOption>
              <NativeSelectOption value="stdout">stdout</NativeSelectOption>
            </NativeSelect>
          </div>

          {edit.isError && (
            <Alert variant="destructive">
              <AlertDescription>{t('governance.scheduler.edit.error')}</AlertDescription>
            </Alert>
          )}
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              edit.reset();
              onClose();
            }}
          >
            {t('governance.scheduler.edit.cancel')}
          </Button>
          <Button type="button" onClick={submit} disabled={edit.isPending}>
            {edit.isPending
              ? t('governance.scheduler.edit.saving')
              : t('governance.scheduler.edit.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
