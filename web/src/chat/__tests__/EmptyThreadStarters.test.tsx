import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import i18n from '../../i18n/i18n';
import { EmptyThreadStarters } from '../EmptyThreadStarters';

const COPY = {
  en: [
    ['Research a topic', 'Research [topic] and compare the most reliable sources.'],
    ['Analyze a file', "Analyze the file I'll attach and summarize the key findings."],
    ['Create an artifact', 'Create a [report/table/document] about [topic].'],
    ['Automate a task', 'Help me plan and automate [repeatable task].'],
  ],
  it: [
    ['Cerca un argomento', 'Cerca [argomento] e confronta le fonti più affidabili.'],
    ['Analizza un file', 'Analizza il file che allegherò e riassumi i risultati principali.'],
    ['Crea un artefatto', 'Crea un [rapporto/tabella/documento] su [argomento].'],
    ["Automatizza un'attività", 'Aiutami a pianificare e automatizzare [attività ripetitiva].'],
  ],
} as const;

afterEach(async () => {
  await act(async () => {
    await i18n.changeLanguage('en');
  });
});

describe('EmptyThreadStarters', () => {
  it.each([
    ['en', 'Suggestions'],
    ['it', 'Suggerimenti'],
  ] as const)(
    'exposes the exact %s starter actions as an accessible group',
    async (language, name) => {
      await i18n.changeLanguage(language);
      const requestDraft = vi.fn();
      render(<EmptyThreadStarters onRequestDraftPrompt={requestDraft} />);

      const group = screen.getByRole('group', { name });
      const buttons = within(group).getAllByRole('button');
      expect(buttons.map((button) => button.textContent)).toEqual(
        COPY[language].map(([label]) => label),
      );
      expect(group.className).toContain('grid-cols-1');
      expect(group.className).toContain('sm:grid-cols-2');

      COPY[language].forEach(([label, body], index) => {
        const button = buttons[index];
        expect(button?.getAttribute('type')).toBe('button');
        expect(button?.className).toContain('min-h-11');
        expect(button?.className).toContain('min-w-0');
        expect(button?.className).toContain('whitespace-normal');
        expect(button?.className).toContain('text-start');
        fireEvent.click(screen.getByRole('button', { name: label }));
        expect(requestDraft).toHaveBeenLastCalledWith(body);
      });
    },
  );
});
