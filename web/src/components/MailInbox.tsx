import { useEffect, useMemo, useState } from 'preact/hooks';
import { Cloud, Flag, HardDrive, Inbox, Lock, Mail, Maximize2, Plus, Reply, Settings as SettingsIcon, Trash2, TriangleAlert } from 'lucide-preact';
import { Dodec } from './Dodec';
import { Modal } from './Modal';
import * as api from '../api';

// MailInbox is the dashboard's mail widget (chunk 4, multi-account in
// chunk 7) in two sizes: the CARD (rail + latest preview, the agenda's
// shape) and the READER (the three-pane overlay the card expands into:
// mailboxes | list | read). Both sizes render an ACCOUNT bar when more
// than one account is configured — any mix of local Maildirs and IMAP
// servers; each account reads through the same three endpoints with
// ?account=, so the widget cannot tell the transports apart and neither
// can the wire shape. The widget only ever READS — mail is the one
// personal surface where nobody writes, model included (the agent's
// personal_mail tool gets the envelope, this read pane gets the letter,
// and no surface anywhere can send, move, or mark).
//
// States are honest in every direction, the chunk-1 doctrine: locked
// when the master switch is off (a 403 mid-flight folds back there),
// "not configured" when there are zero accounts, an account's outage
// rendered inline from the rail's per-account error (one dead server
// never blanks the surface), degraded says so, empty says so — and zero
// gated fetches fire from any of the locked states.

const CARD_PREVIEW = 4;
const READER_PAGE = 50;

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

// formatDate renders a short local stamp ("3 Nov", "3 Nov 14:05" when
// not today) — the reader pane's rows.
function formatDate(iso: string, today: Date): string {
  const d = new Date(iso);
  const sameDay = d.getFullYear() === today.getFullYear() && d.getMonth() === today.getMonth() && d.getDate() === today.getDate();
  const hm = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
  return sameDay ? hm : `${d.getDate()} ${MONTHS[d.getMonth()]} ${d.getFullYear()}`;
}

function formatSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${Math.max(1, Math.round(n / 1024))} kB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

// preferredBox picks the preview's box: "inbox" by convention, else the
// rail's first entry (both sides agree on the server's name order).
export function preferredBox(boxes: api.Mailbox[]): string {
  if (boxes.length === 0) return '';
  for (const b of boxes) {
    if (b.name.toLowerCase() === 'inbox') return b.name;
  }
  return boxes[0].name;
}

// isLockedError folds a 403 (the switch flipped server-side while this
// tab was open) into the locked state — the agenda widget's rule.
function isLockedError(err: unknown): boolean {
  return !!err && typeof err === 'object' && 'message' in err && String((err as Error).message).includes('disabled');
}

// AccountBar is the account switcher both sizes share: one pill per
// resolved account (a drive for a local Maildir, a cloud for an IMAP
// server), the active one filled, an alert dot on any account whose own
// read failed. Zero or one account renders nothing — the bar only earns
// its row when there is a choice to make.
export function AccountBar({ accounts, active, onPick }: { accounts: api.MailAccountInfo[]; active: string; onPick: (name: string) => void }) {
  if (!accounts || accounts.length <= 1) return null;
  return (
    <div class="flex flex-wrap gap-1" role="tablist" aria-label="Mail accounts">
      {accounts.map(a => {
        const isActive = a.name === active;
        return (
          <button
            key={a.name}
            type="button"
            role="tab"
            aria-selected={isActive}
            class={`inline-flex items-center gap-1 px-1.5 py-0.5 rounded-sm text-[11px] border transition-colors duration-fast ${
              isActive ? 'bg-seal-light border-seal/40 text-seal font-medium' : 'bg-paper border-hairline text-ink-light hover:text-seal hover:border-seal/40'
            }`}
            onClick={() => { if (!isActive) onPick(a.name); }}
            title={a.error ? `${a.name} — ${a.error}` : `${a.name} (${a.type})`}
          >
            {a.type === 'imap' ? <Cloud size={10} class="flex-shrink-0" /> : <HardDrive size={10} class="flex-shrink-0" />}
            <span class="max-w-[9rem] truncate">{a.name}</span>
            {a.error && <TriangleAlert size={10} class="text-cinnabar flex-shrink-0" aria-label="account error" />}
          </button>
        );
      })}
    </div>
  );
}

// ---------- the card ----------

interface CardProps {
  status: api.PersonalStatus | null;
  onOpenReader: () => void;
  // onConfigure opens the accounts editor (chunk 8) — the mail
  // surface's config face. Offered whenever the master switch is on (the
  // configurator's endpoints sit behind the same gate); the locked card
  // keeps its config-editor link instead.
  onConfigure: () => void;
  // version is the dashboard's mail refetch trigger: saving accounts
  // bumps it, and the card refetches its rail + status without remount.
  version: number;
  class?: string; // grid placement from the dashboard
}

export function MailCard({ status, onOpenReader, onConfigure, version, class: cls }: CardProps) {
  const [accounts, setAccounts] = useState<api.MailAccountInfo[]>([]);
  const [account, setAccount] = useState('');
  const [rail, setRail] = useState<api.Mailbox[] | null>(null);
  const [railError, setRailError] = useState('');
  const [preview, setPreview] = useState<api.MailMessage[]>([]);
  const [failed, setFailed] = useState(false);
  const [locked, setLocked] = useState(false);

  const enabled = !!status?.enabled;
  // Configured = at least one resolved account. Old servers only report
  // mailDir; the count field is absent there, so the legacy path keeps
  // its meaning.
  const configured = (status?.mailAccounts ?? 0) > 0 || !!status?.mailDir;
  const box = useMemo(() => preferredBox(rail || []), [rail]);

  // The gated fetches fire only when the switch is on AND an account is
  // configured. The rail (with the account metadata) comes first, then
  // the preferred box's preview — both scoped to the active account.
  useEffect(() => {
    if (!enabled || !configured) return;
    let alive = true;
    setRail(null);
    setPreview([]);
    setRailError('');
    setFailed(false);
    setLocked(false);
    api.fetchMailRail(account || undefined)
      .then(r => {
        if (!alive) return;
        if (r.accounts) setAccounts(r.accounts);
        setRail(r.mailboxes);
        setRailError(r.error || '');
        const b = preferredBox(r.mailboxes);
        if (!b) return;
        return api.fetchMailMessages(b, CARD_PREVIEW, r.account || account || undefined)
          .then(m => { if (alive) setPreview(m.messages); });
      })
      .catch((err: Error) => {
        if (!alive) return;
        if (isLockedError(err)) setLocked(true);
        else setFailed(true);
      });
    return () => { alive = false; };
  }, [enabled, configured, account, version]);

  const unreadTotal = useMemo(
    () => (rail || []).reduce((n, b) => n + b.unread, 0),
    [rail],
  );

  const body = () => {
    if (!enabled || locked) {
      return (
        <div class="text-xs text-ink-muted leading-relaxed">
          <p class="flex items-center gap-1.5 text-ink-light mb-1"><Lock size={11} /> Personal data is off</p>
          <p>Turn on the master switch in the config editor to read your mail here.</p>
          <button
            type="button"
            class="mt-2 inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-sm text-xs border border-hairline bg-paper text-ink-light hover:text-seal hover:border-seal/40 transition-colors duration-fast"
            onClick={() => window.open('/settings', '_blank')}
          >
            <SettingsIcon size={12} /> Open config
          </button>
        </div>
      );
    }
    if (!configured) {
      return (
        <div class="text-xs text-ink-muted leading-relaxed">
          <p class="flex items-center gap-1.5 text-ink-light mb-1"><Mail size={11} /> No mail accounts yet</p>
          <p>Add an IMAP server (Gmail, Outlook, your own box) or a local Maildir — app passwords and all, right here.</p>
          <div class="mt-2 flex flex-wrap gap-1.5">
            <button
              type="button"
              class="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-sm text-xs font-medium bg-seal text-on-seal hover:bg-seal/90 transition-colors duration-fast"
              onClick={onConfigure}
            >
              <Plus size={12} /> Add mail account
            </button>
            <button
              type="button"
              class="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-sm text-xs border border-hairline bg-paper text-ink-light hover:text-seal hover:border-seal/40 transition-colors duration-fast"
              onClick={() => window.open('/settings', '_blank')}
            >
              <SettingsIcon size={12} /> Open config
            </button>
          </div>
        </div>
      );
    }
    if (failed) {
      return <p class="text-xs text-ink-muted py-4 text-center">Mail is unavailable right now.</p>;
    }
    if (rail === null) {
      return (
        <div class="py-6 flex justify-center" aria-label="loading mail">
          <Dodec size={28} animate={false} class="opacity-40" />
        </div>
      );
    }
    const today = new Date();
    return (
      <div class="flex flex-col gap-2">
        {/* Accounts first: the switcher, then this account's rail. */}
        <AccountBar accounts={accounts} active={account} onPick={setAccount} />
        {railError && (
          <p class="flex items-start gap-1.5 text-[11px] text-cinnabar leading-relaxed">
            <TriangleAlert size={11} class="flex-shrink-0 mt-0.5" />
            <span class="min-w-0 break-words">{railError}</span>
          </p>
        )}
        {/* Rail: every box with its unread badge. */}
        {!railError && rail.length === 0 && (
          <p class="text-xs text-ink-muted leading-relaxed">
            No mailboxes found — a directory with <code class="text-[11px] bg-ink/[0.06] px-1 rounded-sm">cur/</code> or <code class="text-[11px] bg-ink/[0.06] px-1 rounded-sm">new/</code> inside is a mailbox.
          </p>
        )}
        <div class="flex flex-wrap gap-1">
          {rail.map(b => (
            <span
              key={b.name}
              class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-sm text-[11px] bg-paper border border-hairline text-ink-light"
              title={`${b.total} message${b.total === 1 ? '' : 's'}`}
            >
              {b.name}
              {b.unread > 0 && (
                <span class="inline-flex items-center justify-center min-w-[1.1rem] px-1 rounded-full bg-cinnabar/90 text-on-seal text-[9px] font-semibold tabular-nums">
                  {b.unread}
                </span>
              )}
            </span>
          ))}
        </div>
        {/* Latest preview of the preferred box. */}
        {preview.length === 0 ? (
          <p class="text-xs text-ink-muted py-1">{box} is empty.</p>
        ) : (
          <div class="flex flex-col gap-0.5 max-h-44 overflow-y-auto pr-1 -mr-1">
            {preview.map(m => (
              <button
                key={m.id}
                type="button"
                class="w-full text-left px-2 py-1.5 rounded-sm hover:bg-seal-light transition-colors duration-fast group"
                onClick={onOpenReader}
                title={`From ${m.from || '(nobody)'}`}
              >
                <span class="flex items-center gap-1.5 min-w-0">
                  {!m.seen && <span class="w-1.5 h-1.5 rounded-full bg-cinnabar flex-shrink-0" aria-label="unread" />}
                  <span class={`truncate text-sm ${m.seen ? 'text-ink-light' : 'text-ink-light font-medium'}`}>{m.subject || '(no subject)'}</span>
                  {m.flagged && <Flag size={10} class="text-cinnabar flex-shrink-0" aria-label="flagged" />}
                  {m.trashed && <Trash2 size={10} class="text-ink-muted flex-shrink-0" aria-label="trashed" />}
                </span>
                <span class="flex items-center gap-1.5 text-xs text-ink-muted mt-0.5">
                  <span class="truncate flex-1">{m.from || '(nobody)'}</span>
                  <span class="flex-shrink-0 tabular-nums">{formatDate(m.date, today)}</span>
                </span>
              </button>
            ))}
          </div>
        )}
        <button
          type="button"
          class="mt-1 self-start inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-sm text-xs border border-hairline bg-paper text-ink-light hover:text-seal hover:border-seal/40 transition-colors duration-fast"
          onClick={onOpenReader}
        >
          <Maximize2 size={12} /> Open mail
          {unreadTotal > 0 && <span class="tabular-nums text-ink-muted">({unreadTotal} unread)</span>}
        </button>
      </div>
    );
  };

  return (
    <section class={`bg-paper-card border border-hairline rounded-lg shadow-sm p-4 min-w-0 ${cls || ''}`}>
      <div class="flex items-center gap-2 mb-3">
        <Mail size={13} class="text-ink-muted" />
        <h2 class="text-xs font-semibold text-ink-muted tracking-wider uppercase">Mail</h2>
        {enabled && configured && rail && rail.length > 0 && (
          <span class="ml-auto text-[10px] text-ink-muted">read-only</span>
        )}
        {enabled && (
          <button
            type="button"
            class={`p-1 rounded-sm text-ink-muted hover:text-seal hover:bg-seal-light transition-colors duration-fast ${configured && rail && rail.length > 0 ? '' : 'ml-auto'}`}
            onClick={onConfigure}
            title="Configure accounts"
            aria-label="Configure mail accounts"
          >
            <SettingsIcon size={12} />
          </button>
        )}
      </div>
      {body()}
    </section>
  );
}

// ---------- the reader overlay ----------

interface ReaderProps {
  onClose: () => void;
  // onConfigure opens the accounts editor from inside the reader — the
  // reader closes for it (one overlay at a time), and the dashboard's
  // version bump refetches the rail when the editor saves.
  onConfigure: () => void;
}

// MailReader is the three-pane overlay: the rail on the left, the box's
// message list in the middle, the read pane on the right. Selecting a
// message fetches its body — the one surface that reads letters (the
// agent's facade reads envelopes only).
export function MailReader({ onClose, onConfigure }: ReaderProps) {
  const [accounts, setAccounts] = useState<api.MailAccountInfo[]>([]);
  const [account, setAccount] = useState('');
  const [rail, setRail] = useState<api.Mailbox[] | null>(null);
  const [railError, setRailError] = useState('');
  const [box, setBox] = useState('');
  const [list, setList] = useState<api.MailMessagesResponse | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [full, setFull] = useState<api.MailMessage | null>(null);
  const [fullPending, setFullPending] = useState(false);
  const [fullError, setFullError] = useState('');
  const [listError, setListError] = useState('');

  // The rail follows the active account (the response also carries the
  // account metadata for the switcher); the preferred box opens selected.
  useEffect(() => {
    let alive = true;
    setRail(null);
    setRailError('');
    setBox('');
    setSelected(null);
    setFull(null);
    api.fetchMailRail(account || undefined)
      .then(r => {
        if (!alive) return;
        if (r.accounts) setAccounts(r.accounts);
        setRail(r.mailboxes);
        setRailError(r.error || '');
        const b = preferredBox(r.mailboxes);
        if (b) setBox(b);
      })
      .catch(() => { if (alive) setRail([]); });
    return () => { alive = false; };
  }, [account]);

  // The list follows the selected box + account.
  useEffect(() => {
    if (!box) return;
    let alive = true;
    setList(null);
    setSelected(null);
    setFull(null);
    setListError('');
    api.fetchMailMessages(box, READER_PAGE, account || undefined)
      .then(r => { if (alive) setList(r); })
      .catch((err: Error) => { if (alive) setListError(err.message || 'The mailbox could not be read.'); });
    return () => { alive = false; };
  }, [box, account]);

  // The body follows the selected message.
  useEffect(() => {
    if (!box || !selected) return;
    let alive = true;
    setFull(null);
    setFullError('');
    setFullPending(true);
    api.fetchMailMessage(box, selected, account || undefined)
      .then(r => { if (alive) setFull(r.message); })
      .catch((err: Error) => { if (alive) setFullError(err.message || 'The message could not be read.'); })
      .finally(() => { if (alive) setFullPending(false); });
    return () => { alive = false; };
  }, [box, selected, account]);

  const byId = useMemo(() => {
    const m = new Map<string, api.MailMessage>();
    for (const msg of list?.messages ?? []) m.set(msg.id, msg);
    return m;
  }, [list]);

  const today = new Date();

  const railPane = (
    <nav class="lg:w-44 flex-shrink-0 flex flex-col gap-0.5 lg:border-r border-hairline lg:pr-3 overflow-y-auto max-h-24 lg:max-h-none" aria-label="mailboxes">
      {(rail || []).map(b => (
        <button
          key={b.name}
          type="button"
          class={`w-full text-left px-2 py-1.5 rounded-sm text-sm transition-colors duration-fast flex items-center gap-1.5 ${b.name === box ? 'bg-seal-light text-seal font-medium' : 'text-ink-light hover:bg-seal-light'}`}
          onClick={() => setBox(b.name)}
          title={`${b.total} message${b.total === 1 ? '' : 's'}, ${b.unread} unread`}
        >
          <Inbox size={12} class="flex-shrink-0 opacity-70" />
          <span class="truncate flex-1">{b.name}</span>
          {b.unread > 0 && (
            <span class="inline-flex items-center justify-center min-w-[1.2rem] px-1 rounded-full bg-cinnabar/90 text-on-seal text-[9px] font-semibold tabular-nums flex-shrink-0">
              {b.unread}
            </span>
          )}
        </button>
      ))}
      {rail !== null && rail.length === 0 && <p class="text-xs text-ink-muted px-2 py-1.5">{railError ? '—' : 'No mailboxes.'}</p>}
      {/* The configurator's door inside the reader: the rail's own column
          is the account-ish place, and a dead account found mid-read is
          exactly when "manage accounts" wants to be one click away. */}
      <button
        type="button"
        class="w-full text-left px-2 py-1.5 mt-1 rounded-sm text-xs text-ink-muted border-t border-hairline hover:text-seal transition-colors duration-fast flex items-center gap-1.5 flex-shrink-0"
        onClick={onConfigure}
      >
        <SettingsIcon size={11} class="flex-shrink-0" /> Manage accounts
      </button>
    </nav>
  );

  const listPane = (
    <div class="lg:w-72 flex-shrink-0 flex flex-col lg:border-r border-hairline lg:pr-3 min-w-0 overflow-y-auto max-h-48 lg:max-h-none" aria-label="messages">
      {listError ? (
        <p class="text-xs text-cinnabar px-2 py-2 leading-relaxed">{listError}</p>
      ) : list === null ? (
        <div class="py-6 flex justify-center" aria-label="loading messages"><Dodec size={24} animate={false} class="opacity-40" /></div>
      ) : list.messages.length === 0 ? (
        <p class="text-xs text-ink-muted px-2 py-2">{box} is empty.</p>
      ) : (
        <>
          {list.messages.map(m => (
            <button
              key={m.id}
              type="button"
              class={`w-full text-left px-2 py-2 rounded-sm transition-colors duration-fast ${m.id === selected ? 'bg-seal-light' : 'hover:bg-seal-light'} ${m.trashed ? 'opacity-60' : ''}`}
              onClick={() => setSelected(m.id)}
            >
              <span class="flex items-center gap-1.5 min-w-0">
                {!m.seen && <span class="w-1.5 h-1.5 rounded-full bg-cinnabar flex-shrink-0" aria-label="unread" />}
                <span class={`truncate text-sm ${m.seen ? 'text-ink-light' : 'text-ink-light font-medium'}`}>{m.subject || '(no subject)'}</span>
              </span>
              <span class="block truncate text-xs text-ink-muted mt-0.5">{m.from || '(nobody)'}</span>
              <span class="flex items-center gap-1.5 text-[11px] text-ink-muted mt-0.5">
                <span class="tabular-nums">{formatDate(m.date, today)}</span>
                {m.answered && <Reply size={10} aria-label="answered" />}
                {m.flagged && <Flag size={10} class="text-cinnabar" aria-label="flagged" />}
                {m.trashed && <Trash2 size={10} aria-label="trashed" />}
                <span class="ml-auto tabular-nums">{formatSize(m.size)}</span>
              </span>
            </button>
          ))}
          {list.stats.degraded && (
            <p class="text-[11px] text-ink-muted border-t border-hairline mt-1 pt-2 px-2">
              Some mail was skipped ({list.stats.skipped} message{list.stats.skipped === 1 ? '' : 's'} unparsable).
            </p>
          )}
        </>
      )}
    </div>
  );

  const readPane = (
    <div class="flex-1 min-w-0 overflow-y-auto pl-1" aria-label="message">
      {!selected ? (
        <p class="text-xs text-ink-muted py-6 text-center">Select a message to read it.</p>
      ) : fullError ? (
        <p class="text-xs text-cinnabar py-2 leading-relaxed">{fullError}</p>
      ) : fullPending || !full ? (
        <div class="py-6 flex justify-center" aria-label="loading message"><Dodec size={24} animate={false} class="opacity-40" /></div>
      ) : (
        <article class="flex flex-col gap-2">
          <header class="border-b border-hairline pb-2">
            <h3 class="text-sm font-medium text-ink-light leading-snug">{full.subject || '(no subject)'}</h3>
            <dl class="mt-1.5 grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 text-xs text-ink-muted">
              <dt class="font-medium">From</dt><dd class="truncate">{full.from || '(nobody)'}</dd>
              {full.to && <><dt class="font-medium">To</dt><dd class="truncate">{full.to}</dd></>}
              <dt class="font-medium">Date</dt><dd class="tabular-nums">{new Date(full.date).toLocaleString()}</dd>
              <dt class="font-medium">Size</dt><dd class="tabular-nums">{formatSize(full.size)}</dd>
            </dl>
            <div class="flex items-center gap-2 mt-1.5">
              {!full.seen && <span class="text-[10px] font-semibold uppercase tracking-wider text-cinnabar">unread</span>}
              {full.answered && <span class="inline-flex items-center gap-0.5 text-[10px] text-ink-muted"><Reply size={10} /> answered</span>}
              {full.flagged && <span class="inline-flex items-center gap-0.5 text-[10px] text-cinnabar"><Flag size={10} /> flagged</span>}
              {full.trashed && <span class="inline-flex items-center gap-0.5 text-[10px] text-ink-muted"><Trash2 size={10} /> trashed</span>}
              {full.wasHtml && <span class="text-[10px] text-ink-muted">html → text</span>}
            </div>
          </header>
          {full.truncated ? (
            <p class="text-xs text-ink-muted leading-relaxed py-2 border border-hairline rounded-sm px-2.5 bg-paper">
              This message is larger than the reader's cap — only its headers are shown, never a body cut mid-sentence.
            </p>
          ) : (
            <pre class="text-sm text-ink-light whitespace-pre-wrap break-words font-sans leading-relaxed min-h-0">{full.body || '(no text body)'}</pre>
          )}
        </article>
      )}
    </div>
  );

  return (
    <Modal
      title="Mail"
      subtitle={box ? `${account ? `${account} · ` : ''}${box}${list ? ` · ${list.messages.length} shown` : ''} · read-only` : 'loading…'}
      onClose={onClose}
      size="xl"
    >
      <div class="flex flex-col gap-3 h-[70vh] min-h-0">
        <AccountBar accounts={accounts} active={account} onPick={setAccount} />
        {railError && (
          <p class="flex items-start gap-1.5 text-[11px] text-cinnabar leading-relaxed -mt-1">
            <TriangleAlert size={11} class="flex-shrink-0 mt-0.5" />
            <span class="min-w-0 break-words">{railError}</span>
          </p>
        )}
        <div class="flex flex-col lg:flex-row gap-3 flex-1 min-h-0">
          {railPane}
          {listPane}
          {readPane}
        </div>
      </div>
    </Modal>
  );
}
