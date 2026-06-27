import { useState } from 'react';
import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { CapabilityPicker } from '../CapabilityPicker';

// CapabilityPicker test (ONBD-01a / D-06 / T-28-06-03). The picker is fed the creator's grants
// from /start, already '*'-excluded server-side. This suite proves the no-escalation UX posture:
//  - NO '*' checkbox is ever rendered (even though the test could "ask" for one),
//  - the hint that full-access ('*') can't be granted is shown,
//  - ONLY the server-returned options render as checkboxes (the client invents nothing),
//  - toggling drives the controlled selection.

const OPTIONS = ['skills.read', 'scheduler.read', 'mcp.read'];

function Harness({ options }: { readonly options: readonly string[] }) {
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  return (
    <CapabilityPicker
      options={options}
      selected={selected}
      onToggle={(name) => {
        setSelected((prev) => {
          const next = new Set(prev);
          if (next.has(name)) next.delete(name);
          else next.add(name);
          return next;
        });
      }}
    />
  );
}

describe('CapabilityPicker (D-06)', () => {
  it('renders only the server-returned options as checkboxes — and never a "*" option', () => {
    render(<Harness options={OPTIONS} />);

    const checkboxes = screen.getAllByRole('checkbox');
    expect(checkboxes).toHaveLength(OPTIONS.length);

    for (const name of OPTIONS) {
      expect(screen.getByText(name)).toBeTruthy();
    }

    // No '*' checkbox exists, and the wildcard never appears as a selectable option label.
    const wildcardLabels = screen.queryAllByText('*');
    expect(wildcardLabels).toHaveLength(0);
  });

  it("shows the hint that full-access ('*') can't be granted", () => {
    render(<Harness options={OPTIONS} />);
    expect(
      screen.getByText(
        "You can grant only capabilities you hold. Full-access (`*`) can't be granted.",
      ),
    ).toBeTruthy();
  });

  it('toggles a capability in and out of the controlled selection', () => {
    render(<Harness options={OPTIONS} />);
    const first = screen.getByRole('checkbox', { name: /skills.read/ });
    expect(first.getAttribute('aria-checked')).toBe('false');
    fireEvent.click(first);
    expect(first.getAttribute('aria-checked')).toBe('true');
    fireEvent.click(first);
    expect(first.getAttribute('aria-checked')).toBe('false');
  });

  it('renders the no-grantable-capabilities copy when the option list is empty', () => {
    render(<Harness options={[]} />);
    expect(screen.queryAllByRole('checkbox')).toHaveLength(0);
    expect(
      screen.getByText(
        'You hold no grantable capabilities. The new identity will be created with none.',
      ),
    ).toBeTruthy();
  });
});
