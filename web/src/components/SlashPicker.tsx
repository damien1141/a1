import { useState, useEffect, useRef, useMemo } from 'preact/hooks';

export interface SlashCommand {
  name: string;
  description: string;
  // args is the literal text appended to "/" + name when the user accepts
  // the suggestion (e.g. "staged", "HEAD", "create"). Empty means the
  // command takes no args; the composer submits "/name" directly.
  args?: string[];
  // Whether the command needs an arg. When true, accepting the suggestion
  // leaves the composer at "/name " so the user can type the arg.
  needsArgs?: boolean;
  // rawArgs carries the argument text the user typed after the command name
  // ("/code main.go:42" → "main.go:42") when the command is dispatched from
  // the composer's Enter handler. Overlay-opening commands read it to seed
  // their state (e.g. the CodeViewer's path).
  rawArgs?: string;
}

interface Props {
  query: string;        // text after the leading "/", already lowercased
  commands: SlashCommand[];
  onAccept: (cmd: SlashCommand) => void;
  onClose: () => void;
}

// SlashPicker is the inline autocomplete that opens when the composer sees
// a leading "/". It mirrors the TUI's FilterSlash + FilterSlashArgs: type
// to narrow the name list, arrow keys move the highlight, Tab/Enter
// accepts. The caller passes the query (text after "/") so the picker
// never has to know about the surrounding composer state.
export function SlashPicker({ query, commands, onAccept, onClose }: Props) {
  const [highlight, setHighlight] = useState(0);
  const listRef = useRef<HTMLDivElement>(null);

  const filtered = useMemo(() => {
    const q = query.toLowerCase();
    if (!q) return commands;
    return commands.filter(c => c.name.toLowerCase().includes(q) || c.description.toLowerCase().includes(q));
  }, [query, commands]);

  useEffect(() => {
    setHighlight(0);
  }, [filtered.length]);

  useEffect(() => {
    const list = listRef.current;
    if (!list) return;
    const row = list.children[highlight] as HTMLElement | undefined;
    if (row) row.scrollIntoView({ block: 'nearest' });
  }, [highlight]);

  // Expose keyboard navigation through a window-level capture-phase
  // listener. Capture phase lets us beat the textarea's own onKeyDown so
  // we can swallow the Enter that would otherwise also submit the prompt
  // (the user types "/co" + Enter to accept "compact" — without
  // stopImmediatePropagation, the same Enter bubbles into InputBar's
  // handleKeyDown and sends "/co" as a prompt alongside running the
  // command, a double-action the user does not expect).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        e.stopImmediatePropagation();
        setHighlight(h => Math.min(h + 1, filtered.length - 1));
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        e.stopImmediatePropagation();
        setHighlight(h => Math.max(h - 1, 0));
      } else if (e.key === 'Tab' || (e.key === 'Enter' && filtered[highlight])) {
        e.preventDefault();
        e.stopImmediatePropagation();
        if (filtered[highlight]) onAccept(filtered[highlight]);
      } else if (e.key === 'Escape') {
        e.preventDefault();
        e.stopImmediatePropagation();
        onClose();
      }
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [filtered, highlight, onAccept, onClose]);

  if (filtered.length === 0) {
    return (
      <div class="absolute bottom-full left-0 right-0 mb-1 px-4 py-3 bg-paper-card border border-hairline rounded-md shadow-sm text-sm text-ink-muted animate-fade-in">
        No commands match "/{query}".
      </div>
    );
  }

  return (
    <div
      ref={listRef}
      class="absolute bottom-full left-0 right-0 mb-1 bg-paper-card border border-hairline rounded-md shadow-md max-h-[50vh] overflow-y-auto origin-bottom animate-pop-in"
    >
      {filtered.map((c, i) => (
        <button
          key={c.name}
          type="button"
          class={`w-full flex items-baseline gap-2 px-3 py-1.5 text-left transition-colors duration-fast ${
            i === highlight ? 'bg-seal-light' : 'hover:bg-seal-light/50'
          }`}
          onMouseEnter={() => setHighlight(i)}
          onClick={() => onAccept(c)}
        >
          <span class="font-mono text-sm text-seal flex-shrink-0">/{c.name}</span>
          <span class="text-xs text-ink-muted truncate">{c.description}</span>
        </button>
      ))}
    </div>
  );
}
