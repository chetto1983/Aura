import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import '../../../i18n/i18n';
import type { ComposerSkillRow } from '../api';
import { filterPickerItems, optionId, type PickerItem } from '../skillPickerModel';
import { SkillPicker } from '../SkillPicker';
import { SkillPill } from '../SkillPill';

// SkillPicker.test — the presentational ARIA listbox (grouped role=option rows, active
// aria-selected + JS-scroll-into-view, onSelect, empty→null) and the removable SkillPill.
// jsdom has no scrollIntoView, so the effect's JS-scroll (Pitfall 6) is asserted via a spy.

const SKILLS: readonly ComposerSkillRow[] = [
  { name: 'skill-creator', description: 'Create a new skill', type: 'instruction' },
  { name: 'docx-writer', description: 'Write Word documents', type: 'executable' },
  { name: 'graph-explorer', description: 'Explore the knowledge graph', type: 'instruction' },
];

const GROUPS = filterPickerItems(SKILLS, '');

let scrollSpy: Mock<(arg?: boolean | ScrollIntoViewOptions) => void>;

beforeEach(() => {
  scrollSpy = vi.fn<(arg?: boolean | ScrollIntoViewOptions) => void>();
  Element.prototype.scrollIntoView = scrollSpy;
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('SkillPicker', () => {
  it('renders a role=listbox with grouped role=option rows and localized headers', () => {
    render(
      <SkillPicker
        groups={GROUPS}
        activeOptionId={undefined}
        listboxId="skpick"
        onSelect={vi.fn()}
      />,
    );

    const listbox = screen.getByRole('listbox');
    expect(listbox.id).toBe('skpick');
    const options = within(listbox).getAllByRole('option');
    expect(options).toHaveLength(6); // 3 quick commands + 3 skills
    expect(options[0]?.id).toBe('skpick-opt-0');
    expect(options[5]?.id).toBe('skpick-opt-5');
    // Section headers per group (D-07).
    expect(screen.getByText('Quick commands')).toBeTruthy();
    expect(screen.getByText('Skills')).toBeTruthy();
    // A skill row shows name + description subtitle.
    expect(screen.getByText('skill-creator')).toBeTruthy();
    expect(screen.getByText('Create a new skill')).toBeTruthy();
  });

  it('marks only the active option aria-selected=true', () => {
    render(
      <SkillPicker
        groups={GROUPS}
        activeOptionId={optionId('skpick', 1)}
        listboxId="skpick"
        onSelect={vi.fn()}
      />,
    );

    const options = screen.getAllByRole('option');
    expect(options[1]?.getAttribute('aria-selected')).toBe('true');
    expect(options[0]?.getAttribute('aria-selected')).toBe('false');
    expect(options[2]?.getAttribute('aria-selected')).toBe('false');
  });

  it('JS-scrolls the active option into view on mount and on activeOptionId change', () => {
    const { rerender } = render(
      <SkillPicker
        groups={GROUPS}
        activeOptionId={optionId('skpick', 0)}
        listboxId="skpick"
        onSelect={vi.fn()}
      />,
    );
    expect(scrollSpy).toHaveBeenCalledTimes(1);

    rerender(
      <SkillPicker
        groups={GROUPS}
        activeOptionId={optionId('skpick', 4)}
        listboxId="skpick"
        onSelect={vi.fn()}
      />,
    );
    expect(scrollSpy).toHaveBeenCalledTimes(2);
  });

  it('calls onSelect with the clicked PickerItem', () => {
    const onSelect = vi.fn();
    render(
      <SkillPicker
        groups={GROUPS}
        activeOptionId={undefined}
        listboxId="skpick"
        onSelect={onSelect}
      />,
    );

    const firstSkill = screen.getAllByRole('option')[3]; // index 3 = first skill after 3 commands
    if (firstSkill === undefined) throw new Error('expected a skill option');
    // mousedown is preventDefault'd so focus stays on the composer input (APG combobox);
    // fireEvent returns false when the default was cancelled.
    expect(fireEvent.mouseDown(firstSkill)).toBe(false);
    fireEvent.click(firstSkill);

    expect(onSelect).toHaveBeenCalledTimes(1);
    const arg = onSelect.mock.calls[0]?.[0] as PickerItem;
    expect(arg.kind).toBe('skill');
    if (arg.kind === 'skill') expect(arg.name).toBe('skill-creator');
  });

  it('literally disables every option and suppresses selection when locked', () => {
    const onSelect = vi.fn();
    render(
      <SkillPicker
        groups={GROUPS}
        activeOptionId={undefined}
        listboxId="skpick"
        disabled
        onSelect={onSelect}
      />,
    );

    const options = screen.getAllByRole('option');
    for (const option of options) {
      expect(option).toHaveProperty('disabled', true);
      expect(option.getAttribute('aria-disabled')).toBe('true');
    }
    const firstOption = options[0];
    if (firstOption === undefined) throw new Error('expected an option');
    fireEvent.mouseDown(firstOption);
    fireEvent.click(firstOption);

    expect(onSelect).not.toHaveBeenCalled();
  });

  it('renders nothing when there are no groups (degrade-to-no-op, D-09)', () => {
    const { container } = render(
      <SkillPicker groups={[]} activeOptionId={undefined} listboxId="skpick" onSelect={vi.fn()} />,
    );

    expect(container.firstChild).toBeNull();
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('omits the subtitle for a no-description skill, falls back the icon, and wires aria-labelledby', () => {
    const groups = filterPickerItems([{ name: 'mystery', description: '', type: 'novel' }], '');
    render(
      <SkillPicker
        groups={groups}
        activeOptionId={undefined}
        listboxId="skpick"
        labelledById="composer-label"
        onSelect={vi.fn()}
      />,
    );

    expect(screen.getByRole('listbox').getAttribute('aria-labelledby')).toBe('composer-label');
    // The unknown-type skill renders its name (Sparkles fallback icon) with no subtitle node.
    const skillOption = screen.getByText('mystery').closest('[role="option"]');
    expect(skillOption?.textContent).toBe('mystery');
  });
});

describe('SkillPill', () => {
  it('renders the pinned skill name and fires onRemove from the ghost X', () => {
    const onRemove = vi.fn();
    render(<SkillPill name="docx-writer" onRemove={onRemove} />);

    expect(screen.getByText('docx-writer')).toBeTruthy();
    const removeBtn = screen.getByLabelText('Remove pinned skill docx-writer');
    fireEvent.click(removeBtn);

    expect(onRemove).toHaveBeenCalledTimes(1);
  });
});
