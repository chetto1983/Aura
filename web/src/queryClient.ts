import { QueryClient } from '@tanstack/react-query';

// One shared client for the SPA. Retries are disabled for the health poll's own
// queries via per-query options; this default keeps unrelated future queries from
// hammering a down backend.
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});
