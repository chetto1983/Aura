import type { SupportedArchitecture } from './preflight.js';

export type InstallMode = 'local' | 'remote';

export interface RemoteTarget {
  host: string;
  port: number;
  username: string;
}

// Shared by local.ts's preflightLocal and remote.ts's preflightRemote: architecture and
// existing-install state, the two facts cli.ts still needs after the hardware/command/host
// gates all pass (unlike the reference, Aura has no per-device serial to carry alongside them).
export interface PreflightResult {
  architecture: SupportedArchitecture;
  existingInstall: boolean;
}

export interface InstallSettings {
  installDir: string;
  appliance: boolean;
  gvisor: boolean;
  llmProvider: string;
  llmBaseUrl: string;
  llmModel: string;
  openrouterApiKey?: string;
  embedImage: string;
  embedNgl: string;
}
