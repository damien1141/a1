import { useState, useEffect, useRef, useCallback } from 'preact/hooks';
import type { ToastPayload, ToastLevel } from '../api';

// Toast is one transient notification. The state is owned by App and pushed
// down here as a list; the timer that expires each one also lives at the
// App level so SSE-driven toasts do not race unmount.
export interface ToastEntry extends ToastPayload {
  id: number;
}

interface Props {
  toasts: ToastEntry[];
  onDismiss: (id: number) => void;
}

const LEVEL_STYLES: Record<ToastLevel, string> = {
  success: 'bg-moss/10 border-moss/40 text-moss',
  warning: 'bg-cinnabar/10 border-cinnabar/40 text-cinnabar',
  error: 'bg-cinnabar/15 border-cinnabar text-cinnabar',
  info: 'bg-paper-card border-hairline text-ink-light',
};

export function ToastStack({ toasts, onDismiss }: Props) {
  if (toasts.length === 0) return null;
  return (
    <div class="fixed bottom-6 right-6 z-50 flex flex-col gap-2 max-w-sm">
      {toasts.map(t => (
        <ToastRow key={t.id} toast={t} onDismiss={onDismiss} />
      ))}
    </div>
  );
}

function ToastRow({ toast, onDismiss }: { toast: ToastEntry; onDismiss: (id: number) => void }) {
  const [closing, setClosing] = useState(false);
  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);

  const ms = toast.duration || 3000;

  // Auto-dismiss after the duration (default 3s) so a stale toast never
  // hangs around after the user has read it. 180ms before the deadline the
  // row starts its exit animation (slide-out-right is 170ms), so the
  // removal never reads as a pop.
  useEffect(() => {
    timers.current.push(setTimeout(() => setClosing(true), Math.max(0, ms - 180)));
    timers.current.push(setTimeout(() => onDismiss(toast.id), ms));
    return () => timers.current.forEach(clearTimeout);
  }, [toast.id, toast.duration]);

  // Click-to-dismiss: route through the same closing-state path the
  // auto-dismiss uses so the slide-out animation plays. A direct
  // onDismiss() here would unmount the row immediately, popping it out
  // of existence — visually inconsistent with the timed dismiss.
  const handleDismiss = () => {
    if (closing) return;
    setClosing(true);
    timers.current.push(setTimeout(() => onDismiss(toast.id), 170));
  };

  const cls = LEVEL_STYLES[toast.level || 'info'] || LEVEL_STYLES.info;
  return (
    <div
      role="status"
      class={`relative flex items-start gap-2 px-3 py-2 rounded-sm border shadow-sm text-sm cursor-pointer overflow-hidden ${
        closing ? 'animate-slide-out-right' : 'animate-slide-in-right'
      } ${cls}`}
      onClick={handleDismiss}
    >
      <span class="flex-1 break-words">{toast.message}</span>
      {/* Countdown: drains left→right in lockstep with the dismiss timer. */}
      <span
        class={`toast-progress absolute bottom-0 left-0 right-0 h-0.5 bg-current opacity-25 ${closing ? 'opacity-0' : ''}`}
        style={{ animationDuration: `${ms}ms` }}
      />
    </div>
  );
}

// useToast returns a (push, dismiss) pair for managing the toast list. The
// push helper dedupes consecutive identical messages so a chatty event does
// not pile up the same notification.
export function useToast() {
  const [toasts, setToasts] = useState<ToastEntry[]>([]);
  const nextId = useState(() => ({ n: 1 }))[0];

  const push = useCallback((t: ToastPayload) => {
    setToasts(prev => {
      // Dedupe: if the most recent toast matches, refresh its duration
      // rather than stack a duplicate.
      if (prev.length > 0) {
        const last = prev[prev.length - 1];
        if (last.message === t.message && last.level === t.level) {
          const refreshed = [...prev];
          refreshed[refreshed.length - 1] = { ...last, duration: t.duration };
          return refreshed;
        }
      }
      const id = nextId.n++;
      return [...prev, { ...t, id }];
    });
  }, [nextId]);

  const dismiss = useCallback((id: number) => {
    setToasts(prev => prev.filter(t => t.id !== id));
  }, []);

  return { toasts, push, dismiss };
}
