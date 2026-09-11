import { describe, expect, it } from 'vitest';
import { summarizeOpenRouterKeys } from '../routeStepModel';

const RUN = { identities_minted: [], limits_aligned: [] };

describe('summarizeOpenRouterKeys', () => {
  it('is empty when no write ran the reconciler', () => {
    expect(summarizeOpenRouterKeys([], 'id-admin')).toEqual({
      ownLabel: '',
      servicesLabel: '',
      errors: [],
    });
  });

  it("keeps the caller's own label and the services label from whichever run minted them", () => {
    const runs = [
      { ...RUN, services_label: 'sk-or-v1-srv...1' },
      {
        ...RUN,
        identities_minted: ['id-admin', 'id-member'],
        minted_labels: { 'id-admin': 'sk-or-v1-adm...1', 'id-member': 'sk-or-v1-mem...1' },
      },
      { ...RUN },
    ];
    expect(summarizeOpenRouterKeys(runs, 'id-admin')).toEqual({
      ownLabel: 'sk-or-v1-adm...1',
      servicesLabel: 'sk-or-v1-srv...1',
      errors: [],
    });
  });

  // The cap's PUT runs the reconciler before the management key is stored and says why it
  // waited; the management key's PUT, a moment later, is the one that says what is still wrong.
  it("reports only the last run's errors", () => {
    const waited = { ...RUN, errors: ['services key: the monthly cap is not set'] };
    const refused = { ...RUN, errors: ['identity id-admin: provider 401'] };
    expect(summarizeOpenRouterKeys([waited, { ...RUN }], 'id-admin').errors).toEqual([]);
    expect(summarizeOpenRouterKeys([{ ...RUN }, refused], 'id-admin').errors).toEqual([
      'identity id-admin: provider 401',
    ]);
  });
});
