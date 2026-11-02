package telemetry

import (
	"sync"
	"time"
)

// EventKind classifies a single telemetry event.
type EventKind string

const (
	EventToolCall EventKind = "tool_call"
	EventToolOK   EventKind = "tool_ok"
	EventToolErr  EventKind = "tool_error"
)

// Event is one recorded telemetry point.
type Event struct {
	Kind   EventKind
	Tool   string
	CallID string
	When   time.Time
	Err    string
	// DurationMS is the tool-run wall time (ok / error events only). Zero
	// on tool_call events, which fire before the run starts.
	DurationMS int64 `json:"duration_ms"`
	// SessionID attributes the event to a session; empty when unknown.
	SessionID string `json:"session_id"`
}

// Recorder collects lightweight telemetry events in memory.
type Recorder struct {
	mu       sync.Mutex
	events   []Event
	lastTool map[string]time.Time
}

// NewRecorder creates a new in-memory telemetry recorder.
func NewRecorder() *Recorder {
	return &Recorder{
		lastTool: make(map[string]time.Time),
	}
}

// Record adds a single event. When a global sink is installed the event
// is also handed to it (fire-and-forget; a slow or broken sink must never
// stall the tool loop, so sink errors are dropped here).
func (r *Recorder) Record(e Event) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.events = append(r.events, e)
	r.lastTool[e.Tool] = e.When
	r.mu.Unlock()
	if s := GlobalSink(); s != nil {
		s.Record(e)
	}
}

// ToolCalls returns the total number of tool_call events recorded.
func (r *Recorder) ToolCalls() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return countEvents(r.events, EventToolCall)
}

// ToolOK returns the total number of tool_ok events recorded.
func (r *Recorder) ToolOK() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return countEvents(r.events, EventToolOK)
}

// ToolErrors returns the total number of tool_error events recorded.
func (r *Recorder) ToolErrors() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return countEvents(r.events, EventToolErr)
}

// LastToolAt returns the most recent time the named tool was called.
func (r *Recorder) LastToolAt(tool string) (time.Time, bool) {
	if r == nil {
		return time.Time{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	when, ok := r.lastTool[tool]
	return when, ok
}

// Snapshot returns a point-in-time view of usage counts per tool.
type Snapshot struct {
	TotalCalls  int            `json:"totalCalls"`
	TotalOK     int            `json:"totalOK"`
	TotalErrors int            `json:"totalErrors"`
	ByTool      map[string]int `json:"byTool"`
	OK          map[string]int `json:"ok"`
	Errors      map[string]int `json:"errors"`
}

// Snapshot returns a snapshot of current telemetry data.
func (r *Recorder) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := Snapshot{
		ByTool: make(map[string]int),
		OK:     make(map[string]int),
		Errors: make(map[string]int),
	}
	for _, e := range r.events {
		switch e.Kind {
		case EventToolCall:
			s.TotalCalls++
			s.ByTool[e.Tool]++
		case EventToolOK:
			s.TotalOK++
			s.OK[e.Tool]++
		case EventToolErr:
			s.TotalErrors++
			s.Errors[e.Tool]++
		}
	}
	return s
}

// Reset clears all recorded events.
func (r *Recorder) Reset() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = nil
	r.lastTool = make(map[string]time.Time)
}

var (
	globalRecorder *Recorder
	globalMu       sync.RWMutex
)

// SetGlobalRecorder sets the shared telemetry recorder for the process.
func SetGlobalRecorder(r *Recorder) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalRecorder = r
}

// GlobalRecorder returns the shared telemetry recorder, if any.
func GlobalRecorder() *Recorder {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalRecorder
}

// Sink receives telemetry events for out-of-process persistence. The
// in-memory Recorder stays the query surface for live counters; a sink is
// the durable shadow (Bet 07: SQLite via AttachDB). Implementations must
// be safe for concurrent use and must not block: Record is called on the
// tool-loop path.
type Sink interface {
	Record(Event)
}

var globalSink Sink

// SetGlobalSink installs the process-wide telemetry sink (nil detaches).
func SetGlobalSink(s Sink) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalSink = s
}

// GlobalSink returns the installed sink, or nil.
func GlobalSink() Sink {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalSink
}

func countEvents(events []Event, kind EventKind) int {
	n := 0
	for _, e := range events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}
