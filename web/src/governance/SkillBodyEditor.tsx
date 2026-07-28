import { useEffect } from 'react';
import { EditorContent, useEditor, type Editor } from '@tiptap/react';
import StarterKit from '@tiptap/starter-kit';
import { Markdown } from 'tiptap-markdown';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';

// SkillBodyEditor — the rich-text surface for a skill body, on Tiptap.
//
// A skill on disk is MARKDOWN, and the file is the contract with the loader and with
// whoever wrote it by hand. So the editor is deliberately constrained to what markdown can
// express — headings, emphasis, lists, code, quotes, links — and nothing else. No colour,
// no fonts, no exotic tables: anything the serializer cannot round-trip would be silently
// dropped on save, and an editor that quietly discards what you typed is worse than a
// textarea.
//
// The document is held as markdown in the parent's state, not as HTML. Tiptap owns the
// editing session only; every keystroke is serialized straight back to markdown, so what
// the operator sees, what gets POSTed, and what lands in SKILL.md are one string.

export interface SkillBodyEditorProps {
  /** The markdown body. Only the INITIAL value seeds the editor (see the sync note). */
  readonly value: string;
  readonly onChange: (markdown: string) => void;
  readonly label: string;
}

export function SkillBodyEditor({ value, onChange, label }: SkillBodyEditorProps) {
  const { t } = useTranslation();

  const editor = useEditor({
    extensions: [
      // The heading levels a SKILL.md actually uses; h1 is the skill's own title, which
      // the frontmatter already owns.
      StarterKit.configure({ heading: { levels: [2, 3] } }),
      Markdown.configure({ html: false, breaks: false, transformPastedText: true }),
    ],
    content: value,
    editorProps: {
      attributes: {
        'aria-label': label,
        class:
          'min-h-[18rem] max-w-[70ch] px-6 py-5 outline-none text-[14.5px] leading-relaxed [&_h2]:mb-1.5 [&_h2]:mt-5 [&_h2]:font-display [&_h2]:text-[19px] [&_h2]:font-semibold [&_h2]:text-text [&_h3]:mb-1 [&_h3]:mt-4 [&_h3]:font-semibold [&_h3]:text-text [&_p]:my-2 [&_p]:text-text-muted [&_li]:my-1 [&_li]:text-text-muted [&_ul]:my-2 [&_ul]:list-disc [&_ul]:pl-5 [&_ol]:my-2 [&_ol]:list-decimal [&_ol]:pl-5 [&_code]:rounded-[5px] [&_code]:border [&_code]:border-border [&_code]:bg-surface-2 [&_code]:px-1.5 [&_code]:font-mono [&_code]:text-[12.5px] [&_pre]:my-2 [&_pre]:overflow-x-auto [&_pre]:rounded-md [&_pre]:border [&_pre]:border-border [&_pre]:bg-surface-2 [&_pre]:p-3 [&_blockquote]:my-3 [&_blockquote]:border-l-[3px] [&_blockquote]:border-accent-muted [&_blockquote]:bg-surface-2 [&_blockquote]:px-3.5 [&_blockquote]:py-2 [&_blockquote]:text-text-muted',
      },
    },
    onUpdate: ({ editor: instance }) => {
      onChange(readMarkdown(instance));
    },
  });

  // Seed the editor when the parent swaps document (new skill vs editing an existing one).
  // It deliberately does NOT mirror every parent change back in: the parent's value comes
  // FROM onUpdate, so echoing it would reset the selection on each keystroke.
  useEffect(() => {
    if (editor.isDestroyed) {
      return;
    }
    if (readMarkdown(editor).trim() === value.trim()) {
      return;
    }
    editor.commands.setContent(value, { emitUpdate: false });
    // Only when the document identity changes — value is intentionally read, not watched.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editor]);

  return (
    <div className="flex min-w-0 flex-col">
      <SkillEditorToolbar editor={editor} />
      <EditorContent editor={editor} className="min-w-0" />
      <p className="px-6 pb-3 font-mono text-[11px] tracking-[0.04em] text-text-muted">
        {t('governance.skills.editor.markdownHint')}
      </p>
    </div>
  );
}

/** Serialize the live document back to markdown — the only representation we persist. */
function readMarkdown(editor: Editor): string {
  const storage = editor.storage as { markdown?: { getMarkdown?: () => string } };
  return storage.markdown?.getMarkdown?.() ?? editor.getText();
}

/** The formatting toolbar. Every button maps to a markdown construct — nothing here can
 * produce something the serializer would drop. */
function SkillEditorToolbar({ editor }: { readonly editor: Editor }) {
  const { t } = useTranslation();

  const items = [
    {
      key: 'h2',
      label: 'H2',
      run: () => editor.chain().focus().toggleHeading({ level: 2 }).run(),
      active: () => editor.isActive('heading', { level: 2 }),
    },
    {
      key: 'bold',
      label: 'B',
      run: () => editor.chain().focus().toggleBold().run(),
      active: () => editor.isActive('bold'),
    },
    {
      key: 'italic',
      label: 'I',
      run: () => editor.chain().focus().toggleItalic().run(),
      active: () => editor.isActive('italic'),
    },
    {
      key: 'bulletList',
      label: '•',
      run: () => editor.chain().focus().toggleBulletList().run(),
      active: () => editor.isActive('bulletList'),
    },
    {
      key: 'orderedList',
      label: '1.',
      run: () => editor.chain().focus().toggleOrderedList().run(),
      active: () => editor.isActive('orderedList'),
    },
    {
      key: 'code',
      label: '</>',
      run: () => editor.chain().focus().toggleCode().run(),
      active: () => editor.isActive('code'),
    },
    {
      key: 'codeBlock',
      label: '{ }',
      run: () => editor.chain().focus().toggleCodeBlock().run(),
      active: () => editor.isActive('codeBlock'),
    },
    {
      key: 'blockquote',
      label: '❝',
      run: () => editor.chain().focus().toggleBlockquote().run(),
      active: () => editor.isActive('blockquote'),
    },
  ];

  return (
    <div
      role="toolbar"
      aria-label={t('governance.skills.editor.toolbarLabel')}
      className="flex flex-wrap items-center gap-1 border-b border-border bg-surface-2 px-3 py-1.5"
    >
      {items.map((item) => {
        const on = item.active();
        const label = t(`governance.skills.editor.format.${item.key}`);
        return (
          <Button
            key={item.key}
            type="button"
            variant="ghost"
            size="sm"
            aria-label={label}
            aria-pressed={on}
            title={label}
            onClick={item.run}
            className={`h-8 min-w-8 px-2 font-mono text-[13px] ${
              on ? 'bg-surface-3 text-text' : 'text-text-muted'
            }`}
          >
            {item.label}
          </Button>
        );
      })}
    </div>
  );
}
