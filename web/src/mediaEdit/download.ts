/** Hands `blob` to the browser as a download named `fileName`. The URL is revoked on the next
 *  task: revoking it synchronously can cancel the download in Firefox. */
export function downloadBlob(blob: Blob, fileName: string): void {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = fileName;
  anchor.click();
  setTimeout(() => {
    URL.revokeObjectURL(url);
  }, 0);
}
