import { confirm, input, password, select } from '@inquirer/prompts';

import type { Translator } from './i18n.js';
import {
  embedTargetFor,
  probeGpu,
  probeOllama,
} from './modelroute.js';
import type { CommandRunner } from './process.js';
import type { InstallMode, InstallSettings, RemoteTarget } from './types.js';
import {
  validateBaseUrl,
  validateHost,
  validateInstallDir,
  validateModelId,
  validatePort,
  validateUsername,
} from './validation.js';

export interface SelectOptions {
  message: string;
  choices: readonly { name: string; value: string }[];
}

export interface InputOptions {
  message: string;
  default?: string;
}

export interface PasswordOptions {
  message: string;
  mask: string;
}

export interface ConfirmOptions {
  message: string;
  default: boolean;
}

export interface PromptPort {
  select(options: SelectOptions): Promise<string>;
  input(options: InputOptions): Promise<string>;
  password(options: PasswordOptions): Promise<string>;
  confirm(options: ConfirmOptions): Promise<boolean>;
}

export const inquirerPrompt: PromptPort = {
  select: (options) => select({ message: options.message, choices: [...options.choices] }),
  input: (options) => input(options),
  password: (options) => password(options),
  confirm: (options) => confirm(options),
};

export interface TargetSelection {
  mode: InstallMode;
  installDir: string;
  remote?: RemoteTarget;
}

export async function collectTarget(
  prompt: PromptPort,
  t: Translator,
  requestedMode?: InstallMode,
): Promise<TargetSelection> {
  const mode = requestedMode ?? await prompt.select({
    message: t('modeQuestion'),
    choices: [
      { name: t('modeLocal'), value: 'local' },
      { name: t('modeRemote'), value: 'remote' },
    ],
  }) as InstallMode;

  let remote: RemoteTarget | undefined;
  if (mode === 'remote') {
    remote = {
      host: validateHost(await prompt.input({ message: t('remoteHost') })),
      port: validatePort(await prompt.input({ message: t('remotePort'), default: '22' })),
      // Aura's remote target is a clean Ubuntu Server mini-PC, not a Raspberry Pi -- 'ubuntu'
      // is that image's standard default account, replacing the reference's 'pi'.
      username: validateUsername(await prompt.input({ message: t('remoteUsername'), default: 'ubuntu' })),
    };
  }

  const installDir = validateInstallDir(await prompt.input({
    message: t('installDir'),
    // scripts/install.sh:86 -- /opt/aura is the appliance's own default installation path.
    default: '/opt/aura',
  }));

  return remote ? { mode, installDir, remote } : { mode, installDir };
}

interface GpuChoice {
  cuda: boolean;
  embedImage: string;
  embedNgl: string;
}

async function collectGpuChoice(prompt: PromptPort, t: Translator, probeRunner: CommandRunner | undefined): Promise<GpuChoice> {
  if (probeRunner) return probeGpu(probeRunner);

  // R1 (Task 5 controller ruling): with no runner able to probe the actual install target
  // (remote mode, until Task 6 supplies an SSH-wrapping one), the wizard must not run
  // nvidia-smi against the operator's own laptop and present that as the target's GPU --
  // it asks instead.
  const hasGpu = await prompt.confirm({ message: t('gpuQuestion'), default: false });
  return { cuda: hasGpu, ...embedTargetFor(hasGpu) };
}

async function collectOllamaModel(
  prompt: PromptPort,
  t: Translator,
  probeRunner: CommandRunner | undefined,
  llmBaseUrl: string,
): Promise<string> {
  const probe = probeRunner ? await probeOllama(probeRunner, llmBaseUrl) : undefined;

  if (probe?.reachable && probe.models.length > 0) {
    return prompt.select({
      message: t('ollamaModelSelect'),
      choices: probe.models.map((model) => ({ name: model, value: model })),
    });
  }

  // R2 (Task 4 review, landed here): probe.reachable is false for four different causes --
  // the endpoint refused, docker is absent or its daemon is down, the alpine probe image
  // could not be pulled, or the reply was not JSON. The message must not assert the
  // endpoint is at fault, or the operator debugs a box that was fine. A probe that WAS
  // reachable but listed no models yet (or no probe was attempted at all, e.g. remote mode)
  // gets the plain manual-entry prompt instead.
  const message = probe && !probe.reachable ? t('ollamaModelUnreachable') : t('ollamaModelManualEntry');
  return validateModelId(await prompt.input({ message }));
}

export async function collectSettings(
  prompt: PromptPort,
  t: Translator,
  installDir: string,
  probeRunner: CommandRunner | undefined,
): Promise<InstallSettings | null> {
  const appliance = await prompt.confirm({ message: t('applianceQuestion'), default: true });
  const gvisor = await prompt.confirm({ message: t('gvisorQuestion'), default: false });

  // R4: exactly two model routes. A third llama.cpp route is deliberately not offered --
  // compose.yaml's aura-llm sits behind profiles:[localllm] and needs a ~6.7 GB GGUF that
  // install.sh never fetches, so enabling it would leave the volume empty, the healthcheck
  // would never pass, and `docker compose up -d --wait` would abort the whole install.
  const llmProvider = await prompt.select({
    message: t('modelRouteQuestion'),
    choices: [
      { name: t('modelRouteOpenrouter'), value: 'openrouter' },
      { name: t('modelRouteOllama'), value: 'ollama' },
    ],
  });

  let llmBaseUrl: string;
  let llmModel: string;
  let openrouterApiKey: string | undefined;

  if (llmProvider === 'openrouter') {
    llmBaseUrl = validateBaseUrl(await prompt.input({
      message: t('openrouterBaseUrl'),
      default: 'https://openrouter.ai/api/v1',
    }));
    openrouterApiKey = await prompt.password({ message: t('openrouterApiKey'), mask: '*' });
    llmModel = validateModelId(await prompt.input({ message: t('openrouterModel') }));
  } else {
    llmBaseUrl = validateBaseUrl(await prompt.input({
      message: t('ollamaBaseUrl'),
      // C1 (review round 1): 'localhost' here is wrong twice. probeOllama's request runs
      // INSIDE the throwaway alpine container, where 'localhost' means the container itself,
      // not the operator's host -- modelroute.ts's own comment warns about exactly this ("an
      // Ollama on the host is not at 127.0.0.1 as seen from there"), so the wizard's own
      // default could never probe reachable. And the /v1 suffix is load-bearing:
      // internal/llm/config.go:21's defaultBaseURL carries it, and every other Ollama base
      // URL in this repo is spelled with it (scripts/install_config_test.sh:19,
      // internal/agui/settings_api_test.go:218,
      // internal/channels/telegram/commands_spend_test.go:157) -- without it Aura requests
      // .../11434/chat/completions, which Ollama does not serve.
      default: 'http://host.docker.internal:11434/v1',
    }));
    llmModel = await collectOllamaModel(prompt, t, probeRunner, llmBaseUrl);
  }

  const gpu = await collectGpuChoice(prompt, t, probeRunner);

  const confirmed = await prompt.confirm({ message: t('confirmInstall'), default: true });
  if (!confirmed) return null;

  return {
    installDir,
    appliance,
    gvisor,
    llmProvider,
    llmBaseUrl,
    llmModel,
    // R5: the empty OpenRouter key is emitted, never omitted -- install.sh's
    // parse_install_config accepts an empty openrouter_api_key_base64 and exit 2s on a
    // MISSING key set, so the Ollama route must still produce all nine keys.
    ...(openrouterApiKey === undefined ? {} : { openrouterApiKey }),
    embedImage: gpu.embedImage,
    embedNgl: gpu.embedNgl,
  };
}
