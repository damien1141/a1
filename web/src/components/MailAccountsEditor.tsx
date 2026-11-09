import { useEffect, useRef, useState } from 'preact/hooks';
import { Check, Cloud, Eye, EyeOff, HardDrive, KeyRound, Loader2, Mail, Pencil, Plus, Trash2, TriangleAlert } from 'lucide-preact';
import { Modal } from './Modal';
import { Dodec } from './Dodec';
import * as api from '../api';

// MailAccountsEditor is the dashboard's mail CONFIGURATOR (chunk 8): the
// modal that answers "I'd like the ability to configure multiple accounts
// and mail servers" without opening config.yaml. It edits the SAVED
// account list — local Maildirs and IMAP servers, app passwords and all —
// through three endpoints: the read model (never a password), the
// whole-list save (keepPassword resolves credentials by name
// server-side), and the test dial (persists nothing).
//
// The write ladder it lives under: this surface writes the KEYRING, never
// the letters. No control here sends, moves, marks, or deletes a message
// — the reader stays read-only, and the agent's facade keeps to
// envelopes. Editing an account is editing configuration, the same object
// the /settings page edits; the mail itself stays untouchable by design.
//
// The editing model is a local working copy: rows change in place (Add
// appends a draft, Edit expands the form under the row, Remove drops it),
// the footer's Save persists the WHOLE list in one PUT, and closing
// without saving discards — the subtitle says so while edits are pending.

// Row is the editor's working copy of one account. port stays a string
// because the field is a text input; hasPassword is the server's truth
// (the credential itself never crosses the wire this direction), and it
// drives the password field's placeholder: blank + saved = keep.
interface Row {
  name: string;
  type: 'imap' | 'maildir';
  root: string;
  host: string;
  port: string;
  username: string;
  password: string;
  mailbox: string;
  tls: boolean;
  hasPassword: boolean;
}

function rowFromEntry(e: api.MailAccountEntry): Row {
  return {
    name: e.name,
    type: e.type === 'imap' ? 'imap' : 'maildir',
    root: e.root || '',
    host: e.host || '',
    port: e.port ? String(e.port) : '',
    username: e.username || '',
    password: '',
    mailbox: e.mailbox || '',
    tls: e.tls !== undefined ? e.tls : true,
    hasPassword: e.hasPassword,
  };
}

// specFromRow projects a row into the wire spec: only the fields its type
// carries ride (a leftover host on a maildir row is form noise, not
// config). keepPassword is the blank-field-means-keep contract.
function specFromRow(r: Row): api.MailAccountSpec {
  const spec: api.MailAccountSpec = {
    name: r.name.trim(),
    type: r.type,
  };
  if (r.type === 'imap') {
    spec.host = r.host.trim();
    if (r.port.trim() !== '') spec.port = parseInt(r.port.trim(), 10);
    spec.username = r.username.trim();
    if (r.password !== '') spec.password = r.password;
    else if (r.hasPassword) spec.keepPassword = true;
    if (r.mailbox.trim() !== '') spec.mailbox = r.mailbox.trim();
    spec.tls = r.tls;
  } else {
    spec.root = r.root.trim();
  }
  return spec;
}

// validateRow is the fast client-side pre-check (the server re-validates;
// this one focuses the right form instead of answering a 400).
function validateRow(r: Row): string | null {
  if (r.type === 'imap') {
    if (!r.host.trim()) return 'an IMAP account needs a server';
    if (r.port.trim() !== '') {
      const p = parseInt(r.port.trim(), 10);
      if (isNaN(p) || p < 1 || p > 65535) return 'port must be 1-65535 (or blank for the default)';
    }
  } else {
    if (!r.root.trim()) return 'a Maildir account needs a root path';
  }
  return null;
}

interface Props {
  onClose: () => void;
  // onSaved fires after a successful save so the dashboard can refetch
  // its personal status and the mail card its rail — the new account is
  // live behind this modal while it is still open.
  onSaved: () => void;
}

export function MailAccountsEditor({ onClose, onSaved }: Props) {
  const [rows, setRows] = useState<Row[] | null>(null);
  const [legacyDir, setLegacyDir] = useState('');
  const [loadError, setLoadError] = useState('');
  const [editing, setEditing] = useState<number | null>(null);
  const [formError, setFormError] = useState('');
  const [saveError, setSaveError] = useState('');
  const [saving, setSaving] = useState(false);
  const [savedFlash, setSavedFlash] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<api.MailTestResponse | null>(null);
  const [testError, setTestError] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const savedTimer = useRef<ReturnType<typeof setTimeout>>(null);

  // The initial truth: entries + the legacy root. A 403 mid-flight (the
  // switch flipped while the tab was open) is a load failure like any
  // other — the modal says so honestly instead of guessing.
  useEffect(() => {
    let alive = true;
    api.fetchMailAccounts()
      .then(r => {
        if (!alive) return;
        setRows(r.entries.map(rowFromEntry));
        setLegacyDir(r.legacyDir || '');
      })
      .catch((err: Error) => { if (alive) setLoadError(err.message || 'The account list could not be read.'); });
    return () => { alive = false; };
  }, []);

  useEffect(() => () => { if (savedTimer.current) clearTimeout(savedTimer.current); }, []);

  const update = (i: number, patch: Partial<Row>) => {
    setRows(rs => (rs || []).map((r, j) => (j === i ? { ...r, ...patch } : r)));
    setFormError('');
    setTestResult(null);
    setTestError('');
  };

  const addRow = () => {
    const draft: Row = { name: '', type: 'imap', root: '', host: '', port: '', username: '', password: '', mailbox: '', tls: true, hasPassword: false };
    setRows(rs => [...(rs || []), draft]);
    setEditing((rows || []).length);
    setFormError('');
    setTestResult(null);
    setTestError('');
  };

  const removeRow = (i: number) => {
    setRows(rs => ((rs || []).filter((_, j) => j !== i)));
    if (editing === i) setEditing(null);
    else if (editing !== null && editing > i) setEditing(editing - 1);
    setFormError('');
    setTestResult(null);
    setTestError('');
  };

  // Save persists the whole list. The pre-check opens the first bad row's
  // form with the reason; the server's own 400 (should the client check
  // drift) renders inline the same way.
  const save = async () => {
    if (!rows) return;
    for (let i = 0; i < rows.length; i++) {
      const row = rows[i];
      if (!row) continue;
      const err = validateRow(row);
      if (err) {
        setEditing(i);
        setFormError(err);
        return;
      }
    }
    setSaving(true);
    setSaveError('');
    try {
      const fresh = await api.saveMailAccounts(rows.map(specFromRow));
      setRows(fresh.entries.map(rowFromEntry));
      setEditing(null);
      setFormError('');
      setTestResult(null);
      setTestError('');
      setSavedFlash(true);
      if (savedTimer.current) clearTimeout(savedTimer.current);
      savedTimer.current = setTimeout(() => setSavedFlash(false), 2500);
      onSaved();
    } catch (err) {
      setSaveError((err as Error).message || 'The accounts could not be saved.');
    } finally {
      setSaving(false);
    }
  };

  // Test dials the CURRENT form's spec — before it is saved, with the
  // keep contract intact (a saved account's stored password is used
  // server-side without this client ever seeing it).
  const test = async () => {
    if (editing === null || !rows) return;
    const row = rows[editing];
    if (!row) return;
    const err = validateRow(row);
    if (err) {
      setFormError(err);
      return;
    }
    setTesting(true);
    setTestResult(null);
    setTestError('');
    try {
      setTestResult(await api.testMailAccount(specFromRow(row)));
    } catch (e) {
      setTestError((e as Error).message || 'The test could not run.');
    } finally {
      setTesting(false);
    }
  };

  const inputCls = 'mt-0.5 w-full px-2 py-1.5 text-sm bg-paper border border-hairline rounded-sm outline-none focus:border-seal';
  const labelCls = 'block';
  const labelSpanCls = 'text-xs text-ink-muted';

  // One account row: icon, identity, its server or root, and the edit /
  // remove controls. The form expands beneath it when editing. (Not a
  // component — a function returning the row's chrome, so the caller's
  // map can own the key.)
  const rowCard = (r: Row, i: number) => {
    const isEditing = editing === i;
    const placeholder = r.type === 'imap' ? (r.host.trim() || 'unnamed server') : (r.root.trim() || 'unnamed maildir');
    return (
      <div class={`border border-hairline rounded-sm bg-paper ${isEditing ? '' : 'hover:border-seal/30'} transition-colors duration-fast`}>
        <div class="flex items-center gap-2 px-2.5 py-2">
          {r.type === 'imap'
            ? <Cloud size={13} class="text-seal flex-shrink-0" />
            : <HardDrive size={13} class="text-seal flex-shrink-0" />}
          <div class="min-w-0 flex-1">
            <span class="flex items-center gap-1.5 min-w-0">
              <span class={`truncate text-sm ${r.name ? 'text-ink-light font-medium' : 'text-ink-muted italic'}`}>
                {r.name || placeholder}
              </span>
              {r.type === 'imap' && r.hasPassword && (
                <span class="inline-flex items-center gap-0.5 text-[10px] text-ink-muted flex-shrink-0" title="An app password is saved for this account">
                  <KeyRound size={9} /> saved
                </span>
              )}
            </span>
            <span class="block truncate text-xs text-ink-muted mt-0.5">
              {r.type === 'imap'
                ? `${r.host || '(no server)'}${r.port ? `:${r.port}` : ''}${r.username ? ` · ${r.username}` : ''}`
                : r.root || '(no root)'}
            </span>
          </div>
          <button
            type="button"
            class="p-1.5 rounded-sm text-ink-muted hover:text-seal hover:bg-seal-light transition-colors duration-fast flex-shrink-0"
            onClick={() => { setEditing(isEditing ? null : i); setFormError(''); setTestResult(null); setTestError(''); }}
            title={isEditing ? 'Done' : 'Edit'}
            aria-label={isEditing ? `Done editing ${r.name || placeholder}` : `Edit ${r.name || placeholder}`}
          >
            {isEditing ? <Check size={13} /> : <Pencil size={13} />}
          </button>
          <button
            type="button"
            class="p-1.5 rounded-sm text-ink-muted hover:text-cinnabar hover:bg-cinnabar/10 transition-colors duration-fast flex-shrink-0"
            onClick={() => removeRow(i)}
            title="Remove"
            aria-label={`Remove ${r.name || placeholder}`}
          >
            <Trash2 size={13} />
          </button>
        </div>

        {isEditing && (
          <div class="border-t border-hairline px-2.5 py-3 flex flex-col gap-2.5">
            {/* The kind of account picks the fields. */}
            <div class="flex gap-1" role="radiogroup" aria-label="Account type">
              {(['imap', 'maildir'] as const).map(ty => (
                <button
                  key={ty}
                  type="button"
                  role="radio"
                  aria-checked={r.type === ty}
                  class={`flex-1 inline-flex items-center justify-center gap-1.5 px-2 py-1.5 rounded-sm text-xs border transition-colors duration-fast ${
                    r.type === ty ? 'bg-seal-light border-seal/40 text-seal font-medium' : 'bg-paper border-hairline text-ink-light hover:border-seal/40 hover:text-seal'
                  }`}
                  onClick={() => update(i, { type: ty })}
                >
                  {ty === 'imap' ? <Cloud size={11} /> : <HardDrive size={11} />}
                  {ty === 'imap' ? 'IMAP server' : 'Local Maildir'}
                </button>
              ))}
            </div>

            {r.type === 'imap' ? (
              <>
                <div class="grid grid-cols-1 sm:grid-cols-[1fr_7rem] gap-2.5">
                  <label class={labelCls}>
                    <span class={labelSpanCls}>Server</span>
                    <input
                      type="text"
                      value={r.host}
                      onInput={e => update(i, { host: (e.target as HTMLInputElement).value })}
                      placeholder="imap.gmail.com"
                      class={inputCls}
                      autoComplete="off"
                    />
                  </label>
                  <label class={labelCls}>
                    <span class={labelSpanCls}>Port</span>
                    <input
                      type="text"
                      inputMode="numeric"
                      value={r.port}
                      onInput={e => update(i, { port: (e.target as HTMLInputElement).value })}
                      placeholder={r.tls ? '993' : '143'}
                      class={inputCls}
                      autoComplete="off"
                    />
                  </label>
                </div>
                <div class="grid grid-cols-1 sm:grid-cols-2 gap-2.5">
                  <label class={labelCls}>
                    <span class={labelSpanCls}>Username</span>
                    <input
                      type="text"
                      value={r.username}
                      onInput={e => update(i, { username: (e.target as HTMLInputElement).value })}
                      placeholder="you@example.com"
                      class={inputCls}
                      autoComplete="off"
                    />
                  </label>
                  <label class={labelCls}>
                    <span class={labelSpanCls}>App password</span>
                    <span class="relative block">
                      <input
                        type={showPassword ? 'text' : 'password'}
                        value={r.password}
                        onInput={e => update(i, { password: (e.target as HTMLInputElement).value })}
                        placeholder={r.hasPassword ? 'leave blank to keep the saved one' : 'app password, not your login'}
                        class={`${inputCls} pr-8`}
                        autoComplete="new-password"
                      />
                      <button
                        type="button"
                        class="absolute right-1 top-1/2 -translate-y-1/2 p-1 rounded-sm text-ink-muted hover:text-ink transition-colors duration-fast"
                        onClick={() => setShowPassword(s => !s)}
                        title={showPassword ? 'Hide' : 'Show'}
                        aria-label={showPassword ? 'Hide password' : 'Show password'}
                      >
                        {showPassword ? <EyeOff size={12} /> : <Eye size={12} />}
                      </button>
                    </span>
                  </label>
                </div>
                <p class="text-[11px] text-ink-muted leading-relaxed">
                  Gmail, Outlook, and friends want an <span class="text-ink-light">app password</span>, not your login — it is stored in config.yaml beside the model keys, never shown again.
                </p>
                <div class="flex flex-wrap items-center gap-4">
                  <label class="inline-flex items-center gap-1.5 text-xs text-ink-light cursor-pointer">
                    <input
                      type="checkbox"
                      checked={r.tls}
                      onChange={e => update(i, { tls: (e.target as HTMLInputElement).checked })}
                      class="accent-seal"
                    />
                    Implicit TLS <span class="text-ink-muted">(port 993; off = plaintext 143)</span>
                  </label>
                  <label class={`${labelCls} flex-1 min-w-[8rem]`}>
                    <span class={labelSpanCls}>First mailbox <span class="text-ink-muted/70">(optional)</span></span>
                    <input
                      type="text"
                      value={r.mailbox}
                      onInput={e => update(i, { mailbox: (e.target as HTMLInputElement).value })}
                      placeholder="INBOX"
                      class={inputCls}
                      autoComplete="off"
                    />
                  </label>
                </div>
              </>
            ) : (
              <label class={labelCls}>
                <span class={labelSpanCls}>Maildir root</span>
                <input
                  type="text"
                  value={r.root}
                  onInput={e => update(i, { root: (e.target as HTMLInputElement).value })}
                  placeholder="~/Maildir"
                  class={inputCls}
                  autoComplete="off"
                />
                <span class="block text-[11px] text-ink-muted mt-1">A directory of mailboxes — each a folder with <code class="bg-ink/[0.06] px-1 rounded-sm text-[10px]">cur/</code> and <code class="bg-ink/[0.06] px-1 rounded-sm text-[10px]">new/</code> inside. Read-only, always.</span>
              </label>
            )}

            <label class={labelCls}>
              <span class={labelSpanCls}>Name <span class="text-ink-muted/70">(optional — shown in the account bar)</span></span>
              <input
                type="text"
                value={r.name}
                onInput={e => update(i, { name: (e.target as HTMLInputElement).value })}
                placeholder={r.type === 'imap' ? (r.host.trim() || 'the server') : (r.root.trim() || 'the root')}
                class={inputCls}
                maxLength={64}
              />
              {r.type === 'imap' && r.hasPassword && (
                <span class="block text-[11px] text-ink-muted mt-1">Renaming does not carry the saved app password — re-enter it after a rename.</span>
              )}
            </label>

            {/* The dial: test before saving, with the keep contract. */}
            <div class="flex flex-col gap-1.5">
              <button
                type="button"
                disabled={testing}
                class="self-start inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-sm text-xs border border-hairline bg-paper text-ink-light hover:text-seal hover:border-seal/40 transition-colors duration-fast disabled:opacity-60"
                onClick={test}
              >
                {testing ? <Loader2 size={12} class="animate-spin" /> : <Cloud size={12} />}
                {testing ? 'Testing…' : 'Test connection'}
              </button>
              {testError && (
                <p class="text-[11px] text-cinnabar leading-relaxed">{testError}</p>
              )}
              {testResult && !testResult.ok && (
                <p class="flex items-start gap-1.5 text-[11px] text-cinnabar leading-relaxed">
                  <TriangleAlert size={11} class="flex-shrink-0 mt-0.5" />
                  <span class="min-w-0 break-words">{testResult.error || 'The server did not answer.'}</span>
                </p>
              )}
              {testResult && testResult.ok && (
                <p class="flex items-start gap-1.5 text-[11px] text-seal leading-relaxed">
                  <Check size={11} class="flex-shrink-0 mt-0.5" />
                  <span>
                    Reached it — {testResult.boxes} mailbox{testResult.boxes === 1 ? '' : 'es'}
                    {testResult.sample && testResult.sample.length > 0 && <> ({testResult.sample.join(', ')}{testResult.boxes! > testResult.sample.length ? ', …' : ''})</>}.
                  </span>
                </p>
              )}
            </div>

            {formError && (
              <p class="flex items-start gap-1.5 text-[11px] text-cinnabar leading-relaxed">
                <TriangleAlert size={11} class="flex-shrink-0 mt-0.5" />
                <span>This account needs {formError}.</span>
              </p>
            )}
          </div>
        )}
      </div>
    );
  };

  const body = () => {
    if (loadError) {
      return (
        <div class="p-4 flex flex-col items-center gap-3 text-center">
          <TriangleAlert size={20} class="text-cinnabar" />
          <p class="text-xs text-ink-muted leading-relaxed max-w-sm">{loadError}</p>
          <button
            type="button"
            class="px-2.5 py-1.5 rounded-sm text-xs border border-hairline bg-paper text-ink-light hover:text-seal hover:border-seal/40 transition-colors duration-fast"
            onClick={() => { setLoadError(''); location.reload(); }}
          >
            Reload
          </button>
        </div>
      );
    }
    if (rows === null) {
      return (
        <div class="py-10 flex justify-center" aria-label="loading accounts">
          <Dodec size={28} animate={false} class="opacity-40" />
        </div>
      );
    }
    return (
      <div class="flex flex-col gap-2.5">
        {legacyDir && (
          <div class="flex items-center gap-2 px-2.5 py-2 border border-hairline border-dashed rounded-sm bg-paper-sunk">
            <HardDrive size={13} class="text-ink-muted flex-shrink-0" />
            <div class="min-w-0 flex-1">
              <span class="flex items-center gap-1.5">
                <span class="text-sm text-ink-light font-medium">local</span>
                <span class="text-[10px] text-ink-muted uppercase tracking-wider">mail_dir · read-only</span>
              </span>
              <span class="block truncate text-xs text-ink-muted mt-0.5" title={legacyDir}>{legacyDir}</span>
            </div>
          </div>
        )}
        {rows.length === 0 && !legacyDir && (
          <p class="text-xs text-ink-muted leading-relaxed">
            No mail accounts yet — add an IMAP server (Gmail, Outlook, your own box) or a local Maildir, and they all read here, side by side.
          </p>
        )}
        <ul class="flex flex-col gap-2 list-none m-0 p-0">
          {rows.map((r, i) => (
            <li key={i} class="list-none">{rowCard(r, i)}</li>
          ))}
        </ul>
        <button
          type="button"
          class="self-start inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-sm text-xs border border-dashed border-hairline text-ink-muted hover:text-seal hover:border-seal/40 transition-colors duration-fast"
          onClick={addRow}
        >
          <Plus size={12} /> Add account
        </button>
        {saveError && (
          <p class="flex items-start gap-1.5 text-[11px] text-cinnabar leading-relaxed">
            <TriangleAlert size={11} class="flex-shrink-0 mt-0.5" />
            <span class="min-w-0 break-words">{saveError}</span>
          </p>
        )}
      </div>
    );
  };

  const count = rows?.length ?? 0;
  const subtitle = saving ? 'saving…'
    : savedFlash ? 'saved — the rail is live'
    : editing !== null ? 'editing — unsaved changes are discarded on close'
    : `${count} account${count === 1 ? '' : 's'}${legacyDir ? ' + the mail_dir local' : ''} · the keyring, never the letters`;

  return (
    <Modal
      title="Mail accounts"
      subtitle={subtitle}
      onClose={onClose}
      size="md"
      footer={
        <>
          <span class="mr-auto text-[11px] text-ink-muted flex items-center gap-1">
            {savedFlash ? <Check size={11} class="text-seal" /> : <Mail size={11} />}
            {savedFlash ? 'Saved — accounts are live.' : 'Readers stay read-only; this edits config only.'}
          </span>
          <button
            type="button"
            class="px-3 py-1.5 rounded-sm text-xs border border-hairline bg-paper text-ink-light hover:text-ink transition-colors duration-fast"
            onClick={onClose}
          >
            Close
          </button>
          <button
            type="button"
            disabled={saving || rows === null}
            class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-sm text-xs font-medium bg-seal text-on-seal hover:bg-seal/90 transition-colors duration-fast disabled:opacity-60"
            onClick={save}
          >
            {saving ? <Loader2 size={12} class="animate-spin" /> : <Check size={12} />}
            {count === 0 ? 'Remove all' : `Save ${count} account${count === 1 ? '' : 's'}`}
          </button>
        </>
      }
    >
      <div class="p-4">
        {body()}
      </div>
    </Modal>
  );
}
