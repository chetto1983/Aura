// composerAttach — how a file reaches the chat composer from outside its runtime. The Artifacts
// panel's preview is mounted by the shell beside the chat, not inside its AssistantRuntimeProvider,
// so it cannot call aui.composer itself; the open Composer registers its addAttachment here, and an
// edited artifact goes into the next message through it (the operator's decision of 2026-10-27).

type Attach = (file: File) => Promise<void>;

let current: Attach | undefined;

/** Registers the open composer's attach; the returned close removes it unless a newer one won. */
export function registerComposerAttach(attach: Attach): () => void {
  current = attach;
  return () => {
    if (current === attach) current = undefined;
  };
}

/** Attaches `file` to the open chat composer's next message. */
export async function attachToComposer(file: File): Promise<void> {
  if (current === undefined) throw new Error('no chat composer is open to attach the file to');
  await current(file);
}
