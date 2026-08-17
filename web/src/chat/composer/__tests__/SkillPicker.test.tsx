import type { ReactNode } from 'react';
import type { Unstable_TriggerItem } from '@assistant-ui/react';
import { render } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import '../../../i18n/i18n';
import { SkillPicker } from '../SkillPicker';
import type { ComposerSkillRow } from '../api';

// SkillPicker is presentation over ComposerPrimitive.Unstable_TriggerPopover, so what is worth
// asserting here is the ONE decision the component owns: what happens when the popover reports
// a selection. The primitive's own contract — insert the serialized directive, THEN call
// onExecute — is doubled rather than re-implemented, because driving the real popover would be
// testing the library through a mock of the library.
//
// The distinction under test is the whole reason commands and skills share one behavior
// primitive: `removeOnExecute` is a property of the BEHAVIOR, so it cannot say "keep the text
// for a skill, drop it for a command". The executor does that per item.

const h = vi.hoisted(() => ({
  setText: vi.fn(),
  onExecute: undefined as ((item: Unstable_TriggerItem) => void) | undefined,
  listed: [] as readonly Unstable_TriggerItem[],
}));

vi.mock('@assistant-ui/react', () => {
  const Popover = Object.assign(
    ({
      children,
      adapter,
    }: {
      children: ReactNode;
      adapter: { search?: (q: string) => readonly Unstable_TriggerItem[] };
    }) => {
      h.listed = adapter.search?.('') ?? [];
      return <div>{children}</div>;
    },
    {
      Action: ({ onExecute }: { onExecute: (item: Unstable_TriggerItem) => void }) => {
        h.onExecute = onExecute;
        return null;
      },
      Directive: () => null,
    },
  );
  return {
    useAui: () => ({ composer: { setText: h.setText } }),
    ComposerPrimitive: {
      Unstable_TriggerPopover: Popover,
      Unstable_TriggerPopoverItems: () => null,
      Unstable_TriggerPopoverItem: () => null,
    },
  };
});

const SKILLS: readonly ComposerSkillRow[] = [
  { name: 'skill-creator', description: 'Create a new skill', type: 'instruction' },
];

function mount(onCommand?: (name: string) => void) {
  h.setText.mockClear();
  h.onExecute = undefined;
  return render(<SkillPicker skills={SKILLS} onCommand={onCommand} />);
}

describe('SkillPicker selection', () => {
  it('runs a chosen command and takes its text back out of the composer', () => {
    const onCommand = vi.fn();
    mount(onCommand);

    h.onExecute?.({ id: 'compact', type: 'command', label: 'Compact' });

    expect(onCommand).toHaveBeenCalledWith('compact');
    expect(h.setText).toHaveBeenCalledWith('');
  });

  it('leaves a chosen skill written into the message', () => {
    const onCommand = vi.fn();
    mount(onCommand);

    h.onExecute?.({ id: 'skill-creator', type: 'skill', label: 'skill-creator' });

    expect(onCommand).not.toHaveBeenCalled();
    expect(h.setText).not.toHaveBeenCalled();
  });

  it('lists the command ahead of the skills for a bare slash', () => {
    mount(vi.fn());

    expect(h.listed.map((item) => item.id)).toEqual(['compact', 'skill-creator']);
  });

  // With no executor there is nothing a command row could do, so it is not offered — and the
  // executor stays inert too: a menu entry that does nothing is worse than an absent one.
  it('offers no command when the composer has nothing to run one with', () => {
    mount();

    expect(h.listed.map((item) => item.id)).toEqual(['skill-creator']);
    h.onExecute?.({ id: 'compact', type: 'command', label: 'Compact' });
    expect(h.setText).not.toHaveBeenCalled();
  });

  it('registers no trigger at all with nothing to list', () => {
    const { container } = render(<SkillPicker skills={[]} />);

    expect(container.firstChild).toBeNull();
  });
});
