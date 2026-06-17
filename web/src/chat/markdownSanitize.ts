import { defaultSchema, type Options } from 'rehype-sanitize';

const safeProtocols = ['http', 'https', 'mailto', 'tel'];

export const markdownSanitizeSchema: Options = {
  ...defaultSchema,
  clobberPrefix: 'aura-md-',
  protocols: {
    ...defaultSchema.protocols,
    href: safeProtocols,
    cite: safeProtocols,
    src: ['http', 'https'],
  },
  tagNames: [
    ...(defaultSchema.tagNames ?? []),
    'table',
    'thead',
    'tbody',
    'tfoot',
    'tr',
    'th',
    'td',
  ],
  attributes: {
    ...defaultSchema.attributes,
    a: [...(defaultSchema.attributes?.a ?? []), 'aria-label'],
    code: [...(defaultSchema.attributes?.code ?? []), ['className', /^language-[\w-]+$/]],
    th: ['align'],
    td: ['align'],
  },
  strip: ['script', 'style'],
};
