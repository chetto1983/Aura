import { fireEvent, render, screen } from '@testing-library/react';
import { createPortal } from 'react-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MediaEditorLayer } from '../MediaEditorLayer';

let root: HTMLDivElement;

beforeEach(() => {
  root = document.createElement('div');
  root.id = 'root';
  const opener = document.createElement('button');
  opener.textContent = 'opener';
  root.append(opener);
  document.body.append(root);
  opener.focus();
});

afterEach(() => {
  root.remove();
});

describe('MediaEditorLayer', () => {
  it('covers the page, makes it inert and gives focus back on close', () => {
    const { unmount } = render(
      <MediaEditorLayer label="Edit clip.mp4" onEscape={vi.fn()}>
        body
      </MediaEditorLayer>,
      { container: document.body.appendChild(document.createElement('div')) },
    );
    const layer = screen.getByRole('dialog', { name: 'Edit clip.mp4' });
    expect(layer.parentElement).toBe(document.body);
    expect(layer.className).toContain('z-[80]');
    expect(root.hasAttribute('inert')).toBe(true);
    expect(document.activeElement).toBe(layer);
    unmount();
    expect(root.hasAttribute('inert')).toBe(false);
    expect(document.activeElement?.textContent).toBe('opener');
  });

  it('closes on Escape inside the layer', () => {
    const onEscape = vi.fn();
    render(
      <MediaEditorLayer label="Edit" onEscape={onEscape}>
        <input aria-label="inside" />
      </MediaEditorLayer>,
    );
    fireEvent.keyDown(screen.getByLabelText('inside'), { key: 'Escape' });
    expect(onEscape).toHaveBeenCalledTimes(1);
  });

  it('keeps its Escape from also closing overlays that listen on the document', () => {
    const documentListener = vi.fn();
    document.addEventListener('keydown', documentListener);
    render(
      <MediaEditorLayer label="Edit" onEscape={vi.fn()}>
        <input aria-label="inside" />
      </MediaEditorLayer>,
    );
    fireEvent.keyDown(screen.getByLabelText('inside'), { key: 'Escape' });
    document.removeEventListener('keydown', documentListener);
    expect(documentListener).not.toHaveBeenCalled();
  });

  it('ignores Escape pressed in a menu the editor portalled out of the layer', () => {
    const onEscape = vi.fn();
    render(
      <MediaEditorLayer label="Edit" onEscape={onEscape}>
        {createPortal(<input aria-label="portal menu" />, document.body)}
      </MediaEditorLayer>,
    );
    fireEvent.keyDown(screen.getByLabelText('portal menu'), { key: 'Escape' });
    expect(onEscape).not.toHaveBeenCalled();
  });
});
