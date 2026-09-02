import type { CommandRunner } from './process.js';

// compose.yaml:752 / :781 default AURA_EMBED_IMAGE and AURA_EMBED_NGL to this pair when a
// GPU is present; .github/workflows/ci.yml:754-755 pins the CPU pair used when it is not.
// embedNgl is a string, not a number: it reaches .env as text and serializeInstallConfig
// (config-file.ts) base64s it as text.
export const CUDA_EMBED_IMAGE = 'ghcr.io/ggml-org/llama.cpp:server-cuda';
export const CPU_EMBED_IMAGE = 'ghcr.io/ggml-org/llama.cpp:server';

export interface GpuProbeResult {
  cuda: boolean;
  embedImage: string;
  embedNgl: string;
}

export interface OllamaProbeResult {
  reachable: boolean;
  models: string[];
}

// compose.yaml:801-817 reserves `driver: nvidia` unconditionally for every install, so
// whether the NVIDIA *container* toolkit is present decides whether `docker compose up`
// succeeds at all -- this wizard's image choice cannot influence that either way. Choosing
// between the two llama.cpp images only decides whether a working GPU gets *used*;
// compose.yaml:807-809 documents what happens when it is not: the CUDA image starts anyway
// and silently falls back to CPU ("no usable GPU found" is a warning, not an error). So
// guessing wrong here costs speed, not a failed install, and one host nvidia-smi call is the
// right size for that stake. CommandRunner.run rejects rather than returning a non-zero
// exitCode (process.ts's ProcessRunner.execute), so an absent binary reaches this function as
// a rejected promise, never as a result to branch on.
export async function probeGpu(runner: CommandRunner): Promise<GpuProbeResult> {
  try {
    await runner.run('nvidia-smi');
    return { cuda: true, embedImage: CUDA_EMBED_IMAGE, embedNgl: '99' };
  } catch {
    return { cuda: false, embedImage: CPU_EMBED_IMAGE, embedNgl: '0' };
  }
}

interface OllamaTagsResponse {
  models?: ReadonlyArray<{ name?: string }>;
}

// The operator types the OpenAI-compatible base URL (it becomes AURA_LLM_BASE_URL), but
// Ollama's model list lives at /api/tags on the root, not under /v1.
function tagsUrlFor(baseUrl: string): string {
  return `${baseUrl.replace(/\/v1\/?$/, '')}/api/tags`;
}

// Aura runs the probed endpoint from inside a container, and an Ollama on the host is not
// at 127.0.0.1 as seen from there (project memory: "Hyper-V port forwarding lies -- probe
// via docker network, not 127.0.0.1"). create-aura itself runs BEFORE install.sh though, so
// the aura_default compose network does not exist yet and --network aura_default would fail
// on every fresh host. This mirrors the working host-reachability probe at
// scripts/ingest_media_e2e.sh:27-28 instead: default bridge network plus --add-host
// host.docker.internal:host-gateway, which is load-bearing on Linux where that name does not
// resolve without it. alpine is the probe image because scripts/install.sh:393 already runs
// `docker run --rm --volumes-from ... alpine`, so this installer already requires a host able
// to pull it -- a second image would only add a failure mode.
export async function probeOllama(runner: CommandRunner, url: string): Promise<OllamaProbeResult> {
  const tagsUrl = tagsUrlFor(url);
  try {
    const result = await runner.run('docker', [
      'run', '--rm', '--add-host', 'host.docker.internal:host-gateway', 'alpine',
      'wget', '-qO-', '--timeout=5', tagsUrl,
    ]);
    const parsed = JSON.parse(result.stdout) as OllamaTagsResponse;
    const models = (parsed.models ?? [])
      .map((model) => model.name)
      .filter((name): name is string => typeof name === 'string');
    return { reachable: true, models };
  } catch {
    return { reachable: false, models: [] };
  }
}
