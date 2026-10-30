import { useState, type ComponentProps } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { HttpError } from '../../../api/json';
import i18n from '../../../i18n/i18n';
import { DomainStep } from '../DomainStep';
import {
  RemoteAccessError,
  type RemoteAccessConfiguration,
  type RemoteAccessReason,
  type RemoteAccessStatusDTO,
} from '../remoteAccessApi';
import { NO_DRAFT, type DomainDraft } from '../remoteAccessSteps';

const status: RemoteAccessStatusDTO = {
  enabled: false,
  phase: 'disabled',
  api_token_set: true,
  account_id: 'account',
  account_name: 'Acme',
  generation: 7,
  nameservers: [],
  reason: '',
  accepted: false,
  reachable: false,
  has_resources: false,
  abandoned: [],
};

const addressed: RemoteAccessStatusDTO = {
  ...status,
  enabled: true,
  zone_name: 'example.com',
  public_label: 'casa',
  public_hostname: 'casa.example.com',
  has_resources: true,
};

const refused = (code: number, reason: RemoteAccessReason) =>
  new RemoteAccessError(new HttpError(code, 'refused'), reason);

// The wizard holds the draft, so that it outlives this card; here a test stands in for it.
function Holder(props: Omit<ComponentProps<typeof DomainStep>, 'draft' | 'onDraft'>) {
  const [draft, setDraft] = useState<DomainDraft>(NO_DRAFT);
  return <DomainStep draft={draft} onDraft={setDraft} {...props} />;
}

function renderStep(current: RemoteAccessStatusDTO, onSave = vi.fn(() => Promise.resolve())) {
  const props = {
    onSave,
    onBack: vi.fn(),
    onNext: vi.fn(),
    onOpenStep: vi.fn(),
  };
  render(<Holder status={current} {...props} />);
  return props;
}

const domain = () => screen.getByLabelText<HTMLInputElement>('Dominio');
const avanti = () => screen.getByRole('button', { name: 'Avanti' });

describe('DomainStep', () => {
  beforeEach(async () => {
    await i18n.changeLanguage('it');
  });
  afterEach(async () => {
    await i18n.changeLanguage('en');
  });

  it('an empty domain disables Avanti and says why beside it', () => {
    const { onSave } = renderStep(status);
    expect(avanti().hasAttribute('disabled')).toBe(true);
    expect(screen.getByText('Scrivi il dominio per continuare').textContent).toBeTruthy();
    fireEvent.click(avanti());
    expect(onSave).not.toHaveBeenCalled();
  });

  it('a blank label says so too', () => {
    renderStep(addressed);
    fireEvent.change(screen.getByLabelText(/Etichetta/), { target: { value: ' ' } });
    expect(avanti().hasAttribute('disabled')).toBe(true);
    expect(screen.getByText('Scrivi l’etichetta per continuare').textContent).toBeTruthy();
  });

  it('holds the current address and shows where Aura will open', () => {
    renderStep(addressed);
    expect(domain().value).toBe('example.com');
    expect(screen.getByLabelText<HTMLInputElement>(/Etichetta/).value).toBe('casa');
    expect(screen.getByText('https://casa.example.com')).toBeTruthy();
  });

  it('offers aura as the label of a first address', () => {
    renderStep(status);
    expect(screen.getByLabelText<HTMLInputElement>(/Etichetta/).value).toBe('aura');
  });

  it('saves the address with enabled true and its generation', async () => {
    const { onSave } = renderStep(status);
    fireEvent.change(domain(), { target: { value: ' Example.COM ' } });
    expect(avanti().hasAttribute('disabled')).toBe(false);
    fireEvent.click(avanti());
    await waitFor(() => {
      expect(onSave).toHaveBeenCalledExactlyOnceWith({
        enabled: true,
        generation: 7,
        account_id: 'account',
        zone_name: 'example.com',
        public_label: 'aura',
      } satisfies RemoteAccessConfiguration);
    });
  });

  it('keeps the input on a stale generation', async () => {
    const onSave = vi.fn(() => Promise.reject(refused(409, '')));
    renderStep(status, onSave);
    fireEvent.change(domain(), { target: { value: 'nuovo.example.com' } });
    fireEvent.click(avanti());
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('cambiata nel frattempo');
    expect(alert.textContent).not.toContain('409');
    expect(domain().value).toBe('nuovo.example.com');
    expect(avanti().hasAttribute('disabled')).toBe(false);
  });

  it('marks the domain invalid only when the server said so', async () => {
    const onSave = vi.fn(() => Promise.reject(refused(400, 'domain_invalid')));
    renderStep(status, onSave);
    expect(domain().hasAttribute('aria-invalid')).toBe(false);
    fireEvent.change(domain(), { target: { value: 'nope' } });
    fireEvent.click(avanti());
    await screen.findByRole('alert');
    expect(domain().getAttribute('aria-invalid')).toBe('true');
    fireEvent.change(domain(), { target: { value: 'nope.it' } });
    expect(domain().hasAttribute('aria-invalid')).toBe(false);
  });

  it('a reason names its action, and the action hands the field back', () => {
    renderStep({ ...addressed, phase: 'error', reason: 'domain_elsewhere' });
    fireEvent.click(screen.getByRole('button', { name: 'Usa un altro dominio' }));
    expect(document.activeElement).toBe(domain());
  });

  it('a taken hostname hands back the label, which is what has to change', () => {
    renderStep({ ...addressed, phase: 'error', reason: 'hostname_taken' });
    fireEvent.click(screen.getByRole('button', { name: 'Scegli un’altra etichetta' }));
    expect(document.activeElement).toBe(screen.getByLabelText(/Etichetta/));
  });

  it('a save refused for a dead token opens Cloudflare', async () => {
    const onSave = vi.fn(() => Promise.reject(refused(400, 'token_rejected')));
    const { onOpenStep } = renderStep(status, onSave);
    fireEvent.change(domain(), { target: { value: 'example.com' } });
    fireEvent.click(avanti());
    fireEvent.click(await screen.findByRole('button', { name: 'Incolla un token nuovo' }));
    expect(onOpenStep).toHaveBeenCalledExactlyOnceWith('cloudflare');
  });

  it('a save that could not reach Cloudflare can be sent again from its notice', async () => {
    const onSave = vi
      .fn<() => Promise<void>>()
      .mockRejectedValueOnce(refused(502, 'cloudflare_unreachable'))
      .mockResolvedValueOnce(undefined);
    renderStep(status, onSave);
    fireEvent.change(domain(), { target: { value: 'example.com' } });
    fireEvent.click(avanti());
    fireEvent.click(await screen.findByRole('button', { name: 'Riprova ora' }));
    await waitFor(() => {
      expect(onSave).toHaveBeenCalledTimes(2);
    });
  });

  it('waiting_nameservers lists both nameservers in mono', () => {
    renderStep({
      ...addressed,
      phase: 'waiting_nameservers',
      nameservers: ['ada.ns.cloudflare.com', 'bob.ns.cloudflare.com'],
    });
    for (const name of ['ada.ns.cloudflare.com', 'bob.ns.cloudflare.com']) {
      expect(screen.getByText(name).className).toContain('font-mono');
    }
  });

  it('with no nameservers to show it says where to read them, and never “questi due”', () => {
    renderStep({ ...addressed, phase: 'waiting_nameservers' });
    expect(screen.queryByText(/questi due/)).toBeNull();
    expect(screen.queryByRole('list')).toBeNull();
    expect(screen.getByText(/non riesce a leggere i nameserver/).textContent).toContain(
      'pagina Overview',
    );
  });

  it('with nameservers to show it never says it could not read them', () => {
    renderStep({ ...addressed, phase: 'waiting_nameservers', nameservers: ['ada.ns'] });
    expect(screen.getByText(/questi due/)).toBeTruthy();
    expect(screen.queryByText(/non riesce a leggere i nameserver/)).toBeNull();
  });

  it('a draft shows over the saved address, and an untouched field follows it', () => {
    const props = { onSave: vi.fn(), onBack: vi.fn(), onNext: vi.fn(), onOpenStep: vi.fn() };
    render(
      <DomainStep
        status={addressed}
        draft={{ zone: 'bozza.it', label: null }}
        onDraft={vi.fn()}
        {...props}
      />,
    );
    expect(domain().value).toBe('bozza.it');
    expect(screen.getByLabelText<HTMLInputElement>(/Etichetta/).value).toBe('casa');
  });

  it('a keystroke hands the whole draft up, leaving the other field as it was', () => {
    const onDraft = vi.fn();
    const props = { onSave: vi.fn(), onBack: vi.fn(), onNext: vi.fn(), onOpenStep: vi.fn() };
    render(<DomainStep status={addressed} draft={NO_DRAFT} onDraft={onDraft} {...props} />);
    fireEvent.change(domain(), { target: { value: 'nuovo.it' } });
    expect(onDraft).toHaveBeenCalledExactlyOnceWith({ zone: 'nuovo.it', label: null });
    fireEvent.change(screen.getByLabelText(/Etichetta/), { target: { value: 'tana' } });
    expect(onDraft).toHaveBeenLastCalledWith({ zone: null, label: 'tana' });
  });

  it('while the zone waits, Avanti says the wait is not the operator’s to end', () => {
    renderStep({ ...addressed, phase: 'waiting_nameservers' });
    expect(avanti().hasAttribute('disabled')).toBe(true);
    expect(screen.getByText(/il passo successivo si apre da solo/)).toBeTruthy();
  });

  it('a wrong domain can still be replaced while the zone waits', async () => {
    const { onSave } = renderStep({ ...addressed, phase: 'waiting_nameservers' });
    fireEvent.change(domain(), { target: { value: 'giusto.it' } });
    expect(avanti().hasAttribute('disabled')).toBe(false);
    fireEvent.click(avanti());
    await waitFor(() => {
      expect(onSave).toHaveBeenCalledOnce();
    });
  });

  it('warns that changing a live address retires the old one and repeats the phone test', () => {
    renderStep({ ...addressed, phase: 'healthy', accepted: true });
    expect(screen.queryByText(/smette di funzionare/)).toBeNull();
    fireEvent.change(domain(), { target: { value: 'altro.it' } });
    expect(screen.getByText(/smette di funzionare/)).toBeTruthy();
  });

  it('with nothing changed on a working install, Avanti just walks on', () => {
    const { onSave, onNext } = renderStep({ ...addressed, phase: 'healthy', accepted: true });
    fireEvent.click(avanti());
    expect(onNext).toHaveBeenCalledOnce();
    expect(onSave).not.toHaveBeenCalled();
  });

  it('a failed row saves again even with nothing changed: that is the retry', async () => {
    const { onSave, onNext } = renderStep({
      ...addressed,
      phase: 'error',
      reason: 'domain_elsewhere',
    });
    fireEvent.click(avanti());
    await waitFor(() => {
      expect(onSave).toHaveBeenCalledOnce();
    });
    expect(onNext).not.toHaveBeenCalled();
  });

  it('a switched-off row says it is off, and Avanti turns it back on', async () => {
    const { onSave } = renderStep({ ...addressed, enabled: false });
    expect(screen.getByText(/L’accesso remoto è spento/)).toBeTruthy();
    fireEvent.click(avanti());
    await waitFor(() => {
      expect(onSave).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ enabled: true }));
    });
  });

  it('while Aura deletes, it says a save will need one more press', () => {
    renderStep({ ...addressed, enabled: false, phase: 'deleting' });
    expect(screen.getByText(/premi Avanti ancora una volta/)).toBeTruthy();
    expect(avanti().hasAttribute('disabled')).toBe(false);
  });

  it('Indietro walks back', () => {
    const { onBack } = renderStep(addressed);
    fireEvent.click(screen.getByRole('button', { name: 'Indietro' }));
    expect(onBack).toHaveBeenCalledOnce();
  });

  it('holds Avanti while the save runs', async () => {
    let finish: () => void = () => undefined;
    const onSave = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    renderStep(status, onSave);
    fireEvent.change(domain(), { target: { value: 'example.com' } });
    fireEvent.click(avanti());
    await waitFor(() => {
      expect(avanti().getAttribute('aria-busy')).toBe('true');
    });
    fireEvent.click(avanti());
    expect(onSave).toHaveBeenCalledOnce();
    finish();
    await waitFor(() => {
      expect(avanti().hasAttribute('aria-busy')).toBe(false);
    });
  });
});
