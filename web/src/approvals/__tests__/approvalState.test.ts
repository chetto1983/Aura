import { describe, expect, it } from 'vitest';
import { parseOptions, parseScopeChoice } from '../approvalState';

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

// The gateway ships a stable code plus an English fallback, because it is not locale-aware
// (the same split ResolveOutcome uses). These cases pin the decoding each surface localizes
// from — and the fail-closed direction: anything malformed is NOT a scope, so the card
// renders the server's own label instead of a raw code.
describe('parseScopeChoice', () => {
  it('decodes a scope and its subject', () => {
    expect(parseScopeChoice('gateway_scope:always:calendar delete_event')).toEqual({
      scope: 'always',
      subject: 'calendar delete_event',
    });
    expect(parseScopeChoice('gateway_scope:once:shell_exec')).toEqual({
      scope: 'once',
      subject: 'shell_exec',
    });
  });

  it('is null for an ordinary option, so its label renders unchanged', () => {
    expect(parseScopeChoice('Sì')).toBeNull();
    expect(parseScopeChoice('')).toBeNull();
  });

  it('is null for a malformed or unknown code, never a raw code on a button', () => {
    expect(parseScopeChoice('gateway_scope:always')).toBeNull();
    expect(parseScopeChoice('gateway_scope:always:')).toBeNull();
    expect(parseScopeChoice('gateway_scope:everything:shell_exec')).toBeNull();
  });
});
