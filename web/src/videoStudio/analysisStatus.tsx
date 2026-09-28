import { useTranslation } from 'react-i18next';
import type { AnalysisState } from './analysisState';
import { listeningState, type DuckingListening } from './VideoStudio_ducking';

// analysisStatus.tsx — what an analysis says on screen: on the control that started it, and, for
// ducking's own listening, in the workspace's status line too.

/** What a control says while its analysis runs, and why it failed. */
export function AnalysisStatus({
  state,
  working,
  failed,
}: {
  readonly state: AnalysisState;
  readonly working: string;
  readonly failed: string;
}) {
  const { t } = useTranslation();
  if (state.state === 'working') {
    return (
      <p role="status" className="text-xs text-text-muted">
        {t(working)}
      </p>
    );
  }
  if (state.state === 'failed') {
    return (
      <p role="alert" className="text-xs text-danger">
        {t(failed, { reason: state.reason })}
      </p>
    );
  }
  return null;
}

/** Ducking's listening, said in the workspace's status line: seen whatever is selected. */
export function DuckingStatus({ listening }: { readonly listening: DuckingListening }) {
  return (
    <AnalysisStatus
      state={listeningState(listening)}
      working="videoStudio.audio.listening"
      failed="videoStudio.audio.listenFailed"
    />
  );
}
