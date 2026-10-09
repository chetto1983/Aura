/// <reference types="vite/client" />

// @svar-ui/core-locales ships plain JS with no declarations. It is a word pack -- opaque
// objects handed straight to <Locale words>, never read field by field -- so a shape is
// declared rather than invented.
declare module '@svar-ui/core-locales' {
  export const en: Record<string, unknown>;
  export const it: Record<string, unknown>;
}

// The Kanban word pack, the same plain JS with no declarations.
declare module '@svar-ui/kanban-locales' {
  export const en: Record<string, unknown>;
  export const it: Record<string, unknown>;
}
