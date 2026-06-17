export type AriaInvalid = 'true' | undefined;

export function ariaInvalid(invalid: boolean): AriaInvalid {
  return invalid ? 'true' : undefined;
}
