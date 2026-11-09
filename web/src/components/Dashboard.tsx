import { useEffect, useMemo, useState } from 'preact/hooks';
import type { ComponentChildren } from 'preact';
import { FolderOpen, Settings as SettingsIcon, Sparkles, Star, UsersRound, X } from 'lucide-preact';
import { AgendaCalendar } from './AgendaCalendar';
import { Dodec } from './Dodec';
import { InputBar } from './InputBar';
import { KanbanBoard } from './KanbanBoard';
import { MailCard } from './MailInbox';
import { UsageHeatmap } from './UsageHeatmap';
import { personaAvatar } from './Avatar';
import type { SlashCommand } from './SlashPicker';
import * as api from '../api';

// Dashboard is the SPA's welcome state (the "new chat" landing page): a
// widget grid over the vault + config surfaces, with the composer docked
// bottom-center. Submitting from the docked chatbox is today's exact
// welcome flow — selectSession('') + send — so the chat view takes over
// with zero new routing (there is no router; the Overlay union is the
// app's navigation).
//
// Data contract, chunk by chunk: usage comes from /api/usage/daily (the
// vault rollup), pinned sessions are pure composition over the pins +
// sessions surfaces that already ship, and the personal trio (calendar,
// kanban, mail) is live — each widget owns its fetch + locked states off
// /api/personal/status (the one ungated personal endpoint), and no gated
// fetch fires until the widget's own states allow it.

interface Props {
  sessions: api.SessionMeta[];
  pins: string[];
  persona: api.Persona | null;
  streaming: boolean;
  onSend: (text: string) => void;
  onCancel: () => void;
  slashCommands: SlashCommand[];
  onSlashCommand: (cmd: SlashCommand) => void;
  onSelectSession: (id: string) => void;
  onOpenOverlay: (kind: 'personas' | 'skills' | 'extensions' | 'mail' | 'mail-accounts') => void;
  // onOpenFiles raises the split-screen file browser pane (chunk 5) —
  // not an overlay kind, because the pane is a sibling surface that
  // stays up while the user works. filesOpen/onToggleFiles drive the
  // composer's pane toggle (chunk 7 moved it here from the sidebar —
  // the drawer is collapsed by default now, and the toggle belongs
  // beside the send button it splits the screen for).
  onOpenFiles: () => void;
  filesOpen: boolean;
  onToggleFiles: () => void;
  onToast: (t: api.ToastPayload) => void;
  // mailVersion is the mail surfaces' refetch trigger (chunk 8): the
  // accounts editor bumps it on save, and this component refetches the
  // personal status (the configured count changed) while the mail card
  // refetches its own rail off the same prop.
  mailVersion: number;
}

// Card is the shared widget chrome: paper-card token, hairline border,
// consistent padding — the same visual language as the config editor's
// section cards.
function Card({ title, icon, children, class: cls }: { title: string; icon?: ComponentChildren; children: ComponentChildren; class?: string }) {
  return (
    <section class={`bg-paper-card border border-hairline rounded-lg shadow-sm p-4 min-w-0 ${cls || ''}`}>
      <div class="flex items-center gap-2 mb-3">
        {icon}
        <h2 class="text-xs font-semibold text-ink-muted tracking-wider uppercase">{title}</h2>
      </div>
      {children}
    </section>
  );
}

export function Dashboard({
  sessions, pins, persona, streaming,
  onSend, onCancel, slashCommands, onSlashCommand,
  onSelectSession, onOpenOverlay, onOpenFiles, filesOpen, onToggleFiles, onToast, mailVersion,
}: Props) {
  const [usage, setUsage] = useState<api.UsageDay[] | null>(null);
  const [usageFailed, setUsageFailed] = useState(false);
  const [personal, setPersonal] = useState<api.PersonalStatus | null>(null);
  const [selectedDay, setSelectedDay] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    api.fetchUsageDaily(371)
      .then(r => {
        if (!alive) return;
        setUsage(r.daily);
        // Autoselect today: the newest cell is the day the user is living
        // in, and a pre-selected filter fills the readout with the day's
        // sessions instead of leaving the card's floor an empty scratch
        // space. Deselecting stays one click (the X).
        if (r.daily.length > 0) setSelectedDay(r.daily[r.daily.length - 1].date);
      })
      .catch(() => { if (alive) setUsageFailed(true); });
    return () => { alive = false; };
  }, []);

  // The personal status refetches when the mail accounts change (chunk 8):
  // the editor's save bumps mailVersion, and the configured count — the
  // card's not-configured vs live line — moves with it.
  useEffect(() => {
    let alive = true;
    api.fetchPersonalStatus()
      .then(s => { if (alive) setPersonal(s); })
      .catch(() => { if (alive) setPersonal(null); }); // locked-by-default on failure
    return () => { alive = false; };
  }, [mailVersion]);

  // Pinned-first session rows: the pins surface already ships (pins.json +
  // /api/pins); this widget is pure composition over it and the session
  // list, zero new backend.
  const pinned = useMemo(
    () => pins.map(id => sessions.find(s => s.id === id)).filter((s): s is api.SessionMeta => !!s),
    [pins, sessions],
  );
  const recent = useMemo(
    () => sessions.filter(s => !pins.includes(s.id)).slice(0, Math.max(0, 6 - pinned.length)),
    [pins, sessions, pinned.length],
  );

  // Day filter: sessions last active on the selected UTC day, using the
  // row's machine-readable data-mtime (the friendly "2h ago" label cannot
  // express a calendar day).
  const daySessions = useMemo(() => {
    if (!selectedDay) return [];
    return sessions.filter(s => (s.iso || '').slice(0, 10) === selectedDay);
  }, [sessions, selectedDay]);

  const sessionRow = (s: api.SessionMeta, isPinned: boolean) => (
    <button
      key={s.id}
      type="button"
      class="w-full text-left px-2.5 py-2 rounded-sm hover:bg-seal-light hover:translate-x-0.5 transition-all duration-fast group animate-fade-in"
      onClick={() => onSelectSession(s.id)}
      title={s.preview || s.id}
    >
      <span class="flex items-center gap-1.5">
        {isPinned && <Star size={10} class="text-seal fill-current flex-shrink-0" />}
        <span class="truncate text-sm text-ink-light group-hover:text-seal">{s.preview || s.id.slice(0, 8)}</span>
      </span>
      <span class="block text-xs text-ink-muted mt-0.5">{s.mtime}</span>
    </button>
  );

  // The personal trio, all live now: the board takes the full row (a
  // board is horizontal by nature), then the agenda (two cells) beside
  // the mail card (one) — the card holds the rail + latest preview and
  // expands into the three-pane reader overlay.

  return (
    <div class="flex-1 flex flex-col min-h-0">
      {/* Widget area: scrolls when the grid exceeds the viewport. */}
      <div class="flex-1 overflow-y-auto">
        <div class="max-w-5xl mx-auto px-6 pt-10 pb-6">
          {/* Hero: the brand mark, bigger than the chat-mode composer's,
              with the same staggered rise the welcome screen had. */}
          <div class="flex flex-col items-center mb-8 animate-rise-in">
            <Dodec size={64} class="dodec-hero mb-4" />
            <h1 class="text-center font-serif text-2xl text-ink-light">What should we work on today?</h1>
            {persona && persona.id !== 'default' && (
              <span class="persona-chip flex items-center gap-2 pl-1 pr-2.5 py-1 mt-3 rounded-lg bg-paper-card border border-hairline shadow-sm">
                {personaAvatar(persona, 22)}
                <span class="text-xs text-ink-light">speaking as <span class="font-medium">{persona.name}</span></span>
              </span>
            )}
          </div>

          {/* Widget grid. */}
          <div class="grid grid-cols-1 lg:grid-cols-3 gap-4 animate-rise-in" style={{ animationDelay: '80ms' }}>
            {/* Usage tracker: the widest card — 53 weeks want the room. */}
            <Card title="Usage" class="lg:col-span-2">
              {usageFailed ? (
                <p class="text-xs text-ink-muted py-6 text-center">Usage is unavailable right now.</p>
              ) : usage === null ? (
                <div class="py-6 flex justify-center" aria-label="loading usage">
                  <Dodec size={28} animate={false} class="opacity-40" />
                </div>
              ) : (
                <UsageHeatmap daily={usage} selectedDay={selectedDay} onSelectDay={setSelectedDay} />
              )}
              {/* Day-filter readout: which sessions the selected day maps
                  to, with a one-click clear. */}
              {selectedDay && (
                <div class="mt-3 border-t border-hairline pt-3 animate-fade-in">
                  <div class="flex items-center justify-between mb-1.5">
                    <span class="text-[10px] font-semibold text-ink-muted tracking-wider uppercase">
                      Sessions last active {selectedDay}
                    </span>
                    <button
                      type="button"
                      class="p-0.5 rounded-sm text-ink-muted hover:text-cinnabar hover:bg-cinnabar/10 transition-all duration-fast"
                      onClick={() => setSelectedDay(null)}
                      title="Clear day filter"
                      aria-label="Clear day filter"
                    >
                      <X size={12} />
                    </button>
                  </div>
                  {daySessions.length === 0 ? (
                    <p class="text-xs text-ink-muted">No sessions from that day.</p>
                  ) : (
                    <div class="flex flex-col gap-0.5 max-h-40 overflow-y-auto">
                      {daySessions.map(s => sessionRow(s, pins.includes(s.id)))}
                    </div>
                  )}
                </div>
              )}
            </Card>

            {/* Pinned sessions: pure composition over existing surfaces. */}
            <Card title="Pinned sessions">
              {pinned.length === 0 && recent.length === 0 ? (
                <p class="text-xs text-ink-muted leading-relaxed">
                  No sessions yet — they appear here as you chat. Star one in the sidebar to pin it.
                </p>
              ) : (
                <div class="flex flex-col gap-0.5">
                  {pinned.map(s => sessionRow(s, true))}
                  {pinned.length > 0 && recent.length > 0 && <div class="border-t border-hairline my-1.5" />}
                  {recent.map(s => sessionRow(s, false))}
                </div>
              )}
            </Card>

            {/* The personal trio: kanban takes the full row (a board is
                horizontal by nature), then the agenda (two cells) beside
                the mail card — one cell, expanding into its overlay. */}
            <KanbanBoard status={personal} onToast={onToast} class="lg:col-span-3" />
            <AgendaCalendar status={personal} class="lg:col-span-2" />
            <MailCard status={personal} onOpenReader={() => onOpenOverlay('mail')} onConfigure={() => onOpenOverlay('mail-accounts')} version={mailVersion} />
          </div>

          {/* Quick actions. */}
          <div class="flex flex-wrap items-center gap-2 mt-4 animate-rise-in" style={{ animationDelay: '160ms' }}>
            <span class="text-[10px] font-semibold text-ink-muted tracking-wider uppercase mr-1">Quick actions</span>
            <button
              type="button"
              class="flex items-center gap-1.5 px-2.5 py-1.5 rounded-sm text-xs text-ink-light bg-paper-card border border-hairline shadow-sm hover:text-seal hover:border-seal/40 hover:translate-y-[-1px] transition-all duration-fast"
              onClick={() => window.open('/settings', '_blank')}
            >
              <SettingsIcon size={12} /> Config editor
            </button>
            <button
              type="button"
              class="flex items-center gap-1.5 px-2.5 py-1.5 rounded-sm text-xs text-ink-light bg-paper-card border border-hairline shadow-sm hover:text-seal hover:border-seal/40 hover:translate-y-[-1px] transition-all duration-fast"
              onClick={() => onOpenOverlay('personas')}
            >
              <UsersRound size={12} /> Personas
            </button>
            <button
              type="button"
              class="flex items-center gap-1.5 px-2.5 py-1.5 rounded-sm text-xs text-ink-light bg-paper-card border border-hairline shadow-sm hover:text-seal hover:border-seal/40 hover:translate-y-[-1px] transition-all duration-fast"
              onClick={() => onOpenOverlay('skills')}
            >
              <Sparkles size={12} /> Skills
            </button>
            {/* The file browser is chunk 5's deliverable — the pane it
                opens is the split-screen surface (read · preview · edit). */}
            <button
              type="button"
              class="flex items-center gap-1.5 px-2.5 py-1.5 rounded-sm text-xs text-ink-light bg-paper-card border border-hairline shadow-sm hover:text-seal hover:border-seal/40 hover:translate-y-[-1px] transition-all duration-fast"
              onClick={onOpenFiles}
              title="Split-screen file browser"
            >
              <FolderOpen size={12} /> Files
            </button>
          </div>
        </div>
      </div>

      {/* Docked chatbox: bottom-center, the page's fixed floor. The
          second dodec is gone (chunk 7): the hero already carries the
          brand mark, and an avatar beside the composer read as a stray
          duplicate; the composer takes the dock's full width. Submitting
          from here is the welcome flow verbatim — the chat view takes
          over. */}
      <div class="flex-shrink-0 border-t border-hairline bg-paper/90 backdrop-blur px-6 py-4">
        <div class="max-w-chat mx-auto animate-rise-in" style={{ animationDelay: '60ms' }}>
          <InputBar
            onSend={onSend}
            onCancel={onCancel}
            streaming={streaming}
            welcome={true}
            persona={persona}
            slashCommands={slashCommands}
            onSlashCommand={onSlashCommand}
            filesOpen={filesOpen}
            onToggleFiles={onToggleFiles}
          />
        </div>
      </div>
    </div>
  );
}
