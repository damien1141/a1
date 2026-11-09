import { useState, useEffect, useRef, useCallback, useMemo } from 'preact/hooks';
import { Menu } from 'lucide-preact';
import { Sidebar } from './components/Sidebar';
import { ChatArea } from './components/ChatArea';
import { InputBar } from './components/InputBar';
import { Footer } from './components/Footer';
import { ToastStack, useToast } from './components/Toast';
import { CommandPalette, type Command } from './components/CommandPalette';
import { PersonaManager } from './components/PersonaManager';
import { MailReader } from './components/MailInbox';
import { MailAccountsEditor } from './components/MailAccountsEditor';
import { Dashboard } from './components/Dashboard';
import { FileBrowser } from './components/FileBrowser';
import { personaAvatar } from './components/Avatar';
import type { SlashCommand } from './components/SlashPicker';
import {
  DiffViewer,
  CodeViewer,
  BranchPicker,
  SnapshotManager,
  PermissionInfo,
  ContextBreakdown,
  SkillsPanel,
  ExtensionsPanel,
  SettingsPanel,
} from './components/Overlays';
import * as api from './api';

export interface Message {
  // 'system' rows are transcript markers (compaction cuts): reloaded
  // sessions get them from the stored entry, live chat from the marker
  // SSE event — both render the same divider.
  role: 'user' | 'assistant' | 'system';
  text: string;
  reasoning?: string;
  tools?: api.ToolRun[];
  pending?: boolean;
  // Token readout from replayed transcripts (JSON transcript endpoint).
  usage?: api.TranscriptUsage;
  // msgId identifies the assistant ROUND this bubble streams (engine mints
  // one id per stream round — a turn that says something, calls a tool, and
  // says something else has two). Deltas with a new msgId start a new
  // bubble instead of overwriting the previous round's text. Absent for
  // user rows, replayed rows, and legacy servers without the field.
  msgId?: string;
  // entryId is the session-tree entry id for replayed rows (user and
  // assistant). Live rows mint their ids server-side inside the turn
  // goroutine, so they stay unknown until a reload.
  entryId?: string;
}

// Overlay is a tagged union of every modal the App can render at once.
// Exactly one overlay is open at a time (the TUI shows them full-screen,
// and the same model applies here — opening /diff while /code is open
// replaces it rather than stacking).
type Overlay =
  | { kind: 'none' }
  | { kind: 'palette' }
  | { kind: 'diff' }
  | { kind: 'code'; path: string; line?: number }
  | { kind: 'branch' }
  | { kind: 'snapshot' }
  | { kind: 'permission' }
  | { kind: 'context' }
  | { kind: 'skills' }
  | { kind: 'extensions' }
  | { kind: 'settings' }
  | { kind: 'personas' }
  | { kind: 'mail' }
  | { kind: 'mail-accounts' };

export default function App() {
  const [sessions, setSessions] = useState<api.SessionMeta[]>([]);
  const [pins, setPins] = useState<string[]>([]);
  const [currentSession, setCurrentSession] = useState('');
  // sessionRef mirrors currentSession so the SSE handlers (which close over
  // a stale currentSession inside the subscription effect) always filter
  // against the LATEST value. Without this, a new chat's first turn raced
  // the setCurrentSession from the prompt's 202 response: every delta carried
  // the freshly minted session id, currentSession was still '', and the
  // filter dropped the whole stream — the user saw the turn "never happen".
  // It also lets a tab with no session ADOPT a turn it started (the browser
  // that sent the prompt is the only subscriber that matters, and it has no
  // other session to protect).
  const sessionRef = useRef('');
  useEffect(() => { sessionRef.current = currentSession; }, [currentSession]);
  const [messages, setMessages] = useState<Message[]>([]);
  const [streaming, setStreaming] = useState(false);
  const [statusText, setStatusText] = useState('');
  const [pendingAsk, setPendingAsk] = useState<api.ServerEvent['ask'] | null>(null);
  const [welcome, setWelcome] = useState(true);
  const [state, setState] = useState<api.StatePayload | null>(null);
  const [overlay, setOverlay] = useState<Overlay>({ kind: 'none' });
  // mailVersion is the mail surfaces' refresh token (chunk 8): the
  // accounts editor bumps it on save and the dashboard refetches the
  // personal status + mail rail behind the modal — the new account is
  // live while the editor is still open.
  const [mailVersion, setMailVersion] = useState(0);
  // The file browser is NOT an overlay: it is a split-screen pane that
  // lives beside the chat/dashboard and stays while the user works — the
  // one surfaces-level view state that is neither welcome nor overlay.
  const [filesOpen, setFilesOpen] = useState(false);
  // The session drawer (chunk 7): collapsed by default at EVERY viewport.
  // It opens from the left-edge hover zone (or the hamburger below lg) and
  // closes on pick / X / Escape / pointer-leave. The hover intent is
  // debounced: entering the panel cancels a pending close, leaving it
  // schedules one ~300ms out, so a pointer crossing the gap between the
  // edge zone and the panel never flickers the drawer open-and-shut.
  const [navOpen, setNavOpen] = useState(false);
  const navCloseTimer = useRef<number | null>(null);
  const openNav = useCallback(() => {
    if (navCloseTimer.current !== null) {
      clearTimeout(navCloseTimer.current);
      navCloseTimer.current = null;
    }
    setNavOpen(true);
  }, []);
  const scheduleNavClose = useCallback(() => {
    if (navCloseTimer.current !== null) clearTimeout(navCloseTimer.current);
    navCloseTimer.current = window.setTimeout(() => {
      navCloseTimer.current = null;
      setNavOpen(false);
    }, 300);
  }, []);
  const closeNavNow = useCallback(() => {
    if (navCloseTimer.current !== null) {
      clearTimeout(navCloseTimer.current);
      navCloseTimer.current = null;
    }
    setNavOpen(false);
  }, []);
  // Timer cleanup on unmount.
  useEffect(() => () => {
    if (navCloseTimer.current !== null) clearTimeout(navCloseTimer.current);
  }, []);

  // The drawer's Escape: scoped to while it is open, so a focused modal
  // elsewhere keeps its own. The scrim and the X close it too — three
  // exits, one state.
  useEffect(() => {
    if (!navOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setNavOpen(false);
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [navOpen]);
  // Personas: full registry (palette + manager) and the active profile.
  // The active one rides on `state.persona` (initial GET + every state
  // SSE event), so a switch in another tab lands here instantly.
  const [personas, setPersonas] = useState<api.Persona[]>([]);

  const eventSourceRef = useRef<EventSource | null>(null);
  const messagesRef = useRef<Message[]>([]);
  const { toasts, push: pushToast, dismiss: dismissToast } = useToast();

  useEffect(() => { messagesRef.current = messages; }, [messages]);

  // ---- initial state fetch ----
  useEffect(() => {
    loadSessions();
    loadPins();
    api.fetchState().then(setState).catch(() => {});
    api.fetchPersonas().then(r => setPersonas(r.personas ?? [])).catch(() => {});
  }, []);

  // ---- theme persistence failures ----
  // theme.ts is a framework-free module (it boots before the first render);
  // the CustomEvent is its channel back into the component tree. A theme
  // choice that failed to reach config.yaml must not pass silently for a
  // saved one — the toggle already applied locally, so the toast is the
  // honesty.
  useEffect(() => {
    const onToast = (e: Event) => {
      const detail = (e as CustomEvent<api.ToastPayload>).detail;
      if (detail) pushToast(detail);
    };
    window.addEventListener('a1:toast', onToast);
    return () => window.removeEventListener('a1:toast', onToast);
  }, [pushToast]);

  // ---- SSE subscription ----
  useEffect(() => {
    const es = api.subscribeEvents();
    eventSourceRef.current = es;

    // belongsHere decides whether a live event belongs to the tab's current
    // conversation. A turn started from THIS tab (currentSession '' at send
    // time) mints a fresh id on the server — those events must flow, so an
    // empty sessionRef adopts any stream. Once a session is selected, only
    // its events pass; other tabs' conversations stay invisible.
    const belongsHere = (data: api.ServerEvent): boolean => {
      if (!data.sessionId) return true;
      if (!sessionRef.current) return true;
      return data.sessionId === sessionRef.current;
    };

    const onDelta = (e: MessageEvent) => {
      const data: api.ServerEvent = JSON.parse(e.data);
      if (!belongsHere(data)) return;
      if (!data.delta) return;
      setMessages(prev => {
        const updated = [...prev];
        const last = updated[updated.length - 1];
        const delta = data.delta!;
        const roundId = delta.msgId || '';
        // Segment by round: a delta whose msgId differs from the last
        // assistant bubble's starts a NEW bubble. This is the fix for the
        // "webui eats what the model says" bug — round 2's text used to
        // overwrite round 1's, so anything said before a tool call
        // vanished. Events without a msgId (legacy servers, ForceCompact's
        // empty id) fall through to the last-bubble behavior.
        const sameRound = last && last.role === 'assistant' && last.pending &&
          (roundId === '' || (last.msgId ?? '') === roundId);
        if (sameRound) {
          // Replace the message object so a future React.memo on MessageBubble
          // sees a new prop reference and re-renders. The previous code mutated
          // the inner object in place, which only worked because MessageBubble
          // is not memoized — adding memo would silently break streaming.
          updated[updated.length - 1] = {
            ...last,
            text: delta.text,
            reasoning: delta.reasoning,
            msgId: roundId || last.msgId,
          };
        } else if (last && last.role === 'assistant' && last.pending) {
          // New round: seal the previous bubble (it stays on screen with its
          // text and tool rows) and append a fresh streaming one.
          updated[updated.length - 1] = { ...last, pending: false };
          updated.push({
            role: 'assistant',
            text: delta.text,
            reasoning: delta.reasoning,
            pending: true,
            msgId: roundId || undefined,
          });
        }
        return updated;
      });
    };

    const onTool = (e: MessageEvent) => {
      const data: api.ServerEvent = JSON.parse(e.data);
      if (!belongsHere(data)) return;
      if (!data.tool) return;
      setMessages(prev => {
        const updated = [...prev];
        // Attach the tool row to the bubble of its round (msgId), falling
        // back to the last assistant bubble when the field is absent
        // (legacy servers / replayed events).
        const tool = data.tool!;
        let target = -1;
        for (let i = updated.length - 1; i >= 0; i--) {
          const m = updated[i];
          if (m.role !== 'assistant') continue;
          if (tool.msgId && m.msgId === tool.msgId) { target = i; break; }
          if (!tool.msgId) { target = i; break; }
        }
        if (target === -1 && updated.length && updated[updated.length - 1].role === 'assistant') {
          target = updated.length - 1;
        }
        if (target < 0) return updated;
        // A tool run streams STATUS UPDATES for the same call (queued →
        // in-progress → done/error). Re-emit the same row by toolUseId
        // instead of appending, or every run renders one row per status.
        const tools = [...(updated[target].tools || [])];
        const idx = tools.findIndex(t => t.toolUseId === tool.toolUseId);
        if (idx >= 0) {
          tools[idx] = { ...tools[idx], ...tool };
        } else {
          tools.push(tool);
        }
        updated[target] = { ...updated[target], tools };
        return updated;
      });
    };

    const onStatus = (e: MessageEvent) => {
      const data: api.ServerEvent = JSON.parse(e.data);
      if (!belongsHere(data)) return;
      if (data.status) setStatusText(data.status.text);
      if (data.failed) setStreaming(false);
    };

    // Markers: persistent transcript rows (compaction cuts). The reload
    // path renders these dividers from the stored session entry; the live
    // stream must draw the same cut, or a session watched live and the same
    // session reloaded disagree about where the context was folded. A
    // failed compaction appends no entry and emits no marker — only the
    // transient status line.
    const onMarker = (e: MessageEvent) => {
      const data: api.ServerEvent = JSON.parse(e.data);
      if (!belongsHere(data)) return;
      if (!data.marker) return;
      setMessages(prev => {
        // Seal any still-pending bubble above the cut — what follows the
        // marker resumes from the compacted context, not a continuation.
        const sealed = prev.map(m => (m.pending ? { ...m, pending: false } : m));
        return [...sealed, { role: 'system', text: data.marker!.text }];
      });
    };

    const onDone = () => {
      setStreaming(false);
      setStatusText('');
      setMessages(prev => {
        // Seal every still-pending bubble (normally just the last round's).
        // With per-round segmentation the earlier rounds are already
        // sealed when the next one started; this catches the final one.
        return prev.map(m => (m.pending ? { ...m, pending: false } : m));
      });
      loadSessions();
    };

    const onAsk = (e: MessageEvent) => {
      const data: api.ServerEvent = JSON.parse(e.data);
      if (!belongsHere(data)) return;
      if (data.ask) setPendingAsk(data.ask);
    };

    // Toasts: render globally. A toast with empty sessionID is broadcast;
    // one with a sessionID is filtered here so it only shows when the user
    // is on that session.
    const onToast = (e: MessageEvent) => {
      const data: api.ServerEvent = JSON.parse(e.data);
      if (!data.toast) return;
      if (data.sessionId && sessionRef.current && data.sessionId !== sessionRef.current) return;
      pushToast(data.toast);
    };

    // State events: refresh the live UI knobs. Always applied (no session
    // filter) because mode/model/agents/memory/branch are process-wide.
    const onState = (e: MessageEvent) => {
      const data: api.ServerEvent = JSON.parse(e.data);
      if (!data.state) return;
      setState(data.state);
    };

    es.addEventListener('delta', onDelta);
    es.addEventListener('tool', onTool);
    es.addEventListener('status', onStatus);
    es.addEventListener('marker', onMarker);
    es.addEventListener('done', onDone);
    es.addEventListener('ask', onAsk);
    es.addEventListener('toast', onToast);
    es.addEventListener('state', onState);

    return () => {
      es.close();
    };
  }, [currentSession]);

  // ---- Ctrl+K palette shortcut ----
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // e.key is "k" on some platforms, "K" when Ctrl/Shift is held (the
      // browser uppercases the key when a modifier is active). Match
      // case-insensitively so the palette opens everywhere.
      if ((e.ctrlKey || e.metaKey) && (e.key === 'k' || e.key === 'K')) {
        e.preventDefault();
        setOverlay(prev => prev.kind === 'palette' ? { kind: 'none' } : { kind: 'palette' });
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const loadSessions = async () => {
    const s = await api.fetchSessions();
    setSessions(s);
  };

  const loadPins = async () => {
    const p = await api.fetchPins();
    setPins(p);
  };

  // loadTranscriptMessages fetches a past session through the JSON endpoint
  // so replays render through the same markdown / reasoning / tool-row
  // pipeline as live chat. Falling back to the legacy HTML scrape keeps the
  // loader working against an older server binary that predates the JSON
  // route. The old scrape's textContent extraction is also why loaded
  // sessions read as a wall of text: adjacent <p> blocks concatenate with
  // no whitespace between them.
  const loadTranscriptMessages = async (id: string): Promise<Message[]> => {
    try {
      const data = await api.fetchTranscriptMessages(id);
      return data.messages.map(m => ({
        role: m.role,
        text: m.text || '',
        reasoning: m.reasoning || undefined,
        tools: m.tools,
        usage: m.usage,
        entryId: m.id,
      }));
    } catch {
      return scrapeTranscriptHTML(id);
    }
  };

  // scrapeTranscriptHTML is the pre-JSON fallback. Best-effort: markdown
  // paragraphs concatenate without whitespace (the wall-of-text bug) and
  // tool runs / usage are lost — kept only for old servers.
  const scrapeTranscriptHTML = async (id: string): Promise<Message[]> => {
    const html = await api.fetchTranscript(id);
    if (!html) return [];
    const parser = new DOMParser();
    const doc = parser.parseFromString(html, 'text/html');
    // The server's transcriptFragment emits `.bubble` (with role modifier
    // `.bubble-user` / `.bubble-assistant` / `.bubble-system`).
    const bubbles = doc.querySelectorAll('.bubble');
    const msgs: Message[] = [];
    bubbles.forEach(bubble => {
      if (bubble.classList.contains('bubble-system')) return; // skip compaction marker
      const role: 'user' | 'assistant' = bubble.classList.contains('bubble-user') ? 'user' : 'assistant';
      // Strip the thinking <details> and usage div so their text doesn't
      // pollute the rendered chat bubble.
      const clone = bubble.cloneNode(true) as HTMLElement;
      clone.querySelectorAll('.thinking, .usage').forEach(el => el.remove());
      // Separate block-level children with newlines so paragraphs at least
      // survive as line breaks in the fallback path.
      const text = (clone.textContent || '').replace(/\s*\n\s*/g, '\n').trim();
      msgs.push({ role, text });
    });
    return msgs;
  };

  // activePersona derives from the live state payload; personas[] is only
  // the catalog. Deriving (not duplicating) means a persona switch pushed
  // over SSE is reflected everywhere in the same tick.
  const activePersona: api.Persona | null = state?.persona ?? null;
  const activePersonaId = activePersona?.id ?? 'default';

  const loadPersonas = async () => {
    try {
      const r = await api.fetchPersonas();
      setPersonas(r.personas);
    } catch { /* registry temporarily unavailable — palette falls back to manage-only */ }
  };

  const switchPersona = useCallback(async (id: string) => {
    try {
      const r = await api.setPersonaActive(id);
      // Optimistic local update: the SSE state event confirms it a beat
      // later, but the palette should feel instant.
      setState(prev => (prev ? { ...prev, persona: r.persona } : prev));
      const p = personas.find(x => x.id === id);
      pushToast({ level: 'success', message: `Now speaking as ${r.persona.name}`, duration: 2500 });
      if (!p) await loadPersonas();
    } catch (e) {
      pushToast({ level: 'error', message: `Switch failed: ${String(e)}` });
    }
  }, [personas, pushToast]);

  const selectSession = async (id: string) => {
    if (!id) {
      setCurrentSession('');
      setMessages([]);
      setWelcome(true);
      setStatusText('');
      return;
    }
    setCurrentSession(id);
    setMessages([]);
    const msgs = await loadTranscriptMessages(id);
    setMessages(msgs);
    // Welcome screen shows only for an EMPTY transcript. (This comparison
    // was previously inverted, which made every loaded session bounce back
    // to the welcome screen — the chat area could never render.)
    setWelcome(msgs.length === 0);
  };

  const send = useCallback(async (text: string) => {
    if (!text.trim() || streaming) return;
    setWelcome(false);

    // "!cmd" prefix → run shell command locally. The TUI's BashRunner does
    // the same; the web posts the raw command to /api/prompt which the
    // engine forwards to its bash tool when given a prompt like "run !cmd".
    // For now we render the command output as a fake user message; the
    // engine does the actual execution when the user submits a normal
    // prompt that mentions the bash command.
    setMessages(prev => [...prev, { role: 'user', text }]);
    setMessages(prev => [...prev, { role: 'assistant', text: '', pending: true }]);
    setStreaming(true);
    setStatusText('thinking…');

    try {
      const result = await api.sendPrompt(currentSession, text);
      if (!currentSession) {
        setCurrentSession(result.sessionId);
      }
    } catch (err) {
      setStreaming(false);
      setStatusText('');
      const msg = err instanceof Error ? err.message : String(err);
      setMessages(prev => {
        const updated = [...prev];
        const last = updated[updated.length - 1];
        if (last && last.pending) {
          updated[updated.length - 1] = { ...last, pending: false, text: msg };
        }
        return updated;
      });
    }
  }, [currentSession, streaming]);

  const cancel = useCallback(async () => {
    if (!currentSession || !streaming) return;
    try {
      await api.cancelTurn(currentSession);
    } catch (e) {
      pushToast({ level: 'error', message: e instanceof Error ? e.message : String(e) });
    }
    setStreaming(false);
    setStatusText('');
  }, [currentSession, streaming, pushToast]);

  // regenerate re-rolls the last assistant turn: locally, everything after
  // the last user message is dropped and a fresh pending bubble opens; the
  // server rewinds the session cursor to that user message's parent (the
  // old response chain stays in the tree as an abandoned branch) and runs
  // the turn again, streaming over the same SSE subscription.
  const regenerate = useCallback(async () => {
    if (!currentSession || streaming) return;
    let lastUser = -1;
    for (const [i, m] of messagesRef.current.entries()) {
      if (m.role === 'user') lastUser = i;
    }
    if (lastUser < 0) return;
    setMessages(prev => {
      const kept = prev.slice(0, lastUser + 1);
      kept.push({ role: 'assistant', text: '', pending: true });
      return kept;
    });
    setStreaming(true);
    setStatusText('re-rolling…');
    try {
      await api.regenerateTurn(currentSession);
    } catch (err) {
      failPendingBubble(err);
    }
  }, [currentSession, streaming]);

  // editUser forks a new branch: the user message at userIndex is replaced
  // by the edited text and the turn re-runs from that point. Same rewind
  // semantics as regenerate, with the edited text as the new prompt.
  const editUser = useCallback(async (userIndex: number, text: string) => {
    if (!currentSession || streaming || !text.trim()) return;
    setMessages(prev => {
      let seen = -1;
      let cut = -1;
      for (const [i, m] of prev.entries()) {
        if (m.role === 'user') {
          seen++;
          if (seen === userIndex) { cut = i; break; }
        }
      }
      if (cut < 0) return prev;
      const kept = prev.slice(0, cut);
      kept.push({ role: 'user', text });
      kept.push({ role: 'assistant', text: '', pending: true });
      return kept;
    });
    setStreaming(true);
    setStatusText('thinking…');
    try {
      await api.editUserMessage(currentSession, userIndex, text);
    } catch (err) {
      failPendingBubble(err);
    }
  }, [currentSession, streaming]);

  // failPendingBubble seals the streaming bubble with the error text — the
  // shared failure path for send/regenerate/edit when the POST itself
  // fails (the SSE stream handles mid-turn failures).
  const failPendingBubble = (err: unknown) => {
    setStreaming(false);
    setStatusText('');
    const msg = err instanceof Error ? err.message : String(err);
    setMessages(prev => {
      const updated = [...prev];
      const last = updated[updated.length - 1];
      if (last && last.pending) {
        updated[updated.length - 1] = { ...last, pending: false, text: msg };
      }
      return updated;
    });
  };

  // deleteSession removes a stored session and its pin; standing in it
  // bounces the view to a fresh chat.
  const deleteSession = useCallback(async (id: string) => {
    try {
      await api.deleteSession(id);
      if (id === currentSession) {
        setCurrentSession('');
        setMessages([]);
        setWelcome(true);
        setStatusText('');
      }
      await loadSessions();
      await loadPins();
      pushToast({ level: 'success', message: 'Session deleted', duration: 2200 });
    } catch (e) {
      pushToast({ level: 'error', message: e instanceof Error ? e.message : String(e) });
    }
  }, [currentSession, pushToast]);

  const answerPermission = useCallback(async (approved: boolean, feedback?: string) => {
    if (!pendingAsk) return;
    const ask = pendingAsk;
    setPendingAsk(null);
    try {
      await api.answerAsk(ask.id, approved, feedback || '');
    } catch (e) {
      pushToast({ level: 'error', message: e instanceof Error ? e.message : String(e) });
    }
  }, [pendingAsk, pushToast]);

  const togglePin = useCallback(async (id: string) => {
    try {
      const r = await api.togglePin(id);
      setPins(r.pins);
    } catch (e) {
      pushToast({ level: 'error', message: String(e) });
    }
  }, [pushToast]);

  const cycleMode = useCallback(async () => {
    try {
      const r = await api.setMode('cycle');
      pushToast({ level: 'success', message: `Mode: ${r.label}` });
    } catch (e) {
      pushToast({ level: 'error', message: String(e) });
    }
  }, [pushToast]);

  const compact = useCallback(async () => {
    if (!currentSession) {
      pushToast({ level: 'warning', message: 'Start a session first.' });
      return;
    }
    try {
      const r = await api.compactContext(currentSession);
      pushToast({
        level: r.status === 'noop' ? 'warning' : 'success',
        message: r.status === 'noop' ? 'Nothing to compact' : 'Context compacted',
      });
    } catch (e) {
      pushToast({ level: 'error', message: String(e) });
    }
  }, [currentSession, pushToast]);

  // ---- slash commands ----
  const slashCommands: SlashCommand[] = useMemo(() => [
    { name: 'clear', description: 'Start a new empty session' },
    { name: 'compact', description: 'Compact the context window now' },
    { name: 'context', description: 'Show where the context window is spent' },
    { name: 'diff', description: 'Review git diff — /diff, /diff staged, /diff HEAD', needsArgs: false },
    { name: 'code', description: 'Open a file in the viewer — /code path[:line]', needsArgs: true },
    { name: 'branch', description: 'Switch the working branch — /branch [name] creates', needsArgs: false },
    { name: 'snapshot', description: 'Explicit git snapshot tool — create|rollback|list|delete', needsArgs: true },
    { name: 'permission', description: 'Inspect APPA permission state' },
    { name: 'config', description: 'Open the local config editor' },
    { name: 'sessions', description: 'Browse and resume sessions for this directory' },
    { name: 'skills', description: 'Browse installed skills' },
    { name: 'extensions', description: 'List installed extensions' },
    { name: 'browser', description: 'Control a live web browser — open|proxy|close|profile', needsArgs: true },
  ], []);

  const onSlashCommand = useCallback((cmd: SlashCommand) => {
    switch (cmd.name) {
      case 'clear':
        selectSession('');
        break;
      case 'compact':
        compact();
        break;
      case 'context':
        setOverlay({ kind: 'context' });
        break;
      case 'diff':
        setOverlay({ kind: 'diff' });
        break;
      case 'code':
        // Seed the viewer with the argument the user typed after /code
        // ("/code main.go:42" → path main.go, line 42). Bare /code opens
        // the entry form — the viewer parses "path:line" itself.
        setOverlay({ kind: 'code', path: (cmd.rawArgs || '').trim() });
        break;
      case 'branch':
        setOverlay({ kind: 'branch' });
        break;
      case 'snapshot':
        setOverlay({ kind: 'snapshot' });
        break;
      case 'permission':
        setOverlay({ kind: 'permission' });
        break;
      case 'config':
        window.open('/settings', '_blank');
        break;
      case 'sessions':
        // The sidebar already lists sessions; focus the filter input by
        // toggling welcome to false so the sidebar becomes the focus.
        setWelcome(false);
        pushToast({ level: 'info', message: 'Pick a session from the sidebar.' });
        break;
      case 'skills':
        setOverlay({ kind: 'skills' });
        break;
      case 'extensions':
        setOverlay({ kind: 'extensions' });
        break;
      case 'browser':
        pushToast({ level: 'warning', message: 'Browser tool opens via the agent. Try: open https://example.com' });
        break;
      default:
        pushToast({ level: 'info', message: `/${cmd.name} — no web handler; sending as prompt.` });
    }
  }, [compact, pushToast]);

  // ---- Ctrl+K palette commands ----
  const paletteCommands: Command[] = useMemo(() => [
    { id: 'open-diff', group: 'tools', verb: 'Review git diff', description: 'Open the diff viewer (working tree, staged, HEAD)', keywords: ['git', 'review', 'changes'], run: () => setOverlay({ kind: 'diff' }) },
    { id: 'open-branch', group: 'tools', verb: 'Switch branch', description: 'Pick or create a git branch', keywords: ['git', 'checkout'], run: () => setOverlay({ kind: 'branch' }) },
    { id: 'open-snapshot', group: 'tools', verb: 'Manage snapshots', description: 'create / rollback / list / delete', keywords: ['git', 'snapshot'], run: () => setOverlay({ kind: 'snapshot' }) },
    { id: 'open-code', group: 'tools', verb: 'Open code viewer', description: 'View a file with syntax highlighting', keywords: ['file', 'read'], run: () => setOverlay({ kind: 'code', path: '' }) },
    { id: 'open-context', group: 'tools', verb: 'Context breakdown', description: 'See where the context window is spent', keywords: ['tokens', 'window'], run: () => setOverlay({ kind: 'context' }) },
    { id: 'open-permission', group: 'tools', verb: 'APPA permission state', description: 'Trajectory label, effects, rulings', keywords: ['appa', 'trajectory'], run: () => setOverlay({ kind: 'permission' }) },
    // Personas: one switch entry per profile, then the manager. Switching
    // rebinds every running engine to the persona's prompt + memory bank.
    ...personas.map(p => ({
      id: `persona-${p.id}`,
      group: 'personas',
      verb: activePersonaId === p.id ? `${p.name} — active` : `Speak as ${p.name}`,
      description: p.tagline || 'personality profile with its own memory bank',
      keywords: ['persona', 'personality', 'memory', 'profile', p.name.toLowerCase()],
      icon: personaAvatar(p, 22),
      run: () => { if (p.id !== activePersonaId) switchPersona(p.id); },
    })),
    { id: 'open-personas', group: 'personas', verb: 'Manage personas', description: 'Create · edit · memory banks', keywords: ['persona', 'personality', 'memory', 'profile'], run: () => setOverlay({ kind: 'personas' }) },
    { id: 'compact', group: 'actions', verb: 'Compact context now', description: 'Force a context compaction', keywords: ['compact', 'summarize'], run: compact },
    { id: 'clear', group: 'actions', verb: 'New chat', description: 'Start a fresh session', keywords: ['clear', 'session', 'new'], run: () => selectSession('') },
    { id: 'cycle-mode', group: 'settings', verb: 'Cycle permission mode', description: 'ask → read-only → autopilot → strict → yolo', keywords: ['permission', 'mode', 'cycle'], run: cycleMode },
    { id: 'settings', group: 'settings', verb: 'Settings', description: 'Model · think · agents · memory · theme', keywords: ['config', 'model', 'think', 'agents', 'memory'], run: () => setOverlay({ kind: 'settings' }) },
    { id: 'skills', group: 'browse', verb: 'Browse skills', description: 'List installed SKILL.md files', keywords: ['skill'], run: () => setOverlay({ kind: 'skills' }) },
    { id: 'extensions', group: 'browse', verb: 'Browse extensions', description: 'List discovered a1 extensions', keywords: ['extension', 'plugin'], run: () => setOverlay({ kind: 'extensions' }) },
    { id: 'mail', group: 'browse', verb: 'Read mail', description: 'Mailboxes · messages · read pane (read-only)', keywords: ['mail', 'inbox', 'email', 'maildir', 'unread'], run: () => setOverlay({ kind: 'mail' }) },
    { id: 'mail-accounts', group: 'browse', verb: 'Configure mail accounts', description: 'Add · edit · test IMAP servers and Maildirs', keywords: ['mail', 'account', 'imap', 'password', 'email', 'server', 'maildir'], run: () => setOverlay({ kind: 'mail-accounts' }) },
    { id: 'open-files', group: 'browse', verb: 'Browse files', description: 'Split-screen browser — preview, edit, save (2 MB cap)', keywords: ['file', 'browser', 'edit', 'explorer', 'save', 'folder'], run: () => setFilesOpen(true) },
    { id: 'config', group: 'browse', verb: 'Open config editor', description: 'Opens /settings in a new tab', keywords: ['config', 'yaml'], run: () => window.open('/settings', '_blank') },
  ], [compact, cycleMode, personas, activePersonaId, switchPersona]);

  return (
    <div class="flex flex-col h-screen bg-paper text-ink">
      <div class="flex flex-1 min-h-0">
        {/* The left-edge hover zone: the drawer's opener at every viewport
            (below lg the hamburger bar exists too). A slim invisible strip
            hugging the screen's left edge; crossing it slides the session
            drawer in. It carries the one visible affordance — a small pill
            that brightens as the pointer lands — plus keyboard access
            (focus opens, Enter toggles). z-20: above the page, under the
            drawer itself. */}
        <div
          class="edge-zone fixed inset-y-0 left-0 z-20 w-3"
          onMouseEnter={openNav}
          onClick={() => (navOpen ? closeNavNow() : openNav())}
        >
          <button
            type="button"
            class="sr-only focus:not-sr-only focus:absolute focus:top-1/2 focus:-translate-y-1/2 focus:left-1 focus:px-2 focus:py-1 focus:rounded-sm focus:bg-paper-card focus:border focus:border-hairline focus:text-xs focus:text-ink-light"
            onClick={() => (navOpen ? closeNavNow() : openNav())}
            aria-expanded={navOpen}
            aria-label="Toggle the session list"
          >
            Sessions
          </button>
          <span class="edge-hint" aria-hidden="true" />
        </div>
        {/* The drawer's scrim: below lg only (touch has no hover, so the
            tap-anywhere exit is what closes there) — under the drawer
            (z-20 vs z-30) and under the modal layer (z-40), which is the
            fix for the sidebar floating OVER open dialogs. */}
        {navOpen && (
          <div
            class="lg:hidden fixed inset-0 z-20 bg-ink/25"
            onClick={closeNavNow}
            aria-hidden="true"
          />
        )}
        <Sidebar
          sessions={sessions}
          currentSession={currentSession}
          pins={pins}
          onSelect={selectSession}
          onRefresh={loadSessions}
          onTogglePin={togglePin}
          onDelete={deleteSession}
          persona={activePersona}
          onOpenPersonas={() => setOverlay({ kind: 'personas' })}
          open={navOpen}
          onClose={closeNavNow}
          onMouseEnter={openNav}
          onMouseLeave={scheduleNavClose}
        />
        <main class="flex-1 flex flex-col min-w-0 relative">
          {/* The mobile menu bar: below lg only — the drawer's opener and a
              wordmark, the one chrome row a phone needs. Above lg it is
              nothing; the sidebar is the chrome. */}
          <div class="lg:hidden flex items-center gap-1.5 pl-1.5 pr-3 py-1.5 border-b border-hairline flex-shrink-0">
            <button
              type="button"
              class="p-2 rounded-sm text-ink-muted hover:text-seal hover:bg-seal-light active:scale-90 transition-all duration-fast"
              onClick={openNav}
              aria-label="Open the session list"
              aria-expanded={navOpen}
            >
              <Menu size={16} />
            </button>
            <span class="font-serif text-sm text-ink-light">a1</span>
          </div>
          {welcome ? (
            // The dashboard IS the welcome state: hero + widget grid with
            // the composer docked bottom-center. Submitting from the dock
            // is the old welcome flow verbatim (send → setWelcome(false)),
            // so the chat view takes over with no new routing.
            <Dashboard
              sessions={sessions}
              pins={pins}
              persona={activePersona}
              streaming={streaming}
              onSend={send}
              onCancel={cancel}
              slashCommands={slashCommands}
              onSlashCommand={onSlashCommand}
              onSelectSession={selectSession}
              onOpenOverlay={kind => setOverlay({ kind })}
              onOpenFiles={() => setFilesOpen(true)}
              filesOpen={filesOpen}
              onToggleFiles={() => setFilesOpen(v => !v)}
              onToast={pushToast}
              mailVersion={mailVersion}
            />
          ) : (
            <>
              <ChatArea
                messages={messages}
                streaming={streaming}
                pendingAsk={pendingAsk}
                onAnswer={answerPermission}
                statusText={statusText}
                sessionKey={currentSession}
                onRegenerate={regenerate}
                onEditUser={editUser}
              />
              <InputBar
                onSend={send}
                onCancel={cancel}
                streaming={streaming}
                welcome={false}
                persona={activePersona}
                slashCommands={slashCommands}
                onSlashCommand={onSlashCommand}
                filesOpen={filesOpen}
                onToggleFiles={() => setFilesOpen(v => !v)}
              />
              {/* Footer chrome sits BELOW the composer + tips so the input
                  stays the bottom-most interactive element's neighbor and
                  the status strip reads as a terminal-style status line. */}
              <Footer
                state={state}
                streaming={streaming}
                statusText={statusText}
                hasSession={!!currentSession}
                onSwitchMode={cycleMode}
                onOpenSettings={() => setOverlay({ kind: 'settings' })}
                onOpenBranch={() => setOverlay({ kind: 'branch' })}
                onOpenPermissionInfo={() => setOverlay({ kind: 'permission' })}
                onCompact={compact}
                onOpenContext={() => setOverlay({ kind: 'context' })}
              />
            </>
          )}
        </main>

        {/* The split-screen right pane: full-system file browser. A pane,
            not an overlay — it holds its own scroll, its own address bar,
            and it stays open while the user reads mail or chats. Below lg
            it covers the screen (the responsive sweep is chunk 6's).
            Keep-alive like the session drawer: mounted from app load,
            `open` is a pure class flip (`.file-pane` — width reveal at lg,
            slide below), so the close animates out instead of unmounting
            mid-frame, and reopening returns to the folder it was left in. */}
        <FileBrowser open={filesOpen} onClose={() => setFilesOpen(false)} onToast={pushToast} />
      </div>

      {/* Overlays */}
      {overlay.kind === 'palette' && (
        <CommandPalette
          commands={paletteCommands}
          onClose={() => setOverlay({ kind: 'none' })}
        />
      )}
      {overlay.kind === 'diff' && (
        <DiffViewer
          onClose={() => setOverlay({ kind: 'none' })}
          onInsertIntoChat={(text) => { send(text); setOverlay({ kind: 'none' }); }}
        />
      )}
      {overlay.kind === 'code' && (
        <CodeViewer
          path={overlay.path}
          line={overlay.line}
          onClose={() => setOverlay({ kind: 'none' })}
        />
      )}
      {overlay.kind === 'branch' && (
        <BranchPicker
          onClose={() => setOverlay({ kind: 'none' })}
          onSwitched={(b) => pushToast({ level: 'success', message: `Now on ${b}` })}
        />
      )}
      {overlay.kind === 'snapshot' && (
        <SnapshotManager onClose={() => setOverlay({ kind: 'none' })} />
      )}
      {overlay.kind === 'permission' && (
        <PermissionInfo
          sessionId={currentSession || undefined}
          onClose={() => setOverlay({ kind: 'none' })}
        />
      )}
      {overlay.kind === 'context' && (
        <ContextBreakdown
          sessionId={currentSession || undefined}
          onClose={() => setOverlay({ kind: 'none' })}
        />
      )}
      {overlay.kind === 'skills' && (
        <SkillsPanel onClose={() => setOverlay({ kind: 'none' })} />
      )}
      {overlay.kind === 'extensions' && (
        <ExtensionsPanel onClose={() => setOverlay({ kind: 'none' })} />
      )}
      {overlay.kind === 'settings' && (
        <SettingsPanel onClose={() => setOverlay({ kind: 'none' })} />
      )}
      {overlay.kind === 'personas' && (
        <PersonaManager
          onClose={() => { setOverlay({ kind: 'none' }); loadPersonas(); }}
          onSwitched={p => pushToast({ level: 'success', message: `Now speaking as ${p.name}`, duration: 2500 })}
        />
      )}
      {overlay.kind === 'mail' && (
        <MailReader
          onClose={() => setOverlay({ kind: 'none' })}
          onConfigure={() => setOverlay({ kind: 'mail-accounts' })}
        />
      )}
      {overlay.kind === 'mail-accounts' && (
        <MailAccountsEditor
          onClose={() => setOverlay({ kind: 'none' })}
          onSaved={() => setMailVersion(v => v + 1)}
        />
      )}

      <ToastStack toasts={toasts} onDismiss={dismissToast} />
    </div>
  );
}
