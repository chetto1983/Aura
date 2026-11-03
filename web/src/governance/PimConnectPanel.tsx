import { useTranslation } from 'react-i18next';
import { StartFailedPanel } from './CalendarConnectFields';
import type { PimConnectResult } from './pimConnect';
import { PimDeviceCodePanel } from './PimDeviceCodePanel';
import { PimGoogleConnectPanel } from './PimGoogleConnectPanel';

// PimConnectPanel draws what a sign-in step needs next from the operator, wherever it started: the
// add-account form or an account's row.

export function PimConnectPanel({
  result,
  retrying,
  signingInAgain = false,
  onRetry,
}: {
  readonly result: PimConnectResult;
  readonly retrying: boolean;
  readonly signingInAgain?: boolean;
  readonly onRetry: () => void;
}) {
  const { t } = useTranslation();
  switch (result.kind) {
    case 'google':
      return (
        <PimGoogleConnectPanel
          accountId={result.id}
          start={result.start}
          signingInAgain={signingInAgain}
        />
      );
    case 'device':
      return <PimDeviceCodePanel accountId={result.id} start={result.start} />;
    case 'startFailed':
      return <StartFailedPanel pending={retrying} onRetry={onRetry} />;
    case 'none':
      return (
        <p role="status" className="text-[13px] text-success">
          {t('governance.mcp.calendar.noAuthCreated')}
        </p>
      );
  }
}
