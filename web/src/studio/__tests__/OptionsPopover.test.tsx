import { useState } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { AdvancedPopover } from '../AdvancedPopover';
import { OptionsPopover } from '../OptionsPopover';
import type { StudioModel } from '../studioApi';
import { optionsSummary, type StudioOptions } from '../studioForm';

// The two composer popovers. What is asserted here is the rule the design states twice: a
// control exists only when the model declares its values, the pill reads the current choice,
// and Reset restores the cheapest — never a value nobody chose and never a control that lives
// in the other popover.

const VIDEO: StudioModel = {
  id: 'google/veo-3.1-lite',
  name: 'Veo 3.1 Lite',
  durations: [8, 4],
  resolutions: ['1080p', '720p'],
  aspect_ratios: ['16:9', '9:16', '1:1'],
  audio: true,
  seed: true,
};

const IMAGE: StudioModel = {
  id: 'openai/gpt-image-1-mini',
  name: 'GPT Image 1 mini',
  aspect_ratios: ['1:1', '3:2'],
  audio: false,
  seed: false,
  reference_max: 3,
};

const CHEAPEST: StudioOptions = {
  resolution: '720p',
  duration: 4,
  aspectRatio: '16:9',
  audio: false,
  seed: undefined,
};

// The page owns the draft, so the popovers are rendered controlled here too: a spy alone
// would freeze the seed box at its first value and hide what a second edit reports.
function Controlled({
  initial,
  spy,
  render: renderPopover,
}: {
  readonly initial: StudioOptions;
  readonly spy: (options: StudioOptions) => void;
  readonly render: (
    options: StudioOptions,
    onChange: (next: StudioOptions) => void,
  ) => React.ReactNode;
}) {
  const [options, setOptions] = useState(initial);
  return renderPopover(options, (next) => {
    spy(next);
    setOptions(next);
  });
}

function openOptions(model: StudioModel, options: StudioOptions, kind: 'image' | 'video') {
  const spy = vi.fn();
  render(
    <Controlled
      initial={options}
      spy={spy}
      render={(current, onChange) => (
        <OptionsPopover kind={kind} model={model} options={current} onChange={onChange} />
      )}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Options' }));
  return spy;
}

function openAdvanced(model: StudioModel, options: StudioOptions) {
  const spy = vi.fn();
  render(
    <Controlled
      initial={options}
      spy={spy}
      render={(current, onChange) => (
        <AdvancedPopover model={model} options={current} onChange={onChange} />
      )}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Advanced' }));
  return spy;
}

describe('optionsSummary', () => {
  it('reads the axes a clip has, in the order the bar shows them', () => {
    expect(optionsSummary(CHEAPEST, (s) => `${String(s)}s`)).toBe('16:9 · 720p · 4s');
  });

  it('is the ratio alone for an image, with no empty separators', () => {
    expect(
      optionsSummary(
        { resolution: '', duration: undefined, aspectRatio: '1:1', audio: false, seed: undefined },
        (s) => `${String(s)}s`,
      ),
    ).toBe('1:1');
  });
});

describe('OptionsPopover', () => {
  it('summarizes the current choice on the pill rather than a generic word', () => {
    render(<OptionsPopover kind="video" model={VIDEO} options={CHEAPEST} onChange={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Options' }).textContent).toBe('16:9 · 720p · 4s');
  });

  it('heads the video popover with the video title and offers all three axes', () => {
    openOptions(VIDEO, CHEAPEST, 'video');
    expect(screen.getByRole('heading', { name: 'Video settings' })).toBeTruthy();
    expect(screen.getByRole('radiogroup', { name: 'Aspect ratio' })).toBeTruthy();
    expect(screen.getByRole('radiogroup', { name: 'Resolution' })).toBeTruthy();
    expect(screen.getByRole('group', { name: 'Duration' })).toBeTruthy();
    expect(screen.getByRole('slider')).toBeTruthy();
  });

  it('reports the resolution that was pressed, not the one beside it', () => {
    const onChange = openOptions(VIDEO, CHEAPEST, 'video');
    fireEvent.click(screen.getByRole('radio', { name: '1080p' }));
    expect(onChange).toHaveBeenCalledWith({ ...CHEAPEST, resolution: '1080p' });
  });

  it('reports the ratio that was pressed', () => {
    const onChange = openOptions(VIDEO, CHEAPEST, 'video');
    fireEvent.click(screen.getByRole('radio', { name: '9:16' }));
    expect(onChange).toHaveBeenCalledWith({ ...CHEAPEST, aspectRatio: '9:16' });
  });

  it('steps the duration over the declared stops, in order, never between them', () => {
    const onChange = openOptions(VIDEO, CHEAPEST, 'video');
    fireEvent.keyDown(screen.getByRole('slider'), { key: 'ArrowRight' });
    // 8 is declared second by the catalog and is the longer stop: the slider must have
    // sorted them, or ArrowRight from 4 s would land nowhere.
    expect(onChange).toHaveBeenCalledWith({ ...CHEAPEST, duration: 8 });
  });

  it('shows the chosen duration beside the slider', () => {
    openOptions(VIDEO, { ...CHEAPEST, duration: 8 }, 'video');
    expect(screen.getByText('8s')).toBeTruthy();
  });

  it('resets to the cheapest declared values and leaves the Advanced knobs alone', () => {
    const chosen: StudioOptions = {
      resolution: '1080p',
      duration: 8,
      aspectRatio: '9:16',
      audio: true,
      seed: 42,
    };
    const onChange = openOptions(VIDEO, chosen, 'video');
    fireEvent.click(screen.getByRole('button', { name: 'Reset' }));
    expect(onChange).toHaveBeenCalledWith({
      resolution: '720p',
      duration: 4,
      aspectRatio: '16:9',
      // Sound and seed are Advanced's controls; a Reset here must not silently move a
      // control the operator cannot see from this popover.
      audio: true,
      seed: 42,
    });
  });

  it('offers an image model its ratios and nothing it never declared', () => {
    openOptions(IMAGE, { ...CHEAPEST, resolution: '', duration: undefined }, 'image');
    expect(screen.getByRole('heading', { name: 'Image settings' })).toBeTruthy();
    expect(screen.getByRole('radiogroup', { name: 'Aspect ratio' })).toBeTruthy();
    expect(screen.queryByRole('radiogroup', { name: 'Resolution' })).toBeNull();
    expect(screen.queryByRole('slider')).toBeNull();
  });

  it('renders no pill at all for a model that declares no option', () => {
    render(
      <OptionsPopover
        kind="image"
        model={{ id: 'bare', audio: false, seed: false }}
        options={CHEAPEST}
        onChange={vi.fn()}
      />,
    );
    expect(screen.queryByRole('button')).toBeNull();
  });
});

describe('AdvancedPopover', () => {
  it('offers the seed and the sound switch a video model declares', () => {
    openAdvanced(VIDEO, CHEAPEST);
    expect(screen.getByRole('heading', { name: 'Advanced' })).toBeTruthy();
    expect(screen.getByRole('spinbutton', { name: 'Seed' })).toBeTruthy();
    expect(screen.getByRole('switch', { name: 'Sound' })).toBeTruthy();
  });

  it('turns sound on', () => {
    const onChange = openAdvanced(VIDEO, CHEAPEST);
    fireEvent.click(screen.getByRole('switch', { name: 'Sound' }));
    expect(onChange).toHaveBeenCalledWith({ ...CHEAPEST, audio: true });
  });

  it('reports a typed seed as a number and an emptied box as none', () => {
    const onChange = openAdvanced(VIDEO, CHEAPEST);
    const seed = screen.getByRole('spinbutton', { name: 'Seed' });
    fireEvent.change(seed, { target: { value: '1234' } });
    expect(onChange).toHaveBeenCalledWith({ ...CHEAPEST, seed: 1234 });
    fireEvent.change(seed, { target: { value: '' } });
    // Not 0: an empty box means "let the provider choose", and 0 is a seed that pins every
    // run to the same clip.
    expect(onChange).toHaveBeenLastCalledWith({ ...CHEAPEST, seed: undefined });
  });

  it('shows the seed the draft carries rather than an empty box', () => {
    openAdvanced(VIDEO, { ...CHEAPEST, seed: 77 });
    expect(screen.getByRole('spinbutton', { name: 'Seed' }).getAttribute('value')).toBe('77');
  });

  it('resets sound and the seed together', () => {
    const onChange = openAdvanced(VIDEO, { ...CHEAPEST, audio: true, seed: 9 });
    fireEvent.click(screen.getByRole('button', { name: 'Reset' }));
    expect(onChange).toHaveBeenCalledWith({ ...CHEAPEST, audio: false, seed: undefined });
  });

  it('hides the seed for a model that declares only sound', () => {
    openAdvanced({ ...VIDEO, seed: false }, CHEAPEST);
    expect(screen.queryByRole('spinbutton')).toBeNull();
    expect(screen.getByRole('switch', { name: 'Sound' })).toBeTruthy();
  });

  it('renders no pill for a model that declares neither', () => {
    render(
      <AdvancedPopover
        model={{ ...VIDEO, audio: false, seed: false }}
        options={CHEAPEST}
        onChange={vi.fn()}
      />,
    );
    expect(screen.queryByRole('button')).toBeNull();
  });
});
