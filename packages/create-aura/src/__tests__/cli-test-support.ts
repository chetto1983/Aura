// Shared fixtures for cli.test.ts and cli_local_preflight.test.ts. Split out (not
// duplicated) when cli.test.ts crossed the 600-LOC cap after Task 6 wired local.ts's
// preflightLocal/installLocal into cli.ts -- both spec files exercise the same fake
// CommandRunner shape and the same valid InstallSettings fixture.
import type { CommandRunner, ProcessResult } from '../process.js';

export type FakeRunner = CommandRunner & { calls: Array<{ command: string; args: readonly string[] }> };

export function createFakeRunner(
  responder: (command: string, args: readonly string[]) => Promise<ProcessResult>,
): FakeRunner {
  const calls: Array<{ command: string; args: readonly string[] }> = [];
  return {
    calls,
    async run(command, args = []) {
      calls.push({ command, args });
      return responder(command, args);
    },
  };
}

// Covers preflightLocal's full call surface: the three REQUIRED_COMMANDS existence checks
// (sh), architecture (uname), cpu cores (getconf), memory and disk (sh, disambiguated by
// script content), the existing-install probe (sh), and the three REQUIRED_HOSTS reachability
// checks (curl). 41943040 KiB (40 GiB) clears both the memory floor (15 GiB) and the disk
// floor (20 GiB), so one generic answer satisfies whichever `sh -c` check asks.
export function createPassingPreflightRunner(): FakeRunner {
  return createFakeRunner(async (command) => {
    if (command === 'uname') return { stdout: 'x86_64\n', stderr: '', exitCode: 0 };
    if (command === 'getconf') return { stdout: '8\n', stderr: '', exitCode: 0 };
    if (command === 'curl') return { stdout: '', stderr: '', exitCode: 0 };
    if (command === 'sh') return { stdout: '41943040\n', stderr: '', exitCode: 0 };
    throw new Error(`unexpected command ${command}`);
  });
}

export const validSettings = {
  installDir: '/opt/aura',
  appliance: true,
  gvisor: false,
  llmProvider: 'ollama',
  llmBaseUrl: 'http://localhost:11434',
  llmModel: 'llama3:8b',
  embedImage: 'ghcr.io/ggml-org/llama.cpp:server',
  embedNgl: '0',
};
