// The Profile panel's default-channel options, keyed by route; `unset` is the empty value.
export const profileDefaultNotifyEn = {
  unset: 'Ask me every time',
  telegram: 'Telegram',
  whatsapp: 'WhatsApp',
  email: 'Email',
  stdout: 'The conversation it was scheduled from',
  none: 'No notification',
} as const;

export const profileDefaultNotifyIt = {
  unset: 'Chiedimi ogni volta',
  telegram: 'Telegram',
  whatsapp: 'WhatsApp',
  email: 'Email',
  stdout: 'La conversazione da cui è stato pianificato',
  none: 'Nessuna notifica',
} as const;
