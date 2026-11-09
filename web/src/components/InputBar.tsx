import { useState, useRef, useEffect, useMemo } from 'preact/hooks';
import { Send, Square, Terminal, PanelRight } from "lucide-preact";
import { SlashPicker, type SlashCommand } from './SlashPicker';
import type * as api from '../api';

interface Props {
  onSend: (text: string) => void;
  onCancel: () => void;
  streaming: boolean;
  welcome?: boolean;
  // persona is the active profile — colors the composer placeholder and,
  // on the welcome screen, renders the identity chip under the heading.
  persona?: api.Persona | null;
  // Slash command catalog (passed down from App so all commands share one
  // source of truth with the Ctrl+K palette).
  slashCommands?: SlashCommand[];
  // onSlashCommand is invoked when the user accepts a slash picker entry
  // and the command needs an overlay / side effect App owns (e.g. /diff,
  // /code, /branch). Plain /clear and /compact may be sent straight
  // through as text the way the TUI does.
  onSlashCommand?: (cmd: SlashCommand) => void;
  // filesOpen/onToggleFiles wire the split-screen file browser's toggle
  // (chunk 7): it lives in the composer, left of the send button — the
  // spot the eye is already on when it wants the screen split — and
  // reads as pressed while the pane is up. Omit both to hide it.
  filesOpen?: boolean;
  onToggleFiles?: () => void;
}

export function InputBar({ onSend, onCancel, streaming, welcome = false, persona = null, slashCommands = [], onSlashCommand, filesOpen, onToggleFiles }: Props) {
  const [text, setText] = useState('');
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [showSlash, setShowSlash] = useState(false);

  // Auto-resize textarea.
  useEffect(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = Math.min(el.scrollHeight, welcome ? 240 : 200) + 'px';
  }, [text, welcome]);

  // In welcome mode, focus the input on mount so the user can just start
  // typing — the whole page is the input.
  useEffect(() => {
    if (welcome) {
      textareaRef.current?.focus();
    }
  }, [welcome]);

  // Show the slash picker when text starts with "/" and is not yet
  // submitted. Hide for "!" (shell) prefix — that's a different command
  // surface the user accepts as-is.
  useEffect(() => {
    const t = text.trimStart();
    setShowSlash(t.startsWith('/') && !t.includes(' ') && t.length >= 1);
  }, [text]);

  const slashQuery = useMemo(() => {
    const t = text.trimStart();
    if (!t.startsWith('/')) return '';
    return t.slice(1);
  }, [text]);

  const handleSend = () => {
    if (!text.trim() || streaming) return;
    const trimmed = text.trim();
    // Slash dispatch: BOTH bare "/name" and "/name args" are intercepted
    // as commands when the first word matches the catalog. The old code only
    // intercepted the bare form — "/code main.go:42" fell through and was
    // sent to the agent as a literal prompt instead of opening the viewer.
    if (trimmed.startsWith('/')) {
      const spaceIdx = trimmed.indexOf(' ');
      const name = (spaceIdx === -1 ? trimmed : trimmed.slice(0, spaceIdx)).slice(1);
      const rawArgs = spaceIdx === -1 ? '' : trimmed.slice(spaceIdx + 1).trim();
      const cmd = slashCommands.find(c => c.name === name);
      if (cmd) {
        if (cmd.needsArgs && !rawArgs) {
          // Bare command that needs an argument: park the composer at
          // "/name " so the user can type one.
          setText(`/${name} `);
          return;
        }
        if (onSlashCommand) {
          onSlashCommand({ ...cmd, rawArgs });
          setText('');
          return;
        }
      }
    }
    onSend(trimmed);
    setText('');
  };

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  const acceptSlash = (cmd: SlashCommand) => {
    if (cmd.needsArgs) {
      setText(`/${cmd.name} `);
      setShowSlash(false);
      textareaRef.current?.focus();
      return;
    }
    if (onSlashCommand) {
      onSlashCommand(cmd);
      setText('');
      setShowSlash(false);
      return;
    }
    setText(`/${cmd.name}`);
    setShowSlash(false);
    textareaRef.current?.focus();
  };

  const shellMode = text.trimStart().startsWith('!');

  const Picker = showSlash && slashQuery !== undefined ? (
    <SlashPicker
      query={slashQuery}
      commands={slashCommands}
      onAccept={acceptSlash}
      onClose={() => {
        setShowSlash(false);
        // Clear the partial "/..." text on Escape so the user can immediately
        // type a fresh prompt without backspacing through "/co". Most
        // autocomplete UIs clear on Escape-dismiss; the previous behavior
        // left the partial command in the textarea, forcing manual cleanup.
        if (text.trimStart().startsWith('/')) setText('');
      }}
    />
  ) : null;

  const Input = (
    <div class={`flex items-end gap-2 ${welcome ? 'px-5 py-4' : 'px-4 py-3'} rounded-chat bg-paper-card border ${shellMode ? 'border-cinnabar/40' : 'border-hairline'} focus-within:border-seal focus-within:shadow-[0_0_0_3px_rgb(var(--seal)/0.10)] transition-all duration-fast shadow-sm`}>
      <textarea
        ref={textareaRef}
        value={text}
        onInput={(e) => setText((e.target as HTMLTextAreaElement).value)}
        onKeyDown={handleKeyDown}
        placeholder={shellMode ? "shell command — !cmd args" : (welcome ? "Ask anything, plan anything…  Type / for commands." : `Message ${persona?.name || 'a1'}…  Type / for commands.`)}
        rows={welcome ? 2 : 1}
        class={`flex-1 resize-none bg-transparent ${welcome ? 'text-base' : 'text-sm'} text-ink placeholder:text-ink-muted outline-none ${welcome ? 'max-h-[240px]' : 'max-h-[200px]'} leading-relaxed`}
        disabled={streaming}
      />
      <div class="flex items-center gap-1 flex-shrink-0">
        {shellMode && (
          <span class="text-[10px] text-cinnabar px-1.5 py-0.5 bg-cinnabar/10 rounded-sm flex items-center gap-1 animate-fade-in" title="Shell mode — runs locally">
            <Terminal size={10} />
            shell
          </span>
        )}
        {onToggleFiles && (
          <button
            type="button"
            class={`p-1.5 rounded-sm transition-all duration-fast active:scale-90 ${
              filesOpen
                ? 'text-seal bg-seal-light'
                : 'text-ink-muted hover:text-seal hover:bg-seal-light'
            }`}
            onClick={onToggleFiles}
            title="File browser — split screen"
            aria-label="Toggle the file browser pane"
            aria-pressed={!!filesOpen}
          >
            <PanelRight size={welcome ? 18 : 16} />
          </button>
        )}
        {streaming ? (
          <button
            class={`p-1.5 rounded-sm text-cinnabar hover:bg-cinnabar/10 active:scale-90 transition-all duration-fast`}
            onClick={onCancel}
            title="Stop"
          >
            <Square size={welcome ? 18 : 16} />
          </button>
        ) : (
          <button
            class={`p-1.5 rounded-sm transition-all duration-fast ${
              text.trim()
                ? 'text-seal hover:bg-seal-light hover:scale-110 active:scale-95'
                : 'text-ink-muted cursor-not-allowed'
            }`}
            onClick={handleSend}
            disabled={!text.trim()}
            title="Send (Enter)"
          >
            <Send size={welcome ? 18 : 16} />
          </button>
        )}
      </div>
    </div>
  );

  const Hint = (
    <p class="text-xs text-ink-muted mt-1.5 text-center">
      Enter to send · Shift+Enter for newline · <kbd class="font-mono">/</kbd> for commands · <kbd class="font-mono">!</kbd> for shell
    </p>
  );

  // Welcome mode no longer owns the page: the Dashboard composes the hero
  // and docks this composer bottom-center. The welcome flag keeps its
  // sizing contract (taller textarea, welcome placeholder, autofocus) —
  // only the layout wrapper is the caller's business now.
  if (welcome) {
    return (
      <div class="w-full">
        <div class="relative">
          {Picker}
          {Input}
        </div>
        {Hint}
      </div>
    );
  }

  return (
    <div class="flex-shrink-0 px-6 pb-2 pt-1 bg-paper relative">
      <div class="max-w-chat mx-auto">
        <div class="relative">
          {Picker}
          {Input}
        </div>
        {Hint}
      </div>
    </div>
  );
}
