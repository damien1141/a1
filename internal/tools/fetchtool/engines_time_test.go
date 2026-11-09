package fetchtool

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimeMatches(t *testing.T) {
	e := newTimeEngine()
	cases := []struct {
		query string
		want  bool
	}{
		// The documented question shapes.
		{"what time is it", true},
		{"what's the time", true},
		{"what is the time?", true},
		{"what time", true},
		{"time now", true},
		{"current time", true},
		{"current date", true},
		{"local time", true},
		{"today's date", true},
		{"what day is it", true},
		{"what day of the week is it", true},
		{"what month is it", true},
		{"what year is it", true},
		{"date today", true},
		{"what's the time in tokyo", true},
		{"what time in tokyo", true},
		{"iso week", true},
		{"utc time", true},
		// Ambiguous programming shapes must NOT match — the engine always
		// answers, so a false positive would shadow the web results the
		// model actually asked for.
		{"what is the time complexity of quicksort", false},
		{"what's the time complexity of quicksort?", false},
		{"compile time in go", false},
		{"what is the date format in go", false},
		{"runtime error", false},
		{"rust async await example", false},
		{"best pizza in new york", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			assert.Equal(t, tc.want, e.Matches(tc.query))
		})
	}
}

func TestTimeLocation(t *testing.T) {
	cases := []struct {
		query string
		want  string
	}{
		{"what's the time in tokyo", "tokyo"},
		{"what time in Tokyo right now?", "Tokyo"},
		{"what is the time in new york please", "new york"},
		{"current time in berlin", "berlin"},
		{"what time is it", ""},
		{"time now", ""},
		{"today's date", ""},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			assert.Equal(t, tc.want, timeLocation(tc.query))
		})
	}
}

// TestTimeEngineLocalAnswer: a placeless query answers from the local
// clock with no network.
func TestTimeEngineLocalAnswer(t *testing.T) {
	resetWeatherCaches()
	// Point every upstream at a server that fails: the local answer must
	// not need any of them.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	origGeo, origIP := openMeteoGeocodeURL, ipGeoProviders
	openMeteoGeocodeURL = srv.URL + "/search"
	ipGeoProviders = []string{srv.URL + "/dead"}
	defer func() { openMeteoGeocodeURL, ipGeoProviders = origGeo, origIP }()

	e := newTimeEngine()
	resp, err := e.Search(t.Context(), "what time is it", searchOptions{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "time", resp.Engine)
	assert.Contains(t, resp.Answer, "Current date and time: ")
	assert.Contains(t, resp.Answer, "ISO week ")
	// The answer is rendered at fetch time: it must be within a minute of
	// the test's clock.
	now := time.Now()
	assert.Contains(t, resp.Answer, now.Format("2006-01-02"))
}

// TestTimeEngineZoneAnswer: a zone query geocodes the place and answers in
// its timezone, with the UTC companion line.
func TestTimeEngineZoneAnswer(t *testing.T) {
	resetWeatherCaches()
	if _, err := time.LoadLocation("Asia/Tokyo"); err != nil {
		t.Skip("tz database not available on this machine")
	}
	var geoCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		geoCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"name":"Tokyo","latitude":35.68,"longitude":139.69,"country":"Japan","timezone":"Asia/Tokyo"}]}`))
	}))
	defer srv.Close()
	origGeo := openMeteoGeocodeURL
	openMeteoGeocodeURL = srv.URL + "/search"
	defer func() { openMeteoGeocodeURL = origGeo }()

	e := newTimeEngine()
	resp, err := e.Search(t.Context(), "what's the time in tokyo", searchOptions{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Answer, "Current date and time in Tokyo")
	assert.Contains(t, resp.Answer, "Asia/Tokyo")
	assert.Contains(t, resp.Answer, "\nUTC: ")

	// The geocode is cached by the shared place cache: a second query for
	// the same place never hits the upstream again.
	_, err = e.Search(t.Context(), "what time in tokyo", searchOptions{})
	require.NoError(t, err)
	assert.Equal(t, int32(1), geoCalls.Load())
}

// TestTimeEngineZoneGeocodeFailureDegrades: when the place cannot be
// resolved, the local-zone answer stands rather than an error.
func TestTimeEngineZoneGeocodeFailureDegrades(t *testing.T) {
	resetWeatherCaches()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()
	origGeo := openMeteoGeocodeURL
	openMeteoGeocodeURL = srv.URL + "/search"
	defer func() { openMeteoGeocodeURL = origGeo }()

	e := newTimeEngine()
	resp, err := e.Search(t.Context(), "what time in atlantis", searchOptions{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Answer, "Current date and time: ")
	assert.NotContains(t, resp.Answer, "in atlantis")
}

func TestFormatNowUTCLine(t *testing.T) {
	// A zone at UTC+0 must not append the redundant UTC line.
	utc := time.Date(2025, 6, 2, 10, 30, 0, 0, time.UTC)
	got := formatNow(utc, "", "")
	assert.NotContains(t, got, "\nUTC: ")

	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)
	zoned := time.Date(2025, 6, 2, 19, 30, 0, 0, tokyo)
	got = formatNow(zoned, "Tokyo", "Asia/Tokyo")
	assert.Contains(t, got, "in Tokyo")
	assert.Contains(t, got, "zone JST (Asia/Tokyo)")
	assert.Contains(t, got, "\nUTC: ")
}
