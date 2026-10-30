import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { StepStrip } from '../../components/StepStrip';
import { CloudflareStep } from './CloudflareStep';
import { ConnectionStep } from './ConnectionStep';
import { DomainStep } from './DomainStep';
import { ManageActions } from './ManageActions';
import { PhoneStep } from './PhoneStep';
import type {
  CloudflareAccount,
  RemoteAccessConfiguration,
  RemoteAccessStatusDTO,
} from './remoteAccessApi';
import {
  NO_DRAFT,
  STEP_KEYS,
  currentStep,
  hasExit,
  leftDraft,
  stepStates,
  type DomainDraft,
  type StepKey,
} from './remoteAccessSteps';

export interface RemoteAccessActions {
  readonly verify: (token: string) => Promise<readonly CloudflareAccount[]>;
  readonly configure: (configuration: RemoteAccessConfiguration) => Promise<void>;
  readonly dismissAbandoned: () => Promise<void>;
  readonly reconcile: () => Promise<void>;
  readonly refreshToken: () => Promise<void>;
  readonly disable: () => Promise<void>;
  readonly remove: (hostname: string) => Promise<void>;
  readonly acceptExternal: () => Promise<void>;
}

interface RemoteAccessWizardProps {
  readonly status: RemoteAccessStatusDTO;
  readonly actions: RemoteAccessActions;
}

export function RemoteAccessWizard({ status, actions }: RemoteAccessWizardProps) {
  const { t } = useTranslation();
  // null follows the status, so the wizard moves on by itself; a step the operator opened stays.
  const [open, setOpen] = useState<StepKey | null>(null);
  // Here, not on the card: Indietro unmounts the card, and a first address has nowhere else to live.
  const [draft, setDraft] = useState<DomainDraft>(NO_DRAFT);
  const column = useRef<HTMLDivElement>(null);
  const current = currentStep(status);
  // The list exists only until the operator has seen it, and it can appear behind a green wizard.
  const following = status.abandoned.length > 0 ? 'cloudflare' : current;
  const shown = open ?? following;
  const exit = shown === current && shown !== 'phone' && hasExit(status);
  // The controls the operator can press from here. A delete can leave the step as it was, and its
  // status lands after the press has returned, so this follows what is on screen, not the press.
  const controls = `${shown} ${String(exit)} ${String(status.enabled)}`;
  const seen = useRef(controls);
  const states = stepStates(status);

  useEffect(() => {
    if (seen.current === controls) return;
    seen.current = controls;
    // Body means the control that had focus has gone. Anywhere else, a poll that moved the step
    // would be taking focus from the operator.
    if (document.activeElement === document.body) column.current?.querySelector('h3')?.focus();
  }, [controls]);

  function go(step: StepKey) {
    // Past a saved address a draft is a change nobody asked for: Avanti would re-address the install.
    setDraft(status.zone_name === undefined ? leftDraft : NO_DRAFT);
    setOpen(step === following ? null : step);
  }

  function pin() {
    setOpen(shown);
  }

  function editDraft(next: DomainDraft) {
    setDraft(next);
    pin();
  }

  async function save(configuration: RemoteAccessConfiguration) {
    // Pinned first: a failure refreshes the status, which must not take the card and what was
    // typed in it off the screen.
    pin();
    await actions.configure(configuration);
    setOpen(null);
  }

  async function saveDomain(configuration: RemoteAccessConfiguration) {
    await save(configuration);
    setDraft(NO_DRAFT);
  }

  async function remove(hostname: string) {
    await actions.remove(hostname);
    setDraft(NO_DRAFT);
  }

  return (
    <section aria-labelledby="remote-access-heading" className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-col gap-2">
        <h2 id="remote-access-heading" className="text-[20px] font-semibold text-text">
          {t('remoteAccess.heading')}
        </h2>
        <p className="max-w-3xl text-[15.5px] leading-relaxed text-text-muted">
          {t('remoteAccess.body')}
        </p>
      </div>
      <div ref={column} className="flex max-w-3xl min-w-0 flex-col gap-4">
        <div className="rounded-md border border-border bg-surface px-4 py-3">
          <StepStrip
            steps={STEP_KEYS.map((key) => ({
              key,
              label: t(`remoteAccess.steps.${key}`),
              state: states[key],
            }))}
            current={shown}
            onSelect={go}
            label={t('remoteAccess.steps.label')}
          />
        </div>
        {shown === 'cloudflare' ? (
          <CloudflareStep
            status={status}
            onVerify={actions.verify}
            onSave={save}
            onDismiss={actions.dismissAbandoned}
            onEdit={pin}
            onNext={() => {
              go('domain');
            }}
            onOpenStep={go}
          />
        ) : null}
        {shown === 'domain' ? (
          <DomainStep
            status={status}
            draft={draft}
            onDraft={editDraft}
            onSave={saveDomain}
            onBack={() => {
              go('cloudflare');
            }}
            onNext={() => {
              go('connection');
            }}
            onOpenStep={go}
          />
        ) : null}
        {shown === 'connection' ? (
          <ConnectionStep
            status={status}
            onRetry={actions.reconcile}
            onRefreshToken={actions.refreshToken}
            onBack={() => {
              go('domain');
            }}
            onNext={() => {
              go('phone');
            }}
            onOpenStep={go}
          />
        ) : null}
        {shown === 'phone' ? (
          <PhoneStep
            status={status}
            onAccept={actions.acceptExternal}
            onRetry={actions.reconcile}
            onDisable={actions.disable}
            onDelete={remove}
            onBack={() => {
              go('connection');
            }}
            onOpenStep={go}
          />
        ) : null}
        {exit ? (
          <ManageActions status={status} onDisable={actions.disable} onDelete={remove} />
        ) : null}
      </div>
    </section>
  );
}
