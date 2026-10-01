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

// Record adds a single event.
func (r *Recorder) Record(e Event) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	r.lastTool[e.Tool] = e.When
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

func countEvents(events []Event, kind EventKind) int {
	n := 0
	for _, e := range events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}
