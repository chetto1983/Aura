import type { TFunction } from 'i18next';
import type { Approval, ApprovalPresentation } from './useApprovals';

const GATEWAY_KEY = 'approval.gateway.mutation';
const SHELL_KEY = 'approval.shell.command';
const SCHEDULED_KEY = 'approval.scheduled.task';

function hasParams(presentation: ApprovalPresentation, ...names: readonly string[]): boolean {
  return names.every((name) => typeof presentation.params[name] === 'string');
}

/** Render recognized semantic metadata in the active locale; legacy/invalid rows stay verbatim. */
export function approvalQuestion(approval: Approval, t: TFunction): string {
  const presentation = approval.presentation;
  if (!presentation) return approval.question;

  switch (presentation.key) {
    case GATEWAY_KEY: {
      if (!hasParams(presentation, 'tool', 'risk', 'args')) return approval.question;
      const args =
        presentation.params.args === '(none)'
          ? t('approval.question.none')
          : presentation.params.args;
      return t('approval.question.gateway', { ...presentation.params, args });
    }
    case SHELL_KEY:
      return hasParams(presentation, 'cwd', 'command', 'digest')
        ? t('approval.question.shell', presentation.params)
        : approval.question;
    case SCHEDULED_KEY:
      if (!hasParams(presentation, 'task', 'kind')) return approval.question;
      return hasParams(presentation, 'schedule', 'risk')
        ? t('approval.question.scheduledDetailed', presentation.params)
        : t('approval.question.scheduled', presentation.params);
    default:
      return approval.question;
  }
}
