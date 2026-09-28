import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../../i18n/i18n';
import { TerminalDisplay } from '../TerminalDisplay';
import type { DisplayTerminal } from '../types';

const terminal: DisplayTerminal = {
  command: 'make test',
  output: 'first\nfailed',
  cwd: '/workspace',
  exit_code: 7,
  duration_ms: 34,
  truncated: false,
};

describe('TerminalDisplay', () => {
  it('shows the real exit status, cwd, command, and combined output as text', () => {
    render(<TerminalDisplay terminal={terminal} />);
    expect(screen.getByText('make test')).toBeTruthy();
    expect(screen.getByText('exit 7')).toBeTruthy();
    expect(screen.getByText('first')).toBeTruthy();
    expect(screen.getByText('failed')).toBeTruthy();
    expect(screen.getByText('/workspace')).toBeTruthy();
    expect(screen.getByText('34ms')).toBeTruthy();
  });

  it('escapes a malicious command and can expand collapsed output', () => {
    const many = Array.from({ length: 15 }, (_, i) => `line ${String(i)}`).join('\n');
    render(<TerminalDisplay terminal={{ ...terminal, command: '<img src=x>', output: many }} />);
    expect(screen.getByText('<img src=x>')).toBeTruthy();
    expect(document.querySelector('img')).toBeNull();
    expect(screen.queryByText('line 14')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Show all' }));
    expect(screen.getByText('line 14')).toBeTruthy();
  });
});
