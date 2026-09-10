import type { CommandRunner } from './process.js';

export interface OllamaProbeResult {
  reachable: boolean;
  models: string[];
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
// resolve without it. alpine is the probe image because scripts/install.sh's ensure_embed_model already runs
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
