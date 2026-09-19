import { useState } from 'react';
import { useObjectUrl } from '@/mediaEdit/useObjectUrl';

// The picture itself, for the two places an image attachment is shown.
//
// assistant-ui ships AttachmentPrimitive.unstable_Thumb, which sounds like this and is not:
// it renders the file EXTENSION as text (".png") in a div. It gives the slot, not the image,
// so the <img> is ours either way — the primitive is still used for Root and Remove, where
// it does carry behaviour.
//
// The two call sites differ only in where the bytes are:
//
//   composer   the File is in the browser and the upload may not have finished, so the
//              preview comes from an object URL and must be revoked or the blob leaks for
//              the life of the tab
//   sent turn  the bytes are on the server, addressed by asset id through the same
//              same-origin GET /api/assets/{id}/download the artifact row already uses
//              (session cookie, no presigned URL to mint or expire)
//
// A broken image is rendered as nothing rather than as the browser's torn-page glyph: the
// filename is always beside it, so failing to a name is honest, while a broken-image icon
// reads as damage to the file the operator just attached.

export function LocalImagePreview({
  file,
  className,
}: {
  readonly file: File;
  readonly className?: string | undefined;
}) {
  const url = useObjectUrl(file);
  return url === undefined ? null : (
    <AttachmentImage src={url} alt={file.name} className={className} />
  );
}

export function RemoteImagePreview({
  assetId,
  fileName,
  className,
}: {
  readonly assetId: string;
  readonly fileName: string;
  readonly className?: string | undefined;
}) {
  return (
    <AttachmentImage
      src={`/api/assets/${encodeURIComponent(assetId)}/download`}
      alt={fileName}
      className={className}
    />
  );
}

function AttachmentImage({
  src,
  alt,
  className,
}: {
  readonly src: string;
  readonly alt: string;
  readonly className?: string | undefined;
}) {
  const [broken, setBroken] = useState(false);
  if (broken) return null;
  return (
    <img
      src={src}
      alt={alt}
      loading="lazy"
      decoding="async"
      onError={() => {
        setBroken(true);
      }}
      className={className}
    />
  );
}
