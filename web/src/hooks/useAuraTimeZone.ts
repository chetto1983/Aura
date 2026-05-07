import { useEffect, useState } from 'react';
import { api } from '@/api';
import {
  activeAuraTimeZoneFromSettings,
  defaultAuraTimeZone,
} from '@/lib/timezone';

export function useAuraTimeZone(): string {
  const [timeZone, setTimeZone] = useState(defaultAuraTimeZone);

  useEffect(() => {
    let cancelled = false;
    api.settings()
      .then((res) => {
        if (!cancelled) {
          setTimeZone(activeAuraTimeZoneFromSettings(res.items));
        }
      })
      .catch(() => {
        // Keep the config default. The tasks API still validates writes.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return timeZone;
}
