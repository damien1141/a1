package fetchtool

import (
        "net/http"
        "net/http/httptest"
        "strconv"
        "sync/atomic"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// resetWeatherCaches clears the package-level geocode + IP caches so tests
// cannot read each other's fixtures through them.
func resetWeatherCaches() {
        geocodeCache.Lock()
        geocodeCache.entries = map[string]weatherGeo{}
        geocodeCache.Unlock()
        ipLocateCache.Lock()
        ipLocateCache.geo = weatherGeo{}
        ipLocateCache.at = time.Time{}
        ipLocateCache.Unlock()
}

func TestWeatherMatches(t *testing.T) {
        resetWeatherCaches()
        e := newWeatherEngine()
        cases := []struct {
                query string
                want  bool
        }{
                {"weather", true},
                {"weather in san francisco", true},
                {"what's the weather like in reykjavik", true},
                {"tokyo weather", true},
                {"forecast for paris", true},
                {"is it raining", true},
                {"how hot is it outside", true},
                {"temperature today", true},
                {"rain tomorrow", true},
                {"humidity in lisbon", true},
                {"wind speed right now", true},
                // Dev-signal veto: queries about building with weather data must
                // fall through to the web engines.
                {"open-meteo api docs", false},
                {"weather api json schema", false},
                {"weather app tutorial", false},
                {"github weather library", false},
                // Unrelated queries.
                {"best pizza in new york", false},
                {"what is a monad", false},
                {"100 usd to eur", false},
                {"", false},
        }
        for _, tc := range cases {
                t.Run(tc.query, func(t *testing.T) {
                        assert.Equal(t, tc.want, e.Matches(tc.query))
                })
        }
}

func TestWeatherLocation(t *testing.T) {
        cases := []struct {
                query string
                want  string
        }{
                {"weather in san francisco", "san francisco"},
                {"weather in San Francisco today", "San Francisco"},
                {"what's the weather like in reykjavik?", "reykjavik"},
                {"forecast for paris tomorrow", "paris"},
                {"how hot is it in tokyo right now", "tokyo"},
                {"tokyo weather", "tokyo"},
                {"lisbon humidity", "lisbon"},
                // No place: auto-locate.
                {"weather", ""},
                {"what's the weather", ""},
                {"is it raining", ""},
                {"weather here", ""},
                {"weather in my area", ""},
                {"weather today", ""},
        }
        for _, tc := range cases {
                t.Run(tc.query, func(t *testing.T) {
                        assert.Equal(t, tc.want, weatherLocation(tc.query))
                })
        }
}

// TestWeatherEngineSearchNamedPlace drives the full named-place path
// (geocode → forecast) against fakes and pins the answer + caching.
func TestWeatherEngineSearchNamedPlace(t *testing.T) {
        resetWeatherCaches()
        var geoCalls, fcCalls atomic.Int32
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json")
                switch r.URL.Path {
                case "/v1/search":
                        geoCalls.Add(1)
                        assert.Equal(t, "reykjavik", r.URL.Query().Get("name"))
                        _, _ = w.Write([]byte(`{"results":[{"name":"Reykjavik","latitude":64.1355,"longitude":-21.8954,"country":"Iceland","admin1":"Capital","timezone":"Atlantic/Reykjavik"}]}`))
                case "/v1/forecast":
                        fcCalls.Add(1)
                        assert.InDelta(t, 64.1355, parseFloat(t, r.URL.Query().Get("latitude")), 0.001)
                        _, _ = w.Write([]byte(`{"timezone":"Atlantic/Reykjavik","daily":{"time":["2025-06-02","2025-06-03"],"temperature_2m_max":[12.4,13.1],"temperature_2m_min":[6.2,7.0],"weathercode":[61,0],"precipitation_sum":[4.2,0.0],"wind_speed_10m_max":[42.0,18.0]}}`))
                default:
                        http.NotFound(w, r)
                }
        }))
        defer srv.Close()
        origGeo, origFc := openMeteoGeocodeURL, openMeteoForecastURL
        openMeteoGeocodeURL, openMeteoForecastURL = srv.URL+"/v1/search", srv.URL+"/v1/forecast"
        defer func() { openMeteoGeocodeURL, openMeteoForecastURL = origGeo, origFc }()

        e := newWeatherEngine()
        resp, err := e.Search(t.Context(), "weather in reykjavik", searchOptions{})
        require.NoError(t, err)
        require.NotNil(t, resp)
        assert.Equal(t, "weather", resp.Engine)
        assert.Contains(t, resp.Answer, "This week's weather forecast for Reykjavik, Capital, Iceland (Atlantic/Reykjavik):")
        assert.Contains(t, resp.Answer, "2025-06-02 Mon: rain, high 12°C / low 6°C, 4.2mm precip, wind 42km/h")
        assert.Contains(t, resp.Answer, "2025-06-03 Tue: clear, high 13°C / low 7°C")
        require.Len(t, resp.Sources, 1)
        assert.Equal(t, "open-meteo.com", resp.Sources[0].Host)

        // Second call for the same place is fully cached: no upstream traffic.
        _, err = e.Search(t.Context(), "reykjavik weather", searchOptions{})
        require.NoError(t, err)
        assert.Equal(t, int32(1), geoCalls.Load(), "geocode result is cached")
        assert.Equal(t, int32(1), fcCalls.Load(), "forecast is cached")
}

// TestWeatherEngineSearchAutoLocate drives the placeless path: the IP
// provider resolves the location, no geocode call happens.
func TestWeatherEngineSearchAutoLocate(t *testing.T) {
        resetWeatherCaches()
        var ipCalls, geoCalls atomic.Int32
        mux := http.NewServeMux()
        mux.HandleFunc("/ip", func(w http.ResponseWriter, r *http.Request) {
                ipCalls.Add(1)
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"success":true,"latitude":64.13,"longitude":-21.94,"city":"Reykjavik","country":"Iceland"}`))
        })
        mux.HandleFunc("/v1/search", func(w http.ResponseWriter, r *http.Request) {
                geoCalls.Add(1)
                http.NotFound(w, r)
        })
        mux.HandleFunc("/v1/forecast", func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"timezone":"Atlantic/Reykjavik","daily":{"time":["2025-06-02"],"temperature_2m_max":[12.0],"temperature_2m_min":[6.0],"weathercode":[0],"precipitation_sum":[0.0],"wind_speed_10m_max":[10.0]}}`))
        })
        srv := httptest.NewServer(mux)
        defer srv.Close()

        origGeo, origFc, origIP := openMeteoGeocodeURL, openMeteoForecastURL, ipGeoProviders
        openMeteoGeocodeURL = srv.URL + "/v1/search"
        openMeteoForecastURL = srv.URL + "/v1/forecast"
        ipGeoProviders = []string{srv.URL + "/ip"}
        defer func() {
                openMeteoGeocodeURL, openMeteoForecastURL, ipGeoProviders = origGeo, origFc, origIP
        }()

        e := newWeatherEngine()
        resp, err := e.Search(t.Context(), "what's the weather", searchOptions{})
        require.NoError(t, err)
        require.NotNil(t, resp)
        assert.Contains(t, resp.Answer, "forecast for Reykjavik, Iceland")
        assert.Equal(t, int32(1), ipCalls.Load())
        assert.Equal(t, int32(0), geoCalls.Load(), "a placeless query never geocodes")
}

// TestWeatherEngineAutoLocateFailsErrors: when no location can be
// determined and the query names no place, the engine errors (and the
// router falls through to web search) with a hint to name a place.
func TestWeatherEngineAutoLocateFailsErrors(t *testing.T) {
        resetWeatherCaches()
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                http.NotFound(w, r)
        }))
        defer srv.Close()
        origIP := ipGeoProviders
        ipGeoProviders = []string{srv.URL + "/dead"}
        defer func() { ipGeoProviders = origIP }()

        e := newWeatherEngine()
        _, err := e.Search(t.Context(), "weather", searchOptions{})
        require.Error(t, err)
        assert.Contains(t, err.Error(), "name one")
}

// TestWeatherEngineStaleForecastDegrades: a refetch failure after a good
// cached forecast returns the stale forecast instead of erroring.
func TestWeatherEngineStaleForecastDegrades(t *testing.T) {
        resetWeatherCaches()
        var failNext atomic.Bool
        mux := http.NewServeMux()
        mux.HandleFunc("/v1/search", func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"results":[{"name":"Oslo","latitude":59.91,"longitude":10.75,"country":"Norway","timezone":"Europe/Oslo"}]}`))
        })
        mux.HandleFunc("/v1/forecast", func(w http.ResponseWriter, r *http.Request) {
                if failNext.Load() {
                        http.Error(w, "boom", http.StatusInternalServerError)
                        return
                }
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"timezone":"Europe/Oslo","daily":{"time":["2025-06-02"],"temperature_2m_max":[20.0],"temperature_2m_min":[11.0],"weathercode":[2],"precipitation_sum":[0.0],"wind_speed_10m_max":[12.0]}}`))
        })
        srv := httptest.NewServer(mux)
        defer srv.Close()
        origGeo, origFc := openMeteoGeocodeURL, openMeteoForecastURL
        openMeteoGeocodeURL, openMeteoForecastURL = srv.URL+"/v1/search", srv.URL+"/v1/forecast"
        defer func() { openMeteoGeocodeURL, openMeteoForecastURL = origGeo, origFc }()

        e := newWeatherEngine()
        resp, err := e.Search(t.Context(), "weather in oslo", searchOptions{})
        require.NoError(t, err)
        require.NotNil(t, resp)

        // Expire the forecast cache, break the upstream, ask again: the stale
        // forecast must still answer.
        e.mu.Lock()
        for k, c := range e.forecast {
                c.at = time.Time{}
                e.forecast[k] = c
        }
        e.mu.Unlock()
        failNext.Store(true)
        resp, err = e.Search(t.Context(), "weather in oslo", searchOptions{})
        require.NoError(t, err)
        require.NotNil(t, resp)
        assert.Contains(t, resp.Answer, "Oslo")
}

// TestRenderWeatherTruncatedArrays: a forecast whose parallel arrays are
// short or mismatched renders what it has instead of panicking.
func TestRenderWeatherTruncatedArrays(t *testing.T) {
        fc := &weatherForecast{Timezone: "UTC"}
        fc.Daily.Time = []string{"2025-06-02", "2025-06-03"}
        fc.Daily.Temperature2mMax = []float64{21}
        fc.Daily.Temperature2mMin = []float64{12}
        fc.Daily.Weathercode = []int{0}
        fc.Daily.PrecipitationSum = []float64{0}
        fc.Daily.WindSpeed10mMax = []float64{5}
        got := renderWeather(fc, "Nowhere")
        assert.Contains(t, got, "2025-06-02")
        assert.NotContains(t, got, "2025-06-03", "the truncated day is dropped, not fabricated")
}

func TestWeatherLabel(t *testing.T) {
        cases := []struct {
                code int
                want string
        }{
                {0, "clear"},
                {1, "partly cloudy"},
                {3, "partly cloudy"},
                {45, "fog"},
                {51, "drizzle"},
                {61, "rain"},
                {71, "snow"},
                {80, "showers"},
                {85, "snow showers"},
                {95, "thunderstorm"},
                {42, "code 42"},
        }
        for _, tc := range cases {
                t.Run(tc.want, func(t *testing.T) {
                        assert.Equal(t, tc.want, weatherLabel(tc.code))
                })
        }
}

func parseFloat(t *testing.T, s string) float64 {
        t.Helper()
        f, err := strconv.ParseFloat(s, 64)
        require.NoError(t, err)
        return f
}
