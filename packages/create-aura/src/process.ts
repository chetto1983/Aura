import { StringDecoder } from 'node:string_decoder';

import { execa } from 'execa';

const REDACTED = '[REDACTED]';

export type RedactionValue = string | undefined;
export type OutputSink = (chunk: string) => void;

export interface ProcessRunnerOutput {
  stdout?: OutputSink;
  stderr?: OutputSink;
}

export interface ProcessRunOptions {
  cwd?: string;
  env?: Readonly<Record<string, string | undefined>>;
  redactions?: readonly RedactionValue[];
  terminal?: boolean;
}

export interface ProcessResult {
  stdout: string;
  stderr: string;
  exitCode: number;
}

export interface CommandRunner {
  run(command: string, args?: readonly string[], options?: ProcessRunOptions): Promise<ProcessResult>;
}

export interface InputCommandRunner extends CommandRunner {
  runWithInput(
    command: string,
    args: readonly string[],
    input: string,
    options?: ProcessRunOptions,
  ): Promise<ProcessResult>;
}

function normalizeRedactions(values: readonly RedactionValue[] = []): string[] {
  return [...new Set(values.filter((value): value is string => Boolean(value)))]
    .sort((left, right) => right.length - left.length);
}

export function redactText(text: string, values: readonly RedactionValue[] = []): string {
  return normalizeRedactions(values).reduce(
    (redacted, value) => redacted.split(value).join(REDACTED),
    text,
  );
}

class StreamingRedactor {
  private readonly decoder = new StringDecoder('utf8');
  private readonly secrets: readonly string[];
  private pending = '';

  constructor(
    private readonly sink: OutputSink,
    values: readonly RedactionValue[],
  ) {
    this.secrets = normalizeRedactions(values);
  }

  write(chunk: Buffer | string): void {
    if (this.secrets.length === 0) {
      this.sink(typeof chunk === 'string' ? chunk : this.decoder.write(chunk));
      return;
    }
    this.pending += typeof chunk === 'string' ? chunk : this.decoder.write(chunk);
    this.drain(false);
  }

  end(): void {
    this.pending += this.decoder.end();
    this.drain(true);
  }

  private drain(final: boolean): void {
    let output = '';

    while (this.pending.length > 0) {
      const couldBecomeLongerSecret = this.secrets.some(
        (secret) => secret.length > this.pending.length && secret.startsWith(this.pending),
      );
      if (!final && couldBecomeLongerSecret) break;

      const matchedSecret = this.secrets.find((secret) => this.pending.startsWith(secret));
      if (matchedSecret) {
        output += REDACTED;
        this.pending = this.pending.slice(matchedSecret.length);
        continue;
      }

      const couldBecomeSecret = this.secrets.some((secret) => secret.startsWith(this.pending));
      if (!final && couldBecomeSecret) break;

      output += this.pending[0];
      this.pending = this.pending.slice(1);
    }

    if (output) this.sink(output);
  }
}

export class ProcessExecutionError extends Error {
  constructor(
    command: string,
    public readonly exitCode: number,
    public readonly stdout: string,
    public readonly stderr: string,
  ) {
    super(`${command} exited with code ${exitCode}`);
    this.name = 'ProcessExecutionError';
  }
}

export class ProcessRunner implements InputCommandRunner {
  private readonly stdout: OutputSink;
  private readonly stderr: OutputSink;

  constructor(output: ProcessRunnerOutput = {}) {
    this.stdout = output.stdout ?? ((chunk) => process.stdout.write(chunk));
    this.stderr = output.stderr ?? ((chunk) => process.stderr.write(chunk));
  }

  async run(
    command: string,
    args: readonly string[] = [],
    options: ProcessRunOptions = {},
  ): Promise<ProcessResult> {
    return this.execute(command, args, undefined, options);
  }

  async runWithInput(
    command: string,
    args: readonly string[],
    input: string,
    options: ProcessRunOptions = {},
  ): Promise<ProcessResult> {
    return this.execute(command, args, input, options);
  }

  private async execute(
    command: string,
    args: readonly string[],
    input: string | undefined,
    options: ProcessRunOptions,
  ): Promise<ProcessResult> {
    if (input !== undefined && options.terminal) {
      throw new Error('interactiveInputConflict');
    }
    const redactions = options.redactions ?? [];
    const stdout = new StreamingRedactor(this.stdout, redactions);
    const stderr = new StreamingRedactor(this.stderr, redactions);
    const subprocess = execa(command, [...args], {
      cwd: options.cwd,
      env: options.env,
      reject: false,
      encoding: 'utf8',
      ...(options.terminal ? { stdin: 'inherit', stderr: 'inherit' } : {}),
      ...(input === undefined ? {} : { input }),
    });

    subprocess.stdout?.on('data', (chunk: Buffer) => stdout.write(chunk));
    subprocess.stderr?.on('data', (chunk: Buffer) => stderr.write(chunk));

    try {
      const result = await subprocess;
      const capturedStderr = typeof result.stderr === 'string' ? result.stderr : '';
      const processResult: ProcessResult = {
        stdout: redactText(result.stdout, redactions),
        stderr: redactText(capturedStderr, redactions),
        exitCode: result.exitCode ?? 1,
      };

      if (processResult.exitCode !== 0) {
        throw new ProcessExecutionError(
          command,
          processResult.exitCode,
          processResult.stdout,
          processResult.stderr,
        );
      }

      return processResult;
    } finally {
      stdout.end();
      stderr.end();
    }
  }
}
