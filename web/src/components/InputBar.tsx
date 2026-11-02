import { useState, useRef, useEffect, useMemo } from 'preact/hooks';
import { Send, Square, Terminal } from "lucide-preact";
import { SlashPicker, type SlashCommand } from './SlashPicker';
import { Dodec } from './Dodec';
import { personaAvatar } from './Avatar';
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
}

export function InputBar({ onSend, onCancel, streaming, welcome = false, persona = null, slashCommands = [], onSlashCommand }: Props) {
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

  if (welcome) {
    return (
      <div class="flex-1 flex flex-col items-center justify-center px-6 py-12 relative">
        <div class="w-full max-w-chat">
          {/* Staggered entrance: mark rises first, then heading, input,
              then the hint line — a soft 120ms cascade. The a1 square gave
              way to the Dodec wireframe — the same brand geometry as the
              favicon, live-rotating on a 3D turntable. */}
          <div class="flex justify-center mb-5 animate-rise-in">
            <Dodec size={46} class="dodec-hero" />
          </div>
          <h1 class="text-center font-serif text-2xl text-ink-light mb-3 animate-rise-in" style={{ animationDelay: '80ms' }}>
            What should we work on today?
          </h1>
          {/* Persona identity chip: who answers the next prompt. Hidden for
              the default persona (the Dodec above already says "a1"). */}
          {persona && persona.id !== 'default' && (
            <div class="flex justify-center mb-6 animate-rise-in" style={{ animationDelay: '120ms' }}>
              <span class="persona-chip flex items-center gap-2 pl-1 pr-2.5 py-1 rounded-lg bg-paper-card border border-hairline shadow-sm">
                {personaAvatar(persona, 22)}
                <span class="text-xs text-ink-light">speaking as <span class="font-medium">{persona.name}</span></span>
              </span>
            </div>
          )}
          {(!persona || persona.id === 'default') && <div class="mb-6" />}
          <div class="relative animate-rise-in" style={{ animationDelay: '160ms' }}>
            {Picker}
            {Input}
          </div>
          <div class="animate-rise-in" style={{ animationDelay: '260ms' }}>
            {Hint}
          </div>
        </div>
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
