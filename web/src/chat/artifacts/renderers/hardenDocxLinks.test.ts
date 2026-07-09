import { describe, expect, it } from 'vitest';
import { hardenDocxLinks } from './hardenDocxLinks';

// H-01 regression: docx-preview@0.4.0 copies the OOXML external-relationship target into the
// anchor href VERBATIM (no scheme check), and DocxPreview renders it into our unsandboxed,
// same-origin DOM. hardenDocxLinks is the neutraliser; these tests exercise the REAL scrub
// against a plain DOM fixture (not the mocked library) so the security invariant is pinned.
describe('hardenDocxLinks (H-01)', () => {
  function fixture(): HTMLElement {
    const root = document.createElement('div');
    root.innerHTML = [
      '<a id="js" href="javascript:alert(1)">js</a>',
      '<a id="jsSpaced" href="  JavaScript:alert(1)">js-spaced</a>',
      '<a id="data" href="data:text/html,<script>alert(1)</script>">data</a>',
      '<a id="vbs" href="vbscript:msgbox(1)">vbs</a>',
      '<a id="https" href="https://evil.example/x">https</a>',
      '<a id="http" href="http://evil.example/x">http</a>',
      '<a id="rel" href="/api/x">relative</a>',
    ].join('');
    return root;
  }

  it('removes the href from javascript:/data:/vbscript: anchors (case- and whitespace-insensitive)', () => {
    const root = fixture();
    hardenDocxLinks(root);
    for (const id of ['js', 'jsSpaced', 'data', 'vbs']) {
      const a = root.querySelector(`#${id}`);
      expect(a?.hasAttribute('href')).toBe(false);
    }
  });

  it('hardens external http(s) links with rel=noopener noreferrer and target=_blank', () => {
    const root = fixture();
    hardenDocxLinks(root);
    for (const id of ['https', 'http']) {
      const a = root.querySelector(`#${id}`);
      expect(a?.getAttribute('rel')).toBe('noopener noreferrer');
      expect(a?.getAttribute('target')).toBe('_blank');
      // The href itself stays intact — external links remain navigable, just isolated.
      expect(a?.getAttribute('href')).toContain('evil.example');
    }
  });

  it('leaves a same-origin relative link untouched', () => {
    const root = fixture();
    hardenDocxLinks(root);
    const a = root.querySelector('#rel');
    expect(a?.getAttribute('href')).toBe('/api/x');
    expect(a?.hasAttribute('rel')).toBe(false);
    expect(a?.hasAttribute('target')).toBe(false);
  });

  it('is a no-op on a root with no anchors', () => {
    const root = document.createElement('div');
    root.innerHTML = '<p>no links here</p>';
    expect(() => {
      hardenDocxLinks(root);
    }).not.toThrow();
  });
});
