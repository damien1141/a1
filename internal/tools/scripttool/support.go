package scripttool

import (
	"net"
	"sync"
)

// netListener is the RPC endpoint type (net.Listener); aliased so the
// fallback story in listenRPC reads as one surface.
type netListener = net.Listener

func listenUnix(path string) (netListener, error) {
	return net.Listen("unix", path)
}

func listenLoopbackTCP() (netListener, error) {
	// Loopback only: the endpoint is for this machine's script child, and
	// the token check still applies — but a listener that could be reached
	// from the network would be a gift to port scanners.
	return net.Listen("tcp", "127.0.0.1:0")
}

// keepMode picks which end of a runaway stream the collector retains.
type keepMode int

const (
	// keepHead retains the first bytes: spilled stdout should preserve what
	// the script started producing (hermes spills from the start).
	keepHead keepMode = iota
	// keepTail retains the last bytes: tracebacks live at the end of stderr,
	// after any flood of warnings.
	keepTail
)

// cappedWriter is a bounded, concurrency-safe output collector. total
// always counts every byte seen (an int does not overflow at stream
// scales), so truncation metadata can report true totals even when the
// retained slice is capped.
type cappedWriter struct {
	limit int
	mode  keepMode

	mu    sync.Mutex
	buf   []byte
	total int
}

func newCappedWriter(limit int, mode keepMode) *cappedWriter {
	return &cappedWriter{limit: limit, mode: mode}
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.total += len(p)
	switch w.mode {
	case keepTail:
		w.buf = append(w.buf, p...)
		if len(w.buf) > w.limit {
			w.buf = w.buf[len(w.buf)-w.limit:]
		}
	default: // keepHead
		if len(w.buf) < w.limit {
			take := len(p)
			if room := w.limit - len(w.buf); take > room {
				take = room
			}
			w.buf = append(w.buf, p[:take]...)
		}
	}
	return len(p), nil
}

// truncated reports whether any bytes were dropped by the cap.
func (w *cappedWriter) truncated() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.total > len(w.buf)
}

// totalBytes reports every byte seen, including dropped ones.
func (w *cappedWriter) totalBytes() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.total
}

// String returns the retained slice.
func (w *cappedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}
