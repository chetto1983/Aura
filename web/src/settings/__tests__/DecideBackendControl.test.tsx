import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import '../../i18n/i18n';
import { DecideBackendControl } from '../DecideBackendControl';
import type { LoadedState } from '../modelSettingsState';
import type { PickerBinding } from '../SettingField';
import type { ModelCatalogState } from '../useModelCatalog';

const LICENCE =
  'https://huggingface.co/LiquidAI/d1-3B-GGUF/blob/bb1e436ea78eb96a3f1acb6da865f70c2fbeb563/LICENSE';

type Catalogue = Pick<ModelCatalogState, 'status' | 'error' | 'localSidecar'>;

function pickerFor(catalogue: Catalogue): PickerBinding {
  return {
    catalog: {
      models: [
        { kind: 'decisions', id: 'liquid/d1.1', has_price: false },
        { kind: 'decisions', id: 'liquid/d1.2', has_price: false },
      ],
      ...catalogue,
      reload: () => undefined,
    },
    formatRow: (row) => row.id,
  };
}

function Harness({
  saved,
  catalogue,
  writes,
}: {
  readonly saved: string;
  readonly catalogue: Catalogue;
  readonly writes: string[];
}) {
  const [values, setValues] = useState<Record<string, string>>({ AURA_DECIDE_MODEL: saved });
  const loaded: LoadedState = {
    rows: {},
    values,
    initial: { AURA_DECIDE_MODEL: saved },
    restartRequired: false,
    restartKeys: [],
    restartSupported: true,
  };
  return (
    <DecideBackendControl
      loaded={loaded}
      onValueChange={(key, value) => {
        writes.push(`${key}=${value}`);
        setValues((prev) => ({ ...prev, [key]: value }));
      }}
      modelPicker={pickerFor(catalogue)}
    />
  );
}

function renderControl(saved: string, localSidecar: boolean | undefined) {
  return renderWith(saved, { status: 'ready', error: undefined, localSidecar });
}

function renderWith(saved: string, catalogue: Catalogue) {
  const writes: string[] = [];
  render(<Harness saved={saved} catalogue={catalogue} writes={writes} />);
  return writes;
}

function option(name: string): HTMLElement {
  return screen.getByRole('radio', { name });
}

describe('DecideBackendControl', () => {
  it('offers the local model only where it is installed', () => {
    renderControl('', false);
    expect(option('Local').hasAttribute('disabled')).toBe(true);
    expect(screen.getByText('Local model not installed on this appliance')).toBeTruthy();
    // With no sidecar an empty row is no route at all, and the control says so.
    expect(option('Off').getAttribute('aria-checked')).toBe('true');
  });

  // The flag is unknown while the catalogue loads or when OpenRouter cannot be reached; an
  // offline appliance is exactly the one that runs the local model.
  it.each([
    ['cannot be read', { status: 'error', error: 'unreachable', localSidecar: undefined }],
    ['is loading', { status: 'loading', error: undefined, localSidecar: undefined }],
  ] as const)('keeps the local route when the catalogue %s', (_when, catalogue) => {
    renderWith('', catalogue);
    expect(option('Local').getAttribute('aria-checked')).toBe('true');
    expect(option('Local').hasAttribute('disabled')).toBe(false);
    expect(screen.queryByText('Local model not installed on this appliance')).toBeNull();
  });

  it('lets the operator return to the local route while the flag is unknown', () => {
    const writes = renderWith('off', {
      status: 'error',
      error: 'unreachable',
      localSidecar: undefined,
    });
    expect(option('Off').getAttribute('aria-checked')).toBe('true');
    fireEvent.click(option('Local'));
    expect(writes).toEqual(['AURA_DECIDE_MODEL=']);
  });

  it('offers the local model when the installer put it there', () => {
    renderControl('', true);
    expect(option('Local').hasAttribute('disabled')).toBe(false);
    expect(option('Local').getAttribute('aria-checked')).toBe('true');
    expect(screen.queryByText('Local model not installed on this appliance')).toBeNull();
  });

  it('names the local model’s licence', () => {
    renderControl('', true);
    expect(screen.getByText(/d1-3B by Liquid AI, LFM 1\.0 licence/)).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Read the licence' }).getAttribute('href')).toBe(
      LICENCE,
    );
  });

  it('lists OpenRouter’s decision models and says what leaves the appliance', () => {
    renderControl('liquid/d1.1', true);
    expect(option('OpenRouter').getAttribute('aria-checked')).toBe('true');
    fireEvent.click(screen.getByLabelText('Decision model'));
    expect(screen.getByRole('option', { name: /liquid\/d1\.2/ })).toBeTruthy();
    expect(
      screen.getByText(
        'The subject and the start of each mail go to OpenRouter, only for accounts that allow it.',
      ),
    ).toBeTruthy();
  });

  it('writes a chosen model', () => {
    const writes = renderControl('', true);
    fireEvent.click(option('OpenRouter'));
    expect(writes).toEqual([]);
    fireEvent.click(screen.getByLabelText('Decision model'));
    fireEvent.click(screen.getByRole('option', { name: /liquid\/d1\.2/ }));
    expect(writes).toEqual(['AURA_DECIDE_MODEL=liquid/d1.2']);
  });

  it('switching off writes off', () => {
    const writes = renderControl('liquid/d1.1', true);
    fireEvent.click(option('Off'));
    expect(writes).toEqual(['AURA_DECIDE_MODEL=off']);
    expect(option('Off').getAttribute('aria-checked')).toBe('true');
  });

  it('switching to the local model writes an empty model', () => {
    const writes = renderControl('off', true);
    fireEvent.click(option('Local'));
    expect(writes).toEqual(['AURA_DECIDE_MODEL=']);
  });

  it.each([
    [true, 'Use the local model', 'AURA_DECIDE_MODEL=', 'Local'],
    [false, 'Turn the decision model off', 'AURA_DECIDE_MODEL=off', 'Off'],
    [undefined, 'Use the local model', 'AURA_DECIDE_MODEL=', 'Local'],
  ])(
    'clearing the model returns to the local route, or to off where there is none (sidecar %s)',
    (localSidecar, clearLabel, written, checked) => {
      const writes = renderControl('liquid/d1.1', localSidecar);
      fireEvent.click(screen.getByLabelText('Decision model'));
      fireEvent.click(screen.getByRole('option', { name: new RegExp(clearLabel) }));
      expect(writes).toEqual([written]);
      expect(option(checked).getAttribute('aria-checked')).toBe('true');
    },
  );

  it('renders no restart control', () => {
    renderControl('liquid/d1.1', true);
    expect(screen.queryByRole('button', { name: /restart/i })).toBeNull();
    expect(screen.queryByRole('button', { name: /preview|apply|reset/i })).toBeNull();
  });
});
