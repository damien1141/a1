import { useState, useEffect, useRef } from 'preact/hooks';
import type { StatePayload } from '../api';
import { ChevronDown, Cpu, GitBranch, Brain, Shield, Bot, Database } from "lucide-preact";

interface Props {
  state: StatePayload | null;
  // streaming: when true, the activity indicator pulses.
  streaming: boolean;
  statusText: string;
  // hasSession: when false, compact/appa are disabled (they require an
  // active session; without this, the buttons look clickable, fire the
  // request, and produce a "Start a session first." toast — wasteful UX).
  hasSession?: boolean;
  onSwitchMode: () => void;       // cycle to next permission mode
  onOpenSettings: () => void;      // open the settings modal
  onOpenBranch: () => void;        // open branch picker
  onOpenPermissionInfo: () => void; // open APPA info modal
  onCompact: () => void;           // trigger compaction
  onOpenContext: () => void;       // open context breakdown modal
}

// Footer mirrors the TUI's FooterChrome: a single bottom row that surfaces
// model, mode, branch, and token info, plus quick links to compaction,
// context breakdown, and APPA permission state.
//
// Click targets:
//   model label   → settings modal (model + think level)
//   mode pill      → cycle permission mode (one click = next mode)
//   branch label   → branch picker overlay
//   tokens         → context breakdown modal
//   shield icon    → APPA permission info modal
export function Footer({
  state,
  streaming,
  statusText,
  hasSession = true,
  onSwitchMode,
  onOpenSettings,
  onOpenBranch,
  onOpenPermissionInfo,
  onCompact,
  onOpenContext,
}: Props) {
  // Model label: show "model · think" when both are set, else just model.
  // Empty model means "project default" — render as "default" so the chip
  // is never blank.
  const modelLabel = state?.model || 'default';
  const thinkLabel = state?.think && state.think !== 'default' ? state.think : '';
  const modelDisplay = thinkLabel ? `${modelLabel} · ${thinkLabel}` : modelLabel;

  // Mode label: render the friendly name ("ask" / "read-only" / ...). The
  // server ships a string mode + label in state payloads; we render the
  // label when available, else the raw mode.
  const modeLabel = modeDisplay(state?.mode || '');

  return (
    <div class="flex-shrink-0 flex items-center justify-between gap-2 px-4 py-1 border-t border-hairline bg-paper-sunk text-xs">
      {/* Left cluster: model + think, mode, branch */}
      <div class="flex items-center gap-1 min-w-0 flex-1">
        <FooterChip
          icon={<Cpu size={12} />}
          label={modelDisplay}
          title="Model · click to change"
          onClick={onOpenSettings}
        />
        <FooterChip
          icon={<Shield size={12} />}
          label={modeLabel}
          title="Permission mode · click to cycle"
          onClick={onSwitchMode}
          tone={modeTone(state?.mode || '')}
        />
        <FooterChip
          icon={<GitBranch size={12} />}
          label={state?.branch || 'no repo'}
          title="Current branch · click to switch"
          onClick={onOpenBranch}
          disabled={!state?.branch}
        />
        {state?.agentsEnabled && (
          <FooterBadge icon={<Bot size={12} />} title="Sub-agents enabled" tone="moss" />
        )}
        {state?.memoryEnabled && (
          <FooterBadge icon={<Database size={12} />} title="Agent memory enabled" tone="moss" />
        )}
      </div>

      {/* Center: streaming status (transient). While streaming the dot
          emits a soft ping ring and the label shimmers; once settled the
          text stays static. */}
      <div class="flex items-center gap-2 min-w-0">
        {streaming ? (
          <span class="flex items-center gap-1.5 animate-fade-in">
            <span class="relative flex h-1.5 w-1.5 flex-shrink-0">
              <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-seal opacity-50" />
              <span class="relative inline-flex h-1.5 w-1.5 rounded-full bg-seal" />
            </span>
            <span class="working-text truncate max-w-[280px]">{statusText || 'working…'}</span>
          </span>
        ) : statusText ? (
          <span class="text-ink-muted truncate max-w-[280px]">{statusText}</span>
        ) : null}
      </div>

      {/* Right cluster: tokens, compact, context, permission */}
      <div class="flex items-center gap-1 flex-shrink-0">
        {state && state.contextWindow > 0 && (
          <FooterChip
            icon={<Brain size={12} />}
            label={tokensLabel(state)}
            title="Context window · click for breakdown"
            onClick={onOpenContext}
          />
        )}
        <FooterAction
          label="compact"
          title={hasSession ? "Compact context now" : "Start a session to compact"}
          onClick={onCompact}
          disabled={!hasSession}
        />
        <FooterAction
          icon={<Shield size={12} />}
          label="appa"
          title={hasSession ? "APPA trajectory state" : "Start a session to inspect APPA"}
          onClick={onOpenPermissionInfo}
          disabled={!hasSession}
        />
      </div>
    </div>
  );
}

function FooterChip({
  icon,
  label,
  title,
  onClick,
  disabled,
  tone,
}: {
  icon: any;
  label: string;
  title: string;
  onClick?: () => void;
  disabled?: boolean;
  tone?: 'moss' | 'cinnabar' | 'seal';
}) {
  const toneClass =
    tone === 'moss' ? 'text-moss' :
    tone === 'cinnabar' ? 'text-cinnabar' :
    tone === 'seal' ? 'text-seal' :
    'text-ink-muted';
  return (
    <button
      type="button"
      disabled={disabled}
      title={title}
      onClick={onClick}
      class={`flex items-center gap-1 px-1.5 py-0.5 rounded-sm transition-all duration-fast active:scale-95 ${disabled ? 'opacity-50 cursor-not-allowed' : 'hover:bg-seal-light hover:text-seal'} ${toneClass}`}
    >
      {icon}
      <span class="truncate max-w-[160px]">{label}</span>
    </button>
  );
}

function FooterBadge({ icon, title, tone }: { icon: any; title: string; tone: 'moss' }) {
  const toneClass = tone === 'moss' ? 'text-moss' : 'text-ink-muted';
  return (
    <span class={`flex items-center px-1 py-0.5 rounded-sm ${toneClass}`} title={title}>
      {icon}
    </span>
  );
}

function FooterAction({
  icon,
  label,
  title,
  onClick,
  disabled,
}: {
  icon?: any;
  label: string;
  title: string;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      title={title}
      onClick={onClick}
      disabled={disabled}
      class={`flex items-center gap-1 px-1.5 py-0.5 rounded-sm transition-all duration-fast ${
        disabled
          ? 'text-ink-muted/40 opacity-50 cursor-not-allowed'
          : 'text-ink-muted hover:bg-seal-light hover:text-seal active:scale-95'
      }`}
    >
      {icon}
      <span>{label}</span>
    </button>
  );
}

// modeDisplay maps a permission.Mode wire value to the label the footer
// shows. The Go side already sends a "label" in modeView() responses, but
// the SSE state event only carries the raw mode string, so the client
// needs the same mapping locally.
function modeDisplay(mode: string): string {
  switch (mode) {
    case 'interactive': return 'ask';
    case 'readonly': return 'read-only';
    case 'autopilot': return 'autopilot';
    case 'headless-strict': return 'strict';
    case 'yolo': return 'yolo';
    default: return mode || 'ask';
  }
}

function modeTone(mode: string): 'moss' | 'cinnabar' | 'seal' | undefined {
  switch (mode) {
    case 'readonly': return 'moss';
    case 'yolo': return 'cinnabar';
    case 'autopilot':
    case 'headless-strict': return 'seal';
    default: return undefined;
  }
}

function tokensLabel(state: StatePayload): string {
  // Use the server-computed contextTokens (system prompt + messages + tool
  // outputs + tool schemas) when available — the old toolBytes/4 estimate
  // only counted tool schemas and showed ~5.8k when the real usage was
  // ~9.8k. The server's estimate uses the same 4-bytes-per-token heuristic
  // the /context breakdown uses.
  const tokens = state.contextTokens || Math.round((state.toolBytes || 0) / 4);
  if (state.contextWindow > 0) {
    const pct = Math.min(100, Math.round((tokens / state.contextWindow) * 100));
    return `${pct}% · ${formatK(tokens)}/${formatK(state.contextWindow)}`;
  }
  return formatK(tokens);
}

function formatK(n: number): string {
  if (n >= 1000) return `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}k`;
  return String(n);
}
