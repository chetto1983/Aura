import type { SupportedArchitecture } from './preflight.js';

export type InstallMode = 'local' | 'remote';

export interface RemoteTarget {
  host: string;
  port: number;
  username: string;
  // The private key ssh should authenticate with. Optional, and empty means "whatever ssh
  // would do on its own" -- but supplying it is what makes an install bearable: a remote
  // run opens SIX separate ssh/scp connections (probe, stale cleanup, upload, run, cleanup,
  // final check) and each one authenticates independently, so without a key the operator
  // types the password six times. Connection multiplexing would have been the other fix and
  // is not available: Windows OpenSSH, where this wizard usually runs, does not implement
  // ControlMaster.
  identityFile?: string;
}

// Shared by local.ts's preflightLocal and remote.ts's preflightRemote: architecture and
// existing-install state, the two facts cli.ts still needs after the hardware/command/host
// gates all pass (unlike the reference, Aura has no per-device serial to carry alongside them).
export interface PreflightResult {
  architecture: SupportedArchitecture;
  existingInstall: boolean;
}

// The installer's answers are infrastructure only. The model route, the model and the
// OpenRouter management key are chosen by an admin in the first-run web setup.
export interface InstallSettings {
  installDir: string;
  appliance: boolean;
  gvisor: boolean;
}
