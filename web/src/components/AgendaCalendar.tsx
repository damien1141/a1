import { useEffect, useMemo, useState } from 'preact/hooks';
import { CalendarDays, ChevronLeft, ChevronRight, Clock, Lock, MapPin, Repeat2, Settings as SettingsIcon } from 'lucide-preact';
import { Dodec } from './Dodec';
import * as api from '../api';

// AgendaCalendar is the dashboard's calendar widget. Chunk 2 shipped it
// as a grouped agenda list; chunk 7 grows it the ask's shape — a MONTH
// GRID where each day is a button and the selected day's events list
// below the grid. The month's data comes from the range endpoint (one
// fetch per visible month, the window's right edge exclusive); the
// states stay honest in every direction — locked when the personal
// master switch is off (a 403 mid-flight folds back there too: the
// switch can flip while the dashboard is open), "not configured" when
// the dir is unset, degraded says so, empty says so. The widget only
// ever READS: the write face belongs to the agent tool, behind the
// model's personal_write opt-in.

interface Props {
  status: api.PersonalStatus | null;
  class?: string; // grid placement (e.g. lg:col-span-2) from the dashboard
}

const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

// dayKey is the LOCAL calendar day of an RFC3339 instant — the grid
// groups on the days the user lives in, not UTC buckets.
function dayKey(iso: string): string {
  const d = new Date(iso);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

function formatTime(iso: string): string {
  const d = new Date(iso);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

// todayKey is the local day the viewer is living in.
function todayKey(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

export function AgendaCalendar({ status, class: cls }: Props) {
  const [month, setMonth] = useState(() => {
    const d = new Date();
    return { y: d.getFullYear(), m: d.getMonth() };
  });
  const [data, setData] = useState<api.CalendarAgendaResponse | null>(null);
  const [failed, setFailed] = useState(false);
  const [locked, setLocked] = useState(false);
  const [selDay, setSelDay] = useState<string | null>(null);

  const enabled = !!status?.enabled;
  const configured = !!status?.calendarDir;

  // One fetch per visible month. The window is the month itself: from
  // the 1st, to the 1st of the next month (the range endpoint's right
  // edge is exclusive). Zero gated fetches fire from locked states; a
  // 403 mid-flight (the switch flipped server-side) folds to locked.
  useEffect(() => {
    if (!enabled || !configured) return;
    let alive = true;
    setData(null);
    setFailed(false);
    setLocked(false);
    const from = `${month.y}-${String(month.m + 1).padStart(2, '0')}-01`;
    const nm = month.m === 11 ? 0 : month.m + 1;
    const ny = month.m === 11 ? month.y + 1 : month.y;
    const to = `${ny}-${String(nm + 1).padStart(2, '0')}-01`;
    api.fetchCalendarRange(from, to)
      .then(r => {
        if (!alive) return;
        setData(r);
        // The current month autoselects today — the grid opens showing
        // what is happening NOW, the same autoselect the heatmap runs.
        // Other months open with nothing picked: the grid is the prompt.
        if (r.events.length > 0 || from === `${new Date().getFullYear()}-${String(new Date().getMonth() + 1).padStart(2, '0')}-01`) {
          const tk = todayKey();
          const tkInMonth = tk >= from && tk < to;
          setSelDay(tkInMonth ? tk : null);
        } else {
          setSelDay(null);
        }
      })
      .catch((err: Error) => {
        if (!alive) return;
        if (err.message && err.message.includes('disabled')) setLocked(true);
        else setFailed(true);
      });
    return () => { alive = false; };
  }, [enabled, configured, month]);

  // The month's events bucketed by local day.
  const byDay = useMemo(() => {
    const m = new Map<string, api.CalendarEvent[]>();
    for (const ev of data?.events ?? []) {
      const k = dayKey(ev.start);
      let g = m.get(k);
      if (!g) { g = []; m.set(k, g); }
      g.push(ev);
    }
    return m;
  }, [data]);

  // The grid's geometry: leading blanks (the 1st's weekday), the month's
  // days, trailing blanks to complete the last row.
  const cells = useMemo(() => {
    const first = new Date(month.y, month.m, 1);
    const daysInMonth = new Date(month.y, month.m + 1, 0).getDate();
    const out: { day: number | null }[] = [];
    for (let i = 0; i < first.getDay(); i++) out.push({ day: null });
    for (let d = 1; d <= daysInMonth; d++) out.push({ day: d });
    while (out.length % 7 !== 0) out.push({ day: null });
    return out;
  }, [month]);

  const monthIsNow = month.y === new Date().getFullYear() && month.m === new Date().getMonth();
  const step = (delta: number) => {
    setMonth(prev => {
      const m = prev.m + delta;
      if (m < 0) return { y: prev.y - 1, m: 11 };
      if (m > 11) return { y: prev.y + 1, m: 0 };
      return { y: prev.y, m };
    });
  };

  const selEvents = selDay ? (byDay.get(selDay) ?? []) : [];
  const selDate = selDay ? new Date(selDay + 'T00:00:00') : null;

  const eventRow = (ev: api.CalendarEvent) => (
    <div
      key={ev.uid}
      class="flex items-start gap-2 px-2 py-1.5 rounded-sm bg-paper border border-hairline"
      title={ev.description || ev.summary}
    >
      <span class="flex items-center gap-1 text-[11px] text-ink-muted tabular-nums flex-shrink-0 pt-0.5 min-w-[3.25rem]">
        {ev.allDay
          ? <span class="flex items-center gap-0.5"><CalendarDays size={11} /><span class="sr-only">all day</span>all-day</span>
          : <span class="flex items-center gap-0.5"><Clock size={11} />{formatTime(ev.start)}</span>}
      </span>
      <span class="min-w-0 flex-1">
        <span class="flex items-center gap-1">
          <span class="truncate text-sm text-ink-light">{ev.summary || '(untitled)'}</span>
          {ev.recurring && <Repeat2 size={11} class="text-ink-muted flex-shrink-0" aria-label="recurring" />}
        </span>
        {ev.location && (
          <span class="flex items-center gap-1 text-xs text-ink-muted mt-0.5">
            <MapPin size={10} class="flex-shrink-0" />
            <span class="truncate">{ev.location}</span>
          </span>
        )}
      </span>
    </div>
  );

  const body = () => {
    if (!enabled || locked) {
      return (
        <div class="text-xs text-ink-muted leading-relaxed">
          <p class="flex items-center gap-1.5 text-ink-light mb-1"><Lock size={11} /> Personal data is off</p>
          <p>Turn on the master switch in the config editor to see your calendar here.</p>
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
    if (!configured) {
      return (
        <div class="text-xs text-ink-muted leading-relaxed">
          <p class="flex items-center gap-1.5 text-ink-light mb-1"><CalendarDays size={11} /> No calendar directory</p>
          <p>Set <code class="text-[11px] bg-ink/[0.06] px-1 rounded-sm">personal.calendar_dir</code> to a folder of .ics files to see your agenda here.</p>
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
      return <p class="text-xs text-ink-muted py-4 text-center">The calendar is unavailable right now.</p>;
    }
    if (!data) {
      return (
        <div class="py-6 flex justify-center" aria-label="loading calendar">
          <Dodec size={28} animate={false} class="opacity-40" />
        </div>
      );
    }
    const tk = todayKey();
    return (
      <div class="flex flex-col gap-3">
        {/* Month nav: the grid's own header, independent of the card title. */}
        <div class="flex items-center gap-1">
          <button
            type="button"
            class="p-1 rounded-sm text-ink-muted hover:text-seal hover:bg-seal-light active:scale-90 transition-all duration-fast"
            onClick={() => step(-1)}
            aria-label="Previous month"
          >
            <ChevronLeft size={14} />
          </button>
          <span class="text-sm text-ink-light font-medium tabular-nums px-1" aria-live="polite">
            {MONTHS[month.m]} {month.y}
          </span>
          <button
            type="button"
            class="p-1 rounded-sm text-ink-muted hover:text-seal hover:bg-seal-light active:scale-90 transition-all duration-fast"
            onClick={() => step(1)}
            aria-label="Next month"
          >
            <ChevronRight size={14} />
          </button>
          {!monthIsNow && (
            <button
              type="button"
              class="ml-auto px-2 py-0.5 rounded-sm text-[11px] text-ink-muted border border-hairline hover:text-seal hover:border-seal/40 transition-colors duration-fast"
              onClick={() => { const d = new Date(); setMonth({ y: d.getFullYear(), m: d.getMonth() }); }}
            >
              today
            </button>
          )}
        </div>

        {/* The grid: weekday rail, then the month's cells. Every day with
            a number is a button — the ask's "click on the days to see
            what's going on". */}
        <div class="grid grid-cols-7 gap-0.5" role="grid" aria-label={`${MONTHS[month.m]} ${month.y}`}>
          {WEEKDAYS.map(wd => (
            <div key={wd} class="text-center text-[9px] text-ink-muted tracking-wider uppercase pb-0.5">{wd.slice(0, 2)}</div>
          ))}
          {cells.map((c, i) => {
            if (c.day === null) return <div key={`b${i}`} />;
            const key = `${month.y}-${String(month.m + 1).padStart(2, '0')}-${String(c.day).padStart(2, '0')}`;
            const evs = byDay.get(key) ?? [];
            const isToday = key === tk;
            const isSel = key === selDay;
            return (
              <button
                key={key}
                type="button"
                class={`relative h-8 rounded-sm flex flex-col items-center justify-center transition-all duration-fast ${
                  isSel ? 'bg-seal text-on-seal font-medium' : isToday ? 'text-cinnabar font-medium hover:bg-seal-light' : 'text-ink-light hover:bg-seal-light'
                }`}
                onClick={() => setSelDay(isSel ? null : key)}
                aria-label={`${MONTHS[month.m]} ${c.day}: ${evs.length} event${evs.length === 1 ? '' : 's'}`}
                aria-pressed={isSel}
              >
                <span class="text-xs leading-none tabular-nums">{c.day}</span>
                {evs.length > 0 && (
                  <span class="flex items-center gap-0.5 mt-0.5" aria-hidden="true">
                    {evs.slice(0, 3).map((_, j) => (
                      <span key={j} class={`w-1 h-1 rounded-full ${isSel ? 'bg-on-seal/80' : 'bg-seal/70'}`} />
                    ))}
                    {evs.length > 3 && <span class={`text-[7px] leading-none ${isSel ? 'text-on-seal/80' : 'text-ink-muted'}`}>+{evs.length - 3}</span>}
                  </span>
                )}
                {isToday && !isSel && <span class="absolute inset-x-1.5 bottom-0.5 h-px bg-cinnabar/50" aria-hidden="true" />}
              </button>
            );
          })}
        </div>

        {/* The selected day's events — the grid's answer to "what's going
            on". Today autoselects on the current month's load. */}
        <div class="border-t border-hairline pt-2">
          <div class="flex items-center gap-2 mb-1.5">
            <span class={`text-[10px] font-semibold tracking-wider uppercase ${selDay === tk ? 'text-cinnabar' : 'text-ink-muted'}`}>
              {selDate
                ? `${WEEKDAYS[selDate.getDay()]} ${selDate.getDate()} ${MONTHS[selDate.getMonth()]}${selDay === tk ? ' — today' : ''}`
                : 'Pick a day'}
            </span>
            <span class="h-px flex-1 bg-hairline" aria-hidden="true" />
          </div>
          {selDay === null ? (
            <p class="text-xs text-ink-muted">Click a day in the grid to see its events.</p>
          ) : selEvents.length === 0 ? (
            <p class="text-xs text-ink-muted">Nothing on this day.</p>
          ) : (
            <div class="flex flex-col gap-1 max-h-44 overflow-y-auto pr-1 -mr-1">
              {selEvents.map(eventRow)}
            </div>
          )}
        </div>

        {data.stats.degraded && (
          <p class="text-[11px] text-ink-muted">
            {data.stats.skippedLines > 0 || data.stats.skippedEvents > 0 || data.stats.skippedFiles > 0
              ? `Some calendar data was skipped (${[
                data.stats.skippedFiles && `${data.stats.skippedFiles} file${data.stats.skippedFiles > 1 ? 's' : ''}`,
                data.stats.skippedEvents && `${data.stats.skippedEvents} event${data.stats.skippedEvents > 1 ? 's' : ''}`,
                data.stats.skippedLines && `${data.stats.skippedLines} line${data.stats.skippedLines > 1 ? 's' : ''}`,
              ].filter(Boolean).join(', ')} unparsable).`
              : 'Some calendar data was skipped.'}
          </p>
        )}
      </div>
    );
  };

  return (
    <section class={`bg-paper-card border border-hairline rounded-lg shadow-sm p-4 min-w-0 ${cls || ''}`}>
      <div class="flex items-center gap-2 mb-3">
        <CalendarDays size={13} class="text-ink-muted" />
        <h2 class="text-xs font-semibold text-ink-muted tracking-wider uppercase">Calendar</h2>
        {enabled && configured && data && (
          <span class="ml-auto text-[10px] text-ink-muted">{MONTHS[month.m]} {month.y}</span>
        )}
      </div>
      {body()}
    </section>
  );
}
