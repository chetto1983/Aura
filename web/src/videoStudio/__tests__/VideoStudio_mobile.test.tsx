import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { MobileVideoTools } from '../VideoStudio_mobile';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

describe('MobileVideoTools', () => {
  it('runs clip commands and selects each shared inspector panel', () => {
    const onBack = vi.fn();
    const onSplit = vi.fn();
    const onRemove = vi.fn();
    const onOpenInspector = vi.fn();
    render(
      <MobileVideoTools
        selectedId="clip-1"
        inspectorTab="adjust"
        inspectorOpen={true}
        onBack={onBack}
        onSplit={onSplit}
        onRemove={onRemove}
        onAddClip={vi.fn()}
        onAddTitle={vi.fn()}
        onOpenInspector={onOpenInspector}
      />,
    );

    const tools = screen.getByRole('navigation', { name: 'videoStudio.mobileTools' });
    expect(
      within(tools)
        .getByRole('button', { name: 'videoStudio.mobile.adjust' })
        .getAttribute('aria-pressed'),
    ).toBe('true');
    fireEvent.click(within(tools).getByRole('button', { name: 'videoStudio.mobile.back' }));
    fireEvent.click(within(tools).getByRole('button', { name: 'videoStudio.mobile.split' }));
    fireEvent.click(within(tools).getByRole('button', { name: 'videoStudio.mobile.delete' }));
    for (const tab of ['adjust', 'animations', 'speed', 'audio', 'transform', 'time'] as const) {
      fireEvent.click(within(tools).getByRole('button', { name: `videoStudio.mobile.${tab}` }));
    }

    expect(onBack).toHaveBeenCalledOnce();
    expect(onSplit).toHaveBeenCalledOnce();
    expect(onRemove).toHaveBeenCalledOnce();
    expect(onOpenInspector).toHaveBeenNthCalledWith(1, 'adjust');
    expect(onOpenInspector).toHaveBeenNthCalledWith(2, 'animation');
    expect(onOpenInspector).toHaveBeenNthCalledWith(3, 'speed');
    expect(onOpenInspector).toHaveBeenNthCalledWith(4, 'audio');
    expect(onOpenInspector).toHaveBeenNthCalledWith(5, 'transform');
    expect(onOpenInspector).toHaveBeenNthCalledWith(6, 'time');
  });

  it('switches to the general add tools when no clip is selected', () => {
    const onAddClip = vi.fn();
    const onAddTitle = vi.fn();
    render(
      <MobileVideoTools
        selectedId={undefined}
        inspectorTab="transform"
        inspectorOpen={false}
        onBack={vi.fn()}
        onSplit={vi.fn()}
        onRemove={vi.fn()}
        onAddClip={onAddClip}
        onAddTitle={onAddTitle}
        onOpenInspector={vi.fn()}
      />,
    );

    const tools = screen.getByRole('navigation', { name: 'videoStudio.mobileTools' });
    fireEvent.click(within(tools).getByRole('button', { name: 'videoStudio.command.addSource' }));
    fireEvent.click(within(tools).getByRole('button', { name: 'videoStudio.command.addText' }));
    expect(onAddClip).toHaveBeenCalledOnce();
    expect(onAddTitle).toHaveBeenCalledOnce();
  });
});
