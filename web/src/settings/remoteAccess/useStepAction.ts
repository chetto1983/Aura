import { useState } from 'react';
import { failureOf, type Failure } from './remoteAccessSteps';

/** One press of a wizard button: holds it while it runs and keeps what went wrong, so a failure
 * stays on the card the operator is looking at. */
export function useStepAction() {
  const [failure, setFailure] = useState<Failure | null>(null);
  const [running, setRunning] = useState(false);

  async function run(action: () => Promise<unknown>): Promise<boolean> {
    setRunning(true);
    setFailure(null);
    try {
      await action();
      return true;
    } catch (err) {
      setFailure(failureOf(err));
      return false;
    } finally {
      setRunning(false);
    }
  }

  return {
    failure,
    running,
    run,
    clear: () => {
      setFailure(null);
    },
  };
}
