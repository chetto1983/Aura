// Wire titles and column counts must match internal/agent/display/sidecars.go.
const sidecarTables = {
  calendar_accounts: { title: 'calendarAccounts', columns: ['account', 'provider', 'name'] },
  calendar_emails: {
    title: 'calendarEmails',
    columns: ['email', 'account', 'subject', 'from', 'received'],
  },
  calendar_calendars: {
    title: 'calendarCalendars',
    columns: ['calendar', 'account', 'name', 'owner'],
  },
  calendar_events: {
    title: 'calendarEvents',
    columns: ['event', 'account', 'subject', 'start', 'end'],
  },
  calendar_contacts: {
    title: 'calendarContacts',
    columns: ['contact', 'account', 'name', 'email'],
  },
  whatsapp_chats: {
    title: 'whatsappChats',
    columns: ['chat', 'name', 'lastActive', 'lastMessage'],
  },
  whatsapp_messages: { title: 'whatsappMessages', columns: ['when', 'sender', 'chat', 'message'] },
  whatsapp_contacts: { title: 'whatsappContacts', columns: ['contact', 'name', 'phone'] },
} as const;

export function sidecarTableSpec(title: string | undefined) {
  if (!title || !Object.prototype.hasOwnProperty.call(sidecarTables, title)) return undefined;
  return sidecarTables[title as keyof typeof sidecarTables];
}
