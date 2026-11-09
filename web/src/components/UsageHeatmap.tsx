import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import type * as api from '../api';

// UsageHeatmap is the dashboard's commit-style activity grid: 53 week
// columns × 7 weekday rows (Sunday first, GitHub-style), ending on today.
// Each cell is one UTC day of vault activity — the same day semantics the
// server rollup uses (sessions.created_at, entry timestamps, tool_runs.at
// all normalize to UTC days), so a cell and its tooltip never disagree
// with /api/usage/daily.
//
// Clicking a day toggles a filter: the dashboard narrows its session list
// to sessions last active on that day (the row's data-mtime ISO). The
// selected cell wears a cinnabar ring so the filter state is visible
// without hover.

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

// heatLevel maps an activity value to the 0-4 seal ramp. Quartiles of the
// window's busiest day — same shape GitHub uses for commits, so the ramp
// reads instantly to anyone who has seen a contribution graph.
function heatLevel(v: number, max: number): number {
  if (v <= 0 || max <= 0) return 0;
  return Math.min(4, Math.max(1, Math.ceil((v / max) * 4)));
}

const LEVEL_BG = [
  'bg-ink/[0.06]',
  'bg-seal/25',
  'bg-seal/45',
  'bg-seal/70',
  'bg-seal',
];

interface Props {
  daily: api.UsageDay[];
  // selectedDay is the active filter (YYYY-MM-DD) or null.
  selectedDay: string | null;
  onSelectDay: (date: string | null) => void;
}

export function UsageHeatmap({ daily, selectedDay, onSelectDay }: Props) {
  // On narrow screens the 53-week grid overflows horizontally; scroll to
  // the END on mount so the newest weeks (the ones the user cares about)
  // are what's visible, not last year's.
  const scrollRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollLeft = el.scrollWidth;
  }, []);
  // The window ends today; index math is relative to that anchor so the
  // grid never shifts when a stale cached payload crosses midnight.
  const grid = useMemo(() => {
    const byDate = new Map<string, api.UsageDay>();
    let max = 0;
    for (const d of daily) {
      byDate.set(d.date, d);
      const v = d.messages + d.toolCalls;
      if (v > max) max = v;
    }
    // The window ends on the PAYLOAD's last date (the server zero-fills
    // through today) — anchoring there instead of the client clock keeps
    // the grid and the data consistent even when a cached response crosses
    // midnight between fetch and render.
    const last = daily.length > 0 ? daily[daily.length - 1] : undefined;
    const end = last ? new Date(last.date + 'T00:00:00Z') : new Date();
    const days = daily.length || 371;
    const cells: { date: string; day: api.UsageDay | undefined; level: number; col: number; row: number; inWindow: boolean }[] = [];
    const cols = Math.ceil(days / 7);
    for (let i = 0; i < days; i++) {
      const d = new Date(end);
      d.setUTCDate(d.getUTCDate() - (days - 1 - i));
      const date = d.toISOString().slice(0, 10);
      const day = byDate.get(date);
      const daysAgo = days - 1 - i;
      cells.push({
        date,
        day,
        level: day ? heatLevel(day.messages + day.toolCalls, max) : 0,
        col: cols - 1 - Math.floor(daysAgo / 7),
        row: d.getUTCDay(),
        inWindow: true,
      });
    }
    // Pad the final (current) week's trailing cells after today with
    // blanks so the grid stays rectangular.
    const todayRow = end.getUTCDay();
    for (let row = todayRow + 1; row < 7; row++) {
      cells.push({ date: '', day: undefined, level: -1, col: cols - 1, row, inWindow: false });
    }
    return { cells, cols, max };
  }, [daily]);

  // Month labels: one per column where a month begins inside that column.
  const monthLabels = useMemo(() => {
    const out: { col: number; label: string }[] = [];
    let lastMonth = -1;
    for (let col = 0; col < grid.cols; col++) {
      // The column's first (top) cell.
      const first = grid.cells.find(c => c.col === col && c.row === 0);
      if (!first || !first.inWindow) continue;
      const m = Number(first.date.slice(5, 7)) - 1;
      if (m !== lastMonth) {
        out.push({ col, label: MONTHS[m] ?? '' });
        lastMonth = m;
      }
    }
    return out;
  }, [grid]);

  const fmtDay = (d: api.UsageDay): string =>
    `${d.messages} message${d.messages === 1 ? '' : 's'} · ${d.toolCalls} tool call${d.toolCalls === 1 ? '' : 's'} · ${d.sessions} session${d.sessions === 1 ? '' : 's'} · ${d.tokens.toLocaleString()} tokens`;

  const label = (date: string): string => {
    const y = Number(date.slice(0, 4));
    const m = Number(date.slice(5, 7)) - 1;
    const d = Number(date.slice(8, 10));
    return `${MONTHS[m] ?? ''} ${d}, ${y}`;
  };

  const totals = useMemo(() => {
    let messages = 0, toolCalls = 0, tokens = 0, active = 0;
    for (const d of daily) {
      messages += d.messages;
      toolCalls += d.toolCalls;
      tokens += d.tokens;
      if (d.messages + d.toolCalls > 0) active++;
    }
    return { messages, toolCalls, tokens, active };
  }, [daily]);

  const selected = selectedDay ? daily.find(d => d.date === selectedDay) : undefined;

  return (
    <div class="min-w-0">
      <div class="flex items-baseline justify-between mb-2 flex-wrap gap-x-3 gap-y-1">
        <span class="text-xs font-semibold text-ink-muted tracking-wider uppercase">Activity</span>
        <span class="text-xs text-ink-muted">
          {totals.active} active day{totals.active === 1 ? '' : 's'} · {totals.messages.toLocaleString()} messages · {totals.toolCalls.toLocaleString()} tool calls · {(totals.tokens / 1000).toLocaleString(undefined, { maximumFractionDigits: 1 })}k tokens
        </span>
      </div>
      <div ref={scrollRef} class="overflow-x-auto pb-1">
        <div class="inline-block min-w-full">
          {/* Month labels row: spans the same 14px column pitch as the grid. */}
          <div class="flex ml-6 mb-1" style={{ height: '14px' }}>
            {Array.from({ length: grid.cols }, (_, col) => {
              const m = monthLabels.find(l => l.col === col);
              return (
                <div key={col} class="flex-shrink-0 w-3 mr-[2px] text-[9px] text-ink-muted overflow-visible whitespace-nowrap">
                  {m ? m.label : ''}
                </div>
              );
            })}
          </div>
          <div class="flex">
            {/* Weekday rail: Mon / Wed / Fri only — the full rail is noise. */}
            <div class="w-6 flex-shrink-0 flex flex-col mr-1">
              {WEEKDAYS.map((wd, r) => (
                <div key={wd} class="h-3 mb-[2px] text-[9px] leading-3 text-ink-muted">
                  {r % 2 === 1 ? wd : ''}
                </div>
              ))}
            </div>
            {/* The grid proper: absolute-free, one flex column per week. */}
            <div class="flex">
              {Array.from({ length: grid.cols }, (_, col) => (
                <div key={col} class="flex flex-col mr-[2px]">
                  {Array.from({ length: 7 }, (_, row) => {
                    const cell = grid.cells.find(c => c.col === col && c.row === row);
                    if (!cell || !cell.inWindow) {
                      return <div key={row} class="w-3 h-3 mb-[2px]" />;
                    }
                    const isSel = selectedDay === cell.date;
                    const isToday = cell.date === (daily[daily.length - 1]?.date ?? '');
                    return (
                      <button
                        key={row}
                        type="button"
                        class={`w-3 h-3 mb-[2px] rounded-[2px] transition-all duration-fast ${LEVEL_BG[cell.level] ?? ''} ${
                          isSel ? 'ring-1 ring-cinnabar ring-offset-1 ring-offset-paper-card' : 'hover:ring-1 hover:ring-seal/50'
                        } ${isToday && !isSel ? 'ring-1 ring-seal/60' : ''}`}
                        title={cell.day ? `${label(cell.date)} — ${fmtDay(cell.day)}` : `${label(cell.date)} — no activity`}
                        aria-label={cell.day ? `${label(cell.date)}: ${fmtDay(cell.day)}` : `${label(cell.date)}: no activity`}
                        onClick={() => onSelectDay(isSel ? null : cell.date)}
                      />
                    );
                  })}
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
      {/* Legend + selected-day readout. The readout doubles as the
          mobile tooltip: tap selects, the line under the grid explains. */}
      <div class="flex items-center justify-between mt-2 flex-wrap gap-x-3 gap-y-1">
        <div class="flex items-center gap-1">
          <span class="text-[10px] text-ink-muted mr-1">less</span>
          {LEVEL_BG.map((bg, i) => (
            <span key={i} class={`w-2.5 h-2.5 rounded-[2px] ${bg}`} />
          ))}
          <span class="text-[10px] text-ink-muted ml-1">more</span>
        </div>
        {selectedDay && selected ? (
          <span class="text-xs text-ink-light">
            <span class="font-medium">{label(selectedDay)}</span> — {fmtDay(selected)}
          </span>
        ) : (
          <span class="text-[10px] text-ink-muted">click a day to filter sessions</span>
        )}
      </div>
    </div>
  );
}
