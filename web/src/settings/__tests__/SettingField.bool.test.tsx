import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n';
import { SettingsFields } from '../SettingField';
import type { SettingDef, SettingsKey } from '../modelSettingsDefs';
import type { LoadedState } from '../modelSettingsState';
import type { SettingItem } from '../settingsApi';

// The boolean control used to be exercised only through the model-routing panel, via the
// one bool field it happened to carry. That field was AURA_VISION_CLOUD, retired once the
// image route started reading the model's own card, so the control is tested at its owner
// instead of through whichever panel currently happens to hold a checkbox.
const BOOL_KEY = 'AURA_MEMORY_PRELOAD_ENABLED' as SettingsKey;
const def: SettingDef = { key: BOOL_KEY, kind: 'bool', labelKey: 'settings.fields.memoryPreload' };

function loadedWith(item: Partial<SettingItem>): LoadedState {
  const row = {
    key: BOOL_KEY,
    kind: 'bool',
    value: 'false',
    has_value: true,
    secret: false,
    ...item,
  } as SettingItem;
  return {
    rows: { [BOOL_KEY]: row },
    values: { [BOOL_KEY]: row.value ?? 'false' },
    initial: { [BOOL_KEY]: row.value ?? 'false' },
    restartRequired: false,
    restartKeys: [],
  };
}

function renderField(loaded: LoadedState, onValueChange = vi.fn()) {
  render(
    <SettingsFields
      defs={[def]}
      loaded={loaded}
      resetting={undefined}
      onValueChange={onValueChange}
      onReset={vi.fn()}
    />,
  );
  return onValueChange;
}

describe('SettingsFields boolean control', () => {
  it('renders a false boolean as an unchecked, inactive control', () => {
    renderField(loadedWith({ value: 'false' }));

    const checkbox = screen.getByRole('checkbox');
    if (!(checkbox instanceof HTMLInputElement)) {
      throw new Error('expected the boolean control to be a checkbox input');
    }
    expect(checkbox.checked).toBe(false);
    expect(screen.getByText('Inactive')).toBeTruthy();
    expect(screen.queryByText('Configured')).toBeNull();
  });

  it('reports the new value as a string when toggled on', () => {
    const onValueChange = renderField(loadedWith({ value: 'false' }));

    fireEvent.click(screen.getByRole('checkbox'));

    expect(onValueChange).toHaveBeenCalledWith(BOOL_KEY, 'true');
  });
});
