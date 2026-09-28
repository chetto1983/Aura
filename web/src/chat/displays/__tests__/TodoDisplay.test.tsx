import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import '../../../i18n/i18n';
import { TodoDisplay } from '../TodoDisplay';
import type { DisplayPayload } from '../types';

describe('TodoDisplay', () => {
  it('shows ordered progress and the active form', () => {
    const payload = {
      type: 'todo',
      tool_call_id: 'todo-1',
      todo: {
        items: [
          { content: 'Plan', status: 'completed' },
          { content: 'Build', status: 'in_progress', active_form: 'Building' },
          { content: 'Test', status: 'pending' },
        ],
      },
    } as DisplayPayload;
    render(<TodoDisplay payload={payload} />);
    expect(screen.getByText('Plan')).toBeTruthy();
    expect(screen.getByText('Build')).toBeTruthy();
    expect(screen.getByText('Building')).toBeTruthy();
    expect(screen.getByText('Test')).toBeTruthy();
    expect(screen.getByText('1/3')).toBeTruthy();
  });

  it('shows a cleared list as an empty checklist', () => {
    const payload = {
      type: 'todo',
      tool_call_id: 'todo-empty',
      todo: { items: [] },
    } as DisplayPayload;
    render(<TodoDisplay payload={payload} />);
    expect(screen.getByText('0/0')).toBeTruthy();
  });
});
