import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Mail, MessageCircle, ShieldCheck } from 'lucide-react';
import type { MessageDraft, ResolveMessageDraft } from './useThreadMessageDrafts';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';

// Layout adapted from Tool UI's MIT-licensed message-draft card. Aura keeps the
// editable state local and delegates the one-send claim to its owner-scoped API.
function value(raw: unknown): string {
  return typeof raw === 'string' ? raw : '';
}

function addresses(raw: unknown): string[] {
  return Array.isArray(raw) && raw.every((item) => typeof item === 'string') ? raw : [];
}

function splitAddresses(raw: string): string[] {
  return raw
    .split(',')
    .map((part) => part.trim())
    .filter(Boolean);
}

function attachmentNames(raw: unknown): string[] {
  if (!Array.isArray(raw)) return [];
  return raw.flatMap((item: unknown) => {
    if (item === null || typeof item !== 'object' || !('name' in item)) return [];
    const name = (item as { name?: unknown }).name;
    return typeof name === 'string' && name !== '' ? [name] : [];
  });
}

export interface MessageDraftCardProps {
  readonly draft: MessageDraft;
  readonly busy: boolean;
  readonly onResolve: (input: ResolveMessageDraft) => Promise<unknown>;
}

export function MessageDraftCard({ draft, busy, onResolve }: MessageDraftCardProps) {
  const { t } = useTranslation();
  const args = draft.arguments;
  const [to, setTo] = useState(addresses(args.to).join(', '));
  const [cc, setCc] = useState(addresses(args.cc).join(', '));
  const [subject, setSubject] = useState(value(args.subject));
  const [body, setBody] = useState(value(args.body));
  const [textBody, setTextBody] = useState(value(args.textBody));
  const [htmlBody, setHtmlBody] = useState(value(args.htmlBody));
  const [recipient, setRecipient] = useState(value(args.recipient));
  const [message, setMessage] = useState(value(args.message));
  const [error, setError] = useState('');
  const email = draft.channel === 'email';
  const multipart = value(args.bodyFormat).toLowerCase() === 'multipart';
  const editable = draft.status === 'pending';
  const destination = email ? splitAddresses(to).join(', ') : recipient.trim();

  async function resolve(input: ResolveMessageDraft) {
    setError('');
    try {
      await onResolve(input);
    } catch {
      setError(t('messageDraft.error'));
    }
  }

  function submit() {
    if (!editable || busy) return;
    const overrides = email
      ? multipart
        ? { to: splitAddresses(to), cc: splitAddresses(cc), subject, textBody, htmlBody }
        : { to: splitAddresses(to), cc: splitAddresses(cc), subject, body }
      : { recipient: recipient.trim(), message };
    void resolve({ id: draft.id, action: 'send', overrides });
  }

  const title = email ? t('messageDraft.emailTitle') : t('messageDraft.whatsappTitle');
  const Icon = email ? Mail : MessageCircle;

  return (
    <section
      aria-label={title}
      className="rounded-xl border border-border bg-card px-4 py-3 text-card-foreground shadow-sm"
    >
      <header className="mb-3 flex items-center gap-2 text-sm font-semibold">
        <Icon aria-hidden="true" className="size-4 text-text-muted" />
        <span>{title}</span>
        <ShieldCheck aria-hidden="true" className="ml-auto size-4 text-text-muted" />
      </header>

      {!editable ? (
        <div className="space-y-3">
          <p role="status" className="text-sm text-text-muted">
            {t(`messageDraft.status.${draft.status}`)}
          </p>
          {draft.status !== 'dispatching' && (
            <Button
              type="button"
              disabled={busy}
              onClick={() =>
                void resolve({
                  id: draft.id,
                  action:
                    draft.status === 'declined' || draft.status === 'expired' ? 'decline' : 'send',
                })
              }
            >
              {t('messageDraft.continue')}
            </Button>
          )}
        </div>
      ) : (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            submit();
          }}
          className="space-y-3"
        >
          {email ? (
            <>
              {value(args.accountId) && (
                <p className="text-xs text-text-muted">
                  {t('messageDraft.account')}: {value(args.accountId)}
                </p>
              )}
              {value(args.bodyFormat) && (
                <p className="text-xs text-text-muted">
                  {t('messageDraft.format')}: {value(args.bodyFormat)}
                </p>
              )}
              <div className="space-y-1">
                <Label htmlFor={`draft-to-${draft.id}`}>{t('messageDraft.to')}</Label>
                <Input
                  id={`draft-to-${draft.id}`}
                  value={to}
                  onChange={(event) => {
                    setTo(event.target.value);
                  }}
                  required
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor={`draft-cc-${draft.id}`}>{t('messageDraft.cc')}</Label>
                <Input
                  id={`draft-cc-${draft.id}`}
                  value={cc}
                  onChange={(event) => {
                    setCc(event.target.value);
                  }}
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor={`draft-subject-${draft.id}`}>{t('messageDraft.subject')}</Label>
                <Input
                  id={`draft-subject-${draft.id}`}
                  value={subject}
                  onChange={(event) => {
                    setSubject(event.target.value);
                  }}
                  required
                />
              </div>
              {multipart ? (
                <>
                  <div className="space-y-1">
                    <Label htmlFor={`draft-text-${draft.id}`}>{t('messageDraft.textBody')}</Label>
                    <Textarea
                      id={`draft-text-${draft.id}`}
                      value={textBody}
                      onChange={(event) => {
                        setTextBody(event.target.value);
                      }}
                      required
                    />
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor={`draft-html-${draft.id}`}>{t('messageDraft.htmlBody')}</Label>
                    <Textarea
                      id={`draft-html-${draft.id}`}
                      value={htmlBody}
                      onChange={(event) => {
                        setHtmlBody(event.target.value);
                      }}
                      required
                    />
                  </div>
                </>
              ) : (
                <div className="space-y-1">
                  <Label htmlFor={`draft-body-${draft.id}`}>{t('messageDraft.body')}</Label>
                  <Textarea
                    id={`draft-body-${draft.id}`}
                    value={body}
                    onChange={(event) => {
                      setBody(event.target.value);
                    }}
                    required
                  />
                </div>
              )}
              {Array.isArray(args.attachments) && args.attachments.length > 0 && (
                <p className="text-xs text-text-muted">
                  {t('messageDraft.attachments', { count: args.attachments.length })}
                  {attachmentNames(args.attachments).length > 0 &&
                    `: ${attachmentNames(args.attachments).join(', ')}`}
                </p>
              )}
            </>
          ) : (
            <>
              <div className="space-y-1">
                <Label htmlFor={`draft-recipient-${draft.id}`}>{t('messageDraft.recipient')}</Label>
                <Input
                  id={`draft-recipient-${draft.id}`}
                  value={recipient}
                  onChange={(event) => {
                    setRecipient(event.target.value);
                  }}
                  required
                />
              </div>
              <div className="space-y-1">
                <Label htmlFor={`draft-message-${draft.id}`}>{t('messageDraft.body')}</Label>
                <Textarea
                  id={`draft-message-${draft.id}`}
                  value={message}
                  onChange={(event) => {
                    setMessage(event.target.value);
                  }}
                  required
                />
              </div>
              {value(args.quoted_content) && (
                <p className="text-xs text-text-muted">
                  {t('messageDraft.quoted')}: {value(args.quoted_content)}
                </p>
              )}
            </>
          )}
          <p className="text-xs text-text-muted">{t('messageDraft.sendTo', { destination })}</p>
          <div className="flex flex-wrap justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={() => void resolve({ id: draft.id, action: 'decline' })}
            >
              {t('messageDraft.decline')}
            </Button>
            <Button type="submit" disabled={busy || destination.length === 0}>
              {busy ? t('messageDraft.sending') : t('messageDraft.send')}
            </Button>
          </div>
        </form>
      )}
      {error && (
        <p role="alert" className="mt-2 text-sm text-destructive">
          {error}
        </p>
      )}
    </section>
  );
}
