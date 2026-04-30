/** @ts-nocheck */
/**
 * Full-screen wiki editor — Tiptap WYSIWYG with markdown I/O.
 *
 * Loads a wiki page's markdown body, edits in rich-text, serializes back to
 * markdown, and POSTs to /api/tools/wiki/save. The save round-trip respects
 * the agent's existing wiki invariants (frontmatter preserved, S3 dual-write
 * via storage layer, graph cache refresh).
 *
 * UX:
 *   - Mounted as fixed overlay at z-50; ESC closes; Cmd+S saves.
 *   - Toolbar: Save / Cancel + bold/italic/headings/lists/code/quote/link.
 *   - Body uses Tailwind Typography `prose` classes for readable rendering.
 *   - Editor content type is markdown (Tiptap markdown extension, beta).
 *
 * Frontmatter: the agent's wiki convention requires YAML frontmatter on every
 * page. The editor preserves whatever frontmatter is present in the loaded
 * content, BUT it can't render YAML inside Tiptap. We split the page on the
 * second `---\n`: everything before stays untouched in `_frontmatter`, only
 * the body is editable, then re-concatenated on save. If the page has no
 * frontmatter the whole content is editable.
 */
import { useEditor, EditorContent } from '@tiptap/react';
import StarterKit from '@tiptap/starter-kit';
import Typography from '@tiptap/extension-typography';
import Link from '@tiptap/extension-link';
import { Markdown } from '@tiptap/markdown';
import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';

interface WikiEditorProps {
  pagePath: string;          // e.g. "concepts/sacchi-categories/barriera-sicurezza.md"
  initialMarkdown: string;   // full file body (with frontmatter)
  onClose: () => void;
  onSaved?: (newBody: string) => void;
}

const FRONTMATTER_RE = /^(---\r?\n[\s\S]*?\r?\n---\r?\n)([\s\S]*)$/;

function splitFrontmatter(raw: string): { frontmatter: string; body: string } {
  const m = raw.match(FRONTMATTER_RE);
  if (m) return { frontmatter: m[1], body: m[2] };
  return { frontmatter: '', body: raw };
}

export function WikiEditor({ pagePath, initialMarkdown, onClose, onSaved }: WikiEditorProps) {
  const { frontmatter, body } = splitFrontmatter(initialMarkdown);
  const frontmatterRef = useRef(frontmatter);
  const frontmatterLength = frontmatter.length;
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const editor = useEditor({
    immediatelyRender: false,
    extensions: [
      StarterKit.configure({
        heading: { levels: [1, 2, 3, 4] },
      }),
      Typography,
      Link.configure({ openOnClick: false, HTMLAttributes: { class: 'text-emerald-700 underline' } }),
      Markdown,
    ],
    content: body,
    contentType: 'markdown',
    editorProps: {
      attributes: {
        class: 'prose prose-sm sm:prose-base lg:prose-lg max-w-none focus:outline-none px-8 py-6 min-h-full',
      },
    },
  });

  const save = useCallback(async () => {
    if (!editor || saving) return;
    setSaving(true);
    setError(null);
    try {
      const editedBody: string = editor.getMarkdown ? editor.getMarkdown() : editor.getText();
      const fullContent = (frontmatterRef.current || '') + editedBody;
      const res = await fetch('/api/tools/wiki/save', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          rel_path: pagePath,
          content: fullContent,
          mode: 'replace',
          refresh_graph: true,
        }),
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(`HTTP ${res.status}: ${text.slice(0, 200)}`);
      }
      const json = await res.json();
      toast.success(`Salvato: ${json.path || pagePath} (${json.bytes ?? '?'} byte)`);
      onSaved?.(fullContent);
      onClose();
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      setError(msg);
      toast.error(`Errore salvataggio: ${msg}`);
    } finally {
      setSaving(false);
    }
  }, [editor, onClose, onSaved, pagePath, saving]);

  // Keyboard: ESC to close, Cmd/Ctrl+S to save
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
      } else if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 's') {
        e.preventDefault();
        save();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose, save]);

  if (!editor) {
    return null;
  }

  return (
    <div className="fixed inset-0 z-50 flex flex-col bg-white" role="dialog" aria-label={`Modifica wiki: ${pagePath}`}>
      <header className="flex items-center justify-between border-b border-emerald-200 bg-emerald-50 px-6 py-3">
        <div className="flex flex-col">
          <span className="text-xs font-medium uppercase tracking-wide text-emerald-700">Wiki editor</span>
          <span className="font-mono text-sm text-slate-700">{pagePath}</span>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={onClose}
            disabled={saving}
            className="rounded border border-slate-300 bg-white px-4 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-50"
          >Annulla (Esc)</button>
          <button
            type="button"
            onClick={save}
            disabled={saving}
            className="rounded bg-emerald-600 px-4 py-1.5 text-sm font-medium text-white hover:bg-emerald-700 disabled:opacity-50"
          >{saving ? 'Salvataggio…' : 'Salva (Ctrl+S)'}</button>
        </div>
      </header>

      <Toolbar editor={editor} />

      {error && (
        <div role="alert" className="border-b border-red-200 bg-red-50 px-6 py-2 text-sm text-red-700">
          {error}
        </div>
      )}

      <div className="flex-1 overflow-auto bg-white">
        <EditorContent editor={editor} className="mx-auto max-w-4xl" />
      </div>

      {frontmatterLength > 0 && (
        <footer className="border-t border-slate-200 bg-slate-50 px-6 py-2 text-xs text-slate-500">
          Frontmatter YAML preservato automaticamente ({frontmatterLength} byte non editabili).
        </footer>
      )}
    </div>
  );
}

interface ToolbarProps { editor: ReturnType<typeof useEditor> }

function Toolbar({ editor }: ToolbarProps) {
  if (!editor) return null;
  const btn = (active: boolean, onClick: () => void, label: string, shortcut?: string) => (
    <button
      type="button"
      onClick={onClick}
      title={shortcut}
      className={`rounded border px-2 py-1 text-sm ${active ? 'border-emerald-600 bg-emerald-100 text-emerald-900' : 'border-slate-300 bg-white text-slate-700 hover:bg-slate-50'}`}
    >{label}</button>
  );
  return (
    <div className="flex flex-wrap items-center gap-1 border-b border-slate-200 bg-slate-50 px-4 py-2">
      {btn(editor.isActive('bold'), () => editor.chain().focus().toggleBold().run(), 'B', 'Bold (Ctrl+B)')}
      {btn(editor.isActive('italic'), () => editor.chain().focus().toggleItalic().run(), 'I', 'Italic (Ctrl+I)')}
      {btn(editor.isActive('strike'), () => editor.chain().focus().toggleStrike().run(), 'S', 'Strike')}
      <span className="mx-1 h-5 w-px bg-slate-300" />
      {btn(editor.isActive('heading', { level: 1 }), () => editor.chain().focus().toggleHeading({ level: 1 }).run(), 'H1')}
      {btn(editor.isActive('heading', { level: 2 }), () => editor.chain().focus().toggleHeading({ level: 2 }).run(), 'H2')}
      {btn(editor.isActive('heading', { level: 3 }), () => editor.chain().focus().toggleHeading({ level: 3 }).run(), 'H3')}
      <span className="mx-1 h-5 w-px bg-slate-300" />
      {btn(editor.isActive('bulletList'), () => editor.chain().focus().toggleBulletList().run(), '• Lista')}
      {btn(editor.isActive('orderedList'), () => editor.chain().focus().toggleOrderedList().run(), '1. Lista')}
      {btn(editor.isActive('blockquote'), () => editor.chain().focus().toggleBlockquote().run(), '“ Cita')}
      {btn(editor.isActive('codeBlock'), () => editor.chain().focus().toggleCodeBlock().run(), '</> Code')}
      {btn(editor.isActive('code'), () => editor.chain().focus().toggleCode().run(), '`code`')}
      <span className="mx-1 h-5 w-px bg-slate-300" />
      <button
        type="button"
        onClick={() => {
          const url = window.prompt('URL del link:');
          if (url) editor.chain().focus().setLink({ href: url }).run();
          else editor.chain().focus().unsetLink().run();
        }}
        className="rounded border border-slate-300 bg-white px-2 py-1 text-sm text-slate-700 hover:bg-slate-50"
      >🔗 Link</button>
      <button
        type="button"
        onClick={() => editor.chain().focus().undo().run()}
        disabled={!editor.can().undo()}
        className="rounded border border-slate-300 bg-white px-2 py-1 text-sm text-slate-700 hover:bg-slate-50 disabled:opacity-40"
      >↶</button>
      <button
        type="button"
        onClick={() => editor.chain().focus().redo().run()}
        disabled={!editor.can().redo()}
        className="rounded border border-slate-300 bg-white px-2 py-1 text-sm text-slate-700 hover:bg-slate-50 disabled:opacity-40"
      >↷</button>
    </div>
  );
}

export default WikiEditor;
