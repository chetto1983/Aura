import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import i18n from '../i18n/i18n';
import { StepStrip } from './StepStrip';

const steps = [
  { key: 'cloudflare', label: 'Cloudflare', state: 'done' },
  { key: 'domain', label: 'Dominio', state: 'active' },
  { key: 'connection', label: 'Collegamento', state: 'upcoming' },
  { key: 'phone', label: 'Prova dal telefono', state: 'upcoming' },
] as const;

const allDone = steps.map((step) => ({ ...step, state: 'done' as const }));

function item(name: string) {
  const row = screen.getAllByRole('listitem').find((li) => li.textContent.includes(name));
  if (row === undefined) throw new Error(`no step named ${name}`);
  return row;
}

describe('StepStrip', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('it');
  });
  afterEach(async () => {
    await i18n.changeLanguage('en');
  });

  it('makes a done step a button that names itself to onSelect', () => {
    const onSelect = vi.fn();
    render(<StepStrip steps={allDone} current="phone" onSelect={onSelect} label="Avanzamento" />);
    fireEvent.click(screen.getByRole('button', { name: /Dominio/ }));
    expect(onSelect).toHaveBeenCalledExactlyOnceWith('domain');
  });

  it('makes a failed step a button too, so a failure can be reopened', () => {
    const onSelect = vi.fn();
    render(
      <StepStrip
        steps={[
          { key: 'cloudflare', label: 'Cloudflare', state: 'failed' },
          { key: 'domain', label: 'Dominio', state: 'upcoming' },
        ]}
        current="domain"
        onSelect={onSelect}
        label="Avanzamento"
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /Cloudflare/ }));
    expect(onSelect).toHaveBeenCalledExactlyOnceWith('cloudflare');
  });

  it('never offers an upcoming step', () => {
    render(<StepStrip steps={steps} onSelect={vi.fn()} label="Avanzamento" />);
    expect(screen.queryByRole('button', { name: /Collegamento/ })).toBeNull();
    expect(screen.queryByRole('button', { name: /Prova dal telefono/ })).toBeNull();
  });

  // atmosphere.css gives every button `font: inherit`, unlayered, so a utility on the button itself
  // never applies: a done step rendered larger than the current one until the size moved up here.
  it('sizes the row, so a step that is a button reads the same as one that is not', () => {
    render(<StepStrip steps={steps} onSelect={vi.fn()} label="Avanzamento" />);
    expect(item('Cloudflare').className.split(' ')).toEqual(
      expect.arrayContaining(['text-[13px]', 'font-semibold']),
    );
  });

  it('has no buttons at all without onSelect, as in the onboarding', () => {
    render(<StepStrip steps={allDone} label="Avanzamento" />);
    expect(screen.queryAllByRole('button')).toHaveLength(0);
  });

  it('puts aria-current on the active step only', () => {
    render(<StepStrip steps={steps} onSelect={vi.fn()} label="Avanzamento" />);
    const current = screen.getAllByText((_t, el) => el?.getAttribute('aria-current') === 'step');
    expect(current.map((el) => el.textContent)).toEqual(['Dominio']);
    expect(screen.queryByRole('button', { name: /Dominio/ })).toBeNull();
  });

  it('marks the step on screen when every step is done, keeping its green dot', () => {
    render(<StepStrip steps={allDone} current="phone" onSelect={vi.fn()} label="Avanzamento" />);
    const current = screen.getAllByText((_t, el) => el?.getAttribute('aria-current') === 'step');
    expect(current.map((el) => el.textContent)).toEqual(['Prova dal telefono']);
    expect(item('Prova dal telefono').getAttribute('data-state')).toBe('done');
    expect(item('Prova dal telefono').querySelector('[aria-hidden="true"]')?.className).toContain(
      'bg-success',
    );
  });

  it('lets the active step be reached again when another step is on screen', () => {
    const onSelect = vi.fn();
    render(
      <StepStrip steps={steps} current="cloudflare" onSelect={onSelect} label="Avanzamento" />,
    );
    fireEvent.click(screen.getByRole('button', { name: /Dominio/ }));
    expect(onSelect).toHaveBeenCalledExactlyOnceWith('domain');
  });

  it('tells a failed step by more than its colour', () => {
    render(
      <StepStrip
        steps={[{ key: 'domain', label: 'Dominio', state: 'failed' }]}
        label="Avanzamento"
      />,
    );
    const row = item('Dominio');
    expect(row.getAttribute('data-state')).toBe('failed');
    expect(row.querySelector('svg')).not.toBeNull();
    expect(row.textContent).toContain('da sistemare');
  });

  it('names a done step to a screen reader', () => {
    render(<StepStrip steps={allDone} current="domain" label="Avanzamento" />);
    expect(item('Cloudflare').textContent).toContain('fatto');
  });

  it('at 390px shows "Passo 2 di 4 · Dominio" and hides the list', () => {
    render(<StepStrip steps={steps} label="Avanzamento" />);
    const list = screen.getByRole('list', { name: 'Avanzamento' });
    expect(list.className.split(' ')).toEqual(expect.arrayContaining(['hidden', 'lg:flex']));
    const progress = screen.getByText(/Passo 2 di 4/);
    expect(progress.textContent).toBe('Passo 2 di 4 · Dominio');
    expect(progress.className.split(' ')).toContain('lg:hidden');
    expect(within(progress).getByText('Dominio').className).toContain('text-text');
  });

  it('counts from the step on screen, not from the active one', () => {
    render(<StepStrip steps={allDone} current="connection" label="Avanzamento" />);
    expect(screen.getByText(/Passo 3 di 4/).textContent).toBe('Passo 3 di 4 · Collegamento');
  });

  it('counts in English too', async () => {
    await i18n.changeLanguage('en');
    render(<StepStrip steps={steps} label="Progress" />);
    expect(screen.getByText(/Step 2 of 4/).textContent).toBe('Step 2 of 4 · Dominio');
  });
});
