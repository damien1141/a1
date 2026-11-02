// Theme state for the web GUI: light (warm paper) and dark (warm charcoal).
//
// The palette itself lives in CSS variables (see styles.css :root and
// [data-theme="dark"]) so Tailwind utilities follow the active theme without
// any per-component branching. This module only owns the toggle state:
// applying `data-theme` on <html>, persisting the choice, and notifying
// subscribers. index.html runs a pre-paint boot snippet with the same
// storage key so a reload never flashes the wrong theme.

export type Theme = 'light' | 'dark';

const STORAGE_KEY = 'a1-theme';

let current: Theme = 'light';
const listeners = new Set<() => void>();

function systemTheme(): Theme {
  return typeof window !== 'undefined' && window.matchMedia?.('(prefers-color-scheme: dark)').matches
    ? 'dark'
    : 'light';
}

function storedTheme(): Theme | null {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    return v === 'light' || v === 'dark' ? v : null;
  } catch {
    return null; // storage unavailable (private mode) — follow the system
  }
}

// initTheme resolves the starting theme: an explicit choice beats the OS
// preference. Called once before the first render.
export function initTheme(): Theme {
  current = storedTheme() ?? systemTheme();
  applyToDOM(current);

  // While the user has not picked a side, follow the OS live so a system
  // switch (or sun-set auto dark mode) lands in the open tab too.
  if (typeof window !== 'undefined' && !storedTheme()) {
    window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', e => {
      if (storedTheme()) return; // user chose explicitly meanwhile
      current = e.matches ? 'dark' : 'light';
      applyToDOM(current);
      listeners.forEach(fn => fn());
    });
  }
  return current;
}

function applyToDOM(t: Theme) {
  document.documentElement.dataset.theme = t;
}

export function getTheme(): Theme {
  return current;
}

export function setTheme(t: Theme): Theme {
  current = t;
  applyToDOM(t);
  try {
    localStorage.setItem(STORAGE_KEY, t);
  } catch {
    // Non-fatal: the toggle still works for this tab session.
  }
  listeners.forEach(fn => fn());
  return current;
}

export function toggleTheme(): Theme {
  return setTheme(current === 'dark' ? 'light' : 'dark');
}

// clearTheme drops the stored choice and follows the OS preference again
// (live-updating via the watcher armed in initTheme).
export function clearTheme(): Theme {
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {
    // Storage unavailable: still snap to the system value for this session.
  }
  current = systemTheme();
  applyToDOM(current);
  listeners.forEach(fn => fn());
  return current;
}

// hasStoredChoice reports whether the user picked a theme explicitly (vs
// following the OS). The Settings picker uses it to highlight "system".
export function hasStoredChoice(): boolean {
  return storedTheme() !== null;
}

// subscribe lets hooks (useSyncExternalStore) re-render on theme changes.
export function subscribe(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
