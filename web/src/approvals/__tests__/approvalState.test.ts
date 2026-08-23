import { describe, expect, it } from 'vitest';
import { parseOptions } from '../approvalState';

// The server persists paused_states.options from agent.PauseOption, which marshals as
// [{label, value}] — NOT as a string array. parseOptions used to accept only strings, so
// every options pause fell through to the free-text box and no option button had ever
// rendered. These cases pin both shapes.
describe('parseOptions', () => {
  it('reads the {label,value} shape the server actually writes', () => {
    expect(
      parseOptions([
        { label: 'Approve once', value: 'Approve once' },
        {
          label: 'Always approve calendar delete_event',
          value: 'Always approve calendar delete_event',
        },
      ]),
    ).toEqual([
      { label: 'Approve once', value: 'Approve once' },
      {
        label: 'Always approve calendar delete_event',
        value: 'Always approve calendar delete_event',
      },
    ]);
  });

  it('reads the bare-string shape ask_user also accepts', () => {
    expect(parseOptions(['Sì', 'No'])).toEqual([
      { label: 'Sì', value: 'Sì' },
      { label: 'No', value: 'No' },
    ]);
  });

  it('falls back to the label when an entry carries no value', () => {
    expect(parseOptions([{ label: 'Approve once' }])).toEqual([
      { label: 'Approve once', value: 'Approve once' },
    ]);
  });

  it('drops unusable entries instead of throwing, so the card degrades to free text', () => {
    expect(parseOptions([null, 42, {}, { label: '' }, { value: 'orphan' }, ''])).toEqual([]);
    expect(parseOptions(null)).toEqual([]);
    expect(parseOptions({ label: 'not an array' })).toEqual([]);
  });
});
