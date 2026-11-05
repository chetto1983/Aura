import {
  useMutation,
  useMutationState,
  useQueries,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  addLabel,
  addRule,
  correctDecision,
  disableMailWatch,
  enableMailWatch,
  getDecisions,
  getMailWatch,
  listLabels,
  listRules,
  removeLabel,
  removeRule,
  saveMailWatchSettings,
  updateLabel,
  type MailWatchDay,
  type MailWatchDecision,
  type MailWatchLabelInput,
  type MailWatchRule,
  type MailWatchRuleInput,
  type MailWatchStatus,
  type MailWatchTier,
} from '../mailWatchApi';
import { decisionKey, ruleOnFile, sameRule, shortDay } from './mailWatchLogic';

// useMailWatch.ts is the watch's query state. The decisions of a day are read live from the PIM by
// id, so a correction writes its answer into the cache rather than asking for the whole day again,
// and a key press answers at once while the server reads the sender.

const STATUS_POLL_MS = 30_000;
const CORRECTION_KEY = ['mail-watch', 'correct'] as const;
const STATUS_KEY = ['mail-watch', 'status'] as const;
const RULES_KEY = ['mail-watch', 'rules'] as const;
const LABELS_KEY = ['mail-watch', 'labels'] as const;

function dayKey(accountId: string, day: string | null) {
  return ['mail-watch', 'decisions', accountId, day ?? 'today'] as const;
}

function decisionsQuery(accountId: string, day: string | null, discarded: boolean) {
  return {
    queryKey: [...dayKey(accountId, day), discarded ? 'discarded' : 'kept'],
    queryFn: () => getDecisions(accountId, day, discarded),
    retry: false,
  };
}

export function useMailWatchStatus() {
  return useQuery({
    queryKey: STATUS_KEY,
    queryFn: getMailWatch,
    refetchInterval: STATUS_POLL_MS,
    retry: false,
  });
}

export function useRules() {
  return useQuery({ queryKey: RULES_KEY, queryFn: listRules, retry: false });
}

export function useLabels() {
  return useQuery({ queryKey: LABELS_KEY, queryFn: listLabels, retry: false });
}

export function useDecisions(accountId: string, day: string | null, discarded: boolean) {
  return useQuery(decisionsQuery(accountId, day, discarded));
}

/** The same day across several accounts, on the cache useDecisions fills. `ready` is false until
 * all have answered, so a count never reads as final with an account still out. */
export function useDecisionsOfDay(accountIds: readonly string[], day: string | null) {
  const results = useQueries({ queries: accountIds.map((id) => decisionsQuery(id, day, false)) });
  return {
    ready: accountIds.length > 0 && results.every((result) => result.isSuccess),
    decisions: results.flatMap((result) => result.data?.decisions ?? []),
  };
}

/** The day as it reads inside a sentence: "today", "on 28 October". */
export function useDayPhrase(day: string | null): string {
  const { t, i18n } = useTranslation();
  if (day === null) return t('governance.mailWatch.day.whenToday');
  return t('governance.mailWatch.day.whenDay', { date: shortDay(day, i18n.language) });
}

export function useWatchToggle(accountId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (on: boolean) => (on ? enableMailWatch(accountId) : disableMailWatch(accountId)),
    // Returned, so the switch stays busy until the status it shows has caught up.
    onSuccess: () => queryClient.invalidateQueries({ queryKey: STATUS_KEY }),
  });
}

export function useSaveSettings() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: saveMailWatchSettings,
    onSuccess: (settings) => {
      queryClient.setQueryData<MailWatchStatus>(STATUS_KEY, (status) =>
        status === undefined ? status : { ...status, settings },
      );
    },
  });
}

/** A rule that a write has just put on file, and what it replaced. */
export interface RuleChange {
  readonly rule: MailWatchRule;
  readonly previous: MailWatchRule | undefined;
}

function ruleChange(queryClient: QueryClient, rule: MailWatchRule): RuleChange {
  const rules = queryClient.getQueryData<readonly MailWatchRule[]>(RULES_KEY);
  return { rule, previous: ruleOnFile(rules, rule) };
}

function refreshRules(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: RULES_KEY });
}

export interface CorrectionInput {
  readonly row: MailWatchDecision;
  readonly tier?: MailWatchTier;
  /** "" clears the label. */
  readonly labelId?: string;
}

function corrected(
  row: MailWatchDecision,
  tier: MailWatchTier | undefined,
  labelId: string | undefined,
): MailWatchDecision {
  const { label_id: currentId = '', ...rest } = row;
  const nextId = labelId ?? currentId;
  return {
    ...rest,
    tier: tier ?? row.tier,
    decided_by: 'person',
    ...(nextId === '' ? {} : { label_id: nextId }),
  };
}

/** Whether a row lives in the discarded list, which is where its optimistic write goes. */
function listOf(row: MailWatchDecision): boolean {
  return row.discarded === true;
}

export function useCorrection(
  accountId: string,
  day: string | null,
  handlers: {
    readonly onLearned: (change: RuleChange, created: boolean) => void;
    readonly onFailed: () => void;
  },
) {
  const queryClient = useQueryClient();

  function writeRow(row: MailWatchDecision) {
    const { queryKey } = decisionsQuery(accountId, day, listOf(row));
    queryClient.setQueryData<MailWatchDay>(queryKey, (answer) =>
      answer === undefined
        ? answer
        : {
            ...answer,
            decisions: answer.decisions.map((entry) =>
              decisionKey(entry) === decisionKey(row) ? row : entry,
            ),
          },
    );
  }

  return useMutation({
    mutationKey: CORRECTION_KEY,
    mutationFn: ({ row, tier, labelId }: CorrectionInput) =>
      correctDecision(accountId, {
        resource: row.resource,
        item_id: row.item_id,
        ...(tier === undefined ? {} : { tier }),
        ...(labelId === undefined ? {} : { label_id: labelId }),
      }),
    onMutate: async ({ row, tier, labelId }) => {
      await queryClient.cancelQueries({ queryKey: dayKey(accountId, day) });
      writeRow(corrected(row, tier, labelId));
    },
    onError: (_error, { row }) => {
      writeRow(row);
      handlers.onFailed();
    },
    onSuccess: (answer, { row, labelId }) => {
      writeRow(corrected(row, answer.tier, labelId));
      // Only a row that changed lists is read again: any other correction is the write above.
      if ((answer.discarded === true) !== listOf(row)) {
        void queryClient.invalidateQueries({ queryKey: dayKey(accountId, day) });
      }
      if (answer.rule === undefined) return undefined;
      const change = ruleChange(queryClient, answer.rule);
      // A correction that agrees with the rule on file taught nothing new.
      if (!sameRule(change.previous, change.rule)) {
        handlers.onLearned(change, answer.rule_created === true);
      }
      return refreshRules(queryClient);
    },
  });
}

/** The rows with a correction in flight: a second one on the same row would race the first. */
export function useCorrectionsInFlight(): ReadonlySet<string> {
  const rows = useMutationState({
    filters: { mutationKey: CORRECTION_KEY, status: 'pending' },
    select: (mutation) => (mutation.state.variables as CorrectionInput).row,
  });
  return new Set(rows.map(decisionKey));
}

export function useAddRule(onAdded?: (change: RuleChange, created: boolean) => void) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: MailWatchRuleInput) => {
      const saved = await addRule(input);
      return { change: ruleChange(queryClient, saved), created: saved.rule_created === true };
    },
    onSuccess: ({ change, created }) => {
      onAdded?.(change, created);
      return refreshRules(queryClient);
    },
  });
}

export function useRemoveRule(onRemoved: () => void) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: removeRule,
    onSuccess: () => {
      onRemoved();
      return refreshRules(queryClient);
    },
  });
}

/** Undoes the write of a rule the server said was new. One that merged into or replaced a rule on
 * file is never offered an Undo, because deleting the row would take the person's earlier words
 * with it. */
export function useUndoRule(onUndone: () => void) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ rule }: RuleChange) => {
      await removeRule(rule.id);
    },
    onSuccess: () => {
      onUndone();
      return refreshRules(queryClient);
    },
  });
}

function refreshLabels(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: LABELS_KEY });
}

export function useAddLabel(onAdded: () => void) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: addLabel,
    onSuccess: () => {
      onAdded();
      return refreshLabels(queryClient);
    },
  });
}

export function useUpdateLabel(labelId: string, onSaved: () => void) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: MailWatchLabelInput) => updateLabel(labelId, input),
    onSuccess: () => {
      onSaved();
      return refreshLabels(queryClient);
    },
  });
}

/** The server deletes a label's rules with it, so the rules are read again too. */
export function useRemoveLabel(onRemoved: () => void) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: removeLabel,
    onSuccess: () => {
      onRemoved();
      return Promise.all([refreshLabels(queryClient), refreshRules(queryClient)]);
    },
  });
}
