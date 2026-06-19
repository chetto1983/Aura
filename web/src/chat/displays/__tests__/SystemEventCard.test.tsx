import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { SystemEventCard } from '../SystemEventCard';
import type { DisplayPayload } from '../types';

// SystemEventCard (DISP-04 / D-07): maps every web/errors.go code to a safe friendly
// label (no SSRF internals, no raw URL, no free-form backend text), conveys severity
// by color + icon + text, and is role="status".

function payload(
  reason: string | undefined,
  severity: 'error' | 'warning' = 'error',
  message?: string,
): DisplayPayload {
  return {
    type: 'system_event',
    tool_call_id: 'call-1',
    system: {
      class: 'web_error',
      ...(reason !== undefined ? { reason } : {}),
      ...(message !== undefined ? { message } : {}),
      severity,
    },
  };
}

// The FULL set of 8 codes from internal/web/errors.go → their safe en label.
const CODE_TO_LABEL: readonly (readonly [string, string])[] = [
  ['web_search_unavailable', 'Web search is unavailable right now.'],
  ['blocked_url', 'This URL was blocked by the safety policy.'],
  ['unsupported_scheme', "This link type isn't supported."],
  ['unsupported_content_type', "This content type can't be read."],
  ['response_too_large', 'The source was too large to read.'],
  ['timeout', 'The source timed out.'],
  ['http_error', 'The source returned an error.'],
  ['extraction_failed', "Couldn't read this source's content."],
];

describe('SystemEventCard (DISP-04 / D-07)', () => {
  it.each(CODE_TO_LABEL)('maps the %s code to its safe friendly label', (code, expected) => {
    render(<SystemEventCard payload={payload(code)} />);
    expect(screen.getByText(expected)).toBeTruthy();
  });

  it('falls to a generic safe label for an unknown/unmapped code (no free-form text)', () => {
    render(<SystemEventCard payload={payload('some_future_internal_code')} />);
    expect(screen.getByText('The source could not be processed.')).toBeTruthy();
    // The raw code is never rendered.
    expect(screen.queryByText('some_future_internal_code')).toBeNull();
  });

  it('NEVER renders the free-form message or any raw URL (T-26-12 no SSRF internals)', () => {
    const leaky = 'connect tcp 169.254.169.254:80: blocked https://internal.evil/secret';
    render(<SystemEventCard payload={payload('blocked_url', 'error', leaky)} />);
    // The classified label shows; the leaky message text is absent from the DOM.
    expect(screen.getByText('This URL was blocked by the safety policy.')).toBeTruthy();
    expect(screen.queryByText(leaky)).toBeNull();
    expect(screen.queryByText(/169\.254\.169\.254/)).toBeNull();
    expect(screen.queryByText(/internal\.evil/)).toBeNull();
  });

  it('conveys status by color + icon + text, not color alone (WCAG 1.4.1)', () => {
    const { container } = render(<SystemEventCard payload={payload('blocked_url', 'error')} />);
    // Text: the severity word is present.
    expect(screen.getByText('Error')).toBeTruthy();
    // Icon: an aria-hidden SVG with the danger tint.
    const icon = container.querySelector('svg[aria-hidden="true"]');
    expect(icon).not.toBeNull();
    expect(icon?.getAttribute('class')).toContain('text-danger');
    // Color: the left rule is the danger tint.
    expect(container.querySelector('.border-l-danger')).not.toBeNull();
  });

  it('uses the warning tint + word for a warning severity', () => {
    const { container } = render(<SystemEventCard payload={payload('timeout', 'warning')} />);
    expect(screen.getByText('Warning')).toBeTruthy();
    expect(container.querySelector('.border-l-warning')).not.toBeNull();
    expect(container.querySelector('svg.text-warning')).not.toBeNull();
  });

  it('is role="status" (post-hoc evidence), not role="alert"', () => {
    render(<SystemEventCard payload={payload('http_error')} />);
    expect(screen.getByRole('status')).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('renders the generic safe label when the system slot is missing', () => {
    render(
      <SystemEventCard
        payload={{ type: 'system_event', tool_call_id: 'call-x' } as DisplayPayload}
      />,
    );
    expect(screen.getByText('The source could not be processed.')).toBeTruthy();
    expect(screen.getByRole('status')).toBeTruthy();
  });
});
