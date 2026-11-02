import { useState } from 'preact/hooks';
import { MessageSquarePlus, RefreshCw, Pin, Search, Settings, Trash2, Check, X } from "lucide-preact";
import * as api from '../api';
import { personaAvatar } from './Avatar';
import { ThemeToggleButton } from './ThemeToggle';

interface Props {
  sessions: api.SessionMeta[];
  currentSession: string;
  pins: string[];
  onSelect: (id: string) => void;
  onRefresh: () => void;
  onTogglePin: (id: string) => void;
  // onDelete removes a stored session (with its pin). The row asks for an
  // inline confirmation first — deleting a conversation is destructive
  // and undoable, so a bare trash click must never drop it.
  onDelete: (id: string) => void;
  // persona is the active profile; onOpenPersonas opens the manager.
  persona?: api.Persona | null;
  onOpenPersonas: () => void;
}

export function Sidebar({ sessions, currentSession, pins, onSelect, onRefresh, onTogglePin, onDelete, persona, onOpenPersonas }: Props) {
  const [filter, setFilter] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  // confirmDelete holds the id of the row whose trash button was clicked;
  // a second click on that row's checkmark commits the delete.
  const [confirmDelete, setConfirmDelete] = useState('');
  const pinSet = new Set(pins);

  const doRefresh = () => {
    if (refreshing) return;
    setRefreshing(true);
    // Race the refresh against a 150ms floor: long enough that an instant
    // cache hit still produces a visible spin (otherwise it looks like the
    // click did nothing), short enough that a real network fetch isn't held
    // back artificially. The old 450ms floor made repeated clicks feel
    // sluggish — each click visibly waited 450ms before another was useful.
    Promise.all([
      Promise.resolve(onRefresh()),
      new Promise<void>(r => setTimeout(r, 150)),
    ]).finally(() => setRefreshing(false));
  };

  // Pinned first, then newest (which is the input order) within each group.
  const ordered = [
    ...sessions.filter(s => pinSet.has(s.id)),
    ...sessions.filter(s => !pinSet.has(s.id)),
  ];
  const filtered = filter.trim()
    ? ordered.filter(s => (s.preview || s.id).toLowerCase().includes(filter.toLowerCase()))
    : ordered;

  return (
    <aside class="w-60 flex-shrink-0 flex flex-col bg-paper-sunk border-r border-hairline overflow-hidden">
      {/* Header */}
      <div class="flex items-center justify-between px-4 py-2 flex-shrink-0">
        <span class="text-xs font-semibold text-ink-muted tracking-wider uppercase">
          Sessions
        </span>
        <div class="flex items-center gap-0.5">
          <ThemeToggleButton />
          {/* Config editor: opens /settings — the same editor `a1 config`
              and the TUI's /config surface, mounted on this server. */}
          <button
            class="p-1 rounded-sm text-ink-muted hover:text-seal hover:bg-seal-light active:scale-90 transition-all duration-fast"
            onClick={() => window.open('/settings', '_blank')}
            title="Config editor"
            aria-label="Open config editor"
          >
            <Settings size={14} />
          </button>
          <button
            class={`p-1 rounded-sm text-ink-muted hover:text-seal hover:bg-seal-light active:scale-90 transition-all duration-fast ${refreshing ? 'text-seal' : ''}`}
            onClick={doRefresh}
            title="Refresh"
          >
            <RefreshCw size={14} class={refreshing ? 'animate-spin' : ''} />
          </button>
        </div>
      </div>

      {/* New chat button */}
      <div class="px-2 pb-2">
        <button
          class="w-full flex items-center gap-2 px-3 py-2 rounded-sm text-sm text-ink-light hover:text-seal hover:bg-seal-light transition-colors duration-fast border border-hairline"
          onClick={() => onSelect('')}
        >
          <MessageSquarePlus size={15} />
          New chat
        </button>
      </div>

      {/* Filter */}
      {sessions.length > 0 && (
        <div class="px-2 pb-2">
          <div class="flex items-center gap-1.5 px-2 py-1 rounded-sm bg-paper border border-hairline">
            <Search size={11} class="text-ink-muted flex-shrink-0" />
            <input
              type="text"
              value={filter}
              onInput={e => setFilter((e.target as HTMLInputElement).value)}
              placeholder="Filter…"
              class="flex-1 bg-transparent text-xs text-ink placeholder:text-ink-muted outline-none"
            />
          </div>
        </div>
      )}

      {/* Session list */}
      <div class="flex-1 overflow-y-auto px-2 pb-2">
        {sessions.length === 0 ? (
          <p class="text-center py-8 px-2 text-sm text-ink-muted">
            No sessions yet. Start a new chat.
          </p>
        ) : filtered.length === 0 ? (
          <p class="text-center py-6 px-2 text-xs text-ink-muted">
            No sessions match "{filter}".
          </p>
        ) : (
          filtered.map(s => {
            const isPinned = pinSet.has(s.id);
            const isActive = s.id === currentSession;
            const confirming = confirmDelete === s.id;
            return (
              <div
                key={s.id}
                class={`group relative w-full text-left px-3 py-2 rounded-sm mb-1 transition-all duration-fast animate-fade-in hover:translate-x-0.5 ${
                  isActive
                    ? 'bg-seal-light text-seal before:absolute before:left-0 before:top-1/2 before:-translate-y-1/2 before:h-4 before:w-0.5 before:rounded-full before:bg-seal'
                    : 'hover:bg-seal-light text-ink-light'
                }`}
              >
                {confirming ? (
                  /* Two-step delete: the row swaps to a confirm strip so the
                     destructive click needs an explicit second intent. */
                  <div class="flex items-center gap-1.5 animate-fade-in">
                    <Trash2 size={12} class="text-cinnabar flex-shrink-0" />
                    <span class="text-xs text-ink-muted flex-1 min-w-0 truncate">Delete this session?</span>
                    <button
                      type="button"
                      class="p-1 rounded-sm text-cinnabar hover:bg-cinnabar/10 active:scale-90 transition-all duration-fast"
                      onClick={(e) => { e.stopPropagation(); setConfirmDelete(''); onDelete(s.id); }}
                      title="Confirm delete"
                      aria-label="Confirm delete"
                    >
                      <Check size={12} />
                    </button>
                    <button
                      type="button"
                      class="p-1 rounded-sm text-ink-muted hover:text-ink hover:bg-paper-sunk active:scale-90 transition-all duration-fast"
                      onClick={(e) => { e.stopPropagation(); setConfirmDelete(''); }}
                      title="Keep"
                      aria-label="Keep session"
                    >
                      <X size={12} />
                    </button>
                  </div>
                ) : (
                  <>
                    <button
                      type="button"
                      class="w-full text-left"
                      onClick={() => onSelect(s.id)}
                    >
                      <div class="flex items-center gap-1.5 pr-10">
                        {isPinned && <Pin size={10} class="text-seal flex-shrink-0 fill-current" />}
                        <span class={`truncate text-sm flex-1 ${isActive ? 'font-medium' : ''}`}>{s.preview || s.id.slice(0, 8)}</span>
                      </div>
                      <div class="text-xs text-ink-muted mt-0.5">{s.mtime}</div>
                    </button>
                    <button
                      type="button"
                      class={`absolute top-1.5 right-1.5 p-1 rounded-sm transition-all duration-fast ${
                        isPinned
                          ? 'text-seal opacity-100'
                          // Subtle-but-visible by default (opacity-60) so touch users can
                          // tap the pin button; group-hover reveals full opacity on devices
                          // with a hover pointer. The previous `opacity-0` made the button
                          // both invisible AND effectively untappable on phones / tablets.
                          : 'text-ink-muted opacity-60 hover:opacity-100 group-hover:opacity-100 hover:bg-paper-sunk hover:scale-110'
                      }`}
                      onClick={(e) => { e.stopPropagation(); setConfirmDelete(''); onTogglePin(s.id); }}
                      title={isPinned ? 'Unpin' : 'Pin'}
                      aria-label={isPinned ? 'Unpin session' : 'Pin session'}
                    >
                      <Pin size={11} class={isPinned ? 'fill-current' : ''} />
                    </button>
                    <button
                      type="button"
                      class="absolute bottom-1.5 right-1.5 p-1 rounded-sm text-ink-muted opacity-60 hover:opacity-100 group-hover:opacity-100 hover:text-cinnabar hover:bg-cinnabar/10 active:scale-90 transition-all duration-fast"
                      onClick={(e) => { e.stopPropagation(); setConfirmDelete(s.id); }}
                      title="Delete session"
                      aria-label="Delete session"
                    >
                      <Trash2 size={11} />
                    </button>
                  </>
                )}
              </div>
            );
          })
        )}
      </div>

      {/* Active persona chip: who answers the next prompt. Click opens the
          persona manager (switch / create / memory banks). */}
      <div class="px-2 pb-2 pt-1 border-t border-hairline flex-shrink-0">
        <button
          type="button"
          onClick={onOpenPersonas}
          class="persona-chip w-full flex items-center gap-2.5 px-2 py-2 rounded-sm text-left transition-colors duration-fast hover:bg-seal-light"
          title="Personas — switch, create, manage memory banks"
        >
          {personaAvatar(persona ?? { id: 'default', emoji: '🧭', hue: 200 }, 26)}
          <span class="min-w-0 flex-1">
            <span class="block text-xs text-ink-light truncate">{persona?.name || 'a1'}</span>
            <span class="block text-[10px] text-ink-muted truncate">{persona?.tagline || 'personas'}</span>
          </span>
        </button>
      </div>
    </aside>
  );
}
