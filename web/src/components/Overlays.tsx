import { useState, useEffect, useMemo } from 'preact/hooks';
import { Modal } from './Modal';
import * as api from '../api';
import { ThemeSegmented } from './ThemeToggle';
import { Loader, GitBranch, FileCode, Camera, Shield, Brain, Sparkles, Boxes, Check, X, ChevronRight, Search, Palette } from "lucide-preact";

// =============================================================================
// DiffViewer — /diff overlay. Renders the unified diff with simple line
// coloring (added/removed/context). Mirrors the TUI's DiffPane.
// =============================================================================

interface DiffViewerProps {
  onClose: () => void;
  onInsertIntoChat?: (text: string) => void;
}

export function DiffViewer({ onClose, onInsertIntoChat }: DiffViewerProps) {
  const [spec, setSpec] = useState<string>('');
  const [data, setData] = useState<api.DiffResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const load = async (s: string) => {
    setLoading(true);
    setError('');
    try {
      const r = await api.fetchDiff(s);
      setData(r);
      setSpec(r.spec);
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load('');
  }, []);

  return (
    <Modal
      title="Git Diff"
      subtitle={data ? `spec: ${data.spec}` : 'loading…'}
      onClose={onClose}
      size="xl"
      footer={
        <>
          <DiffSpecButton label="Working tree" active={spec === 'working tree'} onClick={() => load('')} />
          <DiffSpecButton label="Staged" active={spec === 'staged'} onClick={() => load('staged')} />
          <DiffSpecButton label="HEAD" active={spec === 'HEAD'} onClick={() => load('HEAD')} />
          {onInsertIntoChat && data?.diff && (
            <button
              type="button"
              class="ml-auto px-3 py-1.5 rounded-sm text-xs bg-seal text-on-seal hover:opacity-90 transition-opacity"
              onClick={() => onInsertIntoChat('```\n' + data.diff + '\n```')}
            >
              Insert into chat
            </button>
          )}
        </>
      }
    >
      {loading ? <LoadingRow /> : error ? <ErrorRow msg={error} /> : (
        <>
          {data?.empty && (
            <div class="px-4 py-3 text-sm text-ink-muted bg-paper-sunk border-b border-hairline">
              {data.empty}
            </div>
          )}
          {data?.diff ? (
            <pre class="px-4 py-3 text-xs font-mono leading-relaxed whitespace-pre overflow-x-auto">
              {renderDiffLines(data.diff)}
            </pre>
          ) : !data?.empty ? (
            <div class="px-4 py-6 text-sm text-ink-muted text-center">No changes.</div>
          ) : null}
        </>
      )}
    </Modal>
  );
}

function DiffSpecButton({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      class={`px-2.5 py-1 rounded-sm text-xs transition-colors ${
        active ? 'bg-seal text-on-seal' : 'text-ink-muted hover:bg-seal-light hover:text-seal'
      }`}
      onClick={onClick}
    >
      {label}
    </button>
  );
}

function renderDiffLines(diff: string) {
  const lines = diff.split('\n');
  return lines.map((line, i) => {
    let cls = 'text-ink-light';
    if (line.startsWith('+') && !line.startsWith('+++')) cls = 'text-moss bg-moss/10';
    else if (line.startsWith('-') && !line.startsWith('---')) cls = 'text-cinnabar bg-cinnabar/10';
    else if (line.startsWith('@@')) cls = 'text-seal';
    else if (line.startsWith('diff ') || line.startsWith('index ')) cls = 'text-ink-muted font-medium';
    return <div key={i} class={`px-2 ${cls}`}>{line || ' '}</div>;
  });
}

// =============================================================================
// CodeViewer — /code overlay. Renders a file with line numbers and an
// optional highlighted line. Mirrors the TUI's CodePane.
// =============================================================================

// parseCodeTarget parses "path[:line]" the way the TUI's /code argument
// does, so "/code main.go:42" opens main.go with line 42 highlighted.
function parseCodeTarget(raw: string): { path: string; line?: number } {
  const trimmed = raw.trim();
  const m = trimmed.match(/^(.+?):(\d+)$/);
  if (m) return { path: m[1], line: parseInt(m[2], 10) };
  return { path: trimmed };
}

interface CodeViewerProps {
  path: string;
  line?: number;
  onClose: () => void;
  onInsertIntoChat?: (text: string) => void;
}

export function CodeViewer({ path, line, onClose, onInsertIntoChat }: CodeViewerProps) {
  // Effective path/line: seeded from props when /code was invoked with an
  // argument (e.g. "/code main.go:42"), else empty until the user types
  // one. Bare "/code" (and the palette's open-code entry) used to fetch
  // '' and immediately render a 400 error with no way to enter a path.
  const initial = path ? parseCodeTarget(path) : { path: '', line: undefined };
  const [data, setData] = useState<api.CodeFile | null>(null);
  const [loading, setLoading] = useState(!!initial.path);
  const [error, setError] = useState('');
  const [entry, setEntry] = useState('');
  const [activePath, setActivePath] = useState(initial.path);
  const [activeLine, setActiveLine] = useState(line ?? initial.line);

  const open = (raw: string) => {
    const { path: p, line: l } = parseCodeTarget(raw);
    if (!p) return;
    setActivePath(p);
    setActiveLine(l);
    setLoading(true);
    setError('');
  };

  useEffect(() => {
    if (!activePath) return;
    let cancelled = false;
    (async () => {
      setLoading(true);
      setError('');
      try {
        const r = await api.fetchCode(activePath, activeLine);
        if (!cancelled) setData(r);
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e));
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, [activePath, activeLine]);

  const lines = data ? data.content.split('\n') : [];
  const highlightLine = activeLine;

  return (
    <Modal
      title={data?.path || activePath || 'Open a file'}
      subtitle={data ? `${lines.length} lines · ${formatBytes(data.size)}` : (activePath ? 'loading…' : 'Enter a path to view it with line numbers')}
      onClose={onClose}
      size="xl"
      footer={
        <>
          <div class="flex items-center gap-2 flex-1 min-w-0">
            <input
              type="text"
              value={entry}
              onInput={e => setEntry((e.target as HTMLInputElement).value)}
              onKeyDown={e => { if (e.key === 'Enter') open(entry); }}
              placeholder="path/to/file.go:42"
              class="flex-1 min-w-0 px-2 py-1.5 text-sm bg-paper border border-hairline rounded-sm outline-none focus:border-seal font-mono"
            />
            <button
              type="button"
              disabled={!entry.trim() || loading}
              onClick={() => open(entry)}
              class="px-3 py-1.5 rounded-sm text-xs bg-seal text-on-seal hover:opacity-90 disabled:opacity-50 transition-opacity flex-shrink-0"
            >
              Open
            </button>
          </div>
          {onInsertIntoChat && data && (
            <button
              type="button"
              class="px-3 py-1.5 rounded-sm text-xs bg-seal text-on-seal hover:opacity-90 transition-opacity flex-shrink-0"
              onClick={() => onInsertIntoChat(`\`${data.path}\` (L${highlightLine || 1}): …`)}
            >
              Reference in chat
            </button>
          )}
        </>
      }
    >
      {loading ? <LoadingRow /> : error ? <ErrorRow msg={error} /> : !data ? (
        <div class="px-4 py-10 text-center">
          <p class="text-sm text-ink-muted">
            Type a file path (optionally <span class="font-mono">path:line</span>) and press Enter.
          </p>
          <p class="text-xs text-ink-muted mt-2">
            The path is resolved against the project root, like the TUI's <span class="font-mono">/code</span>.
          </p>
        </div>
      ) : (
        <div class="font-mono text-xs leading-relaxed overflow-x-auto">
          {lines.map((l, i) => {
            const n = i + 1;
            const isHighlight = highlightLine && n === highlightLine;
            return (
              <div
                key={i}
                class={`flex ${isHighlight ? 'bg-seal-light' : ''}`}
              >
                <span class="flex-shrink-0 w-12 pr-3 text-right text-ink-muted select-none">{n}</span>
                <span class="flex-1 whitespace-pre-wrap break-all px-2">{l || ' '}</span>
              </div>
            );
          })}
        </div>
      )}
    </Modal>
  );
}

// =============================================================================
// BranchPicker — /branch overlay. Lists local + remote branches, switches
// on click. Matches the TUI's branch picker.
// =============================================================================

interface BranchPickerProps {
  onClose: () => void;
  onSwitched?: (branch: string) => void;
}

export function BranchPicker({ onClose, onSwitched }: BranchPickerProps) {
  const [data, setData] = useState<api.BranchesResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [filter, setFilter] = useState('');
  const [newName, setNewName] = useState('');
  const [busy, setBusy] = useState(false);

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      setData(await api.fetchBranches());
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, []);

  const filtered = useMemo(() => {
    if (!data) return [];
    const q = filter.toLowerCase();
    if (!q) return data.branches;
    return data.branches.filter(b => b.name.toLowerCase().includes(q));
  }, [data, filter]);

  const onSwitch = async (name: string) => {
    setBusy(true);
    setError('');
    try {
      await api.switchBranch(name);
      onSwitched?.(name);
      await load();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  const onCreate = async () => {
    const name = newName.trim();
    if (!name) return;
    setBusy(true);
    setError('');
    try {
      await api.switchBranch(name, { create: true });
      setNewName('');
      onSwitched?.(name);
      await load();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      title="Switch Branch"
      subtitle={data ? `${data.branches.length} branches · on ${data.current || 'detached'}` : 'loading…'}
      onClose={onClose}
      size="lg"
      footer={
        <div class="flex items-center gap-2 w-full">
          <input
            type="text"
            value={newName}
            onInput={e => setNewName((e.target as HTMLInputElement).value)}
            onKeyDown={e => { if (e.key === 'Enter') onCreate(); }}
            placeholder="Create new branch: feat/x"
            class="flex-1 px-2 py-1.5 text-sm bg-paper border border-hairline rounded-sm outline-none focus:border-seal"
          />
          <button
            type="button"
            disabled={!newName.trim() || busy}
            onClick={onCreate}
            class="px-3 py-1.5 rounded-sm text-xs bg-seal text-on-seal hover:opacity-90 transition-opacity disabled:opacity-50"
          >
            Create
          </button>
        </div>
      }
    >
      <div class="px-4 py-2 border-b border-hairline flex items-center gap-2">
        <Search size={12} class="text-ink-muted" />
        <input
          type="text"
          value={filter}
          onInput={e => setFilter((e.target as HTMLInputElement).value)}
          placeholder="Filter branches…"
          class="flex-1 bg-transparent text-sm outline-none placeholder:text-ink-muted"
        />
      </div>

      {loading ? <LoadingRow /> : error ? <ErrorRow msg={error} /> : (
        <div class="py-1">
          {filtered.length === 0 ? (
            <div class="px-4 py-6 text-sm text-ink-muted text-center">No branches match.</div>
          ) : (
            filtered.map(b => (
              <button
                key={b.name}
                type="button"
                disabled={busy || b.current}
                onClick={() => onSwitch(b.name)}
                class={`w-full flex items-center gap-2 px-4 py-2 text-left transition-colors ${
                  b.current ? 'bg-seal-light' : 'hover:bg-seal-light/50'
                } ${busy ? 'opacity-50 cursor-not-allowed' : ''}`}
              >
                {b.current ? <Check size={12} class="text-seal flex-shrink-0" /> : <GitBranch size={12} class="text-ink-muted flex-shrink-0" />}
                <span class="font-mono text-sm flex-1 min-w-0 truncate">{b.name}</span>
                {b.remote && <span class="text-[10px] text-ink-muted px-1.5 py-0.5 bg-paper-sunk rounded-sm">remote</span>}
                {b.upstream && <span class="text-[10px] text-ink-muted">{b.ahead ? `↑${b.ahead}` : ''}{b.behind ? `↓${b.behind}` : ''}</span>}
                {b.committed && <span class="text-[10px] text-ink-muted flex-shrink-0">{b.committed}</span>}
              </button>
            ))
          )}
        </div>
      )}
    </Modal>
  );
}

// =============================================================================
// SnapshotManager — /snapshot overlay. create / rollback / list / delete.
// =============================================================================

export function SnapshotManager({ onClose }: { onClose: () => void }) {
  const [action, setAction] = useState<'list' | 'create' | 'rollback' | 'delete'>('list');
  const [branch, setBranch] = useState('');
  const [result, setResult] = useState<api.SnapshotResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const run = async () => {
    setBusy(true);
    setError('');
    setResult(null);
    try {
      const r = await api.runSnapshot(action, branch.trim() || undefined);
      setResult(r);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      title="Git Snapshots"
      subtitle="create · rollback · list · delete"
      onClose={onClose}
      size="md"
      footer={
        <>
          <button
            type="button"
            disabled={busy}
            onClick={run}
            class="px-3 py-1.5 rounded-sm text-xs bg-seal text-on-seal hover:opacity-90 disabled:opacity-50 transition-opacity"
          >
            Run
          </button>
        </>
      }
    >
      <div class="px-4 py-3 space-y-3">
        <div class="flex gap-1">
          {(['list', 'create', 'rollback', 'delete'] as const).map(a => (
            <button
              key={a}
              type="button"
              onClick={() => setAction(a)}
              class={`px-2.5 py-1 rounded-sm text-xs transition-colors ${
                action === a ? 'bg-seal text-on-seal' : 'text-ink-muted hover:bg-seal-light hover:text-seal'
              }`}
            >
              {a}
            </button>
          ))}
        </div>

        {(action === 'create' || action === 'rollback' || action === 'delete') && (
          <div>
            <input
              type="text"
              value={branch}
              onInput={e => setBranch((e.target as HTMLInputElement).value)}
              placeholder={action === 'create'
                ? 'branch to snapshot (blank = current branch)'
                : action === 'rollback'
                  ? 'branch to roll back to'
                  : 'branch to delete'}
              class="w-full px-2 py-1.5 text-sm bg-paper border border-hairline rounded-sm outline-none focus:border-seal font-mono"
            />
            {action === 'create' && (
              <p class="text-xs text-ink-muted mt-1">
                Leave blank to snapshot the current branch. The snapshot tool stashes uncommitted work to a snapshot ref.
              </p>
            )}
          </div>
        )}

        {error && <ErrorRow msg={error} />}
        {result && (
          <div class="bg-paper-sunk border border-hairline rounded-sm p-3 text-xs font-mono whitespace-pre-wrap break-all">
            {result.summary || result.content || result.output || 'OK'}
          </div>
        )}
      </div>
    </Modal>
  );
}

// =============================================================================
// PermissionInfo — /permission overlay. APPA trajectory + mode summary.
// =============================================================================

export function PermissionInfo({ sessionId, onClose }: { sessionId?: string; onClose: () => void }) {
  const [info, setInfo] = useState<api.PermissionInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    (async () => {
      setLoading(true);
      try {
        const r = await api.fetchPermissionInfo(sessionId);
        if (!cancelled) setInfo(r);
      } catch (e) {
        if (!cancelled) setError(String(e));
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, [sessionId]);

  return (
    <Modal
      title="APPA Permission State"
      subtitle={info ? `mode: ${info.label} · ${info.summary}` : 'loading…'}
      onClose={onClose}
      size="md"
    >
      {loading ? <LoadingRow /> : error ? <ErrorRow msg={error} /> : info && (
        <div class="px-4 py-3 space-y-3 text-sm">
          <Row label="Mode" value={info.label} />
          <Row label="Summary" value={info.summary} />
          {info.trajectory && <Row label="Trajectory" value={info.trajectory} mono />}
          {typeof info.effects === 'number' && <Row label="Effects" value={String(info.effects)} />}
          {typeof info.rulings === 'number' && <Row label="Rulings" value={String(info.rulings)} />}
          {typeof info.acceptedNarrowing === 'number' && <Row label="Accepted narrowing" value={String(info.acceptedNarrowing)} />}
        </div>
      )}
    </Modal>
  );
}

// =============================================================================
// ContextBreakdown — /context overlay.
// =============================================================================

export function ContextBreakdown({ sessionId, onClose }: { sessionId?: string; onClose: () => void }) {
  const [data, setData] = useState<api.ContextBreakdown | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    (async () => {
      if (!sessionId) {
        setLoading(false);
        return;
      }
      setLoading(true);
      try {
        const r = await api.fetchContextBreakdown(sessionId);
        if (!cancelled) setData(r);
      } catch (e) {
        if (!cancelled) setError(String(e));
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, [sessionId]);

  return (
    <Modal
      title="Context Window"
      subtitle={data ? `${formatK(data.total)} / ${formatK(data.contextWindow)} tokens (${data.usedPercent}% used)` : 'loading…'}
      onClose={onClose}
      size="md"
    >
      {!sessionId ? (
        <div class="px-4 py-6 text-sm text-ink-muted text-center">Start a session first.</div>
      ) : loading ? <LoadingRow /> : error ? <ErrorRow msg={error} /> : data && (
        <div class="px-4 py-3 space-y-2">
          {data.categories.map((c, i) => (
            <div key={i} class="flex items-center gap-2">
              <span class="flex-1 text-sm text-ink-light">{c.name}</span>
              <span class="font-mono text-xs text-ink-muted">{formatK(c.tokens)}</span>
              {c.note && <span class="text-[10px] text-ink-muted">{c.note}</span>}
            </div>
          ))}
          {data.contextWindow > 0 && (
            <div class="mt-3 h-2 bg-paper-sunk rounded-sm overflow-hidden">
              {/* Fill grows from the left edge on mount (scaleX 0 → 1). */}
              <div class="h-full bg-seal origin-left animate-grow-x" style={{ width: `${Math.min(100, data.usedPercent)}%` }} />
            </div>
          )}
        </div>
      )}
    </Modal>
  );
}

// =============================================================================
// SkillsPanel — /skills overlay.
// =============================================================================

export function SkillsPanel({ onClose }: { onClose: () => void }) {
  const [data, setData] = useState<{ skills: api.Skill[]; dir: string } | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    (async () => {
      setLoading(true);
      try {
        const r = await api.fetchSkills();
        if (!cancelled) setData(r);
      } catch (e) {
        if (!cancelled) setError(String(e));
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, []);

  return (
    <Modal
      title="Skills"
      subtitle={data ? `${data.skills.length} discovered · ${data.dir}` : 'loading…'}
      onClose={onClose}
      size="lg"
    >
      {loading ? <LoadingRow /> : error ? <ErrorRow msg={error} /> : data && (
        <div class="py-1">
          {data.skills.length === 0 ? (
            <div class="px-4 py-6 text-sm text-ink-muted text-center">
              No skills found in <code class="font-mono">{data.dir}</code>.
            </div>
          ) : (
            data.skills.map(s => (
              <div key={s.path} class="px-4 py-2 border-b border-hairline last:border-b-0">
                <div class="flex items-center gap-2">
                  <Sparkles size={12} class="text-seal flex-shrink-0" />
                  <span class="font-medium text-sm text-ink-light">{s.name}</span>
                </div>
                {s.description && (
                  <p class="text-xs text-ink-muted mt-0.5 pl-5">{s.description}</p>
                )}
                <p class="text-[10px] text-ink-muted mt-1 pl-5 font-mono truncate">{s.path}</p>
              </div>
            ))
          )}
        </div>
      )}
    </Modal>
  );
}

// =============================================================================
// ExtensionsPanel — /extensions overlay.
// =============================================================================

export function ExtensionsPanel({ onClose }: { onClose: () => void }) {
  const [data, setData] = useState<{ extensions: api.Extension[]; warnings: { path?: string; message: string }[] } | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    (async () => {
      setLoading(true);
      try {
        const r = await api.fetchExtensions();
        if (!cancelled) setData(r);
      } catch (e) {
        if (!cancelled) setError(String(e));
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, []);

  return (
    <Modal
      title="Extensions"
      subtitle={data ? `${data.extensions.length} discovered` : 'loading…'}
      onClose={onClose}
      size="lg"
    >
      {loading ? <LoadingRow /> : error ? <ErrorRow msg={error} /> : data && (
        <div class="py-1">
          {data.extensions.length === 0 ? (
            <div class="px-4 py-6 text-sm text-ink-muted text-center">No extensions discovered.</div>
          ) : (
            data.extensions.map(e => (
              <div key={e.path} class="px-4 py-2 border-b border-hairline last:border-b-0">
                <div class="flex items-center gap-2">
                  <Boxes size={12} class="text-seal flex-shrink-0" />
                  <span class="font-medium text-sm text-ink-light">{e.name || e.id}</span>
                  {e.version && <span class="text-[10px] text-ink-muted px-1.5 py-0.5 bg-paper-sunk rounded-sm">v{e.version}</span>}
                  {e.source && <span class="text-[10px] text-ink-muted">{e.source}</span>}
                </div>
                {e.description && <p class="text-xs text-ink-muted mt-0.5 pl-5">{e.description}</p>}
                <p class="text-[10px] text-ink-muted mt-1 pl-5 font-mono truncate">{e.path}</p>
              </div>
            ))
          )}
          {data.warnings.length > 0 && (
            <div class="px-4 py-2 border-t border-hairline bg-cinnabar/5">
              <div class="text-xs font-medium text-cinnabar mb-1">Warnings</div>
              {data.warnings.map((w, i) => (
                <div key={i} class="text-xs text-cinnabar/80 font-mono">{w.path ? `${w.path}: ` : ''}{w.message}</div>
              ))}
            </div>
          )}
        </div>
      )}
    </Modal>
  );
}

// =============================================================================
// Settings — opens settings-* palette entries as a single panel.
// =============================================================================

interface SettingsProps {
  onClose: () => void;
}

export function SettingsPanel({ onClose }: SettingsProps) {
  const [catalog, setCatalog] = useState<api.SettingsCatalog | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      setCatalog(await api.fetchSettings());
    } catch (e) {
      setError(String(e));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { load(); }, []);

  const setModel = async (name: string) => {
    setBusy(true);
    try {
      await api.setModel(name);
      await load();
    } catch (e) { setError(String(e)); } finally { setBusy(false); }
  };
  const setThink = async (level: string) => {
    setBusy(true);
    try {
      await api.setThink(level);
      await load();
    } catch (e) { setError(String(e)); } finally { setBusy(false); }
  };
  const setAgents = async (enabled: boolean) => {
    setBusy(true);
    try {
      await api.setAgentsEnabled(enabled);
      await load();
    } catch (e) { setError(String(e)); } finally { setBusy(false); }
  };
  const setMemory = async (enabled: boolean) => {
    setBusy(true);
    try {
      await api.setMemoryEnabled(enabled);
      await load();
    } catch (e) { setError(String(e)); } finally { setBusy(false); }
  };
  // Mode buttons used to fire-and-forget api.setMode without reloading,
  // so the panel kept showing the previous mode as active until it was
  // closed and reopened — every other section already reloaded.
  const setMode = async (mode: string) => {
    setBusy(true);
    try {
      await api.setMode(mode);
      await load();
    } catch (e) { setError(String(e)); } finally { setBusy(false); }
  };

  return (
    <Modal
      title="Settings"
      subtitle="Model · think · agents · memory · theme"
      onClose={onClose}
      size="lg"
    >
      {loading ? <LoadingRow /> : error ? <ErrorRow msg={error} /> : catalog && (
        <div class="px-4 py-3 space-y-4">
          <SettingsSection label="Model" icon={<Brain size={12} />}>
            <div class="flex flex-wrap gap-1">
              {catalog.models.map(name => {
                const active = catalog.state.model === name;
                return (
                  <button
                    key={name}
                    type="button"
                    disabled={busy}
                    onClick={() => setModel(name)}
                    class={`px-2.5 py-1 rounded-sm text-xs font-mono transition-colors ${
                      active ? 'bg-seal text-on-seal' : 'bg-paper-sunk text-ink-light hover:bg-seal-light hover:text-seal'
                    } disabled:opacity-50`}
                  >
                    {name}
                  </button>
                );
              })}
              <button
                type="button"
                disabled={busy}
                onClick={() => setModel('')}
                class={`px-2.5 py-1 rounded-sm text-xs transition-colors ${
                  !catalog.state.model ? 'bg-seal text-on-seal' : 'bg-paper-sunk text-ink-muted hover:bg-seal-light hover:text-seal'
                } disabled:opacity-50`}
              >
                project default
              </button>
            </div>
          </SettingsSection>

          <SettingsSection label="Think level" icon={<Sparkles size={12} />}>
            <div class="flex flex-wrap gap-1">
              {catalog.thinkLevels.map(level => {
                const active = (catalog.state.think || 'default') === level;
                return (
                  <button
                    key={level}
                    type="button"
                    disabled={busy}
                    onClick={() => setThink(level)}
                    class={`px-2.5 py-1 rounded-sm text-xs transition-colors ${
                      active ? 'bg-seal text-on-seal' : 'bg-paper-sunk text-ink-light hover:bg-seal-light hover:text-seal'
                    } disabled:opacity-50`}
                  >
                    {level === 'default' ? 'default' : level}
                  </button>
                );
              })}
            </div>
          </SettingsSection>

          <SettingsSection label="Permission mode" icon={<Shield size={12} />}>
            <div class="flex flex-wrap gap-1">
              {catalog.modes.map(m => {
                return (
                  <button
                    key={m.mode}
                    type="button"
                    disabled={busy}
                    onClick={() => setMode(m.mode)}
                    title={m.summary}
                    class={`px-2.5 py-1 rounded-sm text-xs transition-colors ${
                      m.active ? 'bg-seal text-on-seal' : 'bg-paper-sunk text-ink-light hover:bg-seal-light hover:text-seal'
                    } disabled:opacity-50`}
                  >
                    {m.label}
                  </button>
                );
              })}
            </div>
            {catalog.modes.find(m => m.active) && (
              <p class="text-xs text-ink-muted mt-1">{catalog.modes.find(m => m.active)?.summary}</p>
            )}
          </SettingsSection>

          <SettingsSection label="Sub-agents" icon={<Boxes size={12} />}>
            <Toggle checked={catalog.state.agentsEnabled} disabled={busy} onChange={setAgents} />
            <p class="text-xs text-ink-muted mt-1">Registers agent_* tools so sub-agents can spawn.</p>
          </SettingsSection>

          <SettingsSection label="Agent memory" icon={<Sparkles size={12} />}>
            <Toggle checked={catalog.state.memoryEnabled} disabled={busy} onChange={setMemory} />
            <p class="text-xs text-ink-muted mt-1">Enables remember/recall tools. Persisted to ~/.a1/memory.</p>
          </SettingsSection>

          <SettingsSection label="Appearance" icon={<Palette size={12} />}>
            <ThemeSegmented />
          </SettingsSection>
        </div>
      )}
    </Modal>
  );
}

function SettingsSection({ label, icon, children }: { label: string; icon: any; children: any }) {
  return (
    <div>
      <div class="flex items-center gap-1.5 mb-1.5">
        {icon}
        <span class="text-xs font-semibold uppercase tracking-wider text-ink-muted">{label}</span>
      </div>
      {children}
    </div>
  );
}

function Toggle({ checked, disabled, onChange }: { checked: boolean; disabled?: boolean; onChange: (v: boolean) => void }) {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={() => onChange(!checked)}
      class={`relative w-10 h-5 rounded-full transition-colors ${checked ? 'bg-seal' : 'bg-paper-sunk border border-hairline'} ${disabled ? 'opacity-50' : ''}`}
    >
      {/* Knob color is per-state so it reads on both tracks in both themes:
          on seal (ON) it follows the on-seal convention — paper-card works
          because light-mode seal is mid-tone and dark-mode seal is light —
          and on the sunk OFF track the dark theme needs a light knob
          (ink-light) or the switch reads as an empty pill. */}
      <span class={`absolute top-0.5 left-0.5 w-4 h-4 rounded-full shadow-sm transition-transform ${checked ? 'bg-paper-card' : 'bg-paper-card dark:bg-ink-light'} ${checked ? 'translate-x-5' : ''}`} />
    </button>
  );
}

// =============================================================================
// Shared helpers
// =============================================================================

function LoadingRow() {
  return (
    <div class="px-4 py-8 flex items-center justify-center gap-2 text-sm text-ink-muted">
      <Loader size={14} class="animate-spin" />
      <span>Loading…</span>
    </div>
  );
}

function ErrorRow({ msg }: { msg: string }) {
  return (
    <div class="px-4 py-3 text-sm text-cinnabar bg-cinnabar/5 border-b border-cinnabar/20 break-all">
      {msg}
    </div>
  );
}

function Row({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div class="flex items-baseline gap-2">
      <span class="text-xs text-ink-muted uppercase tracking-wider w-28 flex-shrink-0">{label}</span>
      <span class={`text-sm text-ink-light flex-1 break-all ${mono ? 'font-mono text-xs' : ''}`}>{value}</span>
    </div>
  );
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

function formatK(n: number): string {
  if (n >= 1000) return `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}k`;
  return String(n);
}
