import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import {
  ArrowRight, ArrowUp, ChevronRight, FileSymlink, FileText, Folder,
  FolderSymlink, FolderTree, Pencil, Save, Undo2, X,
} from 'lucide-preact';
import { Dodec } from './Dodec';
import * as api from '../api';
import { highlightCode } from '../highlight';

// FileBrowser is the dashboard's split-screen right pane (chunk 5): a
// full-system file browser — the address bar accepts any absolute path the
// server's own user may visit, because the ask was exactly that. The pane
// is workspace machinery, not personal data: no personalGate, no master
// switch, and the ONE write (Save) is the textarea's full text over an
// atomic temp+rename — every refusal (read-only mode, symlink targets,
// missing parents, the 2 MB cap) comes back from the server as an honest
// error and lands in a toast, never a silent rollback.
//
// The pane is KEEP-ALIVE: it mounts once with the app and `open` is a
// class flip, never a mount/unmount — the same architecture as the
// session drawer. That is what makes the exit animation possible at all
// (nothing can unmount mid-frame) and it comes with two behaviors that
// fall out for free: reopening returns to the folder (and file, and
// draft) the pane was left in, and the InputBar's toggle — which has
// never carried the close button's dirty guard — can no longer silently
// drop a draft, because closing no longer destroys anything. The reveal
// itself is `.file-pane` (styles.css): below lg the pane is a
// full-screen sheet that slides from the right; at lg it is a flex
// sibling of the chat whose WIDTH grows from the screen's right edge —
// the inner column below keeps a fixed width and is clipped by the
// shell, so the content is uncovered by the right edge sweeping left,
// never re-flowed. When closed the shell carries `inert`, so the hidden
// pane is out of the tab order and the accessibility tree.
//
// The pane's two views share one address bar + breadcrumb strip:
//   listing  — dirs first (the server's sort), click a dir to navigate,
//              click a file to preview
//   file     — highlight.js preview (the language the server's extension
//              map picked; unmapped ⇒ plaintext) with Edit → textarea,
//              a dirty indicator, Save (Ctrl/Cmd+S) and Discard. Leaving
//              with unsaved edits is refused — the guard is a disabled
//              button and a toast, not a modal.

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

function formatSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${Math.max(1, Math.round(n / 1024))} kB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

// formatStamp renders "3 Nov 14:05" (today), "3 Nov 14:05" any same year,
// "3 Nov 2024" otherwise — the mail rows' clock, reused.
function formatStamp(iso: string, today: Date): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return '';
  const hm = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
  const day = `${d.getDate()} ${MONTHS[d.getMonth()]}`;
  return d.getFullYear() === today.getFullYear() ? `${day} ${hm}` : `${day} ${d.getFullYear()}`;
}

function basename(p: string): string {
  const parts = p.split('/').filter(Boolean);
  return parts.length ? parts[parts.length - 1] : p || '/';
}

// isDisabledError spots the kill-switch's 503 — the one failure that is a
// configuration ruling rather than a filesystem fact, so it gets its own
// copy instead of a red error line.
function isDisabledError(err: string): boolean {
  return err.includes('A1_FILES');
}

// editHighlightCap bounds the live-highlighted editing: past it the draft
// edits as a plain textarea (the honest fallback — re-highlighting a
// multi-megabyte file per keystroke would cost more than the color is
// worth). The preview keeps highlighting at any size; only the per-
// keystroke editor path is capped.
const editHighlightCap = 256 * 1024;

interface Props {
  open: boolean;
  onClose: () => void;
  onToast: (t: api.ToastPayload) => void;
}

export function FileBrowser({ open, onClose, onToast }: Props) {
  // dir is '' until the first listing resolves (empty path ⇒ the server's
  // cwd); after that it is always the absolute path the crumbs show.
  const [dir, setDir] = useState('');
  const [listing, setListing] = useState<api.FilesListResponse | null>(null);
  const [listPending, setListPending] = useState(true);
  const [listError, setListError] = useState('');
  const [reloadKey, setReloadKey] = useState(0);

  // The address bar is a DRAFT: it syncs from the resolved listing path
  // after every navigation, and what the user types only matters on Enter
  // or Go.
  const [addr, setAddr] = useState('');

  // The file view.
  const [selected, setSelected] = useState<string | null>(null);
  const [file, setFile] = useState<api.FilesReadResponse | null>(null);
  const [filePending, setFilePending] = useState(false);
  const [fileError, setFileError] = useState('');

  // The edit flow.
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState('');
  const [saving, setSaving] = useState(false);

  // Highlighted editing (chunk 7): the ask was "the file browser only
  // shows syntax highlighting on preview — not when editing". The trick
  // is the classic editor overlay: a live-highlighted <pre> BENEATH a
  // transparent-text textarea, identical font/leading/padding so the
  // glyphs land on the same grid, the textarea owning every event (the
  // pre is pointer-events-none and aria-hidden) and scrolling both layers
  // in lockstep. The highlight trails the draft synchronously up to the
  // cap; past it the textarea keeps its own color (plain but honest).
  const taRef = useRef<HTMLTextAreaElement>(null);
  const preRef = useRef<HTMLPreElement>(null);
  const syncScroll = () => {
    if (taRef.current && preRef.current) {
      preRef.current.scrollTop = taRef.current.scrollTop;
      preRef.current.scrollLeft = taRef.current.scrollLeft;
    }
  };
  const editLang = file?.lang || '';
  const editHighlight = useMemo(() => {
    if (!editing || draft.length > editHighlightCap) return '';
    // The trailing newline: a <pre> collapses the empty line after a
    // final \n while the textarea's caret sits there — one extra newline
    // keeps the two layers the same height.
    return highlightCode(draft + '\n', editLang);
  }, [editing, draft, editLang]);

  const dirty = !!file && draft !== file.content;
  const today = new Date();

  // ---- listing ----
  // Gated on `open`: the pane is mounted from app load (keep-alive), so
  // the fetch must wait for the first open — and re-firing on every open
  // is the quiet refresh that catches changes made behind the pane's
  // back. The listing stays rendered while it refreshes (the spinner is
  // first-load only), so reopening never flashes.
  useEffect(() => {
    if (!open) return;
    let alive = true;
    setListPending(true);
    setListError('');
    api.fetchFilesList(dir || undefined)
      .then(r => {
        if (!alive) return;
        setListing(r);
        setAddr(r.path);
      })
      .catch((err: Error) => { if (alive) setListError(err.message || 'The folder could not be read.'); })
      .finally(() => { if (alive) setListPending(false); });
    return () => { alive = false; };
  }, [open, dir, reloadKey]);

  // ---- the selected file ----
  useEffect(() => {
    if (!selected) return;
    let alive = true;
    setFile(null);
    setFileError('');
    setFilePending(true);
    setEditing(false);
    setDraft('');
    api.fetchFilesRead(selected)
      .then(r => { if (alive) { setFile(r); setDraft(r.content); } })
      .catch((err: Error) => { if (alive) setFileError(err.message || 'The file could not be read.'); })
      .finally(() => { if (alive) setFilePending(false); });
    return () => { alive = false; };
  }, [selected]);

  // ---- navigation with the dirty guard ----

  // leaveWith guards every way out of an unsaved edit: the button the user
  // is about to click goes quiet and the toast says why. A modal would be
  // heavier than the stake (the draft is one textarea, the cap is 2 MB).
  const leaveWith = (action: () => void) => {
    if (editing && dirty) {
      onToast({ level: 'warning', message: 'Save or discard the edit first — the pane refuses to drop a draft quietly.' });
      return;
    }
    action();
  };

  const navigate = (p: string) => leaveWith(() => {
    setSelected(null);
    setDir(p.replace(/\/+$/, '') || (p === '/' ? '/' : ''));
  });

  const join = (base: string, name: string) =>
    base === '/' ? `/${name}` : `${base}/${name}`;

  const goAddr = () => leaveWith(() => {
    setSelected(null);
    setDir(addr.trim());
  });

  const goUp = () => {
    if (!listing) return;
    if (listing.path === '/') return; // already the root
    navigate(listing.path.replace(/\/[^/]*$/, '') || '/');
  };

  const backToListing = () => leaveWith(() => setSelected(null));

  // ---- save / discard ----

  const save = () => {
    if (!selected || !dirty || saving) return;
    setSaving(true);
    api.writeFiles(selected, draft)
      .then(r => {
        setFile(f => (f ? { ...f, content: draft, size: r.size } : f));
        setEditing(false);
        setReloadKey(k => k + 1); // the listing's size/mtime catch up quietly
        onToast({ level: 'success', message: `Saved ${basename(r.path)} — ${formatSize(r.size)} on disk.` });
      })
      .catch((err: Error) => {
        // The server's refusals are sentences already (read-only mode,
        // symlink targets, missing parents, the cap) — show them whole.
        onToast({ level: 'error', message: err.message || 'The save was refused.' });
      })
      .finally(() => setSaving(false));
  };

  const discard = () => {
    if (!file) return;
    setDraft(file.content);
    setEditing(false);
  };

  // ---- breadcrumbs ----
  const crumbs = useMemo(() => {
    const p = listing?.path || '';
    if (!p) return [];
    const out: { label: string; path: string }[] = [{ label: '/', path: '/' }];
    let acc = '';
    for (const part of p.split('/').filter(Boolean)) {
      acc += `/${part}`;
      out.push({ label: part, path: acc });
    }
    return out;
  }, [listing]);

  // Escape backs out one level at a time (file → listing, edit → file) —
  // scoped to the pane's own keydown, so a focused modal elsewhere keeps
  // its own Escape. A dirty edit refuses the exit, same as the buttons.
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key !== 'Escape') return;
    if (editing) {
      if (!dirty) setEditing(false);
      return;
    }
    if (selected) backToListing();
  };

  // ---- the rows ----
  const row = (e: api.FileEntry) => {
    const full = listing ? join(listing.path, e.name) : e.name;
    const isSel = full === selected;
    const tip = [
      e.name,
      e.dir ? 'directory' : formatSize(e.size),
      listing ? formatStamp(e.mtime, today) : '',
      e.symlink ? `→ ${e.target || '(unreadable target)'}` : '',
    ].filter(Boolean).join(' · ');
    return (
      <button
        key={e.name}
        type="button"
        title={tip}
        aria-label={`${e.name}${e.dir ? ' (directory)' : ''}`}
        class={`w-full flex items-center gap-2 px-2 py-1.5 rounded-sm text-left transition-colors duration-fast ${
          isSel ? 'bg-seal-light' : 'hover:bg-seal-light'
        }`}
        onClick={() => (e.dir ? navigate(full) : leaveWith(() => setSelected(full)))}
      >
        {e.dir
          ? (e.symlink ? <FolderSymlink size={13} class="flex-shrink-0 text-ink-muted" /> : <Folder size={13} class="flex-shrink-0 text-ink-muted" />)
          : (e.symlink ? <FileSymlink size={13} class="flex-shrink-0 text-ink-muted" /> : <FileText size={13} class="flex-shrink-0 text-ink-muted" />)}
        <span class="min-w-0 flex-1 truncate text-sm text-ink-light">{e.name}</span>
        {e.symlink && <ChevronRight size={10} class="flex-shrink-0 text-ink-muted opacity-60" aria-label="symlink" />}
        {!e.dir && <span class="flex-shrink-0 text-[11px] text-ink-muted tabular-nums">{formatSize(e.size)}</span>}
      </button>
    );
  };

  const listingBody = () => {
    if (listPending && !listing) {
      return <div class="py-8 flex justify-center" aria-label="loading folder"><Dodec size={26} animate={false} class="opacity-40" /></div>;
    }
    if (listError) {
      if (isDisabledError(listError)) {
        return (
          <div class="text-xs text-ink-muted leading-relaxed px-1 py-6 text-center">
            <p class="flex items-center justify-center gap-1.5 text-ink-light mb-1"><FolderTree size={12} /> The file surface is off</p>
            <p>The supervisor set <code class="text-[11px] bg-ink/[0.06] px-1 rounded-sm">A1_FILES=off</code> — every file endpoint answers 503 until it is cleared.</p>
          </div>
        );
      }
      return <p class="text-xs text-cinnabar leading-relaxed px-1 py-3">{listError}</p>;
    }
    if (!listing) return null;
    if (listing.entries.length === 0) {
      return <p class="text-xs text-ink-muted px-1 py-6 text-center">This folder is empty.</p>;
    }
    return <div class="flex flex-col gap-0.5">{listing.entries.map(row)}</div>;
  };

  const fileBody = () => {
    if (fileError) {
      return <p class="text-xs text-cinnabar leading-relaxed px-1 py-3">{fileError}</p>;
    }
    if (filePending || !file) {
      return <div class="py-8 flex justify-center" aria-label="loading file"><Dodec size={26} animate={false} class="opacity-40" /></div>;
    }
    if (editing) {
      return (
        <div class="relative flex-1 min-h-0 mx-2.5 mb-2.5 border border-hairline rounded-sm bg-paper-card focus-within:border-seal transition-colors duration-fast">
          {editHighlight && (
            <pre
              ref={preRef}
              aria-hidden="true"
              class="absolute inset-0 m-0 p-2.5 font-mono text-xs leading-relaxed whitespace-pre overflow-hidden pointer-events-none"
            ><code
              class="hljs"
              dangerouslySetInnerHTML={{ __html: editHighlight }}
            /></pre>
          )}
          <textarea
            ref={taRef}
            class={`absolute inset-0 w-full h-full resize-none p-2.5 font-mono text-xs leading-relaxed outline-none overflow-auto ${
              editHighlight ? 'bg-transparent text-transparent caret-seal' : 'bg-transparent text-ink'
            }`}
            wrap="off"
            value={draft}
            onInput={e => setDraft(e.currentTarget.value)}
            onScroll={syncScroll}
            onKeyDown={e => {
              if ((e.ctrlKey || e.metaKey) && e.key === 's') {
                e.preventDefault();
                save();
              }
            }}
            spellcheck={false}
            aria-label={`editing ${basename(file.path)}`}
          />
          {draft.length > editHighlightCap && (
            <span class="absolute bottom-1 right-2 text-[10px] text-ink-muted pointer-events-none">
              highlighting off — file too large to re-color per keystroke
            </span>
          )}
        </div>
      );
    }
    return (
      <pre class="font-mono text-xs leading-relaxed text-ink-light whitespace-pre-wrap break-words min-h-0 flex-1"><code
        class="hljs"
        dangerouslySetInnerHTML={{ __html: highlightCode(file.content, file.lang || '') }}
      /></pre>
    );
  };

  // The shell is the animated box: fixed full-screen sheet below lg
  // (slides right when closed), width-animated flex sibling at lg (w-0
  // when closed, so the chat takes the full row). The inner column keeps
  // the pane's true width and is right-anchored (lg:ml-auto) inside the
  // clipping shell — stationary content, moving viewport, which is why
  // nothing re-wraps mid-reveal.
  return (
    <section
      class={`fixed inset-0 z-40 bg-paper min-w-0 file-pane lg:static lg:inset-auto lg:z-auto lg:flex-none lg:overflow-hidden ${
        open
          ? 'translate-x-0 lg:w-[27rem] xl:w-[32rem]'
          : 'translate-x-full lg:translate-x-0 lg:w-0'
      }`}
      inert={!open}
      aria-label="file browser"
      onKeyDown={onKeyDown}
    >
      <div class="w-full h-full flex flex-col min-w-0 bg-paper lg:w-[27rem] xl:w-[32rem] lg:ml-auto lg:border-l border-hairline">
        {/* Header */}
        <div class="flex items-center gap-2 px-3 py-2 flex-shrink-0 border-b border-hairline">
          <FolderTree size={13} class="text-ink-muted flex-shrink-0" />
          <h2 class="text-xs font-semibold text-ink-muted tracking-wider uppercase">Files</h2>
          <span class="text-[10px] text-ink-muted truncate">
            {listing ? `${listing.entries.length} item${listing.entries.length === 1 ? '' : 's'}` : ''}
          </span>
          <button
            type="button"
            class={`ml-auto p-1 rounded-sm transition-all duration-fast ${
              editing && dirty
                ? 'text-ink-muted opacity-40 cursor-not-allowed'
                : 'text-ink-muted hover:text-cinnabar hover:bg-cinnabar/10 active:scale-90'
            }`}
            onClick={() => leaveWith(onClose)}
            title={editing && dirty ? 'Save or discard the edit first' : 'Close the file browser'}
            aria-label="Close the file browser"
          >
            <X size={14} />
          </button>
        </div>

        {/* Address bar: any absolute path the server's user may visit. */}
        <div class="flex items-center gap-1.5 px-2.5 py-2 flex-shrink-0">
          <button
            type="button"
            class="p-1.5 rounded-sm text-ink-muted hover:text-seal hover:bg-seal-light active:scale-90 transition-all duration-fast disabled:opacity-40 disabled:hover:bg-transparent disabled:hover:text-ink-muted"
            onClick={goUp}
            disabled={!listing || listing.path === '/'}
            title="Up one folder"
            aria-label="Up one folder"
          >
            <ArrowUp size={13} />
          </button>
          <input
            type="text"
            value={addr}
            onInput={e => setAddr(e.currentTarget.value)}
            onKeyDown={e => { if (e.key === 'Enter') goAddr(); }}
            placeholder="absolute path — empty is the server's cwd"
            spellcheck={false}
            class="flex-1 min-w-0 px-2 py-1.5 text-xs font-mono bg-paper-card border border-hairline rounded-sm outline-none focus:border-seal"
            aria-label="folder path"
          />
          <button
            type="button"
            class="p-1.5 rounded-sm text-ink-muted hover:text-seal hover:bg-seal-light active:scale-90 transition-all duration-fast"
            onClick={goAddr}
            title="Go"
            aria-label="Go to the typed path"
          >
            <ArrowRight size={13} />
          </button>
        </div>

        {/* Breadcrumbs */}
        {crumbs.length > 0 && (
          <nav class="flex items-center gap-0.5 px-2.5 pb-2 flex-shrink-0 overflow-x-auto text-xs" aria-label="breadcrumbs">
            {crumbs.map((c, i) => (
              <span key={c.path} class="flex items-center gap-0.5 flex-shrink-0">
                {i > 0 && <ChevronRight size={10} class="text-ink-muted opacity-50" />}
                <button
                  type="button"
                  class={`px-1 py-0.5 rounded-sm transition-colors duration-fast ${
                    i === crumbs.length - 1
                      ? 'text-ink-light font-medium cursor-default'
                      : 'text-ink-muted hover:text-seal hover:bg-seal-light'
                  }`}
                  onClick={() => navigate(c.path)}
                  title={c.path}
                >
                  {c.label}
                </button>
              </span>
            ))}
          </nav>
        )}

        {/* Content: the listing, or the file (preview / edit). */}
        {selected ? (
          <div class="flex-1 min-h-0 flex flex-col border-t border-hairline">
            <div class="flex items-center gap-1.5 px-2.5 py-1.5 flex-shrink-0">
              <button
                type="button"
                class="p-1 rounded-sm text-ink-muted hover:text-seal hover:bg-seal-light active:scale-90 transition-all duration-fast"
                onClick={backToListing}
                title="Back to the folder"
                aria-label="Back to the folder"
              >
                <ArrowUp size={13} class="rotate-[-90deg]" />
              </button>
              <span class="min-w-0 flex-1 truncate font-mono text-xs text-ink-light" title={selected}>
                {basename(selected)}
              </span>
              {file && !filePending && (
                <span class="flex-shrink-0 text-[10px] text-ink-muted tabular-nums">
                  {formatSize(file.size)}{file.lang ? ` · ${file.lang}` : ''}
                </span>
              )}
              {editing ? (
                <>
                  {dirty && (
                    <span class="flex-shrink-0 inline-flex items-center gap-1 text-[10px] font-semibold text-cinnabar" title="Unsaved changes">
                      <span class="w-1.5 h-1.5 rounded-full bg-cinnabar" aria-hidden="true" /> unsaved
                    </span>
                  )}
                  <button
                    type="button"
                    disabled={!dirty || saving}
                    onClick={save}
                    class="flex-shrink-0 inline-flex items-center gap-1 px-2 py-1 rounded-sm text-xs bg-seal text-on-seal hover:opacity-90 disabled:opacity-50 transition-opacity"
                    title="Save (Ctrl/Cmd+S)"
                  >
                    <Save size={11} />{saving ? 'Saving…' : 'Save'}
                  </button>
                  <button
                    type="button"
                    disabled={!dirty || saving}
                    onClick={discard}
                    class="flex-shrink-0 inline-flex items-center gap-1 px-2 py-1 rounded-sm text-xs border border-hairline bg-paper text-ink-light hover:text-seal hover:border-seal/40 disabled:opacity-50 transition-colors duration-fast"
                    title="Discard the edits"
                  >
                    <Undo2 size={11} /> Discard
                  </button>
                </>
              ) : (
                <button
                  type="button"
                  disabled={filePending || !!fileError}
                  onClick={() => setEditing(true)}
                  class="flex-shrink-0 inline-flex items-center gap-1 px-2 py-1 rounded-sm text-xs border border-hairline bg-paper text-ink-light hover:text-seal hover:border-seal/40 disabled:opacity-50 transition-colors duration-fast"
                  title="Edit this file"
                >
                  <Pencil size={11} /> Edit
                </button>
              )}
            </div>
            {fileBody()}
          </div>
        ) : (
          <div class="flex-1 min-h-0 overflow-y-auto px-1.5 pb-2">
            {listingBody()}
          </div>
        )}
      </div>
    </section>
  );
}
