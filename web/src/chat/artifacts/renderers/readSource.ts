export const MAX_SOURCE_BYTES = 256 * 1024;
export interface SourceText {
  readonly data: string;
  readonly truncated: boolean;
  readonly binary: boolean;
}

/** Stop network consumption before a large source becomes a large JS string. */
export async function readSource(response: Response): Promise<SourceText> {
  if (!response.body) return { data: '', truncated: false, binary: false };
  const reader = response.body.getReader();
  const bytes = new Uint8Array(MAX_SOURCE_BYTES);
  let length = 0;
  let truncated = false;
  try {
    for (;;) {
      const chunk = await reader.read();
      if (chunk.done) break;
      const remaining = MAX_SOURCE_BYTES - length;
      bytes.set(chunk.value.subarray(0, remaining), length);
      length += Math.min(chunk.value.length, remaining);
      if (chunk.value.length > remaining) {
        truncated = true;
        await reader.cancel();
        break;
      }
    }
  } finally {
    reader.releaseLock();
  }
  const held = bytes.subarray(0, length);
  const binary = held.some((byte) => byte === 0 || byte < 9 || (byte > 13 && byte < 32));
  try {
    const data = new TextDecoder('utf-8', { fatal: true }).decode(held, { stream: truncated });
    return { data: binary ? '' : data, truncated, binary };
  } catch {
    return { data: '', truncated, binary: true };
  }
}
