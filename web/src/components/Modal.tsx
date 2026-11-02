import type { ComponentChildren } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { X } from "lucide-preact";

interface Props {
  title: string;
  subtitle?: string;
  onClose: () => void;
  children: ComponentChildren;
  // size: 'sm' (450px) | 'md' (640px) | 'lg' (900px) | 'xl' (1100px)
  size?: 'sm' | 'md' | 'lg' | 'xl';
  // footer: optional bottom row of buttons.
  footer?: ComponentChildren;
}

// Modal is the shared shell for every overlay the TUI shows full-screen
// (DiffPane, CodePane) or as a picker (BranchPicker, SessionPicker). On
// the web we render them as centered modals with the same keyboard
// contract: Escape closes, click on the backdrop closes, focus is
// trapped inside the modal body.
//
// Animation contract: the backdrop fades while the panel pops in with a
// short rise + scale (ease-out-expo). Closing plays the reverse pair
// before unmounting, so every close path (Esc, backdrop, X button, a
// child action) routes through requestClose() rather than calling
// onClose() directly.
export function Modal({ title, subtitle, onClose, children, size = 'md', footer }: Props) {
  const [closing, setClosing] = useState(false);
  const closeTimer = useRef<ReturnType<typeof setTimeout>>(null);
  // Hold the latest onClose in a ref so the Escape effect can mount once and
  // never re-subscribe. Inline arrow `onClose` callbacks (which App passes)
  // would otherwise re-run this effect on every parent render — clearing the
  // close timer mid-flight and leaving the modal stuck in its exit animation.
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;
  // Track whether we've already kicked off the close so repeated Escape
  // presses don't reset the timer.
  const closingRef = useRef(false);

  const requestClose = () => {
    if (closingRef.current) return;
    closingRef.current = true;
    setClosing(true);
    // Matches animate-pop-out (140ms); small buffer so the exit frame lands.
    closeTimer.current = setTimeout(() => onCloseRef.current(), 160);
  };

  // Close on Escape. Mounts once per modal so the handler does not leak,
  // and never re-subscribes when the parent re-renders (e.g. on every SSE
  // delta while streaming) — the close timer would otherwise be cleared
  // mid-flight and the modal would hang in the exit animation.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        requestClose();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('keydown', onKey);
      if (closeTimer.current) clearTimeout(closeTimer.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const widthClass =
    size === 'sm' ? 'max-w-md' :
    size === 'lg' ? 'max-w-5xl' :
    size === 'xl' ? 'max-w-7xl' :
    'max-w-2xl';

  return (
    <div
      class={`fixed inset-0 z-40 flex items-center justify-center p-4 bg-ink/40 ${closing ? 'animate-fade-out' : 'animate-fade-in'}`}
      onClick={requestClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        class={`w-full ${widthClass} max-h-[90vh] flex flex-col bg-paper-card border border-hairline rounded-md shadow-xl overflow-hidden ${closing ? 'animate-pop-out' : 'animate-pop-in'}`}
        onClick={e => e.stopPropagation()}
      >
        <header class="flex items-start justify-between gap-2 px-4 py-3 border-b border-hairline flex-shrink-0">
          <div class="min-w-0">
            <h2 class="text-sm font-semibold text-ink truncate">{title}</h2>
            {subtitle && (
              <p class="text-xs text-ink-muted mt-0.5 truncate">{subtitle}</p>
            )}
          </div>
          <button
            type="button"
            class="p-1 rounded-sm text-ink-muted hover:text-ink hover:bg-paper-sunk active:scale-90 transition-all duration-fast flex-shrink-0"
            onClick={requestClose}
            title="Close (Esc)"
          >
            <X size={16} />
          </button>
        </header>

        <div class="flex-1 overflow-y-auto">
          {children}
        </div>

        {footer && (
          <footer class="px-4 py-2.5 border-t border-hairline bg-paper-sunk flex items-center justify-end gap-2 flex-shrink-0">
            {footer}
          </footer>
        )}
      </div>
    </div>
  );
}
