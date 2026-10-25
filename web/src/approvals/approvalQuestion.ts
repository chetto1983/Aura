import type { TFunction } from 'i18next';
import type { Approval, ApprovalPresentation } from './useApprovals';

const GATEWAY_KEY = 'approval.gateway.mutation';
const SHELL_KEY = 'approval.shell.command';
const SCHEDULED_KEY = 'approval.scheduled.task';
const PLUGIN_INSTALL_KEY = 'approval.plugin.install';
const PLUGIN_REMOVE_KEY = 'approval.plugin.remove';

function hasParams(presentation: ApprovalPresentation, ...names: readonly string[]): boolean {
  return names.every((name) => typeof presentation.params[name] === 'string');
}

// A plugin card's params are its lines (internal/approvaltext/presentation_plugin.go). Only
// whole values are the card's own words to translate; a list keeps the words it carries
// beside the plugin's quoted strings.
function pluginParams(params: Readonly<Record<string, string>>, t: TFunction) {
  const list = (value = '') =>
    value.endsWith('): none')
      ? `${value.slice(0, -'none'.length)}${t('approval.question.plugin.none')}`
      : value;
  const mcp = params.mcp ?? '';
  const disabled = 'disabled — ';
  const data = params.data ?? '';
  return {
    ...params,
    skills: list(params.skills),
    connectors: list(params.connectors),
    skipped: list(params.skipped),
    notes: list(params.notes),
    mcp:
      mcp === 'enabled'
        ? t('approval.question.plugin.mcpEnabled')
        : mcp.startsWith(disabled)
          ? t('approval.question.plugin.mcpDisabled', { reason: mcp.slice(disabled.length) })
          : mcp,
    data:
      data === 'kept'
        ? t('approval.question.plugin.dataKept')
        : data === 'deleted in your box'
          ? t('approval.question.plugin.dataDeleted')
          : data,
  };
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
      const question = t('approval.question.gateway', { ...presentation.params, args });
      const target = presentation.params.target;
      return target ? `${question}\n${t('approval.question.target', { target })}` : question;
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
    case PLUGIN_INSTALL_KEY:
      return hasParams(
        presentation,
        'plugin',
        'source',
        'skills',
        'connectors',
        'skipped',
        'mcp',
        'notes',
        'digest',
      )
        ? t('approval.question.plugin.install', pluginParams(presentation.params, t))
        : approval.question;
    case PLUGIN_REMOVE_KEY:
      return hasParams(presentation, 'plugin', 'source', 'skills', 'connectors', 'data', 'digest')
        ? t('approval.question.plugin.remove', pluginParams(presentation.params, t))
        : approval.question;
    default:
      return approval.question;
  }
}
