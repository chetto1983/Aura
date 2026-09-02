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
