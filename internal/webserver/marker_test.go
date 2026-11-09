package webserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/session"
)

// A live compaction must leave a persistent marker row on the SSE stream, not
// just a transient status line: the reload path renders a divider for the
// stored compaction entry, and the running transcript has to show the same
// cut or the two views visibly disagree. This pins the marker event (and the
// exact text the reload renders, so live and reloaded transcripts match).
func TestCompactionCompletePublishesMarkerEvent(t *testing.T) {
	s := &Server{}
	s.mu.Lock()
	s.hub = newHub()
	s.mu.Unlock()

	ch, release := s.events().subscribe()
	defer release()

	s.projectEvent("sess", 0, "", session.CompactionComplete{TokensBefore: 58000})

	var statusText, markerText string
	for range 2 {
		ev := <-ch
		switch {
		case ev.Status != nil:
			statusText = ev.Status.Text
		case ev.Marker != nil:
			markerText = ev.Marker.Text
			assert.Equal(t, eventMarker, ev.Kind, "marker rides its own event kind")
		}
	}
	assert.Equal(t, "compacted context (58000 tokens)", statusText,
		"the transient status line still announces the cut")
	assert.Equal(t, "Compacted context (58000 tokens)", markerText,
		"the persistent marker carries the reload divider's exact text")
}

// A failed compaction appends no session entry, so the reload renders no
// divider for it — the live view must not render one either.
func TestFailedCompactionPublishesNoMarker(t *testing.T) {
	s := &Server{}
	s.mu.Lock()
	s.hub = newHub()
	s.mu.Unlock()

	ch, release := s.events().subscribe()
	defer release()

	s.projectEvent("sess", 0, "", session.CompactionComplete{Failed: true})

	select {
	case ev := <-ch:
		require.NotNil(t, ev.Status, "the failure still surfaces as a status line")
		assert.Equal(t, "compaction failed", ev.Status.Text)
		assert.Nil(t, ev.Marker, "no marker for a failed compaction")
	default:
		t.Fatal("expected a status event for the failed compaction")
	}
}
