// Theme state for the web GUI: light (warm paper) and dark (warm charcoal).
//
// The palette itself lives in CSS variables (see styles.css :root and
// [data-theme="dark"]) so Tailwind utilities follow the active theme without
// any per-component branching. This module owns the toggle state: applying
// `data-theme` on <html>, caching the choice, and notifying subscribers.
//
// PERSISTENCE (the config-file ruling): the source of truth is
// web.dark_mode in ~/.a1/config.yaml, written by PUT /api/theme — the
// theme button's key, toggled on and off in the config file. The server
// stamps the choice onto the served document's <html> tag, so the very
// first paint is right in EVERY browser (not just the one that clicked),
// and the /settings config screen — a separate document that reads the
// file server-side — follows the same key. localStorage stays as the
// boot-time cache and the dev-server fallback: the boot snippet syncs it
// whenever the server stamps a choice, and pre-paint reads still come
// from it (a cache is not a source of truth, but it is instant).

import { saveTheme } from './api';

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

function cacheTheme(t: Theme) {
  try {
    localStorage.setItem(STORAGE_KEY, t);
  } catch {
    // Non-fatal: the toggle still works for this tab session.
  }
}

// persistTheme writes the choice to config.yaml (web.dark_mode). The local
// apply already happened — the toggle must feel instant — so this is
// fire-and-forget with an honest failure channel: a CustomEvent the App
// routes to the toast stack. An unsaved choice must never pass silently
// for a saved one.
function persistTheme(theme: 'light' | 'dark' | 'system'): void {
  if (typeof window === 'undefined') return;
  saveTheme(theme).catch(e => {
    const msg = e instanceof Error ? e.message : String(e);
    window.dispatchEvent(new CustomEvent('a1:toast', {
      detail: { level: 'error', message: `Theme not saved to config.yaml — ${msg}` },
    }));
  });
}

// initTheme resolves the starting theme: an explicit choice beats the OS
// preference. Called once before the first render. The boot snippet in
// index.html already applied (and, when the server stamped the config
// file's choice, cached) the decision pre-paint; this syncs the module's
// copy of it.
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

  // Cross-tab sync: another tab's toggle (or "System" clear) rewrites the
  // cache; the storage event carries it here so every open tab agrees
  // without waiting for a reload. The PUT made it the file's choice too —
  // this tab just follows sooner.
  if (typeof window !== 'undefined') {
    window.addEventListener('storage', e => {
      if (e.key !== STORAGE_KEY) return;
      if (e.newValue === 'light' || e.newValue === 'dark') {
        current = e.newValue;
      } else {
        current = systemTheme(); // cleared — the OS preference drives again
      }
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

// setTheme applies the choice everywhere it belongs: the DOM now, the
// cache for the next pre-paint, and the config file (via PUT /api/theme)
// so it survives the browser, reaches the config screen, and lands in
// every other browser on the next load.
export function setTheme(t: Theme): Theme {
  current = t;
  applyToDOM(t);
  cacheTheme(t);
  persistTheme(t);
  listeners.forEach(fn => fn());
  return current;
}

export function toggleTheme(): Theme {
  return setTheme(current === 'dark' ? 'light' : 'dark');
}

// clearTheme drops the stored choice — cache AND config key — and follows
// the OS preference again (live-updating via the watcher armed in
// initTheme).
export function clearTheme(): Theme {
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {
    // Storage unavailable: still snap to the system value for this session.
  }
  persistTheme('system');
  current = systemTheme();
  applyToDOM(current);
  listeners.forEach(fn => fn());
  return current;
}

// hasStoredChoice reports whether the user picked a theme explicitly (vs
// following the OS). The Settings picker uses it to highlight "system" —
// true when either the config file or this browser's cache holds a choice.
export function hasStoredChoice(): boolean {
  return storedTheme() !== null;
}

// subscribe lets hooks (useSyncExternalStore) re-render on theme changes.
export function subscribe(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
