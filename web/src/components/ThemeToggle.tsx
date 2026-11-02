import { useEffect, useState } from 'preact/hooks';
import { Moon, Sun } from "lucide-preact";
import * as theme from '../theme';

// useTheme subscribes the component to the theme store so a toggle anywhere
// (sidebar, settings) re-renders every button that shows the current state.
// Plain subscribe/useState: preact/hooks has no useSyncExternalStore export.
export function useTheme(): theme.Theme {
  const [t, setT] = useState<theme.Theme>(() => theme.getTheme());
  useEffect(() => theme.subscribe(() => setT(theme.getTheme())), []);
  return t;
}

// ThemeToggleButton is the one-click icon toggle for the sidebar header:
// shows what you'd switch TO (moon in light, sun in dark).
export function ThemeToggleButton() {
  const t = useTheme();
  const dark = t === 'dark';
  return (
    <button
      type="button"
      class={`p-1 rounded-sm text-ink-muted hover:text-seal hover:bg-seal-light active:scale-90 transition-all duration-fast`}
      onClick={() => theme.toggleTheme()}
      title={dark ? 'Switch to light mode' : 'Switch to dark mode'}
      aria-label={dark ? 'Switch to light mode' : 'Switch to dark mode'}
    >
      {dark ? <Sun size={14} /> : <Moon size={14} />}
    </button>
  );
}

// ThemeSegmented is the explicit light/dark picker in Settings. "System"
// clears the stored choice so the OS preference drives again (armed live in
// theme.initTheme).
export function ThemeSegmented() {
  const t = useTheme();
  const stored = theme.hasStoredChoice();
  return (
    <div class="flex flex-wrap gap-1">
      <button
        type="button"
        onClick={() => theme.setTheme('light')}
        class={`px-2.5 py-1 rounded-sm text-xs transition-colors ${
          stored && t === 'light' ? 'bg-seal text-on-seal' : 'bg-paper-sunk text-ink-light hover:bg-seal-light hover:text-seal'
        }`}
      >
        Light (paper)
      </button>
      <button
        type="button"
        onClick={() => theme.setTheme('dark')}
        class={`px-2.5 py-1 rounded-sm text-xs transition-colors ${
          stored && t === 'dark' ? 'bg-seal text-on-seal' : 'bg-paper-sunk text-ink-light hover:bg-seal-light hover:text-seal'
        }`}
      >
        Dark (charcoal)
      </button>
      <button
        type="button"
        onClick={() => theme.clearTheme()}
        class={`px-2.5 py-1 rounded-sm text-xs transition-colors ${
          !stored ? 'bg-seal text-on-seal' : 'bg-paper-sunk text-ink-light hover:bg-seal-light hover:text-seal'
        }`}
      >
        System
      </button>
      <p class="text-xs text-ink-muted mt-1 w-full">
        System follows your OS preference until you pick light or dark.
      </p>
    </div>
  );
}
