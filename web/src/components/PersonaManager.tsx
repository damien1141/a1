import { useEffect, useRef, useState } from 'preact/hooks';
import { Pencil, Plus, Trash2, UserRoundCheck, Brain, X, ImagePlus, ShieldAlert } from "lucide-preact";
import * as api from '../api';
import { Modal } from './Modal';
import { Avatar, personaAvatar } from './Avatar';

// PersonaManager is the personas overlay: personality profiles with their
// own memory banks. Left pane lists profiles (rounded-square avatars, memory
// counts); right pane shows the selected one — prompt, memories, and the
// switch/edit/delete actions. Creating and editing share one form.

const EMOJI_CHOICES = ['🧭', '📐', '✒️', '🤝', '🦉', '🦊', '🐉', '⚙️', '🧪', '🎸', '🌙', '⚡'];
const HUE_CHOICES = [200, 8, 258, 145, 40, 320, 175, 90];

interface Props {
  onClose: () => void;
  // onSwitched fires after a successful activation so App can toast it.
  onSwitched: (p: api.Persona) => void;
}

interface FormState {
  name: string;
  tagline: string;
  emoji: string;
  hue: number;
  prompt: string;
  overrideSystemPrompt: boolean;
  // Per-persona runtime overrides. Empty strings mean "inherit the global
  // config"; memoryEnabled is the explicit per-persona toggle.
  model: string;
  permissionMode: string;
  memoryEnabled: boolean;
  // memoryModel is the second model that runs the small background jobs —
  // memory consolidation and smart session names. Empty = use the main
  // model. Pointing it at a cheap local 8B model frees the main LLM.
  memoryModel: string;
}

const blankForm: FormState = { name: '', tagline: '', emoji: '🧭', hue: 200, prompt: '', overrideSystemPrompt: false, model: '', permissionMode: '', memoryEnabled: false, memoryModel: '' };

// Permission modes offered per persona — same set the web modes API serves,
// minus headless-strict (that one is for unattended runs, not personas).
const PERMISSION_MODES: Array<{ value: string; label: string }> = [
  { value: 'interactive', label: 'Interactive — prompts before anything risky' },
  { value: 'readonly', label: 'Read-only — no writes or bash' },
  { value: 'autopilot', label: 'Autopilot — allowlisted tools run free' },
  { value: 'yolo', label: 'YOLO — no prompts at all (dangerous)' },
];

export function PersonaManager({ onClose, onSwitched }: Props) {
  const [list, setList] = useState<api.Persona[]>([]);
  const [active, setActive] = useState('default');
  const [memoryEnabled, setMemoryEnabled] = useState(false);
  const [selected, setSelected] = useState('default');
  const [memories, setMemories] = useState<api.MemorySummary[] | null>(null);
  const [editing, setEditing] = useState(false);
  const [isNew, setIsNew] = useState(false);
  const [form, setForm] = useState<FormState>(blankForm);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // Uploaded profile pictures (this backend's extension over the original
  // emoji-only avatars). The file input is triggered from the form's
  // picture section; picking a file uploads immediately for the selected
  // persona. avatarVersion bumps so the <img> cache-busts after a re-upload.
  const [avatarVersion, setAvatarVersion] = useState(0);
  const fileInputRef = useRef<HTMLInputElement>(null);
  // Model catalog for the settings' model dropdown. Fetched once; an
  // empty list (no /api/models or config without models) degrades the
  // dropdown to just the Default entry.
  const [models, setModels] = useState<api.ModelInfo[]>([]);

  const load = async (keepSelected?: string) => {
    try {
      const r = await api.fetchPersonas();
      setList(Array.isArray(r.personas) ? r.personas : []);
      setActive(r.active ?? 'default');
      setMemoryEnabled(!!r.memoryEnabled);
      const sel = keepSelected && r.personas?.some(p => p.id === keepSelected) ? keepSelected : (r.active ?? 'default');
      setSelected(sel);
    } catch (e) {
      setError(String(e));
    }
  };

  useEffect(() => { load(); }, []);

  useEffect(() => {
    api.fetchModels().then(setModels).catch(() => setModels([]));
  }, []);

  // Load the selected persona's memories (its memory bank preview).
  useEffect(() => {
    if (selected === '' || editing) return;
    setMemories(null);
    api.fetchPersonaMemories(selected)
      .then(r => setMemories(r.memories ?? []))
      .catch(() => setMemories([]));
  }, [selected, editing, list.length]);

  const sel = list.find(p => p.id === selected) ?? null;

  const startEdit = (p: api.Persona) => {
    setIsNew(false);
    setEditing(true);
    setError('');
    setForm({ name: p.name, tagline: p.tagline, emoji: p.emoji, hue: p.hue, prompt: p.prompt, overrideSystemPrompt: !!p.overrideSystemPrompt, model: p.model ?? '', permissionMode: p.permissionMode ?? '', memoryEnabled: !!p.memoryEnabled, memoryModel: p.memoryModel ?? '' });
  };

  const startNew = () => {
    setIsNew(true);
    setEditing(true);
    setError('');
    setForm(blankForm);
  };

  const save = async () => {
    if (!form.name.trim()) {
      setError('Name is required.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      const input = {
        name: form.name.trim(),
        tagline: form.tagline.trim(),
        emoji: form.emoji.trim() || '🧭',
        hue: form.hue,
        prompt: form.prompt.trim(),
        // Always sent explicitly: the backend treats a missing field as
        // "keep stored", which would make the toggle impossible to turn off.
        overrideSystemPrompt: form.overrideSystemPrompt,
        // Settings ride the same update: "" clears an override so the
        // persona falls back to the global config value.
        model: form.model,
        permissionMode: form.permissionMode,
        memoryEnabled: form.memoryEnabled,
        memoryModel: form.memoryModel,
      };
      let savedId = selected;
      if (isNew) {
        const r = await api.createPersona(input);
        savedId = r.persona.id;
      } else if (sel) {
        await api.updatePersona(sel.id, input);
        savedId = sel.id;
      }
      setEditing(false);
      await load(savedId);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  const switchTo = async (p: api.Persona) => {
    setBusy(true);
    setError('');
    try {
      await api.setPersonaActive(p.id);
      setActive(p.id);
      await load(p.id);
      onSwitched(p);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (p: api.Persona) => {
    setBusy(true);
    setError('');
    try {
      await api.deletePersona(p.id);
      await load(active === p.id ? 'default' : active);
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  const forget = async (mid: number) => {
    try {
      await api.forgetPersonaMemory(selected, mid);
      setMemories(m => (m ?? []).filter(x => x.id !== mid));
      setList(l => l.map(p => (p.id === selected ? { ...p, memoryCount: Math.max(0, p.memoryCount - 1) } : p)));
    } catch (e) {
      setError(String(e));
    }
  };

  // uploadPicture attaches an image to the selected persona. The image
  // overrides the emoji-on-gradient avatar everywhere; the emoji + hue
  // pickers remain as the fallback when no picture is set.
  const uploadPicture = (id: string) => {
    if (fileInputRef.current) {
      fileInputRef.current.dataset.targetId = id;
      fileInputRef.current.click();
    }
  };

  const onFilePicked = async (e: Event) => {
    const input = e.target as HTMLInputElement;
    const id = input.dataset.targetId;
    if (!id || !input.files || input.files.length === 0) return;
    setError('');
    try {
      await api.uploadPersonaAvatar(id, input.files[0]);
      setAvatarVersion(v => v + 1);
      await load(id);
    } catch (e) {
      setError(String(e));
    } finally {
      input.value = '';
    }
  };

  // ---- form fragment (create + edit share it) ----

  const formBody = (
    <div class="space-y-3">
      <label class="block">
        <span class="text-xs text-ink-muted">Name</span>
        <input
          type="text"
          value={form.name}
          onInput={e => setForm({ ...form, name: (e.target as HTMLInputElement).value })}
          placeholder="The Closer"
          class="mt-0.5 w-full px-2 py-1.5 text-sm bg-paper border border-hairline rounded-sm outline-none focus:border-seal"
          maxLength={40}
        />
      </label>
      <label class="block">
        <span class="text-xs text-ink-muted">Tagline — one line shown under the name</span>
        <input
          type="text"
          value={form.tagline}
          onInput={e => setForm({ ...form, tagline: (e.target as HTMLInputElement).value })}
          placeholder="Sells the value — frames every answer as the obvious yes."
          class="mt-0.5 w-full px-2 py-1.5 text-sm bg-paper border border-hairline rounded-sm outline-none focus:border-seal"
          maxLength={100}
        />
      </label>
      <div>
        <span class="text-xs text-ink-muted">Picture — emoji on a rounded square, or an uploaded image</span>
        <div class="mt-1 flex items-center gap-1.5 flex-wrap">
          {EMOJI_CHOICES.map(e => (
            <button
              key={e}
              type="button"
              onClick={() => setForm({ ...form, emoji: e })}
              class={`w-8 h-8 rounded-lg flex items-center justify-center text-base transition-all duration-fast ${
                form.emoji === e ? 'ring-2 ring-seal scale-105 bg-paper-card' : 'bg-paper-sunk hover:bg-hairline/40'
              }`}
            >
              {e}
            </button>
          ))}
          <input
            type="text"
            value={form.emoji}
            onInput={e => setForm({ ...form, emoji: (e.target as HTMLInputElement).value })}
            class="w-14 px-2 py-1.5 text-sm text-center bg-paper border border-hairline rounded-sm outline-none focus:border-seal"
            maxLength={8}
            title="Custom emoji"
          />
          <button
            type="button"
            onClick={() => uploadPicture(selected)}
            class="w-8 h-8 rounded-lg flex items-center justify-center bg-paper-sunk hover:bg-hairline/40 text-ink-muted hover:text-seal transition-all duration-fast"
            title="Upload a custom picture (overrides the emoji)"
          >
            <ImagePlus size={14} />
          </button>
        </div>
      </div>
      <div>
        <span class="text-xs text-ink-muted">Color</span>
        <div class="mt-1 flex items-center gap-1.5 flex-wrap">
          {HUE_CHOICES.map(h => (
            <button
              key={h}
              type="button"
              onClick={() => setForm({ ...form, hue: h })}
              class={`w-7 h-7 rounded-lg transition-all duration-fast ${
                form.hue === h ? 'ring-2 ring-seal scale-105' : 'hover:scale-105'
              }`}
              style={{ background: `linear-gradient(135deg, hsl(${h} 44% 62%), hsl(${(h + 38) % 360} 40% 44%))` }}
            />
          ))}
        </div>
      </div>
      <label class="block">
        <span class="text-xs text-ink-muted">Personality — injected into the system prompt</span>
        <textarea
          value={form.prompt}
          onInput={e => setForm({ ...form, prompt: (e.target as HTMLInputElement).value })}
          placeholder="You are The Closer. Lead with the outcome and the number that proves it…"
          rows={7}
          class="mt-0.5 w-full px-2 py-1.5 text-xs bg-paper border border-hairline rounded-sm outline-none focus:border-seal leading-relaxed resize-y"
          maxLength={4000}
        />
      </label>
      {/* Replace-default-system-prompt toggle: the persona's prompt stops
          riding alongside a1's built-in instructions and becomes THE system
          prompt, so the persona fully owns the agent's voice and behavior. */}
      <div class={`flex items-start gap-2.5 px-2.5 py-2 rounded-sm border transition-colors ${form.overrideSystemPrompt ? 'bg-seal-light border-seal/40' : 'bg-paper-sunk border-hairline'}`}>
        <button
          type="button"
          role="switch"
          aria-checked={form.overrideSystemPrompt}
          disabled={!form.prompt.trim()}
          onClick={() => setForm({ ...form, overrideSystemPrompt: !form.overrideSystemPrompt })}
          class={`relative flex-shrink-0 mt-0.5 rounded-full transition-colors duration-fast disabled:opacity-40 disabled:cursor-not-allowed ${form.overrideSystemPrompt ? 'bg-seal' : 'bg-hairline'}`}
          style={{ width: '2rem', height: '1.125rem' }}
          title={form.prompt.trim() ? undefined : 'Write a prompt first — an empty prompt cannot replace the system prompt'}
        >
          <span
            class={`absolute top-0.5 left-0.5 rounded-full shadow-sm transition-transform duration-fast ${form.overrideSystemPrompt ? 'translate-x-3.5 bg-paper' : 'bg-paper dark:bg-ink-light'}`}
            style={{ width: '0.875rem', height: '0.875rem' }}
          />
        </button>
        <label class="min-w-0 flex-1 cursor-pointer" onClick={e => { if (form.prompt.trim()) { e.preventDefault(); setForm({ ...form, overrideSystemPrompt: !form.overrideSystemPrompt }); } }}>
          <span class="block text-xs font-medium text-ink">Replace default system prompt</span>
          <span class="block text-[11px] text-ink-muted leading-snug mt-0.5">
            {form.prompt.trim()
              ? 'On: this persona\u2019s prompt replaces a1\u2019s built-in instructions entirely — the persona truly takes over.'
              : 'Write a prompt above to enable this — an empty prompt keeps the default.'}
          </span>
        </label>
      </div>
      {form.overrideSystemPrompt && (
        <div class="flex items-start gap-2 px-2.5 py-2 rounded-sm bg-cinnabar/10 border border-cinnabar/30 text-[11px] text-cinnabar leading-snug">
          <ShieldAlert size={13} class="flex-shrink-0 mt-px" />
          <span>a1’s built-in discipline (tool routing, coding standards, safety rails) is gone while this is on. Write those rules into the prompt if you need them.</span>
        </div>
      )}
      {/* Runtime settings: model / permission mode / memory. These are the
          per-persona overrides applied on switch — switching to this persona
          swaps the model (and its context window), rebuilds the permission
          gate, and scopes its memory bank. Empty = inherit global config. */}
      <div class="space-y-2 px-2.5 py-2 rounded-sm border border-hairline bg-paper-sunk">
        <span class="text-xs font-medium text-ink">Runtime settings — applied on switch</span>
        <label class="block">
          <span class="text-xs text-ink-muted">Model — overrides the default config model</span>
          <select
            value={form.model}
            onChange={e => setForm({ ...form, model: (e.target as HTMLSelectElement).value })}
            class="mt-0.5 w-full px-2 py-1.5 text-sm bg-paper border border-hairline rounded-sm outline-none focus:border-seal"
          >
            <option value="">Default (global config)</option>
            {models.map(m => (
              <option key={m.name} value={m.name}>
                {m.name}{m.contextWindow > 0 ? ` — ${(m.contextWindow / 1000).toFixed(0)}K ctx` : ''}{m.default ? ' (default)' : ''}
              </option>
            ))}
          </select>
        </label>
        <label class="block">
          <span class="text-xs text-ink-muted">Permission mode — overrides the live mode on switch</span>
          <select
            value={form.permissionMode}
            onChange={e => setForm({ ...form, permissionMode: (e.target as HTMLSelectElement).value })}
            class="mt-0.5 w-full px-2 py-1.5 text-sm bg-paper border border-hairline rounded-sm outline-none focus:border-seal"
          >
            <option value="">Default (keep current mode)</option>
            {PERMISSION_MODES.map(m => (
              <option key={m.value} value={m.value}>{m.label}</option>
            ))}
          </select>
        </label>
        <label class="block">
          <span class="text-xs text-ink-muted">Memory ops model — background jobs: memory consolidation, session naming</span>
          <select
            value={form.memoryModel}
            onChange={e => setForm({ ...form, memoryModel: (e.target as HTMLSelectElement).value })}
            class="mt-0.5 w-full px-2 py-1.5 text-sm bg-paper border border-hairline rounded-sm outline-none focus:border-seal"
          >
            <option value="">Same as model (no second model)</option>
            {models.map(m => (
              <option key={m.name} value={m.name}>
                {m.name}{m.contextWindow > 0 ? ` — ${(m.contextWindow / 1000).toFixed(0)}K ctx` : ''}
              </option>
            ))}
          </select>
          <span class="block text-[11px] text-ink-muted mt-1 leading-snug">
            A cheap local model here does the small write-backs (remembering, titling sessions) without spending the main model's context.
          </span>
        </label>
        <div class={`flex items-start gap-2.5 px-2 py-1.5 rounded-sm border transition-colors ${form.memoryEnabled ? 'bg-seal-light border-seal/40' : 'bg-paper border-hairline'}`}>
          <button
            type="button"
            role="switch"
            aria-checked={form.memoryEnabled}
            onClick={() => setForm({ ...form, memoryEnabled: !form.memoryEnabled })}
            class={`relative flex-shrink-0 mt-0.5 rounded-full transition-colors duration-fast ${form.memoryEnabled ? 'bg-seal' : 'bg-hairline'}`}
            style={{ width: '2rem', height: '1.125rem' }}
          >
            <span
              class={`absolute top-0.5 left-0.5 rounded-full shadow-sm transition-transform duration-fast ${form.memoryEnabled ? 'translate-x-3.5 bg-paper' : 'bg-paper dark:bg-ink-light'}`}
              style={{ width: '0.875rem', height: '0.875rem' }}
            />
          </button>
          <label class="min-w-0 flex-1 cursor-pointer" onClick={() => setForm({ ...form, memoryEnabled: !form.memoryEnabled })}>
            <span class="block text-xs font-medium text-ink">Memory</span>
            <span class="block text-[11px] text-ink-muted leading-snug mt-0.5">
              {form.memoryEnabled
                ? 'On: this persona writes to and recalls its own semantic memory bank automatically.'
                : 'Off: this persona runs without semantic memory — no auto-injection, no auto-writes.'}
            </span>
          </label>
        </div>
      </div>
      <div class="flex items-center gap-2 pt-1">
        <Avatar emoji={form.emoji || '🧭'} hue={form.hue} size={30} />
        <span class="text-xs text-ink-muted">preview — every persona gets its own memory bank</span>
      </div>
    </div>
  );

  // ---- detail fragment ----

  const detail = sel && !editing && (
    <div class="space-y-4">
      <div class="flex items-start gap-3">
        <div class="pt-0.5">{personaAvatar({ ...sel, avatarVersion }, 44)}</div>
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2 flex-wrap">
            <h3 class="text-sm font-semibold text-ink">{sel.name}</h3>
            {active === sel.id && (
              <span class="text-[10px] px-1.5 py-0.5 rounded-sm bg-seal-light text-seal font-medium">active</span>
            )}
            {sel.builtin && (
              <span class="text-[10px] px-1.5 py-0.5 rounded-sm bg-paper-sunk text-ink-muted">built-in</span>
            )}
          </div>
          {sel.tagline && <p class="text-xs text-ink-muted mt-0.5">{sel.tagline}</p>}
          <p class="text-[11px] text-ink-muted mt-1 flex items-center gap-1.5 flex-wrap">
            <Brain size={11} class="inline" />
            <span>{sel.memoryCount} {sel.memoryCount === 1 ? 'memory' : 'memories'} in this bank</span>
            {sel.overrideSystemPrompt && (
              <span class="ml-1 px-1.5 py-0.5 rounded-sm bg-seal-light text-seal font-medium" title="This persona's prompt replaces a1's default system prompt">system override</span>
            )}
            {sel.model && (
              <span class="px-1.5 py-0.5 rounded-sm bg-seal-light text-seal font-medium" title="Model override applied on switch">{sel.model}</span>
            )}
            {sel.memoryModel && (
              <span class="px-1.5 py-0.5 rounded-sm bg-moss/15 text-moss font-medium" title="Second model for memory ops + session naming">mem-ops: {sel.memoryModel}</span>
            )}
            {sel.permissionMode && (
              <span class="px-1.5 py-0.5 rounded-sm bg-cinnabar/10 text-cinnabar font-medium" title="Permission mode applied on switch">{sel.permissionMode}</span>
            )}
            {sel.memoryEnabled && (
              <span class="px-1.5 py-0.5 rounded-sm bg-paper-sunk text-ink-muted" title="Persona-scoped semantic memory is on">memory on</span>
            )}
          </p>
        </div>
      </div>

      {sel.prompt && (
        <div class="bg-paper-sunk border border-hairline rounded-sm p-3 text-xs text-ink-light whitespace-pre-wrap leading-relaxed max-h-40 overflow-y-auto">
          {sel.prompt}
        </div>
      )}

      {active !== sel.id && (
        <button
          type="button"
          disabled={busy}
          onClick={() => switchTo(sel)}
          class="w-full flex items-center justify-center gap-1.5 px-3 py-2 rounded-sm text-xs bg-seal text-on-seal hover:opacity-90 disabled:opacity-50 transition-opacity"
        >
          <UserRoundCheck size={13} />
          Speak as {sel.name}
        </button>
      )}

      {/* Memory bank */}
      <div>
        <div class="flex items-center justify-between mb-1.5">
          <span class="text-[10px] uppercase tracking-wider text-ink-muted">Memory bank</span>
          {!memoryEnabled && (
            <span class="text-[10px] text-cinnabar">agent memory is off — enable it in Settings</span>
          )}
        </div>
        {memories === null ? (
          <div class="text-xs text-ink-muted py-2">loading bank…</div>
        ) : memories.length === 0 ? (
          <div class="text-xs text-ink-muted py-2">
            Empty bank. With agent memory on, everything this persona <span class="font-medium">remember</span>s lands here — other personas never see it.
          </div>
        ) : (
          <ul class="space-y-1.5">
            {memories.map(m => (
              <li key={m.id} class="group flex items-start gap-2 bg-paper border border-hairline rounded-sm px-2.5 py-2">
                <span class="text-[10px] px-1 py-0.5 rounded-sm bg-seal-light text-seal flex-shrink-0 mt-0.5">{m.kind}</span>
                <span class="text-xs text-ink-light leading-relaxed min-w-0 flex-1 break-words">{m.content}</span>
                <button
                  type="button"
                  onClick={() => forget(m.id)}
                  class="opacity-0 group-hover:opacity-100 text-ink-muted hover:text-cinnabar transition-all duration-fast flex-shrink-0 mt-0.5"
                  title="Forget this memory"
                >
                  <Trash2 size={12} />
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );

  return (
    <Modal
      title="Personas"
      subtitle="personality profiles, each with its own memory bank"
      onClose={onClose}
      size="lg"
      footer={
        editing ? (
          <>
            <button
              type="button"
              onClick={() => { setEditing(false); setError(''); }}
              class="px-3 py-1.5 rounded-sm text-xs text-ink-muted hover:text-ink hover:bg-paper-sunk transition-colors"
            >
              Cancel
            </button>
            <button
              type="button"
              disabled={busy}
              onClick={save}
              class="px-3 py-1.5 rounded-sm text-xs bg-seal text-on-seal hover:opacity-90 disabled:opacity-50 transition-opacity"
            >
              {isNew ? 'Create persona' : 'Save changes'}
            </button>
          </>
        ) : (
          <>
            {!sel?.builtin && sel && (
              <button
                type="button"
                disabled={busy}
                onClick={() => remove(sel)}
                class="mr-auto px-3 py-1.5 rounded-sm text-xs text-cinnabar hover:bg-cinnabar/10 disabled:opacity-50 transition-colors flex items-center gap-1.5"
              >
                <Trash2 size={12} />
                Delete
              </button>
            )}
            {sel && (
              <button
                type="button"
                onClick={() => startEdit(sel)}
                class="px-3 py-1.5 rounded-sm text-xs text-ink-muted hover:text-ink hover:bg-paper-sunk transition-colors flex items-center gap-1.5"
              >
                <Pencil size={12} />
                Edit
              </button>
            )}
            <button
              type="button"
              onClick={startNew}
              class="px-3 py-1.5 rounded-sm text-xs bg-seal text-on-seal hover:opacity-90 transition-opacity flex items-center gap-1.5"
            >
              <Plus size={12} />
              New persona
            </button>
          </>
        )
      }
    >
      {error && (
        <div class="mx-4 mt-3 px-2.5 py-1.5 rounded-sm bg-cinnabar/10 border border-cinnabar/30 text-xs text-cinnabar flex items-center gap-2">
          <X size={12} class="flex-shrink-0" />
          <span class="flex-1">{error}</span>
          <button type="button" class="text-ink-muted hover:text-ink" onClick={() => setError('')}>dismiss</button>
        </div>
      )}
      <input
        ref={fileInputRef}
        type="file"
        accept="image/png,image/jpeg,image/webp,image/gif"
        class="hidden"
        onChange={onFilePicked}
      />
      <div class="flex min-h-[420px]">
        {/* Left: profile list */}
        <div class="w-56 flex-shrink-0 border-r border-hairline overflow-y-auto p-2 space-y-1">
          {list.map(p => (
            <button
              key={p.id}
              type="button"
              onClick={() => { setSelected(p.id); setEditing(false); }}
              class={`w-full flex items-center gap-2.5 px-2 py-2 rounded-sm text-left transition-colors ${
                selected === p.id ? 'bg-seal-light' : 'hover:bg-paper-sunk'
              }`}
            >
              {personaAvatar(p, 30)}
              <span class="min-w-0 flex-1">
                <span class={`block text-xs truncate ${selected === p.id ? 'text-seal font-medium' : 'text-ink-light'}`}>
                  {p.name}
                </span>
                <span class="block text-[10px] text-ink-muted truncate">{p.tagline || '—'}</span>
              </span>
              {active === p.id && (
                <span class="w-1.5 h-1.5 rounded-full bg-seal flex-shrink-0" title="active" />
              )}
            </button>
          ))}
          <button
            type="button"
            onClick={startNew}
            class="w-full flex items-center justify-center gap-1.5 px-2 py-2 rounded-sm text-xs text-ink-muted hover:text-seal hover:bg-seal-light transition-colors border border-dashed border-hairline"
          >
            <Plus size={12} />
            New persona
          </button>
        </div>

        {/* Right: detail or form */}
        <div class="flex-1 min-w-0 p-4 overflow-y-auto">
          {editing ? formBody : detail ?? (
            <div class="text-xs text-ink-muted py-8 text-center">Select a persona.</div>
          )}
        </div>
      </div>
    </Modal>
  );
}
