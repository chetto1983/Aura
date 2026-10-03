import { useEffect, useState } from 'react';

/** Wall-clock milliseconds, re-read every `intervalMs`. Reading the clock in render is impure
 * (react/purity); this keeps the read in state so a component still follows real time. */
export function useNow(intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => {
      setNow(Date.now());
    }, intervalMs);
    return () => {
      window.clearInterval(timer);
    };
  }, [intervalMs]);
  return now;
}
