import { describe, expect, it } from 'vitest';

import {
  ProcessExecutionError,
  ProcessRunner,
  redactText,
} from '../process.js';

describe('redactText', () => {
  it('redacts overlapping values longest first', () => {
    expect(redactText('token-long token', ['token', 'token-long'])).toBe('[REDACTED] [REDACTED]');
  });

  it('ignores empty redaction values', () => {
    expect(redactText('visible', ['', undefined])).toBe('visible');
  });
});

describe('ProcessRunner', () => {
  it('captures stdout while terminal mode leaves authentication channels inherited', async () => {
    const runner = new ProcessRunner({ stdout: () => undefined, stderr: () => undefined });

    const result = await runner.run(
      process.execPath,
      ['-e', 'process.stdout.write("terminal output")'],
      { terminal: true },
    );

    expect(result).toMatchObject({ stdout: 'terminal output', stderr: '', exitCode: 0 });
  });

  it('sends dedicated input to a subprocess without putting it in argv', async () => {
    const runner = new ProcessRunner({ stdout: () => undefined, stderr: () => undefined });
    const input = 'remote probe input';
    const script = [
      'let value = "";',
      'process.stdin.setEncoding("utf8");',
      'process.stdin.on("data", (chunk) => { value += chunk; });',
      'process.stdin.on("end", () => process.stdout.write(value));',
    ].join('\n');

    const result = await runner.runWithInput(process.execPath, ['-e', script], input);

    expect(result.stdout).toBe(input);
    expect(JSON.stringify([process.execPath, '-e', script])).not.toContain(input);
  });

  it('redacts secrets from streamed and captured stdout and stderr', async () => {
    const secret = 'correct horse battery staple';
    let stdout = '';
    let stderr = '';
    const runner = new ProcessRunner({
      stdout: (chunk) => { stdout += chunk; },
      stderr: (chunk) => { stderr += chunk; },
    });
    const script = [
      `const value = ${JSON.stringify(secret)};`,
      'process.stdout.write(value.slice(0, 8));',
      'setTimeout(() => {',
      '  process.stdout.write(value.slice(8));',
      '  process.stderr.write(`failure: ${value}`);',
      '}, 10);',
    ].join('\n');

    const result = await runner.run(process.execPath, ['-e', script], { redactions: [secret] });

    expect(stdout).toBe('[REDACTED]');
    expect(stderr).toBe('failure: [REDACTED]');
    expect(result.stdout).toBe('[REDACTED]');
    expect(result.stderr).toBe('failure: [REDACTED]');
    expect(`${stdout}${stderr}${result.stdout}${result.stderr}`).not.toContain(secret);
  });

  it('throws a redacted error for a failed process', async () => {
    const secret = 'never expose this password';
    const runner = new ProcessRunner({ stdout: () => undefined, stderr: () => undefined });
    const script = `process.stderr.write(${JSON.stringify(secret)}); process.exit(7);`;

    const execution = runner.run(process.execPath, ['-e', script], { redactions: [secret] });

    await expect(execution).rejects.toBeInstanceOf(ProcessExecutionError);
    await expect(execution).rejects.toMatchObject({
      exitCode: 7,
      stderr: '[REDACTED]',
    });
    await expect(execution).rejects.not.toThrow(secret);
  });
});
