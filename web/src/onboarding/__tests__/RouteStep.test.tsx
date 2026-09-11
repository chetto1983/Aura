import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import type { SaveOutcome } from '../../settings/modelSettingsState';
import { requestRestart, watchRestart, type RestartWatchDeps } from '../../settings/restartAura';
import { fetchOnboardingStatus, type OnboardingStatus } from '../onboardingApi';
import { RouteStep } from '../RouteStep';

// RouteStep. The ModelSettingsPanel it wraps is replaced by two buttons that report a scripted
// save, so each case pins one decision the step makes after a save: move on, stay and explain, or
// restart Aura and resume. restartAura's timing has its own suite.

const h = vi.hoisted(() => ({ outcome: { openRouterKeys: [] } as SaveOutcome }));

vi.mock('../../settings/ModelSettingsPanel', () => ({
  ModelSettingsPanel: (props: {
    readonly onComplete: (outcome?: SaveOutcome) => Promise<void>;
    readonly skippable?: boolean;
  }) => (
    <div>
      <button type="button" onClick={() => void props.onComplete(h.outcome)}>
        Save
      </button>
      {props.skippable === false ? null : (
        <button type="button" onClick={() => void props.onComplete()}>
          Skip
        </button>
      )}
    </div>
  ),
}));
vi.mock('../../settings/restartAura', () => ({
  browserRestartDeps: {},
  requestRestart: vi.fn(),
  watchRestart: vi.fn(),
}));
vi.mock('../onboardingApi', () => ({ fetchOnboardingStatus: vi.fn() }));
vi.mock('../../admin/useAdmin', () => ({ useCapabilities: () => ({ identityId: 'id-admin' }) }));

const MINTED = {
  services_label: 'sk-or-v1-srv...ce1',
  identities_minted: ['id-admin'],
  minted_labels: { 'id-admin': 'sk-or-v1-adm...in1' },
  limits_aligned: [],
};

function status(routeRequired: boolean): OnboardingStatus {
  return { required: false, completed: true, skipped: false, routeRequired };
}

function renderStep(required: boolean) {
  const onDone = vi.fn();
  const onRequiredChange = vi.fn();
  render(<RouteStep required={required} onRequiredChange={onRequiredChange} onDone={onDone} />);
  return { onDone, onRequiredChange };
}

describe('RouteStep', () => {
  beforeEach(() => {
    h.outcome = { openRouterKeys: [] };
    vi.mocked(fetchOnboardingStatus).mockResolvedValue(status(false));
    vi.mocked(requestRestart).mockResolvedValue('accepted');
    vi.mocked(watchRestart).mockImplementation((deps: RestartWatchDeps) => {
      deps.reload();
      return () => undefined;
    });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('moves on at once when an optional save minted nothing', async () => {
    const { onDone } = renderStep(false);
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(onDone).toHaveBeenCalledTimes(1);
    });
    expect(fetchOnboardingStatus).not.toHaveBeenCalled();
    expect(requestRestart).not.toHaveBeenCalled();
  });

  it('shows the minted keys, restarts Aura once, and resumes when it is back', async () => {
    h.outcome = { openRouterKeys: [MINTED] };
    const { onDone, onRequiredChange } = renderStep(true);
    expect(screen.queryByRole('button', { name: 'Skip' })).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    const next = await screen.findByRole('button', { name: 'Continue' });
    expect(onRequiredChange).toHaveBeenCalledWith(false);
    expect(requestRestart).toHaveBeenCalledTimes(1);
    expect(
      screen.getByText('Your OpenRouter key: sk-or-v1-adm...in1, no spending limit.'),
    ).toBeTruthy();
    expect(screen.getByText(/sk-or-v1-srv\.\.\.ce1/)).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
    expect(onDone).not.toHaveBeenCalled();
    fireEvent.click(next);
    expect(onDone).toHaveBeenCalledTimes(1);
  });

  it('stays on the form with what OpenRouter refused', async () => {
    h.outcome = {
      openRouterKeys: [
        { identities_minted: [], limits_aligned: [], errors: ['identity id-admin: provider 401'] },
      ],
    };
    const { onDone } = renderStep(true);
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect((await screen.findByRole('alert')).textContent).toContain(
      'identity id-admin: provider 401',
    );
    expect(screen.getByRole('button', { name: 'Save' })).toBeTruthy();
    expect(onDone).not.toHaveBeenCalled();
    expect(requestRestart).not.toHaveBeenCalled();
  });

  it('says what is still missing when the save left the route required', async () => {
    vi.mocked(fetchOnboardingStatus).mockResolvedValue(status(true));
    const { onDone, onRequiredChange } = renderStep(true);
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(
      await screen.findByText(/needs the OpenRouter management key, or a local route/),
    ).toBeTruthy();
    expect(onRequiredChange).toHaveBeenCalledWith(true);
    expect(onDone).not.toHaveBeenCalled();
  });

  it('lets the admin continue when Aura cannot restart itself', async () => {
    h.outcome = { openRouterKeys: [MINTED] };
    vi.mocked(requestRestart).mockResolvedValue('unsupported');
    const { onDone } = renderStep(true);
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(await screen.findByText(/Aura didn't restart/)).toBeTruthy();
    expect(watchRestart).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    expect(onDone).toHaveBeenCalledTimes(1);
  });
});
