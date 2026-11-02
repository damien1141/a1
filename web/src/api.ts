// API client — wraps fetch calls to the Go backend.
// All endpoints are relative (same origin in production, proxied by Vite in dev).

export interface SessionMeta {
  id: string;
  preview: string;
  mtime: string;
}

export interface TranscriptEntry {
  role: 'user' | 'assistant' | 'tool';
  text: string;
  tools?: ToolRun[];
}

export interface ToolRun {
  toolUseId: string;
  name: string;
  status: string;
  detail: string;
  output: string;
  error: string;
  // msgId ties the tool row to the assistant round it ran in (the same id
  // the round's delta events carry). The browser uses it to attach the row
  // to the right bubble when one turn spans several say-something / call-a-
  // tool rounds. Absent on replayed transcripts and legacy servers.
  msgId?: string;
  // Sources is populated by search-mode fetch calls. Each entry is one
  // ranked DuckDuckGo result with URL, title, host (no www.), favicon
  // URL, snippet, and (when fetch_pages=true) the page body. The web
  // frontend renders these as small source pills below the answer bubble.
  // Empty/absent for non-search tool calls — the existing pill render
  // path is unchanged.
  sources?: Source[];
}

// Source is one ranked reference produced by a search-style fetch call.
// Field names match the Go session.ToolSource / tooldef.Source JSON
// tags exactly so no per-field translation is needed in the renderer.
export interface Source {
  rank: number;        // 1-indexed
  url: string;         // canonical, redirect-unwrapped
  title: string;       // search-result listing title
  host: string;        // hostname, www-stripped
  favicon_url: string; // https://icons.duckduckgo.com/ip3/<host>.ico
  snippet: string;     // DDG's short result-listing excerpt
  content?: string;    // fetched page body when fetch_pages=true
  fetch_error?: string; // populated when the per-page fetch failed
}

// ServerEvent is the SSE envelope. New kinds: toast, state.
export interface ServerEvent {
  kind: 'session' | 'delta' | 'tool' | 'ask' | 'status' | 'done' | 'toast' | 'state';
  sessionId: string;
  delta?: { text: string; reasoning: string; msgId?: string };
  tool?: ToolRun;
  ask?: { id: string; tool: string; reason: string };
  status?: { text: string };
  toast?: ToastPayload;
  state?: StatePayload;
  failed?: boolean;
}

export type ToastLevel = 'success' | 'warning' | 'error' | 'info';

export interface ToastPayload {
  level: ToastLevel;
  message: string;
  duration?: number; // milliseconds
}

export interface StatePayload {
  model: string;
  think: string;
  mode: string;
  branch: string;
  contextWindow: number;
  toolBytes: number;
  toolCount: number;
  // contextTokens is the estimated total context usage (system prompt +
  // messages + tool outputs + tool schemas) in tokens. The footer uses
  // this instead of toolBytes/4 because toolBytes only counts schemas.
  // Absent in older server builds — the footer falls back to toolBytes/4.
  contextTokens?: number;
  agentsEnabled: boolean;
  memoryEnabled: boolean;
  persona?: Persona;
}

// ---- personas ----

export interface Persona {
  id: string;
  name: string;
  tagline: string;
  description?: string; // legacy alias of tagline
  emoji: string;
  hue: number;
  prompt: string;
  // When true (and the persona has a prompt), the persona's prompt REPLACES
  // a1's built-in system prompt instead of riding alongside it.
  overrideSystemPrompt?: boolean;
  builtin: boolean;
  memoryCount: number;
  // Uploaded-picture extras (this backend's extension over the original
  // emoji-only avatars): hasAvatar reports a stored picture, avatarVersion
  // cache-busts the <img> after a re-upload, isActive marks the active row.
  hasAvatar?: boolean;
  isActive?: boolean;
  avatarVersion?: number;
  // Per-persona runtime overrides. model/permissionMode are "" (or absent)
  // when the persona inherits the global config; memoryEnabled is undefined
  // when it inherits the global memory toggle.
  model?: string;
  permissionMode?: string;
  memoryEnabled?: boolean;
  // memoryModel is the second model for background write-backs — memory
  // consolidation and smart session names. "" (or absent) = same model as
  // the main one.
  memoryModel?: string;
}

export interface PersonaListResponse {
  personas: Persona[];
  active: string;
  memoryEnabled: boolean;
}

export interface PersonaInput {
  name: string;
  tagline: string;
  emoji: string;
  hue: number;
  prompt: string;
  // Tri-state over the wire: undefined = keep stored value, true/false = set.
  overrideSystemPrompt?: boolean;
  // Per-persona settings. The editor sends all of them explicitly; "" clears
  // an override (back to the global default). undefined = keep stored.
  model?: string;
  permissionMode?: string;
  memoryEnabled?: boolean;
  // Explicit "inherit the global memory toggle": clears the persona's
  // memory_enabled override. Needed because memoryEnabled: undefined and
  // null both mean "keep stored" on the wire — a tri-state editor cannot
  // express "remove the override" without this flag.
  clearMemoryEnabled?: boolean;
  memoryModel?: string;
}

// ModelInfo is one entry of GET /api/models: the persona editor's model
// dropdown options, with the context window the footer also consumes.
export interface ModelInfo {
  name: string;
  contextWindow: number;
  default: boolean;
}

export async function fetchModels(): Promise<ModelInfo[]> {
  const res = await fetch('/api/models');
  if (!res.ok) throw new Error(`models: ${res.status}`);
  const body = await res.json();
  return Array.isArray(body.models) ? body.models : [];
}

export interface MemorySummary {
  id: number;
  content: string;
  kind: string;
  scope: string;
  thread?: string;
  createdAt: string;
}

export async function fetchPersonas(): Promise<PersonaListResponse> {
  const res = await fetch('/api/personas');
  if (!res.ok) throw new Error(`personas: ${res.status}`);
  // Tolerate a malformed/legacy body: an unusable registry must degrade to
  // "no personas in the palette", never crash the app on render.
  const body = await res.json().catch(() => ({}));
  return {
    personas: Array.isArray(body.personas) ? body.personas : [],
    active: typeof body.active === 'string' ? body.active : 'default',
    memoryEnabled: !!body.memoryEnabled,
  };
}

async function parseJSONorThrow<T>(res: Response): Promise<T> {
  if (!res.ok) throw new Error(await parseErrorBody(res));
  return res.json();
}

export async function createPersona(input: PersonaInput): Promise<{ persona: Persona }> {
  const res = await fetch('/api/personas', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  });
  return parseJSONorThrow(res);
}

export async function updatePersona(id: string, input: Partial<PersonaInput>): Promise<{ persona: Persona }> {
  const res = await fetch(`/api/personas/${encodeURIComponent(id)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  });
  return parseJSONorThrow(res);
}

export async function deletePersona(id: string): Promise<void> {
  const res = await fetch(`/api/personas/${encodeURIComponent(id)}`, { method: 'DELETE' });
  if (!res.ok) throw new Error(await parseErrorBody(res));
}

export async function setPersonaActive(id: string): Promise<{ active: string; persona: Persona }> {
  const res = await fetch('/api/personas/active', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id }),
  });
  return parseJSONorThrow(res);
}

export async function fetchPersonaMemories(id: string): Promise<{ memories: MemorySummary[]; memoryEnabled: boolean }> {
  const res = await fetch(`/api/personas/memories?id=${encodeURIComponent(id)}`);
  if (!res.ok) throw new Error(`persona memories: ${res.status}`);
  return res.json();
}

export async function forgetPersonaMemory(id: string, mid: number): Promise<void> {
  const res = await fetch(`/api/personas/memories?id=${encodeURIComponent(id)}&mid=${mid}`, { method: 'DELETE' });
  if (!res.ok) throw new Error(`forget memory: ${res.status}`);
}

// uploadPersonaAvatar attaches a profile picture to a persona. The image
// overrides the emoji-on-gradient avatar everywhere it is rendered.
export async function uploadPersonaAvatar(id: string, file: File): Promise<{ persona: Persona }> {
  const form = new FormData();
  form.append('id', id);
  form.append('avatar', file);
  const res = await fetch('/api/personas/avatar', { method: 'POST', body: form });
  return parseJSONorThrow(res);
}

// personaAvatarURL returns the URL that serves the persona's uploaded
// picture, or '' when the persona has none (callers fall back to the
// emoji-on-gradient avatar).
export function personaAvatarURL(p: Pick<Persona, 'id' | 'hasAvatar' | 'avatarVersion'>): string {
  if (!p.hasAvatar) return '';
  const v = p.avatarVersion ?? 0;
  return `/api/personas/${encodeURIComponent(p.id)}/avatar${v > 0 ? `?v=${v}` : ''}`;
}

// ---- legacy chat API ----

export async function fetchSessions(): Promise<SessionMeta[]> {
  const res = await fetch('/api/sessions');
  if (!res.ok) return [];
  const html = await res.text();
  const parser = new DOMParser();
  const doc = parser.parseFromString(html, 'text/html');
  const rows = doc.querySelectorAll('.session-row');
  return Array.from(rows).map(row => ({
    id: row.getAttribute('data-session-id') || '',
    preview: row.querySelector('.session-title')?.textContent || '',
    mtime: row.querySelector('.session-meta')?.textContent || '',
  }));
}

export async function fetchTranscript(sessionId: string): Promise<string> {
  const res = await fetch(`/api/transcript?id=${encodeURIComponent(sessionId)}`);
  if (!res.ok) return '';
  return res.text();
}

// ---- structured transcript (JSON) ----
//
// The HTML fragment above exists for the htmx-era scrape; the client prefers
// this JSON shape because scraping textContent out of rendered HTML flattened
// markdown paragraphs into one unbroken wall and dropped reasoning, usage and
// tool runs entirely.

export interface TranscriptUsage {
  in: number;
  out: number;
  tps?: number;
  total?: number;
}

export interface TranscriptMessage {
  role: 'user' | 'assistant' | 'system';
  text?: string;
  reasoning?: string;
  usage?: TranscriptUsage;
  tools?: ToolRun[];
  // id is the session-tree entry id of this row (absent for synthetic
  // rows like the trailing-tool-run fallback). The edit-user-message
  // flow identifies its target by user-row INDEX, so this is informational
  // today — kept on the wire so future affordances (branch pickers, jump
  // links) can address entries directly.
  id?: string;
}

export interface TranscriptData {
  sessionId: string;
  messages: TranscriptMessage[];
}

export async function fetchTranscriptMessages(sessionId: string): Promise<TranscriptData> {
  const res = await fetch(`/api/transcript/messages?id=${encodeURIComponent(sessionId)}`);
  if (!res.ok) throw new Error(await parseErrorBody(res));
  const body = await res.json();
  // Tolerate a legacy/absent payload: an unusable body degrades to "no
  // messages" so the caller can fall back to the HTML scrape.
  return {
    sessionId: typeof body.sessionId === 'string' ? body.sessionId : sessionId,
    messages: Array.isArray(body.messages) ? body.messages : [],
  };
}

// parseErrorBody unwraps the backend's JSON error envelope so the chat
// bubble shows `prompt is empty` rather than `Error: Error: {"error":...}`.
// Falls back to the raw body (or statusText) when the body isn't JSON.
async function parseErrorBody(res: Response): Promise<string> {
  const text = await res.text();
  if (!text) return res.statusText || `HTTP ${res.status}`;
  try {
    const body = JSON.parse(text);
    if (body && typeof body.error === 'string' && body.error) return body.error;
    if (body && typeof body.message === 'string' && body.message) return body.message;
  } catch {
    // Not JSON — fall through to raw text.
  }
  return text.trim();
}

export async function sendPrompt(sessionId: string, prompt: string): Promise<{ sessionId: string }> {
  const res = await fetch('/api/prompt', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sessionId, prompt }),
  });
  if (!res.ok) throw new Error(await parseErrorBody(res));
  return res.json();
}

export async function cancelTurn(sessionId: string): Promise<void> {
  const res = await fetch('/api/cancel', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sessionId }),
  });
  if (!res.ok) throw new Error(await parseErrorBody(res));
}

// deleteSession removes a stored session (jsonl file or vault row) plus
// its pin. The server refuses when the session has a turn in flight.
export async function deleteSession(sessionId: string): Promise<void> {
  const res = await fetch(`/api/session?id=${encodeURIComponent(sessionId)}`, { method: 'DELETE' });
  if (!res.ok) throw new Error(await parseErrorBody(res));
}

// regenerate drops the last assistant response chain back to its
// preceding user message and streams a fresh completion (a re-roll on a
// new sibling branch; the old one stays abandoned in the session tree).
export async function regenerateTurn(sessionId: string): Promise<void> {
  const res = await fetch('/api/regenerate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sessionId }),
  });
  if (!res.ok) throw new Error(await parseErrorBody(res));
}

// editUserMessage rewrites a past user message and re-runs the turn from
// there as a new branch. userIndex is the 0-based position of the message
// among the session's user rows (the server walks the current path's user
// entries in order), which stays stable across live + replayed rows —
// unlike entry ids, which live turns mint server-side unseen.
export async function editUserMessage(sessionId: string, userIndex: number, text: string): Promise<void> {
  const res = await fetch('/api/edit', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sessionId, userIndex, prompt: text }),
  });
  if (!res.ok) throw new Error(await parseErrorBody(res));
}

export async function answerAsk(id: string, approved: boolean, feedback: string): Promise<void> {
  const res = await fetch('/api/ask', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id, approved, feedback }),
  });
  if (!res.ok) throw new Error(await parseErrorBody(res));
}

export function subscribeEvents(): EventSource {
  return new EventSource('/api/events');
}

// ---- mode + pins (already shipped server-side) ----

export interface ModeEntry {
  mode: string;
  label: string;
  summary: string;
  active: boolean;
}

export interface ModeResponse {
  mode: string;
  label: string;
  summary: string;
  modes: ModeEntry[];
}

export async function fetchMode(): Promise<ModeResponse> {
  const res = await fetch('/api/mode');
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export async function setMode(mode: string): Promise<ModeResponse> {
  const res = await fetch('/api/mode', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ mode }),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export async function fetchPins(): Promise<string[]> {
  const res = await fetch('/api/pins');
  if (!res.ok) return [];
  const body = await res.json();
  return body.pins || [];
}

export async function togglePin(sessionId: string): Promise<{ pinned: boolean; pins: string[] }> {
  const res = await fetch('/api/pin', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sessionId }),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

// ---- state ----

export async function fetchState(): Promise<StatePayload> {
  const res = await fetch('/api/state');
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

// ---- compact / context ----

export async function compactContext(sessionId: string): Promise<{ status: string }> {
  const res = await fetch('/api/compact', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sessionId }),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export interface ContextCategory {
  name: string;
  tokens: number;
  note?: string;
}

export interface ContextBreakdown {
  contextWindow: number;
  total: number;
  usedPercent: number;
  categories: ContextCategory[];
  text: string;
}

export async function fetchContextBreakdown(sessionId: string): Promise<ContextBreakdown> {
  const res = await fetch(`/api/context?id=${encodeURIComponent(sessionId)}`);
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

// ---- branches ----

export interface Branch {
  name: string;
  current: boolean;
  remote: boolean;
  upstream?: string;
  ahead?: number;
  behind?: number;
  committed?: string;
  subject?: string;
}

export interface BranchesResponse {
  branches: Branch[];
  recent: string[];
  current: string;
  dir: string;
}

export async function fetchBranches(dir?: string): Promise<BranchesResponse> {
  const q = dir ? `?dir=${encodeURIComponent(dir)}` : '';
  const res = await fetch(`/api/branches${q}`);
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export async function switchBranch(name: string, opts?: { dir?: string; create?: boolean }): Promise<{ branch: string; current: string }> {
  const res = await fetch('/api/branch', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, dir: opts?.dir || '', create: opts?.create || false }),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

// ---- diff ----

export interface DiffResponse {
  spec: string;
  diff: string;
  empty: string;
}

export async function fetchDiff(what?: string, dir?: string): Promise<DiffResponse> {
  const params = new URLSearchParams();
  if (what) params.set('what', what);
  if (dir) params.set('dir', dir);
  const q = params.toString() ? `?${params.toString()}` : '';
  const res = await fetch(`/api/diff${q}`);
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

// ---- code ----

export interface CodeFile {
  path: string;
  abs: string;
  line: number;
  content: string;
  size: number;
}

export async function fetchCode(path: string, line?: number): Promise<CodeFile> {
  const params = new URLSearchParams({ path });
  if (line) params.set('line', String(line));
  const res = await fetch(`/api/code?${params.toString()}`);
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

// ---- snapshot ----

export interface SnapshotResult {
  action: string;
  content: string;
  output: string;
  summary: string;
}

export async function runSnapshot(action: string, branch?: string): Promise<SnapshotResult> {
  const res = await fetch('/api/snapshot', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ action, branch: branch || '' }),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

// ---- permission info ----

export interface PermissionInfo {
  mode: string;
  label: string;
  summary: string;
  trajectory?: string;
  effects?: number;
  rulings?: number;
  acceptedNarrowing?: number;
}

export async function fetchPermissionInfo(sessionId?: string): Promise<PermissionInfo> {
  const q = sessionId ? `?id=${encodeURIComponent(sessionId)}` : '';
  const res = await fetch(`/api/permission/info${q}`);
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

// ---- settings ----

export interface SettingsCatalog {
  state: {
    model: string;
    think: string;
    agentsEnabled: boolean;
    memoryEnabled: boolean;
  };
  models: string[];
  thinkLevels: string[];
  modes: ModeEntry[];
  themes: string[];
}

export async function fetchSettings(): Promise<SettingsCatalog> {
  const res = await fetch('/api/settings');
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export async function setModel(name: string): Promise<StatePayload> {
  const res = await fetch('/api/settings/model', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export async function setThink(level: string): Promise<StatePayload> {
  const res = await fetch('/api/settings/think', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ level }),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export async function setAgentsEnabled(enabled: boolean): Promise<StatePayload> {
  const res = await fetch('/api/settings/agents', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled }),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export async function setMemoryEnabled(enabled: boolean): Promise<StatePayload> {
  const res = await fetch('/api/settings/memory', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ enabled }),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

// ---- skills ----

export interface Skill {
  name: string;
  description?: string;
  path: string;
}

export async function fetchSkills(): Promise<{ skills: Skill[]; dir: string }> {
  const res = await fetch('/api/skills');
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

// ---- extensions ----

export interface Extension {
  id: string;
  name: string;
  version?: string;
  description?: string;
  path: string;
  source?: string;
}

export async function fetchExtensions(): Promise<{ extensions: Extension[]; warnings: { path?: string; message: string }[] }> {
  const res = await fetch('/api/extensions');
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}
