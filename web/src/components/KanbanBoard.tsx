import { useEffect, useMemo, useState } from 'preact/hooks';
import { Check, GripVertical, KanbanSquare, Lock, Pencil, Plus, Settings as SettingsIcon, Trash2, X } from 'lucide-preact';
import { Dodec } from './Dodec';
import { Modal } from './Modal';
import * as api from '../api';

// KanbanBoard is the dashboard's board widget (chunk 3): three columns
// (todo / doing / done) over the gated /api/personal/kanban surface.
// Unlike the calendar this widget WRITES — dragging a card IS the
// product, and the human clicking their own dashboard needs no per-model
// opt-in (those flags govern the agent tool) — so every mutation is a
// POST / PUT / DELETE behind the same master switch, followed by a
// refetch of the whole board. No optimistic state: the server's stored
// truth is the only state, and a failed mutation toasts and refetches, so
// the board can never show a move that did not land.
//
// Drag & drop is HTML5-native: a card carries its id in the dataTransfer,
// dropping ON a card inserts before it, dropping on the column appends.
// The same-column insert index is adjusted for the mover's own removal
// (the store inserts among the destination's OTHER cards). Keyboard users
// are not stranded: the edit modal moves a card between columns without
// the mouse — fine ordering stays a drag affordance until the chunk-6
// accessibility sweep.

const FALLBACK_COLUMNS = ['todo', 'doing', 'done'];

// Structural event types: just what the drag handlers use, so the Preact
// synthetic events slot in without importing JSX event types.
interface DropEvent {
  preventDefault(): void;
  stopPropagation(): void;
  dataTransfer: DataTransfer | null;
}

interface Props {
  status: api.PersonalStatus | null;
  onToast: (t: api.ToastPayload) => void;
  class?: string;
}

interface EditDraft {
  id: string;
  title: string;
  notes: string;
  col: string;
  originalCol: string;
}

export function KanbanBoard({ status, onToast, class: cls }: Props) {
  const [board, setBoard] = useState<api.KanbanBoardResponse | null>(null);
  const [failed, setFailed] = useState(false);
  const [locked, setLocked] = useState(false);
  const [dragId, setDragId] = useState<string | null>(null);
  const [dragCol, setDragCol] = useState<string | null>(null);
  const [addingCol, setAddingCol] = useState<string | null>(null);
  const [addText, setAddText] = useState('');
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
  const [edit, setEdit] = useState<EditDraft | null>(null);

  const enabled = !!status?.enabled;

  // The gated fetch fires only when the switch is on — zero gated
  // fetches from the locked card. A 403 mid-flight (the switch flipped
  // server-side while this tab was open) folds back to locked, exactly
  // like the agenda widget. There is no "not configured" state: the
  // board lives in the vault, not a directory.
  useEffect(() => {
    if (!enabled) return;
    let alive = true;
    setBoard(null);
    setFailed(false);
    setLocked(false);
    api.fetchKanbanBoard()
      .then(b => { if (alive) setBoard(b); })
      .catch((err: Error) => {
        if (!alive) return;
        if (err.message && err.message.includes('disabled')) setLocked(true);
        else setFailed(true);
      });
    return () => { alive = false; };
  }, [enabled]);

  const columns = board && board.columns.length > 0 ? board.columns : FALLBACK_COLUMNS;

  // Cards grouped by column; the server sends (col, position) order, the
  // group just partitions it (defensively skipping an unknown column).
  const byCol = useMemo(() => {
    const m = new Map<string, api.KanbanCard[]>();
    for (const c of columns) m.set(c, []);
    for (const card of board?.cards ?? []) {
      const list = m.get(card.col);
      if (list) list.push(card);
    }
    return m;
  }, [board, columns]);

  const reload = () => api.fetchKanbanBoard()
    .then(b => { setBoard(b); setFailed(false); setLocked(false); })
    .catch((err: Error) => {
      if (err.message && err.message.includes('disabled')) {
        setLocked(true);
        setBoard(null);
        return;
      }
      setFailed(true);
      setBoard(null);
    });

  // mutate runs one write, then refetches on BOTH paths — a failure
  // toasts and the refetch restores the server's truth over whatever
  // the UI hoped would happen.
  const mutate = (op: Promise<unknown>) => {
    op.catch((err: Error) => {
      onToast({ level: 'error', message: err.message || 'The board write failed.' });
    }).finally(reload);
  };

  // ---------- drag & drop ----------

  const finishDrag = () => {
    setDragId(null);
    setDragCol(null);
  };

  const startDrag = (e: { dataTransfer: DataTransfer | null }, id: string) => {
    setDragId(id);
    setConfirmDelete(null);
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', id);
    }
  };

  const dragOver = (e: { preventDefault(): void; dataTransfer: DataTransfer | null }, col: string) => {
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
    setDragCol(col);
  };

  const draggedCard = (e: DropEvent): api.KanbanCard | null => {
    const id = (e.dataTransfer && e.dataTransfer.getData('text/plain')) || dragId;
    if (!id || !board) return null;
    return board.cards.find(c => c.id === id) || null;
  };

  // Drop ON a card: insert before it.
  const dropOnCard = (e: DropEvent, col: string, target: api.KanbanCard) => {
    e.preventDefault();
    e.stopPropagation();
    finishDrag();
    const dragged = draggedCard(e);
    if (!dragged || dragged.id === target.id) return;
    // The store inserts at the given index among the destination's
    // OTHER cards — when the mover already sits above the target in the
    // same column, its own removal shifts the target up first, so pass
    // one less to land directly before it.
    let position = target.position;
    if (dragged.col === col && dragged.position < target.position) position -= 1;
    if (dragged.col === col && position === dragged.position) return; // a no-op drop
    const input: { id: string; col?: string; position?: number } = { id: dragged.id, position };
    if (dragged.col !== col) input.col = col;
    mutate(api.kanbanUpdateCard(input));
  };

  // Drop on the column body: append (the explicit index keeps a
  // same-column append from being an empty PUT — col alone changes
  // nothing without a position).
  const dropOnColumn = (e: DropEvent, col: string) => {
    e.preventDefault();
    finishDrag();
    const dragged = draggedCard(e);
    if (!dragged) return;
    const list = byCol.get(col) ?? [];
    const last = list.length > 0 ? list[list.length - 1] : undefined;
    if (dragged.col === col) {
      if (!last || last.id === dragged.id) return; // already at the end
      mutate(api.kanbanUpdateCard({ id: dragged.id, position: list.length - 1 }));
    } else {
      mutate(api.kanbanUpdateCard({ id: dragged.id, col }));
    }
  };

  // ---------- add / edit / delete ----------

  const submitAdd = (col: string) => {
    const title = addText.trim();
    setAddingCol(null);
    setAddText('');
    if (!title) return;
    mutate(api.kanbanAddCard({ col, title }));
  };

  const openEdit = (card: api.KanbanCard) => {
    setConfirmDelete(null);
    setEdit({ id: card.id, title: card.title, notes: card.notes || '', col: card.col, originalCol: card.col });
  };

  const saveEdit = () => {
    if (!edit) return;
    const title = edit.title.trim();
    if (!title) return;
    const input: { id: string; col?: string; title?: string; notes?: string } = {
      id: edit.id,
      title,
      notes: edit.notes,
    };
    if (edit.col !== edit.originalCol) input.col = edit.col;
    setEdit(null);
    mutate(api.kanbanUpdateCard(input));
  };

  // ---------- rendering ----------

  const confirmStrip = (card: api.KanbanCard) => {
    const short = card.title.length > 24 ? `${card.title.slice(0, 24)}…` : card.title;
    return (
      <div key={card.id} class="flex items-center gap-1.5 px-2.5 py-2 rounded-sm bg-paper border border-cinnabar/40 animate-fade-in">
        <Trash2 size={12} class="text-cinnabar flex-shrink-0" />
        <span class="text-xs text-ink-muted flex-1 min-w-0 truncate">Delete “{short}”?</span>
        <button
          type="button"
          class="p-1 rounded-sm text-cinnabar hover:bg-cinnabar/10 active:scale-90 transition-all duration-fast"
          onClick={() => { setConfirmDelete(null); mutate(api.kanbanDeleteCard(card.id)); }}
          title="Confirm delete"
          aria-label="Confirm delete"
        >
          <Check size={12} />
        </button>
        <button
          type="button"
          class="p-1 rounded-sm text-ink-muted hover:text-ink hover:bg-paper-sunk active:scale-90 transition-all duration-fast"
          onClick={() => setConfirmDelete(null)}
          title="Keep"
          aria-label="Keep card"
        >
          <X size={12} />
        </button>
      </div>
    );
  };

  const boardCard = (card: api.KanbanCard, col: string) => (
    <div
      key={card.id}
      draggable={true}
      onDragStart={e => startDrag(e, card.id)}
      onDragEnd={finishDrag}
      onDrop={e => dropOnCard(e, col, card)}
      class={`group relative cursor-grab active:cursor-grabbing px-2.5 py-2 rounded-sm bg-paper border border-hairline hover:border-seal/40 transition-all duration-fast ${dragId === card.id ? 'opacity-50' : ''}`}
      title={card.notes ? `${card.title}\n\n${card.notes}` : card.title}
    >
      <button type="button" class="w-full text-left" onClick={() => openEdit(card)} aria-label={`Edit ${card.title}`}>
        <span class="flex items-start gap-1.5">
          <GripVertical size={11} class="text-ink-muted/70 flex-shrink-0 mt-0.5" aria-hidden="true" />
          <span class="min-w-0 flex-1">
            <span class="block text-sm text-ink-light leading-snug break-words">{card.title}</span>
            {card.notes && (
              <span class="block text-xs text-ink-muted mt-1 leading-snug line-clamp-2 break-words">{card.notes}</span>
            )}
          </span>
        </span>
      </button>
      <span class="absolute top-1 right-1 flex items-center gap-0.5 opacity-0 group-hover:opacity-100 focus-within:opacity-100 transition-opacity duration-fast">
        <button
          type="button"
          class="p-1 rounded-sm text-ink-muted hover:text-seal hover:bg-seal-light active:scale-90 transition-all duration-fast"
          onClick={e => { e.stopPropagation(); openEdit(card); }}
          title="Edit card"
          aria-label={`Edit ${card.title}`}
        >
          <Pencil size={11} />
        </button>
        <button
          type="button"
          class="p-1 rounded-sm text-ink-muted hover:text-cinnabar hover:bg-cinnabar/10 active:scale-90 transition-all duration-fast"
          onClick={e => { e.stopPropagation(); setConfirmDelete(card.id); }}
          title="Delete card"
          aria-label={`Delete ${card.title}`}
        >
          <Trash2 size={11} />
        </button>
      </span>
    </div>
  );

  const addInput = (col: string) => (
    <div class="animate-fade-in">
      <input
        type="text"
        value={addText}
        autoFocus
        placeholder="Card title — Enter to add"
        class="w-full px-2 py-1.5 rounded-sm text-sm text-ink bg-paper border border-hairline focus:border-seal/50 focus:outline-none placeholder:text-ink-muted"
        onInput={e => setAddText((e.target as HTMLInputElement).value)}
        onKeyDown={e => {
          if (e.key === 'Enter') {
            e.preventDefault();
            submitAdd(col);
          } else if (e.key === 'Escape') {
            setAddingCol(null);
            setAddText('');
          }
        }}
        onBlur={() => { setAddingCol(null); setAddText(''); }}
        // A card dropped on the open input must not paste its id into the
        // field — kill the default, let the drop bubble to the column.
        onDrop={e => e.preventDefault()}
      />
    </div>
  );

  const body = () => {
    if (!enabled || locked) {
      return (
        <div class="text-xs text-ink-muted leading-relaxed">
          <p class="flex items-center gap-1.5 text-ink-light mb-1"><Lock size={11} /> Personal data is off</p>
          <p>Turn on the master switch in the config editor to use your board here.</p>
          <button
            type="button"
            class="mt-2 inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-sm text-xs border border-hairline bg-paper text-ink-light hover:text-seal hover:border-seal/40 transition-colors duration-fast"
            onClick={() => window.open('/settings', '_blank')}
          >
            <SettingsIcon size={12} /> Open config
          </button>
        </div>
      );
    }
    if (failed) {
      return <p class="text-xs text-ink-muted py-4 text-center">The board is unavailable right now.</p>;
    }
    if (!board) {
      return (
        <div class="py-6 flex justify-center" aria-label="loading board">
          <Dodec size={28} animate={false} class="opacity-40" />
        </div>
      );
    }
    return (
      <div>
        {board.cards.length === 0 && (
          <p class="text-xs text-ink-muted mb-2">
            Nothing on the board yet — add your first card with the + buttons. The agent can work the same board through its personal_kanban tool.
          </p>
        )}
        <div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
          {columns.map(col => {
            const cards = byCol.get(col) ?? [];
            return (
              <div key={col} class="flex flex-col min-w-0">
                <div class="flex items-center gap-1.5 mb-2 flex-shrink-0">
                  <span class={`text-[10px] font-semibold tracking-wider uppercase ${col === 'done' ? 'text-seal' : 'text-ink-muted'}`}>{col}</span>
                  <span class="text-[10px] text-ink-muted tabular-nums">{cards.length}</span>
                  <button
                    type="button"
                    class="ml-auto p-1 rounded-sm text-ink-muted hover:text-seal hover:bg-seal-light active:scale-90 transition-all duration-fast"
                    onClick={() => { setAddingCol(col); setAddText(''); setConfirmDelete(null); }}
                    title={`Add a card to ${col}`}
                    aria-label={`Add a card to ${col}`}
                  >
                    <Plus size={12} />
                  </button>
                </div>
                <div
                  class={`flex-1 flex flex-col gap-1.5 min-h-16 rounded-sm p-1 -m-1 transition-colors duration-fast ${dragCol === col && dragId ? 'bg-seal-light ring-1 ring-seal/30' : ''}`}
                  onDragOver={e => dragOver(e, col)}
                  onDrop={e => dropOnColumn(e, col)}
                >
                  {cards.map(card => (confirmDelete === card.id ? confirmStrip(card) : boardCard(card, col)))}
                  {cards.length === 0 && addingCol !== col && (
                    <p class="text-[11px] text-ink-muted text-center py-2 select-none">Nothing here yet</p>
                  )}
                  {addingCol === col && addInput(col)}
                </div>
              </div>
            );
          })}
        </div>
      </div>
    );
  };

  return (
    <section class={`bg-paper-card border border-hairline rounded-lg shadow-sm p-4 min-w-0 ${cls || ''}`}>
      <div class="flex items-center gap-2 mb-3">
        <KanbanSquare size={13} class="text-ink-muted" />
        <h2 class="text-xs font-semibold text-ink-muted tracking-wider uppercase">Kanban</h2>
        {enabled && board && (
          <span class="ml-auto text-[10px] text-ink-muted">
            {board.cards.length} card{board.cards.length === 1 ? '' : 's'}
          </span>
        )}
      </div>
      {body()}
      {edit && (
        <Modal
          title="Edit card"
          subtitle="Text edits and column moves; drag for ordering."
          onClose={() => setEdit(null)}
          size="sm"
          footer={
            <>
              <button
                type="button"
                class="px-3 py-1.5 rounded-sm text-xs text-ink-light bg-paper border border-hairline hover:text-seal hover:border-seal/40 transition-colors duration-fast"
                onClick={() => setEdit(null)}
              >
                Cancel
              </button>
              <button
                type="button"
                class="px-3 py-1.5 rounded-sm text-xs bg-seal text-on-seal hover:opacity-90 transition-opacity duration-fast disabled:opacity-50"
                disabled={!edit.title.trim()}
                onClick={saveEdit}
              >
                Save
              </button>
            </>
          }
        >
          <div class="p-4 flex flex-col gap-3">
            <label class="block">
              <span class="block text-xs font-medium text-ink-light mb-1">Title</span>
              <input
                type="text"
                value={edit.title}
                autoFocus
                class="w-full px-2.5 py-1.5 rounded-sm text-sm text-ink bg-paper border border-hairline focus:border-seal/50 focus:outline-none"
                onInput={e => setEdit({ ...edit, title: (e.target as HTMLInputElement).value })}
              />
            </label>
            <label class="block">
              <span class="block text-xs font-medium text-ink-light mb-1">
                Notes <span class="text-ink-muted font-normal">(optional — saving an empty box clears them)</span>
              </span>
              <textarea
                rows={3}
                value={edit.notes}
                class="w-full px-2.5 py-1.5 rounded-sm text-sm text-ink bg-paper border border-hairline focus:border-seal/50 focus:outline-none resize-y"
                onInput={e => setEdit({ ...edit, notes: (e.target as HTMLTextAreaElement).value })}
              />
            </label>
            <label class="block">
              <span class="block text-xs font-medium text-ink-light mb-1">Column</span>
              <select
                value={edit.col}
                class="w-full px-2.5 py-1.5 rounded-sm text-sm text-ink bg-paper border border-hairline focus:border-seal/50 focus:outline-none"
                onChange={e => setEdit({ ...edit, col: (e.target as HTMLSelectElement).value })}
              >
                {columns.map(c => <option key={c} value={c}>{c}</option>)}
              </select>
            </label>
          </div>
        </Modal>
      )}
    </section>
  );
}
