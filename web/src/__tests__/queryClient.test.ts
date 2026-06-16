import { describe, expect, it } from 'vitest';
import { queryClient } from '../queryClient';

describe('queryClient', () => {
  it('caps retries and disables window-focus refetch for the shared SPA client', () => {
    const queries = queryClient.getDefaultOptions().queries;
    expect(queries?.retry).toBe(1);
    expect(queries?.refetchOnWindowFocus).toBe(false);
  });
});
