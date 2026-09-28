const nativeLists = {
  native_tasks: { title: 'tasks', columns: ['task', 'kind', 'schedule', 'next', 'state'] },
  native_skills: { title: 'skills', columns: ['skill', 'description'] },
  native_packs: {
    title: 'packs',
    columns: ['pack', 'version', 'skills', 'connectors', 'commands', 'source'],
  },
} as const;

export function nativeListSpec(title: string | undefined) {
  if (!title || !Object.prototype.hasOwnProperty.call(nativeLists, title)) return undefined;
  return nativeLists[title as keyof typeof nativeLists];
}
