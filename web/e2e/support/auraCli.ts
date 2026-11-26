// The `aura` CLI the live specs drive. On an appliance it lives in the `aura` container; on the
// CI host it is the binary scripts/web_e2e_ci.sh serves, so AURA_E2E_AURA_CMD names it there.
const containerCLI = ['docker', 'exec', 'aura', 'aura'];
const interactiveContainerCLI = ['docker', 'exec', '-i', 'aura', 'aura'];

/** auraCommand returns the program and argv that run `aura <args>`; interactive keeps stdin open. */
export function auraCommand(args: readonly string[], interactive = false): [string, string[]] {
  const configured = process.env.AURA_E2E_AURA_CMD?.trim();
  const base =
    configured !== undefined && configured !== ''
      ? configured.split(/\s+/)
      : interactive
        ? interactiveContainerCLI
        : containerCLI;
  const [program, ...rest] = base;
  return [program ?? 'aura', [...rest, ...args]];
}
