import { lazy, type ReactNode } from 'react';
import type { PreviewKind } from '../artifactMeta';
import type { RendererProps } from './PreviewStatus';

// previewDispatch — the one place a PreviewKind becomes a renderer. The cockpit modal and the
// share page previewed the same assets through two identical switches; a seventh kind (video)
// made them long enough for the duplication gate to fail, which is the gate doing its job:
// a kind added to one and forgotten in the other is a silent blank frame on that surface.
//
// Each renderer keeps its own lazy boundary (D-08): the dynamic import() holds its bytes — and
// docx/xlsx's heavy parsers — out of the importing chunk until an asset of that kind is opened.
// Callers differ only in what "no safe renderer" looks like, so that arm is a prop. This is a
// component rather than a plain function because the file defines the lazy renderers, and a
// module that exports a non-component alongside them loses fast refresh (only-export-components,
// the same rule assetSourceContext.ts documents).

const ImagePreview = lazy(() => import('./ImagePreview'));
const PdfPreview = lazy(() => import('./PdfPreview'));
const TextPreview = lazy(() => import('./TextPreview'));
const HtmlPreview = lazy(() => import('./HtmlPreview'));
const DocxPreview = lazy(() => import('./DocxPreview'));
const XlsxPreview = lazy(() => import('./XlsxPreview'));
const VideoPreview = lazy(() => import('./VideoPreview'));

export interface PreviewByKindProps {
  readonly kind: PreviewKind;
  readonly asset: RendererProps;
  /** What to show for a kind no renderer may open — SVG chief among them. */
  readonly downloadFallback: ReactNode;
}

export function PreviewByKind({ kind, asset, downloadFallback }: PreviewByKindProps): ReactNode {
  switch (kind) {
    case 'image':
      return <ImagePreview {...asset} />;
    case 'pdf':
      return <PdfPreview {...asset} />;
    case 'text':
      return <TextPreview {...asset} />;
    case 'html':
      return <HtmlPreview {...asset} />;
    case 'docx':
      return <DocxPreview {...asset} />;
    case 'xlsx':
      return <XlsxPreview {...asset} />;
    case 'video':
      return <VideoPreview {...asset} />;
    case 'download':
      return downloadFallback;
  }
}
