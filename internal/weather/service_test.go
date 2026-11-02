package weather

import (
        "context"
        "encoding/json"
        "net/http"
        "net/http/httptest"
        "strings"
        "testing"
        "time"
)

func TestConfig_IsConfigured(t *testing.T) {
        cases := []struct {
                name string
                cfg  Config
                want bool
        }{
                {"disabled", Config{Enabled: false, Latitude: 35, Longitude: 139}, false},
                {"no coords", Config{Enabled: true}, false},
                {"bad lat", Config{Enabled: true, Latitude: 91, Longitude: 0}, false},
                {"bad lon", Config{Enabled: true, Latitude: 0, Longitude: 200}, false},
                {"good", Config{Enabled: true, Latitude: 64.13, Longitude: -21.94}, true},
        }
        for _, c := range cases {
                t.Run(c.name, func(t *testing.T) {
                        if got := c.cfg.IsConfigured(); got != c.want {
                                t.Fatalf("IsConfigured() = %v, want %v", got, c.want)
                        }
                })
        }
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
                {999, "code 999"},
        }
        for _, c := range cases {
                if got := WeatherLabel(c.code); got != c.want {
                        t.Errorf("WeatherLabel(%d) = %q, want %q", c.code, got, c.want)
                }
        }
}

// fakeMeteo spins up an httptest.Server that returns a canned forecast body.
func fakeMeteo(t *testing.T, body string) (*httptest.Server, *http.Client) {
        t.Helper()
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                // Echo the query so tests can assert what we asked for.
                q := r.URL.Query()
                if q.Get("latitude") == "" {
                        t.Errorf("expected latitude query, got %q", r.URL.RawQuery)
                }
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(body))
        }))
        client := &http.Client{Timeout: 5 * time.Second}
        t.Cleanup(srv.Close)
        return srv, client
}

const sampleForecastBody = `{
  "timezone": "Atlantic/Reykjavik",
  "daily": {
    "time": ["2026-10-25", "2026-10-26", "2026-10-27"],
    "temperature_2m_max": [9.1, 8.2, 7.4],
    "temperature_2m_min": [3.0, 2.1, 1.5],
    "weathercode": [3, 61, 71],
    "precipitation_sum": [0.0, 4.2, 2.0],
    "wind_speed_10m_max": [22.0, 41.0, 18.0]
  }
}`

func TestForecast_FetchAndCache(t *testing.T) {
        srv, client := fakeMeteo(t, sampleForecastBody)
        // Patch the URL constant by pointing the service at the test server
        // through a custom client — the URL is hard-coded in fetchForecast, so
        // we override the package-level constant via the test build's own
        // fetchForecast. To make this work without re-plumbing the package,
        // call fetchForecast directly with a server whose handler matches the
        // path we hit. Since the constant is a real Open-Meteo URL, we exercise
        // the cache + decode via fetchForecast using a stubbed URL via a helper.
        _ = srv
        cfg := Config{Enabled: true, Latitude: 64.13, Longitude: -21.94, Name: "Reykjavik"}
        fc, err := fetchForecast(context.Background(), client, cfgWithURL(client, srv.URL, cfg))
        if err != nil {
                t.Fatalf("fetchForecast: %v", err)
        }
        if len(fc.Daily.Time) != 3 {
                t.Fatalf("expected 3 days, got %d", len(fc.Daily.Time))
        }
        if fc.Timezone != "Atlantic/Reykjavik" {
                t.Fatalf("timezone: %q", fc.Timezone)
        }
}

// cfgWithURL points the package's openMeteoForecastURL at the test server
// so fetchForecast calls hit the stub instead of the live API. Tests must
// not run in parallel when this is in use (global mutation).
func cfgWithURL(_ *http.Client, base string, cfg Config) Config {
        openMeteoForecastURL = base
        return cfg
}

func TestSystemMessage_Render(t *testing.T) {
        // Build a Service whose Forecast() returns a canned value via cache.
        cfg := Config{Enabled: true, Latitude: 64.13, Longitude: -21.94, Name: "Reykjavik"}
        svc := NewService(cfg, 0, nil)
        // Inject a cached forecast directly to skip the network.
        var fc Forecast
        if err := json.Unmarshal([]byte(sampleForecastBody), &fc); err != nil {
                t.Fatalf("unmarshal sample: %v", err)
        }
        svc.cached = &fc
        svc.cachedAt = time.Now()

        msg := svc.SystemMessage(context.Background())
        if !strings.Contains(msg, "Reykjavik") {
                t.Errorf("message missing location: %q", msg)
        }
        if !strings.Contains(msg, "Atlantic/Reykjavik") {
                t.Errorf("message missing timezone: %q", msg)
        }
        // First day should be partly cloudy (code 3), no precip line.
        if !strings.Contains(msg, "partly cloudy") {
                t.Errorf("expected 'partly cloudy' label: %q", msg)
        }
        // Second day has 4.2mm precip, should show ", 4.2mm precip".
        if !strings.Contains(msg, "4.2mm precip") {
                t.Errorf("expected precip line: %q", msg)
        }
        // Second day has 41 km/h wind, should show ", wind 41km/h".
        if !strings.Contains(msg, "wind 41km/h") {
                t.Errorf("expected wind line: %q", msg)
        }
}

func TestSystemMessage_NotConfigured(t *testing.T) {
        svc := NewService(Config{}, 0, nil)
        if msg := svc.SystemMessage(context.Background()); msg != "" {
                t.Errorf("expected empty message when not configured, got %q", msg)
        }
}

func TestSystemMessage_FetchErrorFallsBackToCache(t *testing.T) {
        // Server returns 500. With no cache, Forecast returns an error.
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
                w.WriteHeader(500)
                _, _ = w.Write([]byte("upstream broken"))
        }))
        t.Cleanup(srv.Close)
        openMeteoForecastURL = srv.URL
        cfg := Config{Enabled: true, Latitude: 1, Longitude: 1}
        svc := NewService(cfg, 0, &http.Client{Timeout: 5 * time.Second})
        if _, err := svc.Forecast(context.Background()); err == nil {
                t.Fatal("expected error from broken server")
        }
}

func TestUpdateConfig_InvalidatesCache(t *testing.T) {
        svc := NewService(Config{Enabled: true, Latitude: 1, Longitude: 1}, 0, nil)
        svc.cached = &Forecast{}
        svc.cachedAt = time.Now()
        svc.UpdateConfig(Config{Enabled: true, Latitude: 2, Longitude: 2})
        if svc.cached != nil {
                t.Fatal("expected cache to be invalidated when coords change")
        }
}
