import { useState, useEffect, useRef, useMemo } from 'preact/hooks';
import type { VNode } from 'preact';
import { Search, ChevronRight } from "lucide-preact";

// Command is one entry in the palette. Submenus (the TUI's settings-* palette)
// are flattened to top-level entries; a command with submenu uses open()
// to drill in.
export interface Command {
  id: string;
  // Group label for visual grouping (e.g. "settings", "sessions", "tools").
  group: string;
  // Verb is the visible label.
  verb: string;
  // Keywords power the search filter.
  keywords?: string[];
  // Description shown under the verb in the picker.
  description?: string;
  // Icon renders before the verb (e.g. a persona avatar chip).
  icon?: VNode;
  // Run executes the command. May open a submenu by calling open().
  run: () => void;
}

interface Props {
  commands: Command[];
  onClose: () => void;
}

// CommandPalette is the Ctrl+K palette. Open with Ctrl+K (or Cmd+K on Mac).
// Type to filter; arrow keys move the highlight; Enter runs the highlighted
// command; Escape closes. Mirrors the TUI's palette component shape but
// draws with HTML so the same input latency model holds.
//
// Animation contract: backdrop fades, panel drops in from 12vh with a
// small scale, and rows cascade in with a per-row delay (capped so a
// long list does not feel slow). Rows are keyed by command id, so Preact
// reuses DOM nodes while filtering — only newly-revealed rows animate.
export function CommandPalette({ commands, onClose }: Props) {
  const [query, setQuery] = useState('');
  const [highlight, setHighlight] = useState(0);
  const [closing, setClosing] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const closeTimer = useRef<ReturnType<typeof setTimeout>>(null);

  const requestClose = () => {
    if (closing) return;
    setClosing(true);
    closeTimer.current = setTimeout(onClose, 150);
  };

  // Focus the input on open so the user can type immediately.
  useEffect(() => {
    inputRef.current?.focus();
    return () => {
      if (closeTimer.current) clearTimeout(closeTimer.current);
    };
  }, []);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return commands;
    return commands.filter(c => {
      const haystack = [c.verb, c.group, c.description || '', ...(c.keywords || [])].join(' ').toLowerCase();
      return haystack.includes(q);
    });
  }, [query, commands]);

  // Reset highlight when the filtered list changes shape.
  useEffect(() => {
    setHighlight(0);
  }, [filtered.length]);

  // Keep the highlighted row scrolled into view.
  //
  // The list's DOM children are per-GROUP wrapper divs (a sticky header plus
  // that group's buttons), so indexing children by the flattened command
  // index landed on the wrong node entirely — scrollIntoView on a whole
  // group block could then jump the viewport to the bottom of the list.
  // Rows are tagged with data-idx (their index in the flattened `filtered`
  // array) and looked up explicitly instead.
  useEffect(() => {
    const list = listRef.current;
    if (!list) return;
    const row = list.querySelector<HTMLElement>(`[data-idx="${highlight}"]`);
    if (row) row.scrollIntoView({ block: 'nearest' });
  }, [highlight, filtered]);

  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      requestClose();
      return;
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setHighlight(h => Math.min(h + 1, filtered.length - 1));
      return;
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      setHighlight(h => Math.max(h - 1, 0));
      return;
    }
    if (e.key === 'Enter') {
      e.preventDefault();
      const cmd = filtered[highlight];
      if (cmd) {
        cmd.run();
        requestClose();
      }
      return;
    }
  };

  // Group filtered commands by their `group` field for display.
  const groups = useMemo(() => {
    const byGroup = new Map<string, Command[]>();
    for (const c of filtered) {
      if (!byGroup.has(c.group)) byGroup.set(c.group, []);
      byGroup.get(c.group)!.push(c);
    }
    return Array.from(byGroup.entries());
  }, [filtered]);

  let runningIndex = 0;

  return (
    <div
      class={`fixed inset-0 z-40 flex items-start justify-center pt-[12vh] px-4 bg-ink/30 ${closing ? 'animate-fade-out' : 'animate-fade-in'}`}
      onClick={requestClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        class={`w-full max-w-xl bg-paper-card border border-hairline rounded-md shadow-lg overflow-hidden origin-top ${closing ? 'animate-pop-out' : 'animate-pop-in'}`}
        onClick={e => e.stopPropagation()}
      >
        <div class="flex items-center gap-2 px-3 py-2.5 border-b border-hairline">
          <Search size={14} class="text-ink-muted flex-shrink-0" />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onInput={e => setQuery((e.target as HTMLInputElement).value)}
            onKeyDown={onKey}
            placeholder="Type a command or search…"
            class="flex-1 bg-transparent text-sm text-ink placeholder:text-ink-muted outline-none"
          />
          <kbd>esc</kbd>
        </div>

        <div ref={listRef} class="max-h-[60vh] overflow-y-auto">
          {filtered.length === 0 ? (
            <div class="px-4 py-6 text-sm text-ink-muted text-center animate-fade-in">
              No commands match "{query}".
            </div>
          ) : (
            groups.map(([group, items]) => (
              <div key={group}>
                <div class="px-3 py-1 text-[10px] uppercase tracking-wider text-ink-muted bg-paper-sunk/50 sticky top-0">
                  {group}
                </div>
                {items.map(c => {
                  const idx = runningIndex++;
                  // Stagger: 16ms per row, capped at the 12th row so a long
                  // list never feels like it is trickling in.
                  const delay = Math.min(idx, 12) * 16;
                  const active = idx === highlight;
                  return (
                    <button
                      key={c.id}
                      type="button"
                      data-idx={idx}
                      style={{ animationDelay: `${delay}ms` }}
                      class={`w-full flex items-start gap-2 px-3 py-2 text-left transition-colors duration-fast animate-fade-in ${
                        active ? 'bg-seal-light' : 'hover:bg-seal-light/50'
                      }`}
                      onMouseEnter={() => setHighlight(idx)}
                      onClick={() => {
                        c.run();
                        requestClose();
                      }}
                    >
                      <ChevronRight
                        size={12}
                        class={`flex-shrink-0 mt-0.5 transition-transform duration-fast ${
                          active ? 'text-seal translate-x-0.5' : 'text-ink-muted'
                        }`}
                      />
                      {c.icon && <span class="flex-shrink-0 mt-0.5">{c.icon}</span>}
                      <div class="flex-1 min-w-0">
                        <div class={`text-sm ${active ? 'text-seal' : 'text-ink-light'}`}>{c.verb}</div>
                        {c.description && (
                          <div class="text-xs text-ink-muted mt-0.5">{c.description}</div>
                        )}
                      </div>
                    </button>
                  );
                })}
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
