// hardenDocxLinks — the anchor-neutraliser applied to docx-preview's OUTPUT (H-01). DocxPreview
// is the single untrusted-render surface that runs in OUR origin without a sandbox, and
// docx-preview@0.4.0's renderHyperlink copies the OOXML external-relationship target into the
// anchor href VERBATIM with no scheme check. A prompt-injected agent can therefore deliver a
// .docx whose hyperlink target is `javascript:…`, which one user click would run with full
// session-cookie access. We strip every active-scheme href the library emitted and force
// external http(s) links to open isolated (noopener/noreferrer, new tab). Kept as a PURE
// function in its own module so it is unit-testable independently of the mocked docx-preview
// library — the reason the original suite missed this (renderAsync is mocked in renderers.test).
export function hardenDocxLinks(root: HTMLElement): void {
  root.querySelectorAll('a[href]').forEach((a) => {
    const href = a.getAttribute('href') ?? '';
    if (/^\s*(javascript|data|vbscript):/i.test(href)) {
      a.removeAttribute('href');
    } else if (/^\s*https?:/i.test(href)) {
      a.setAttribute('rel', 'noopener noreferrer');
      a.setAttribute('target', '_blank');
    }
  });
}
