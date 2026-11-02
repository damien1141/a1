// Package weather fetches a weekly forecast from the Open-Meteo API and
// renders it as a compact system-message block for the agent context.
//
// Open-Meteo is free, requires no API key, and is documented at
// https://open-meteo.com/en/docs. Forecasts are cached with a TTL so a long
// chat session does not refetch on every turn.
package weather

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "io"
        "net/http"
        "strings"
        "sync"
        "time"

        "github.com/damien1141/a1/internal/util"
)

// DefaultTTL bounds how long a fetched forecast is reused before we refetch.
// Open-Meteo updates the forecast hourly; a 30-minute TTL is fresh enough
// without hammering the endpoint on every turn.
const DefaultTTL = 30 * time.Minute

// openMeteoForecastURL is the endpoint we hit. It is a `var` (not a `const`)
// so tests can point it at an httptest.Server. daily parameters picked:
//   - temperature_2m_max / temperature_2m_min — high & low for each day
//   - weathercode                       — WMO code, mapped to a label
//   - precipitation_sum                 — mm of rain/snow
//   - wind_speed_10m_max                — km/h gust ceiling
var openMeteoForecastURL = "https://api.open-meteo.com/v1/forecast"

// Config is the user-configurable weather settings. It mirrors the
// `weather:` block in config.yaml.
type Config struct {
        Enabled   bool    `yaml:"enabled"    json:"enabled"`
        Latitude  float64 `yaml:"latitude"   json:"latitude"`
        Longitude float64 `yaml:"longitude"  json:"longitude"`
        Name      string  `yaml:"name,omitempty" json:"name,omitempty"`
}

// IsConfigured reports whether the config has a usable lat/lon. Enabled
// without coordinates is treated as not-configured so the agent context
// silently skips weather rather than emitting an error every turn. We
// additionally treat (0, 0) as unset: it is the zero value of float64, so
// an account that flips `enabled: true` without filling in coords would
// otherwise hit the live API for the Gulf of Guinea every turn.
func (c Config) IsConfigured() bool {
        return c.Enabled && validLat(c.Latitude) && validLon(c.Longitude) &&
                !(c.Latitude == 0 && c.Longitude == 0)
}

func validLat(lat float64) bool { return lat >= -90 && lat <= 90 }
func validLon(lon float64) bool { return lon >= -180 && lon <= 180 }

// Forecast is the shape of the Open-Meteo /v1/forecast response we use.
// We only deserialize the daily block; the response also contains an
// hourly block we deliberately ignore to keep payloads small.
type Forecast struct {
        Daily struct {
                Time             []string `json:"time"`
                Temperature2mMax []float64 `json:"temperature_2m_max"`
                Temperature2mMin []float64 `json:"temperature_2m_min"`
                Weathercode      []int     `json:"weathercode"`
                PrecipitationSum []float64 `json:"precipitation_sum"`
                WindSpeed10mMax  []float64 `json:"wind_speed_10m_max"`
        } `json:"daily"`
        Timezone string `json:"timezone"`
}

// Service fetches forecasts and caches them per (lat,lon) key.
type Service struct {
        cfg     Config
        ttl     time.Duration
        client  *http.Client

        mu      sync.Mutex
        cached  *Forecast
        cachedAt time.Time
}

// NewService builds a weather service for the given config. The TTL defaults
// to DefaultTTL when zero. The HTTP client defaults to util.DefaultHTTPClient
// when nil so a noisily-instrumented test client can be injected instead.
func NewService(cfg Config, ttl time.Duration, client *http.Client) *Service {
        if ttl <= 0 {
                ttl = DefaultTTL
        }
        if client == nil {
                c := *util.DefaultHTTPClient()
                client = &c
        }
        return &Service{cfg: cfg, ttl: ttl, client: client}
}

// UpdateConfig replaces the in-memory config and invalidates the cache. Safe
// to call from a goroutine while a Forecast() call is in flight.
func (s *Service) UpdateConfig(cfg Config) {
        s.mu.Lock()
        defer s.mu.Unlock()
        if cfg.Latitude != s.cfg.Latitude || cfg.Longitude != s.cfg.Longitude || cfg.Enabled != s.cfg.Enabled {
                s.cached = nil
                s.cachedAt = time.Time{}
        }
        s.cfg = cfg
}

// Forecast returns a cached forecast when fresh, or fetches a new one. A
// fetch error returns the cached value (if any) plus the error so callers
// can decide whether to surface it.
func (s *Service) Forecast(ctx context.Context) (*Forecast, error) {
        if !s.cfg.IsConfigured() {
                return nil, ErrNotConfigured
        }
        s.mu.Lock()
        cached, age := s.cached, time.Since(s.cachedAt)
        s.mu.Unlock()
        if cached != nil && age < s.ttl {
                return cached, nil
        }
        fc, err := fetchForecast(ctx, s.client, s.cfg)
        if err != nil {
                if cached != nil {
                        return cached, err
                }
                return nil, err
        }
        s.mu.Lock()
        s.cached = fc
        s.cachedAt = time.Now()
        s.mu.Unlock()
        return fc, nil
}

// SystemMessage renders the forecast as a one-line-per-day block suitable for
// injection as a system message in the agent context. Returns "" when the
// service is not configured (so the caller can simply prepend the result).
func (s *Service) SystemMessage(ctx context.Context) string {
        fc, err := s.Forecast(ctx)
        if err != nil || fc == nil || len(fc.Daily.Time) == 0 {
                return ""
        }
        var b strings.Builder
        location := s.cfg.Name
        if location == "" {
                location = fmt.Sprintf("%.3f,%.3f", s.cfg.Latitude, s.cfg.Longitude)
        }
        b.WriteString("This week's weather forecast for ")
        b.WriteString(location)
        if fc.Timezone != "" {
                b.WriteString(" (")
                b.WriteString(fc.Timezone)
                b.WriteString(")")
        }
        b.WriteString(":\n")
        // Open-Meteo returns 7 days by default. Show up to 7.
        days := len(fc.Daily.Time)
        if days > 7 {
                days = 7
        }
        for i := 0; i < days; i++ {
                day := fc.Daily.Time[i]
                hi := fc.Daily.Temperature2mMax[i]
                lo := fc.Daily.Temperature2mMin[i]
                code := fc.Daily.Weathercode[i]
                precip := fc.Daily.PrecipitationSum[i]
                wind := fc.Daily.WindSpeed10mMax[i]
                // Short weekday: parse "2006-01-02" → "Mon".
                weekday := ""
                if t, err := time.Parse("2006-01-02", day); err == nil {
                        weekday = t.Format("Mon")
                }
                fmt.Fprintf(&b, "- %s %s: %s, high %.0f°C / low %.0f°C", day, weekday, WeatherLabel(code), hi, lo)
                if precip > 0 {
                        fmt.Fprintf(&b, ", %.1fmm precip", precip)
                }
                if wind > 30 {
                        fmt.Fprintf(&b, ", wind %.0fkm/h", wind)
                }
                b.WriteByte('\n')
        }
        return b.String()
}

// fetchForecast calls the Open-Meteo API. The request is bounded by ctx.
func fetchForecast(ctx context.Context, client *http.Client, cfg Config) (*Forecast, error) {
        url := fmt.Sprintf(
                "%s?latitude=%.4f&longitude=%.4f&daily=temperature_2m_max,temperature_2m_min,weathercode,precipitation_sum,wind_speed_10m_max&timezone=auto&forecast_days=7",
                openMeteoForecastURL, cfg.Latitude, cfg.Longitude,
        )
        req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
        if err != nil {
                return nil, fmt.Errorf("weather: build request: %w", err)
        }
        req.Header.Set("Accept", "application/json")
        resp, err := client.Do(req)
        if err != nil {
                return nil, fmt.Errorf("weather: fetch: %w", err)
        }
        defer resp.Body.Close()
        body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
        if err != nil {
                return nil, fmt.Errorf("weather: read body: %w", err)
        }
        if resp.StatusCode < 200 || resp.StatusCode >= 300 {
                return nil, fmt.Errorf("weather: open-meteo returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
        }
        var fc Forecast
        if err := json.Unmarshal(body, &fc); err != nil {
                return nil, fmt.Errorf("weather: decode: %w", err)
        }
        if len(fc.Daily.Time) == 0 {
                return nil, ErrEmptyForecast
        }
        return &fc, nil
}

// Errors. Distinct values so callers can switch on them.
var (
        ErrNotConfigured  = errors.New("weather: not configured (set enabled, latitude, longitude)")
        ErrEmptyForecast  = errors.New("weather: open-meteo returned no daily data")
)

// WeatherLabel maps a WMO weather code to a short human-readable label.
// Reference: https://open-meteo.com/en/docs (WMO Weather interpretation codes).
// We compress to a single short phrase per code so the system message stays
// tight (the agent reads this on every turn when the feature is enabled).
func WeatherLabel(code int) string {
        switch {
        case code == 0:
                return "clear"
        case code <= 3:
                return "partly cloudy"
        case code >= 45 && code <= 48:
                return "fog"
        case code >= 51 && code <= 57:
                return "drizzle"
        case code >= 61 && code <= 67:
                return "rain"
        case code >= 71 && code <= 77:
                return "snow"
        case code >= 80 && code <= 82:
                return "showers"
        case code >= 85 && code <= 86:
                return "snow showers"
        case code >= 95 && code <= 99:
                return "thunderstorm"
        }
        return fmt.Sprintf("code %d", code)
}
