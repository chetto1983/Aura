// The operator profile, as something you come back to.
//
// Onboarding's seed form fires once, behind a gate, and asks six fields. This is the rest of
// the profile and the only way to CHANGE any of it: the fields ride the agent's always-block
// on every turn, so a stale one is wrong on every turn.
//
// PUT is a full replace, deliberately: the panel renders every field, so a field submitted
// empty means the operator cleared it. (The Idempotency-Key mutations need is attached by
// the installMutationIdempotency fetch wrapper.)

export interface ProfileDoc {
  readonly name: string;
  readonly role: string;
  readonly company: string;
  readonly location: string;
  readonly timezone: string;
  readonly lang: string;
  readonly tonePreference: string;
  readonly responseLength: string;
  readonly customInstructions: string;
  readonly voiceMode?: boolean;
  readonly canProactiveMessage?: boolean;
  readonly expertise: readonly string[];
  readonly stack: readonly string[];
  readonly projects: readonly string[];
  readonly goals: readonly string[];
  readonly interests: readonly string[];
  readonly people: readonly string[];
  readonly vetoes: readonly string[];
}

export function emptyProfile(): ProfileDoc {
  return {
    name: '',
    role: '',
    company: '',
    location: '',
    timezone: '',
    lang: '',
    tonePreference: '',
    responseLength: '',
    customInstructions: '',
    expertise: [],
    stack: [],
    projects: [],
    goals: [],
    interests: [],
    people: [],
    vetoes: [],
  };
}

async function readProfile(res: Response): Promise<ProfileDoc> {
  if (!res.ok) {
    throw new Error(await readErrorMessage(res));
  }
  // Merged over the empty profile rather than cast: the server sends every field, but a
  // partial body (an older daemon, a proxy that trimmed it) would otherwise leave a list
  // undefined and take the whole settings page down on the first render.
  return { ...emptyProfile(), ...((await res.json()) as Partial<ProfileDoc>) };
}

// A rejected write answers {"error": "<why>"} and that string is the only thing naming WHICH
// field was refused. Collapsing it to "HTTP 400" turns an actionable message into a shrug.
async function readErrorMessage(res: Response): Promise<string> {
  try {
    const body: unknown = await res.json();
    if (body !== null && typeof body === 'object' && 'error' in body) {
      const message = (body as { readonly error?: unknown }).error;
      if (typeof message === 'string' && message.trim() !== '') return message;
    }
  } catch {
    // Not JSON: a proxy page or an empty body. The status is all there is.
  }
  return `HTTP ${String(res.status)}`;
}

export async function fetchProfile(): Promise<ProfileDoc> {
  const res = await fetch('/api/profile', {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  return readProfile(res);
}

export async function saveProfile(profile: ProfileDoc): Promise<ProfileDoc> {
  const res = await fetch('/api/profile', {
    method: 'PUT',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    credentials: 'same-origin',
    body: JSON.stringify(profile),
  });
  return readProfile(res);
}

// The lists are edited as one comma-separated line each: seven separate list editors would
// bury the four fields that matter, and every entry here is a short phrase.
export function parseList(raw: string): string[] {
  return raw
    .split(',')
    .map((item) => item.trim())
    .filter((item) => item !== '');
}

export function formatList(items: readonly string[]): string {
  return items.join(', ');
}
