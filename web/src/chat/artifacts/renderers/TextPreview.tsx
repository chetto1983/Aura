import { extOf } from '../artifactMeta';
import { HighlightedSource } from './HighlightedSource';
import type { RendererProps } from './PreviewStatus';

// text/* + the code/data extension family (D-07): the raw bytes, React-escaped and highlighted
// by the file's extension when Shiki has that grammar. Never parsed as markdown or HTML, so
// there is zero injection surface (T-37B-10).
export default function TextPreview({ assetId, fileName }: RendererProps) {
  return (
    <HighlightedSource
      assetId={assetId}
      fileName={fileName}
      lang={extOf(fileName) || fileName}
      wrap
    />
  );
}
