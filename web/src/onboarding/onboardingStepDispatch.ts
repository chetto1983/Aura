import { useCallback } from 'react';
import {
  stepOnboarding,
  type OnboardingAnswers,
  type OnboardingStepResponse,
} from './onboardingApi';
import { isTerminalStatus } from './onboardingWizardModel';

export type OnboardingStepIntent = 'answer' | 'confirm' | 'edit' | 'skip';

export function useOnboardingStepDispatch({
  onError,
  onResponse,
  onTerminal,
  sessionToken,
  setBusy,
}: {
  readonly sessionToken: string;
  readonly setBusy: (busy: boolean) => void;
  readonly onResponse: (response: OnboardingStepResponse) => void;
  readonly onTerminal: (response: OnboardingStepResponse) => void;
  readonly onError: (error: unknown) => void;
}) {
  return useCallback(
    async (intent: OnboardingStepIntent, text?: string, answers?: OnboardingAnswers) => {
      if (sessionToken === '') return;
      setBusy(true);
      try {
        const resp = await stepOnboarding(sessionToken, {
          intent,
          ...(text !== undefined ? { text } : {}),
          ...(answers !== undefined ? { answers } : {}),
        });
        onResponse(resp);
        if (isTerminalStatus(resp.status)) {
          onTerminal(resp);
        }
      } catch (err) {
        onError(err);
      } finally {
        setBusy(false);
      }
    },
    [onError, onResponse, onTerminal, sessionToken, setBusy],
  );
}
