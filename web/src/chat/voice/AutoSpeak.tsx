import { useAutoSpeak } from './useAutoSpeak';

// AutoSpeak — mounts the shouldSpeak-gated auto-speak effect (useAutoSpeak) INSIDE the
// runtime provider, where it has runtime access. Renders nothing; 37C-05 mounts one
// instance per chat subtree so a newly-completed reply auto-speaks under voice mode OR
// after a dictated turn (D-07). A component (not a bare hook call) so ExternalStoreChat
// can mount it declaratively next to the thread.
export function AutoSpeak(): null {
  useAutoSpeak();
  return null;
}
