import { useEffect, useRef, useState } from 'preact/hooks';
import type { Message } from '../App';
import type * as api from '../api';
import { renderMarkdown } from '../markdown';
import { Wrench, Check, X, ChevronDown, Loader, Copy, RefreshCw, Pencil, BookOpen, ExternalLink } from "lucide-preact";

interface Props {
  messages: Message[];
  // streaming is still passed by App (it drives the Footer's ping indicator)
  // but the transcript itself no longer keys off it: the pending flag on the
  // last bubble is the single source of truth for the thinking indicator.
  streaming: boolean;
  pendingAsk: api.ServerEvent['ask'] | null;
  onAnswer: (approved: boolean, feedback?: string) => void;
  // Transient status line from the server (shown next to the typing dots).
  statusText?: string;
  // Changing this key (the session id) force-scrolls to the bottom, even
  // if the user had scrolled away in the previous session.
  sessionKey?: string;
  // onRegenerate re-runs the last assistant turn from its preceding user
  // message (drop the response chain, stream a fresh completion).
  onRegenerate?: () => void;
  // onEditUser forks a new branch: the user message at userIndex (0-based,
  // counting only user rows) is replaced by the edited text and the turn
  // re-runs from there.
  onEditUser?: (userIndex: number, text: string) => void;
}

// ChatArea renders the transcript. Scroll behavior follows one rule: the
// view sticks to the bottom while new content streams in, but never yanks
// the viewport if the user scrolled up to read. A floating pill offers a
// one-click way back down when unpinned.
export function ChatArea({ messages, pendingAsk, onAnswer, statusText, sessionKey, onRegenerate, onEditUser }: Props) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const pinnedRef = useRef(true);      // true = viewport is at/near the bottom
  const [pinned, setPinned] = useState(true);
  // The full-source sidebar: null = closed, otherwise the turn's complete
  // source list (rendered by SourcesSidebar, opened from a TurnSources
  // "+N" badge when the pill row is capped).
  const [sourcesPanel, setSourcesPanel] = useState<api.Source[] | null>(null);

  const scrollToBottom = (smooth: boolean) => {
    const el = scrollRef.current;
    if (!el) return;
    if (smooth) {
      el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' });
    } else {
      // Direct assignment: NOT affected by the container's CSS
      // scroll-behavior:smooth, so it lands in one step and fires a single
      // scroll event at the final position (a smooth animation would fire
      // intermediate "user scrolled away" events and flash the pill).
      el.scrollTop = el.scrollHeight;
    }
  };

  const onScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    const near = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    pinnedRef.current = near;
    setPinned(near);
  };

  // Follow new content only while the user is pinned to the bottom.
  // Instant tracking: smooth-animating every delta chunk reads as lag.
  useEffect(() => {
    if (pinnedRef.current) scrollToBottom(false);
  }, [messages]);

  // Session switch: reset to a pinned state, close any open sources
  // sidebar (its sources belong to the previous transcript), and jump to
  // the end of the fresh transcript.
  useEffect(() => {
    pinnedRef.current = true;
    setPinned(true);
    setSourcesPanel(null);
    requestAnimationFrame(() => scrollToBottom(false));
  }, [sessionKey]);

  // Delegated copy handler for markdown code blocks. The markdown payload
  // is injected via dangerouslySetInnerHTML, so the per-block copy buttons
  // the renderer emits can't carry Preact handlers — one listener on the
  // transcript container resolves .md-copy → .md-code → pre>code at click
  // time and swaps the label for a beat of confirmation.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const onClick = (e: Event) => {
      const target = e.target as HTMLElement;
      const btn = target.closest ? (target.closest('.md-copy') as HTMLButtonElement | null) : null;
      if (!btn) return;
      const wrap = btn.closest('.md-code') as HTMLElement | null;
      const code = wrap ? (wrap.querySelector('pre code') as HTMLElement | null) : null;
      if (!code) return;
      const label = btn.querySelector('.md-copy-label');
      const settle = () => {
        btn.classList.add('copied');
        if (label) label.textContent = 'copied';
        setTimeout(() => {
          btn.classList.remove('copied');
          if (label) label.textContent = 'copy';
        }, 1200);
      };
      const text = code.textContent || '';
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(text).then(settle, settle);
      } else {
        settle();
      }
    };
    el.addEventListener('click', onClick);
    return () => el.removeEventListener('click', onClick);
  }, []);

  // Turn segmentation for the source row: a turn starts at each user
  // message and runs to the next one. Sources aggregate across the whole
  // turn (every search every round made) and render once, below the
  // turn's last row — not under each individual tool row, which scattered
  // them mid-conversation.
  const sourcesAt = new Map<number, api.Source[]>();
  {
    let start = -1;
    for (let i = 0; i <= messages.length; i++) {
      const msg = i < messages.length ? messages[i] : undefined;
      const isUser = msg?.role === 'user';
      if (isUser || i === messages.length) {
        if (start >= 0 && i > start) {
          const srcs = turnSources(messages, start, i - 1);
          if (srcs.length > 0) sourcesAt.set(i - 1, srcs);
        }
        start = isUser ? i : -1;
      }
    }
  }

  // Render plan: pure-tool assistant rounds (the model said nothing — the
  // heads-down fetch/grep phase) merge with their neighbors into ONE
  // visual block: a single dropdown cluster carrying EVERY tool call of
  // the barrage, the newest call's detail as the preview. Rounds that
  // carry real text (or reasoning) stay individual MessageBubbles — the
  // words are conversation content. "Said nothing" is judged after
  // trimming: several models emit whitespace-only text alongside tool_use
  // blocks, and before the trim those rounds dodged the merge — every
  // fetch round rendered its own dropdown, usage line and orphaned copy
  // button instead of one "fetch ×15". The thinking-dots case (pending,
  // no text, no tools yet) also stays a bubble.
  type PlanItem =
    | { kind: 'bubble'; idx: number; msg: Message }
    | { kind: 'toolgroup'; start: number; last: number; tools: api.ToolRun[]; usage?: api.TranscriptUsage };
  const plan: PlanItem[] = [];
  {
    const saysNothing = (m: Message) =>
      !(m.text && m.text.trim()) && !(m.reasoning && m.reasoning.trim());
    let group: api.ToolRun[] = [];
    let start = -1;
    let last = -1;
    const flush = () => {
      if (group.length > 0 && start >= 0) {
        // Usage is summed across the merged rounds (replayed transcripts
        // carry a per-round readout; a five-round barrage reads as one
        // line, not five). tok/s is per-round by definition — dropped.
        let tin = 0, tout = 0, seen = false;
        for (let i = start; i <= last; i++) {
          const u = messages[i]?.usage;
          if (u) { tin += u.in || 0; tout += u.out || 0; seen = true; }
        }
        plan.push({ kind: 'toolgroup', start, last, tools: group, usage: seen ? { in: tin, out: tout } : undefined });
      }
      group = [];
      start = -1;
      last = -1;
    };
    for (const [i, m] of messages.entries()) {
      if (m.role === 'assistant' && saysNothing(m) && m.tools && m.tools.length > 0) {
        if (start < 0) start = i;
        // last tracks the MESSAGE index: one round can carry several
        // parallel calls, so start + group.length no longer equals it
        // (the old arithmetic mis-keyed sourcesAt / regenerate lookups).
        last = i;
        group = group.concat(m.tools);
      } else {
        flush();
        plan.push({ kind: 'bubble', idx: i, msg: m });
      }
    }
    flush();
  }

  // The regenerate affordance rides the last assistant bubble of the last
  // complete turn: only when nothing is streaming and an assistant row is
  // the transcript's final word (a turn to undo exists).
  let lastUserIndex = -1;   // count of user rows seen so far
  let regenerateAfter = -1;  // message index that carries the button
  for (const [i, msg] of messages.entries()) {
    if (msg.role === 'user') lastUserIndex = i;
    if (msg.role === 'assistant' && !msg.pending) regenerateAfter = i;
  }
  const canRegenerate = !!onRegenerate && regenerateAfter === messages.length - 1 && lastUserIndex >= 0;

  let userCount = 0;

  return (
    <div class="flex-1 min-h-0 relative flex">
      <div
        ref={scrollRef}
        onScroll={onScroll}
        data-chat-scroll="1"
        // NOTE: no scroll-smooth here. CSS scroll-behavior:smooth would turn
        // even direct scrollTop assignments into animated scrolls, making
        // the initial jump fire intermediate "scrolled away" events (pill
        // flash) and lagging behind fast streams. Smoothness is requested
        // explicitly per-call in scrollToBottom instead.
        class="flex-1 overflow-y-auto px-6 py-6"
      >
        <div class="max-w-chat mx-auto">
          {plan.map((item) => {
            if (item.kind === 'toolgroup') {
              // Merged pure-tool rounds: ONE dropdown cluster with every
              // call of the barrage inside it. A turn that ends on a
              // tool-only round (budget trip, verdict stop) still gets its
              // end-of-turn source row and — when it is the transcript's
              // last word — the regenerate affordance.
              const srcs = sourcesAt.get(item.last);
              // The tail round may still be streaming (spinner in the
              // cluster header). The old bubble path surfaced the live
              // status text ("searching arxiv…") next to its thinking
              // dots — keep that signal alive under the merged group.
              const liveStatus =
                item.last === messages.length - 1 &&
                messages[item.last]?.pending ? statusText : undefined;
              return (
                <div key={`tg-${item.start}`} class="mb-6 animate-msg-in">
                  {item.usage && (
                    <div class="mb-1.5 text-[10px] font-mono text-ink-muted/80">
                      {item.usage.in} in · {item.usage.out} out
                    </div>
                  )}
                  <ToolCluster tools={item.tools} />
                  {liveStatus && (
                    <div class="mt-2 pl-1 animate-fade-in">
                      <span class="working-text text-sm truncate inline-block max-w-full align-bottom">{liveStatus}</span>
                    </div>
                  )}
                  {srcs && (
                    <div class="mt-3">
                      <TurnSources sources={srcs} onShowAll={setSourcesPanel} />
                    </div>
                  )}
                  {canRegenerate && item.last === regenerateAfter && (
                    <div class="msg-actions mt-2">
                      <button
                        type="button"
                        class="msg-action"
                        onClick={onRegenerate}
                        title="Regenerate"
                        aria-label="Regenerate this turn"
                      >
                        <RefreshCw size={12} />
                      </button>
                    </div>
                  )}
                </div>
              );
            }
            const { idx: i, msg } = item;
            const isUser = msg.role === 'user';
            const userIndex = isUser ? userCount++ : -1;
            const bubble = (
              <MessageBubble
                key={i}
                message={msg}
                // The live status text rides with the pending bubble's dots so
                // there is exactly ONE thinking indicator in the transcript.
                statusText={msg.pending && i === messages.length - 1 ? statusText : undefined}
                onRegenerate={canRegenerate && i === regenerateAfter ? onRegenerate : undefined}
                onEdit={isUser && onEditUser && i !== messages.length - 1 ? (text) => onEditUser(userIndex, text) : undefined}
              />
            );
            const srcs = sourcesAt.get(i);
            return srcs ? (
              <div key={i} class="mb-6">
                {bubble}
                <TurnSources sources={srcs} onShowAll={setSourcesPanel} />
              </div>
            ) : bubble;
          })}

          {/* Permission prompt */}
          {pendingAsk && (
            <div class="my-3 p-4 rounded-md border border-hairline bg-paper-card animate-pop-in">
              <div class="flex items-start gap-3">
                <div class="flex-shrink-0 mt-0.5 p-1.5 rounded-sm bg-seal-light">
                  <Wrench size={14} class="text-seal" />
                </div>
                <div class="flex-1 min-w-0">
                  <p class="text-sm font-medium text-ink">
                    Permission required: <span class="font-mono text-xs">{pendingAsk.tool}</span>
                  </p>
                  {pendingAsk.reason && (
                    <p class="text-sm text-ink-muted mt-1">{pendingAsk.reason}</p>
                  )}
                  <div class="flex gap-2 mt-3">
                    <button
                      class="flex items-center gap-1.5 px-3 py-1.5 rounded-sm text-sm bg-moss text-on-seal hover:opacity-90 active:scale-95 transition-all duration-fast"
                      onClick={() => onAnswer(true)}
                    >
                      <Check size={14} />
                      Approve
                    </button>
                    <button
                      class="flex items-center gap-1.5 px-3 py-1.5 rounded-sm text-sm border border-hairline text-ink-light hover:bg-paper-sunk active:scale-95 transition-all duration-fast"
                      onClick={() => onAnswer(false)}
                    >
                      <X size={14} />
                      Deny
                    </button>
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Jump-to-latest pill: only when the user has scrolled away. */}
      {!pinned && (
        <button
          type="button"
          class="absolute bottom-4 right-6 z-10 p-1.5 rounded-full bg-paper-card border border-hairline shadow-md text-ink-light hover:text-seal hover:border-seal/40 active:scale-90 transition-all duration-fast animate-fade-in"
          onClick={() => { pinnedRef.current = true; setPinned(true); scrollToBottom(true); }}
          title="Jump to latest"
        >
          <ChevronDown size={16} />
        </button>
      )}

      {/* Full source list: a right sidebar slid over the transcript, opened
          by a TurnSources "+N" badge when the pill row hits its cap. Qwen-
          style — favicon, rank, title and the DDG snippet per source, the
          whole card is the link. Escape / backdrop click / the X close it. */}
      {sourcesPanel && (
        <SourcesSidebar
          sources={sourcesPanel}
          onClose={() => setSourcesPanel(null)}
        />
      )}
    </div>
  );
}

// turnSources collects every source every search in the turn produced,
// newest search first, deduped by URL. Walking messages and tool runs
// backwards means the last search's results (rank order preserved within
// it) lead the row, and a URL both an early and a late search returned
// keeps its latest position.
function turnSources(msgs: Message[], from: number, upto: number): api.Source[] {
  const seen = new Set<string>();
  const out: api.Source[] = [];
  for (let i = upto; i >= from; i--) {
    const tools = msgs[i]?.tools;
    if (!tools) continue;
    for (let t = tools.length - 1; t >= 0; t--) {
      const srcs = tools[t]?.sources;
      if (!srcs) continue;
      for (const s of srcs) {
        if (!s.url || seen.has(s.url)) continue;
        seen.add(s.url);
        out.push(s);
      }
    }
  }
  return out;
}

// TurnSources is the end-of-turn source row: one line of favicon chips,
// each opening its result in a new tab. Pills stagger in left-to-right
// (CSS animation-delay via inline style) so a completed search lands with
// a soft cascade instead of one block.
//
// Cap: a research turn can easily aggregate 100+ results (every search of
// every round, deduped by URL) — rendering them all was a wall of
// same-looking chips. The row now shows the first MAX_SOURCE_PILLS and
// folds the rest behind a "+N" circle badge that opens the full list in
// the right sidebar (SourcesSidebar).
const MAX_SOURCE_PILLS = 10;

function TurnSources({ sources, onShowAll }: { sources: api.Source[]; onShowAll?: (sources: api.Source[]) => void }) {
  const shown = sources.slice(0, MAX_SOURCE_PILLS);
  const hidden = sources.length - shown.length;
  return (
    <div class="animate-fade-in" aria-label="Sources consulted this turn">
      <div class="flex items-center gap-1.5 mb-1.5">
        <BookOpen size={11} class="text-ink-muted" />
        <span class="text-[10px] uppercase tracking-wider text-ink-muted select-none">
          {sources.length} {sources.length === 1 ? 'source' : 'sources'}
        </span>
      </div>
      <div class="flex flex-wrap gap-1.5">
        {shown.map((s, i) => (
          <a
            key={`${s.url}-${i}`}
            class="source-pill"
            href={s.url}
            target="_blank"
            rel="noopener noreferrer"
            title={s.title || s.url}
            style={{ animationDelay: `${Math.min(i, 8) * 24}ms` }}
          >
            <span class="source-pill-rank">{s.rank}</span>
            <img
              class="source-pill-favicon"
              src={s.favicon_url}
              alt=""
              width={14}
              height={14}
              loading="lazy"
              onError={(e) => {
                // DDG has no favicon for some hosts: hide the broken-image
                // glyph and let the host text carry the chip.
                (e.currentTarget as HTMLImageElement).style.visibility = 'hidden';
              }}
            />
            <span class="source-pill-host">{s.host}</span>
          </a>
        ))}
        {hidden > 0 && (
          <button
            type="button"
            class="source-pill-more"
            onClick={() => onShowAll?.(sources)}
            aria-label={`Show all ${sources.length} sources`}
            title={`View all ${sources.length} sources`}
          >
            +{hidden}
          </button>
        )}
      </div>
    </div>
  );
}

// SourcesSidebar is the full end-of-turn source list: a right-hand panel
// that slides in over the transcript. Every source renders as a card —
// rank badge, favicon, host, the search-result title, and the DDG snippet
// as the description — and the whole card is the external link. Escape,
// the backdrop, or the header X closes it; the list itself scrolls under
// a sticky header so a 100-source turn stays scannable.
function SourcesSidebar({ sources, onClose }: { sources: api.Source[]; onClose: () => void }) {
  // Escape closes the panel. stopPropagation so a modal open underneath
  // doesn't also swallow the keypress.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div class="absolute inset-0 z-20 flex justify-end" role="dialog" aria-label="Sources">
      <button
        type="button"
        class="absolute inset-0 bg-ink/10 animate-fade-in"
        onClick={onClose}
        aria-label="Close sources panel"
        tabIndex={-1}
      />
      <aside class="relative h-full w-[380px] max-w-[88vw] flex flex-col bg-paper-card border-l border-hairline shadow-2xl animate-slide-in-right">
        <header class="flex items-center gap-2 px-4 py-3 border-b border-hairline flex-shrink-0">
          <BookOpen size={13} class="text-seal flex-shrink-0" />
          <span class="text-sm font-medium text-ink">Sources</span>
          <span class="px-1.5 py-0.5 rounded-full bg-seal/10 text-seal font-mono text-[10px] leading-none">
            {sources.length}
          </span>
          <button
            type="button"
            class="msg-action ml-auto"
            onClick={onClose}
            title="Close"
            aria-label="Close sources panel"
          >
            <X size={14} />
          </button>
        </header>
        <div class="flex-1 overflow-y-auto p-3 flex flex-col gap-2">
          {sources.map((s, i) => (
            <a
              key={`${s.url}-${i}`}
              class="source-card group"
              href={s.url}
              target="_blank"
              rel="noopener noreferrer"
              style={{ animationDelay: `${Math.min(i, 12) * 18}ms` }}
            >
              <div class="flex items-center gap-1.5">
                <span class="source-pill-rank">{s.rank}</span>
                <img
                  class="source-pill-favicon"
                  src={s.favicon_url}
                  alt=""
                  width={14}
                  height={14}
                  loading="lazy"
                  onError={(e) => {
                    (e.currentTarget as HTMLImageElement).style.visibility = 'hidden';
                  }}
                />
                <span class="text-[10px] text-ink-muted truncate">{s.host}</span>
                <ExternalLink
                  size={11}
                  class="ml-auto flex-shrink-0 text-ink-muted/50 group-hover:text-seal transition-colors duration-fast"
                />
              </div>
              <div class="mt-1 text-xs font-medium text-ink leading-snug">
                {s.title || s.url}
              </div>
              {s.snippet && (
                <div class="source-card-snippet mt-0.5 text-[11px] leading-relaxed text-ink-muted">
                  {s.snippet}
                </div>
              )}
            </a>
          ))}
        </div>
      </aside>
    </div>
  );
}

function MessageBubble({
  message,
  statusText,
  onRegenerate,
  onEdit,
}: {
  message: Message;
  statusText?: string;
  // Hover action availability. Copy always shows; regenerate only on the
  // turn's last sealed assistant bubble; edit only on user rows that have
  // a later message to re-run from.
  onRegenerate?: () => void;
  onEdit?: (text: string) => void;
}) {
  const isUser = message.role === 'user';
  // While the assistant bubble is still streaming its main text, the
  // markdown container gets a blinking block caret after its last block.
  const isStreamingText = !!message.pending && !!message.text;
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState('');

  const startEdit = () => {
    setDraft(message.text);
    setEditing(true);
  };

  const submitEdit = () => {
    const text = draft.trim();
    if (!text || !onEdit) return;
    setEditing(false);
    onEdit(text);
  };

  const editKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      submitEdit();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      setEditing(false);
    }
  };

  // Replay markers (compaction cuts from loaded transcripts) render as a
  // quiet centered divider instead of a chat bubble — they are metadata
  // about the conversation, not a turn in it.
  if (message.role === 'system') {
    return (
      <div class="my-5 flex items-center gap-3 animate-fade-in" aria-label={message.text}>
        <span class="flex-1 border-t border-hairline" />
        <span class="px-2 py-0.5 rounded-full border border-hairline bg-paper-sunk text-[10px] uppercase tracking-wider text-ink-muted whitespace-nowrap">
          {message.text}
        </span>
        <span class="flex-1 border-t border-hairline" />
      </div>
    );
  }

  return (
    <div class={`mb-6 animate-msg-in group ${isUser ? 'flex flex-col items-end' : ''}`}>
      {isUser ? (
        editing ? (
          <div class="max-w-[85%] w-full animate-fade-in">
            <div class="px-4 py-3 rounded-chat bg-seal-light border border-seal/40 focus-within:border-seal transition-colors duration-fast">
              <textarea
                value={draft}
                onInput={(e) => setDraft((e.target as HTMLTextAreaElement).value)}
                onKeyDown={editKeyDown}
                rows={Math.min(10, Math.max(2, draft.split('\n').length))}
                class="w-full resize-none bg-transparent text-sm text-ink outline-none leading-relaxed"
                autoFocus
                // Focus places the caret at the end: the whole text is the
                // edit surface, selecting all would force a full retyping.
                ref={(el) => { if (el) { el.focus(); el.setSelectionRange(el.value.length, el.value.length); } }}
              />
              <div class="flex items-center justify-end gap-2 mt-2 pt-2 border-t border-hairline/60">
                <span class="text-[10px] text-ink-muted mr-auto">editing re-runs the turn from here as a new branch</span>
                <button
                  type="button"
                  class="flex items-center gap-1 px-2 py-1 rounded-sm text-xs text-ink-muted hover:text-ink hover:bg-paper-sunk transition-colors duration-fast"
                  onClick={() => setEditing(false)}
                >
                  <X size={12} />
                  Cancel
                </button>
                <button
                  type="button"
                  class="flex items-center gap-1 px-2.5 py-1 rounded-sm text-xs bg-seal text-on-seal hover:opacity-90 active:scale-95 transition-all duration-fast disabled:opacity-50"
                  onClick={submitEdit}
                  disabled={!draft.trim()}
                >
                  <Check size={12} />
                  Save &amp; rerun
                </button>
              </div>
            </div>
          </div>
        ) : (
          <>
            <div class="max-w-[85%] px-4 py-3 rounded-chat bg-seal-light text-ink group-hover:border-seal/30 transition-colors duration-fast">
              <div class="md-content text-sm whitespace-pre-wrap">{message.text}</div>
            </div>
            <div class="msg-actions mt-1 flex-row-reverse">
              <CopyButton getText={() => message.text} title="Copy message" />
              {onEdit && (
                <button
                  type="button"
                  class="msg-action"
                  onClick={startEdit}
                  title="Edit & branch"
                  aria-label="Edit message and rerun from here"
                >
                  <Pencil size={12} />
                </button>
              )}
            </div>
          </>
        )
      ) : (
        <div class="w-full">
          {/* Reasoning (collapsible) */}
          {!!(message.reasoning && message.reasoning.trim()) && (
            <details class="mb-2 group/details">
              <summary class="cursor-pointer text-xs text-ink-muted hover:text-ink-light flex items-center gap-1 select-none transition-colors duration-fast">
                <ChevronDown size={12} class="transition-transform duration-fast group-open/details:rotate-180" />
                Reasoning
              </summary>
              <div class="mt-1 pl-4 border-l border-hairline text-sm text-ink-muted italic whitespace-pre-wrap animate-fade-in">
                {message.reasoning}
              </div>
            </details>
          )}

          {/* Main text — whitespace-only payloads render as nothing (a
              " \n" round carrying tool calls used to leave an empty
              paragraph box behind). */}
          {!!(message.text && message.text.trim()) && (
            <div
              class={`md-content text-sm ${isStreamingText ? 'streaming' : ''}`}
              dangerouslySetInnerHTML={{ __html: renderMarkdown(message.text) }}
            />
          )}

          {/* Token readout: replayed transcripts carry per-turn usage from
              the JSON transcript endpoint; live streaming still omits it. */}
          {message.usage && !message.pending && (
            <div class="mt-1.5 text-[10px] font-mono text-ink-muted/80">
              {message.usage.in} in · {message.usage.out} out
              {message.usage.tps ? ` · ${Math.round(message.usage.tps)} tok/s` : ''}
            </div>
          )}

          {/* Tool calls: the same dropdown treatment pure-tool rounds get
              — every call of the round inside one ToolCluster, the newest
              call's detail as the preview. */}
          {message.tools && message.tools.length > 0 && (
            <div class="mt-2">
              <ToolCluster tools={message.tools} />
            </div>
          )}

          {/* Pending state: three bouncing dots + live status while nothing
              has arrived yet. This is the transcript's single thinking
              indicator — the status text rides along instead of rendering
              a second dots row below the bubble. */}
          {message.pending && !(message.text && message.text.trim()) && !(message.reasoning && message.reasoning.trim()) && (
            <div class="flex items-center gap-2.5 py-2 animate-fade-in">
              <span class="flex items-center gap-1">
                <span class="typing-dot" />
                <span class="typing-dot" />
                <span class="typing-dot" />
              </span>
              {statusText && (
                <span class="text-sm text-ink-muted truncate">{statusText}</span>
              )}
            </div>
          )}

          {/* Hover actions: copy the raw markdown (nothing to copy on a
              whitespace-only round — the button used to orphan under the
              tool rows), and — on the turn's last sealed bubble — re-roll
              the whole turn. */}
          {!message.pending && (!!onRegenerate || !!(message.text && message.text.trim())) && (
            <div class="msg-actions mt-1">
              {!!(message.text && message.text.trim()) && (
                <CopyButton getText={() => message.text} title="Copy message" />
              )}
              {onRegenerate && (
                <button
                  type="button"
                  class="msg-action"
                  onClick={onRegenerate}
                  title="Regenerate"
                  aria-label="Regenerate this turn"
                >
                  <RefreshCw size={12} />
                </button>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// CopyButton copies raw text on click and confirms with a check for a
// beat. Touch users see it at half opacity (group-hover can't fire); mouse
// users get the fade-in.
function CopyButton({ getText, title }: { getText: () => string; title: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    const text = getText();
    if (!text) return;
    try {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        await navigator.clipboard.writeText(text);
      }
    } catch {
      // Clipboard permission denied — still flash the check; the user
      // selected-and-copied fallback remains.
    }
    setCopied(true);
    setTimeout(() => setCopied(false), 1200);
  };
  return (
    <button
      type="button"
      class={`msg-action ${copied ? 'text-moss' : ''}`}
      onClick={copy}
      title={copied ? 'Copied' : title}
      aria-label={title}
    >
      {copied ? <Check size={12} /> : <Copy size={12} />}
    </button>
  );
}

// ToolCluster collapses a run of tool calls into ONE dropdown row: status
// icon + name + ×count + the NEWEST call's detail as the preview. While
// the run streams in, that detail reads as a live ticker of what the agent
// is doing right now; once sealed it names the last call made. EVERY tool
// call gets this treatment — singles included, per user feedback ("all
// toolcalls should be in that dropdown like menu with the latest tool call
// as the preview") — so a mixed barrage reads as one quiet list instead of
// alternating dropdowns and bordered rows. A group spanning several tool
// names summarizes them per-name ("fetch ×3 · read ×2"); expanding reveals
// every call numbered in a sunken list — the count badge and chevron are
// the affordance.
function ToolCluster({ tools }: { tools: api.ToolRun[] }) {
  const [open, setOpen] = useState(false);
  const running = tools.some(t => t.status === 'in_progress');
  const failed = !running && tools.some(t => t.status === 'error');
  let latest = '';
  for (const t of tools) if (t.detail) latest = t.detail;
  // Unique names in first-seen order with per-name counts. One name keeps
  // the "fetch ×17" header (no badge for a single call — ×1 is noise);
  // several render "fetch ×3 · read ×2".
  const names: string[] = [];
  const counts = new Map<string, number>();
  for (const t of tools) {
    const n = t.name || 'tool';
    if (!counts.has(n)) { names.push(n); counts.set(n, 0); }
    counts.set(n, (counts.get(n) || 0) + 1);
  }
  const mixed = names.length > 1;
  return (
    <div class="tool-cluster">
      <button
        type="button"
        class="tool-cluster-header"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
        title={`${tools.length} tool call${tools.length === 1 ? '' : 's'} — click to ${open ? 'collapse' : 'expand'}`}
      >
        {running ? (
          <Loader size={12} class="animate-spin text-seal flex-shrink-0" />
        ) : failed ? (
          <X size={12} class="text-cinnabar flex-shrink-0" />
        ) : (
          <Check size={12} class="text-moss flex-shrink-0" />
        )}
        {mixed ? (
          <span class="font-mono font-medium text-ink-light whitespace-nowrap">
            {names.map((n, i) => (
              <span key={n}>
                {i > 0 && <span class="text-ink-muted/50"> · </span>}
                {n}<span class="text-ink-muted/70"> ×{counts.get(n)}</span>
              </span>
            ))}
          </span>
        ) : (
          <>
            <span class="font-mono font-medium text-ink-light">{names[0]}</span>
            {tools.length > 1 && <span class="tool-cluster-count">×{tools.length}</span>}
          </>
        )}
        <span class="flex-1 min-w-0 truncate text-ink-muted">{latest}</span>
        <ChevronDown
          size={12}
          class={`flex-shrink-0 text-ink-muted transition-transform duration-fast ${open ? 'rotate-180' : ''}`}
        />
      </button>
      {open && (
        <div class="tool-cluster-body animate-fade-in">
          {tools.map((t, i) => (
            <ToolCall key={t.toolUseId || i} tool={t} index={i + 1} showName={mixed} />
          ))}
        </div>
      )}
    </div>
  );
}

// ToolCall is one row inside an expanded ToolCluster: a run index instead
// of the repeated tool name, and the call's detail as the payload. The
// detail wraps instead of truncating — expanding a cluster is how you read
// the full query/command the header preview clipped. Mixed groups keep the
// name on every row so "fetch / read / fetch" stays legible.
function ToolCall({ tool, index, showName }: { tool: api.ToolRun; index?: number; showName?: boolean }) {
  const statusIcon = () => {
    switch (tool.status) {
      case 'in_progress':
        return <Loader size={12} class="animate-spin text-seal" />;
      case 'done':
        return <Check size={12} class="text-moss" />;
      case 'error':
        return <X size={12} class="text-cinnabar" />;
      default:
        return null;
    }
  };

  return (
    <div class="flex items-start gap-2 px-2.5 py-1 text-xs animate-fade-in">
      <span class="mt-0.5 flex-shrink-0 inline-flex">{statusIcon()}</span>
      {index !== undefined && (
        <span class="mt-0.5 w-4 flex-shrink-0 text-right font-mono text-[10px] leading-none text-ink-muted/70 tabular-nums select-none">
          {index}
        </span>
      )}
      {showName && (
        <span class="mt-0.5 flex-shrink-0 font-mono font-medium text-ink-light">{tool.name}</span>
      )}
      {tool.detail ? (
        <span class="flex-1 min-w-0 whitespace-pre-wrap break-words text-ink-muted">{tool.detail}</span>
      ) : !showName ? (
        <span class="font-mono font-medium text-ink-light">{tool.name}</span>
      ) : (
        <span class="flex-1 text-ink-muted/50">(no detail)</span>
      )}
    </div>
  );
}
