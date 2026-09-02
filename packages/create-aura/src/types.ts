export type InstallMode = 'local' | 'remote';

export interface RemoteTarget {
  host: string;
  port: number;
  username: string;
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
