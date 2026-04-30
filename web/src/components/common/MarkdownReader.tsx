import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

type MarkdownDocument = {
  body: string;
  meta: Record<string, string>;
};

function parseFrontmatter(markdown: string): MarkdownDocument {
  const text = String(markdown || "").replace(/^\uFEFF/, "");
  if (!text.startsWith("---")) return { body: text.trim(), meta: {} };

  const end = text.indexOf("\n---", 3);
  if (end === -1) return { body: text.trim(), meta: {} };

  const rawMeta = text.slice(3, end).trim();
  const body = text.slice(end + 4).trim();
  const meta: Record<string, string> = {};

  for (const line of rawMeta.split(/\r?\n/)) {
    const match = line.match(/^([A-Za-z0-9_-]+):\s*(.*)$/);
    if (!match) continue;
    const value = match[2].trim().replace(/^["']|["']$/g, "");
    if (value) meta[match[1]] = value;
  }

  return { body, meta };
}

export function MarkdownReader({ markdown }: { markdown: string }) {
  const doc = parseFrontmatter(markdown);

  if (!doc.body) {
    return (
      <div className="sacchi-md-empty">
        File Markdown senza contenuto leggibile.
      </div>
    );
  }

  return (
    <article className="sacchi-md-reader">
      {(doc.meta.type || doc.meta.updated) && (
        <div className="sacchi-md-reader__meta" aria-label="Metadati pagina">
          {doc.meta.type && <span>{doc.meta.type}</span>}
          {doc.meta.updated && <span>Aggiornata {doc.meta.updated}</span>}
        </div>
      )}
      <div className="md">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{doc.body}</ReactMarkdown>
      </div>
    </article>
  );
}
