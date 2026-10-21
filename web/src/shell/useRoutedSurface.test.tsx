import { act, fireEvent, render, screen } from '@testing-library/react';
import { createMemoryRouter, RouterProvider, useBlocker } from 'react-router';
import { afterEach, expect, it } from 'vitest';
import { useRoutedSurface } from './useRoutedSurface';

afterEach(() => {
  localStorage.clear();
});
function Shell() {
  const { surface, setSurface } = useRoutedSurface();
  return (
    <>
      <p>{surface}</p>
      <button
        onClick={() => {
          setSurface('board');
        }}
      >
        Board
      </button>
      <button
        onClick={() => {
          setSurface('documents');
        }}
      >
        Documents
      </button>
      {surface === 'documents' && <DirtyDocument />}
    </>
  );
}
function DirtyDocument() {
  const blocker = useBlocker(true);
  return (
    <>
      <input defaultValue="draft" aria-label="Document draft" />
      {blocker.state === 'blocked' && (
        <>
          <button
            onClick={() => {
              blocker.reset();
            }}
          >
            Keep editing
          </button>
          <button
            onClick={() => {
              blocker.proceed();
            }}
          >
            Leave
          </button>
        </>
      )}
    </>
  );
}
it('preserves a routed dirty document when a global mode or browser Back is blocked', async () => {
  const router = createMemoryRouter([{ path: '/*', element: <Shell /> }], {
    initialEntries: ['/', '/documents/document/d1'],
    initialIndex: 1,
  });
  render(<RouterProvider router={router} />);
  fireEvent.change(screen.getByLabelText('Document draft'), { target: { value: 'unsaved' } });
  fireEvent.click(screen.getByRole('button', { name: 'Board' }));
  expect(screen.getByLabelText('Document draft')).toHaveProperty('value', 'unsaved');
  fireEvent.click(screen.getByRole('button', { name: 'Keep editing' }));
  await act(() => router.navigate(-1));
  expect(screen.getByLabelText('Document draft')).toHaveProperty('value', 'unsaved');
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: 'Leave' }));
    await Promise.resolve();
  });
  expect(screen.queryByLabelText('Document draft')).toBeNull();
  expect(screen.getByText('board')).toBeTruthy();
});
