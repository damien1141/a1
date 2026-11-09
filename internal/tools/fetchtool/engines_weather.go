package fetchtool

import (
        "context"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "net/url"
        "regexp"
        "strings"
        "sync"
        "time"

        "github.com/damien1141/a1/internal/tools/tooldef"
)

// engines_weather.go — the weather subtool.
//
// Weather used to ride the runtime-context rider: a forecast block injected
// on every turn, configured (enabled + lat/lon) through the config editor.
// That shape cost context forever (the block froze into history at read
// price each turn) and needed configuration nobody wanted to do. As a fetch
// engine it costs nothing until the model actually asks: the query carries
// the place ("weather in Reykjavik"), and a placeless query auto-locates
// via IP geolocation — no config, no API keys, it just works.
//
// All three upstreams are free and keyless:
//   - Open-Meteo geocoding  — place name → lat/lon/timezone
//   - Open-Meteo forecast   — lat/lon → 7-day daily forecast
//   - ipwho.is / ipapi.co   — the requester's approximate lat/lon + city
//     (IP geolocation does reveal the requester's IP to those services,
//     same as any forecast request already does; it only runs when the
//     query names no place).

// TTLs and cache caps. The geocode + IP caches are package-level because
// the time engine shares them (zone lookups geocode the same way); the
// forecast cache lives on the engine, which is the only consumer.
const (
        weatherForecastTTL = 30 * time.Minute // Open-Meteo refreshes hourly
        weatherGeoTTL      = 24 * time.Hour   // places do not move
        weatherIPTTL       = 6 * time.Hour    // neither do routers, usually
        weatherCacheCap    = 64               // bounded memory; reset when full
)

// Upstream endpoints. Vars (not consts) so tests can point them at
// httptest servers, exactly like the old weather package did.
var (
        openMeteoForecastURL = "https://api.open-meteo.com/v1/forecast"
        openMeteoGeocodeURL  = "https://geocoding-api.open-meteo.com/v1/search"
        // ipGeoProviders are tried in order until one answers. ipwho.is first
        // (no rate limit published for light use), ipapi.co as the fallback
        // (1k/day free tier). Both are keyless and HTTPS.
        ipGeoProviders = []string{"https://ipwho.is/", "https://ipapi.co/json/"}
)

// weatherGeo is one resolved place: coordinates, a display label, the IANA
// timezone (Open-Meteo geocoding returns it, and the time engine needs it
// for "time in Tokyo" style queries), and the moment it was resolved (the
// cache TTL check).
type weatherGeo struct {
        lat      float64
        lon      float64
        label    string
        timezone string
        at       time.Time
}

// geocodeCache is the shared name → place cache (weather + time engines).
// A plain map with a timestamp per entry; the cap resets it wholesale —
// places are cheap to re-resolve and 64 covers any realistic session.
var geocodeCache = struct {
        sync.Mutex
        entries map[string]weatherGeo
}{entries: map[string]weatherGeo{}}

// ipLocateCache is the single auto-location result. One machine is one
// place; the TTL just keeps a long session from trusting a stale lookup.
var ipLocateCache = struct {
        sync.Mutex
        geo weatherGeo
        at  time.Time
}{}

// weatherEngine answers weather queries with a direct Open-Meteo forecast.
type weatherEngine struct {
        mu       sync.Mutex
        forecast map[string]cachedForecast
}

// cachedForecast is one forecast under its "lat,lon" key.
type cachedForecast struct {
        fc  *weatherForecast
        at  time.Time
}

func newWeatherEngine() *weatherEngine {
        return &weatherEngine{forecast: map[string]cachedForecast{}}
}

func (*weatherEngine) Name() string { return "weather" }

// weatherQueryRe is the positive signal: the query asks about conditions,
// not about weather-as-a-topic. word-boundaries keep "tailwind" and
// "untime" out.
var weatherQueryRe = regexp.MustCompile(`(?i)` +
        `\b(?:weather|forecast|humidity|uv index|wind chill|heat index)\b` +
        `|\bhow (?:hot|cold|warm|windy|freezing)\b` +
        `|\bis it (?:raining|snowing|sunny|cloudy|foggy|windy|freezing)\b` +
        `|\b(?:temperature|temp)\s+(?:outside|today|tomorrow|tonight|right now)\b` +
        `|\b(?:rain|snow|showers?|storm)\s+(?:today|tomorrow|tonight|this week|this weekend|expected)\b` +
        `|\bwind\s+speed\b`)

// weatherDevSignalRe is the negative signal: the query is about BUILDING
// with weather data, not knowing it. Without this, "open-meteo api docs"
// would get a forecast instead of the documentation the model asked for.
var weatherDevSignalRe = regexp.MustCompile(`(?i)` +
        `\b(?:api|apis|docs?|documentation|tutorial|example|examples|sdk|library|libraries|package|module|schema|github|source|code|json|endpoint)\b` +
        `|\bopen[- ]meteo\b`)

// Matches reports whether the query asks for weather. Two regexes because
// Go's RE2 has no lookahead: positive signal required, dev signal vetoed.
func (e *weatherEngine) Matches(query string) bool {
        return weatherQueryRe.MatchString(query) && !weatherDevSignalRe.MatchString(query)
}

// weatherLocTailRe captures the place after a preposition ("weather in
// Reykjavik", "how hot is it in tokyo"); weatherLocHeadRe the place before
// the keyword ("tokyo weather"). The tail regex anchors to end-of-query so
// "weather in the api docs" style middles do not half-match.
var (
        weatherLocTailRe = regexp.MustCompile(`(?i)\b(?:in|at|for|near|around)\s+([^?]+?)\s*[?.!]*$`)
        weatherLocHeadRe = regexp.MustCompile(`(?i)^(.{2,60}?)\s+(?:weather|forecast|temperature|humidity|climate)\b`)
)

// weatherQuestionWords are the tokens that mark a question head, not a
// place. The head-form regex ("tokyo weather") cannot tell "tokyo" from
// "what's the" on its own — this veto keeps "what's the weather" from
// geocoding a place called "what's the".
var weatherQuestionWords = map[string]bool{
        "what": true, "whats": true, "how": true, "is": true, "it": true,
        "the": true, "a": true, "tell": true, "show": true, "me": true,
        "like": true, "this": true, "that": true, "when": true, "where": true,
        "will": true, "be": true, "there": true, "going": true, "today": true,
        "tomorrow": true, "tonight": true, "outside": true, "current": true,
        "currently": true, "week": true, "weekend": true,
}

// looksLikePlace reports whether the candidate reads as a place name
// rather than a leftover question fragment.
func looksLikePlace(candidate string) bool {
        for _, tok := range strings.Fields(strings.ToLower(candidate)) {
                if weatherQuestionWords[tok] {
                        return false
                }
        }
        return true
}

// weatherStopwords are trailing time qualifiers and filler that ride along
// with the place ("weather in san francisco today") plus the words that
// mean "right here" — the caller should auto-locate instead of geocoding
// them. Ordered longest-first so "tokyo right now" sheds "right now" in
// one strip instead of "now" first and stranding "right".
var weatherStopwords = []string{
        "this weekend", "right now", "my location", "my area", "my city",
        "this week", "this area", "this city", "today", "tomorrow",
        "tonight", "currently", "outside", "please", "now", "here", "me",
}

// weatherLocation extracts the place name from a weather query, "" when the
// query names none (auto-locate) or names only a stopword.
func weatherLocation(query string) string {
        q := strings.TrimSpace(query)
        candidate := ""
        if m := weatherLocTailRe.FindStringSubmatch(q); len(m) == 2 {
                candidate = m[1]
        } else if m := weatherLocHeadRe.FindStringSubmatch(q); len(m) == 2 {
                candidate = m[1]
        } else {
                return ""
        }
        candidate = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(candidate), "?.!"))
        lower := strings.ToLower(candidate)
        for _, stop := range weatherStopwords {
                if lower == stop {
                        return ""
                }
                if strings.HasSuffix(lower, " "+stop) {
                        candidate = strings.TrimSpace(candidate[:len(candidate)-len(stop)])
                        lower = strings.ToLower(candidate)
                }
        }
        if len(candidate) < 2 || lower == "the" || lower == "it" {
                return ""
        }
        if len(candidate) > 60 {
                return ""
        }
        if !looksLikePlace(candidate) {
                return ""
        }
        return candidate
}

// Search resolves the place (query name → geocode, else IP auto-locate),
// fetches the forecast, and renders it as the direct answer.
func (e *weatherEngine) Search(ctx context.Context, query string, _ searchOptions) (*SearchResponse, error) {
        place := weatherLocation(query)
        var geo weatherGeo
        var err error
        if place != "" {
                geo, err = geocodePlace(ctx, place)
                if err != nil {
                        return nil, err
                }
        } else {
                geo, err = locateIP(ctx)
                if err != nil {
                        return nil, fmt.Errorf("weather: could not determine a location (%v); name one, e.g. \"weather in Reykjavik\"", err)
                }
        }
        fc, err := e.forecastFor(ctx, geo.lat, geo.lon)
        if err != nil {
                return nil, err
        }
        answer := renderWeather(fc, geo.label)
        sourceURL := fmt.Sprintf("%s?latitude=%.4f&longitude=%.4f&daily=weathercode,temperature_2m_max", openMeteoForecastURL, geo.lat, geo.lon)
        return &SearchResponse{
                Engine: e.Name(),
                Query:  query,
                Answer: answer,
                Sources: []tooldef.Source{{
                        Rank:       1,
                        URL:        sourceURL,
                        Title:      "Open-Meteo 7-day forecast · " + geo.label,
                        Host:       "open-meteo.com",
                        FaviconURL: formatFaviconURL("open-meteo.com"),
                        Snippet:    "Daily highs, lows, precipitation, and wind for " + geo.label,
                }},
        }, nil
}

// forecastFor returns the TTL-cached forecast for the coordinates, fetching
// a fresh one when the cache is empty or stale. (Not `forecast` — the engine's
// cache field owns that name.)
func (e *weatherEngine) forecastFor(ctx context.Context, lat, lon float64) (*weatherForecast, error) {
        key := fmt.Sprintf("%.4f,%.4f", lat, lon)
        e.mu.Lock()
        if c, ok := e.forecast[key]; ok && time.Since(c.at) < weatherForecastTTL {
                e.mu.Unlock()
                return c.fc, nil
        }
        e.mu.Unlock()

        fc, err := fetchWeatherForecast(ctx, lat, lon)
        if err != nil {
                // Degrade to the stale cache when the refetch fails — a forecast
                // from an hour ago beats no forecast.
                e.mu.Lock()
                if c, ok := e.forecast[key]; ok {
                        e.mu.Unlock()
                        return c.fc, nil
                }
                e.mu.Unlock()
                return nil, err
        }
        e.mu.Lock()
        if len(e.forecast) >= weatherCacheCap {
                e.forecast = map[string]cachedForecast{}
        }
        e.forecast[key] = cachedForecast{fc: fc, at: time.Now()}
        e.mu.Unlock()
        return fc, nil
}

// weatherForecast is the slice of the Open-Meteo /v1/forecast response we
// use: the daily block only (the hourly block is deliberately ignored to
// keep payloads small) plus the resolved timezone.
type weatherForecast struct {
        Daily struct {
                Time             []string  `json:"time"`
                Temperature2mMax []float64 `json:"temperature_2m_max"`
                Temperature2mMin []float64 `json:"temperature_2m_min"`
                Weathercode      []int     `json:"weathercode"`
                PrecipitationSum []float64 `json:"precipitation_sum"`
                WindSpeed10mMax  []float64 `json:"wind_speed_10m_max"`
        } `json:"daily"`
        Timezone string `json:"timezone"`
}

// geocodeResponse is the Open-Meteo geocoding search shape.
type geocodeResponse struct {
        Results []struct {
                Name      string  `json:"name"`
                Latitude  float64 `json:"latitude"`
                Longitude float64 `json:"longitude"`
                Country   string  `json:"country"`
                Admin1    string  `json:"admin1"`
                Timezone  string  `json:"timezone"`
        } `json:"results"`
}

// geocodePlace resolves a place name through the shared geocode cache.
// The label is "name, country" (admin1 when present) so the answer reads
// "Reykjavik, Iceland" rather than bare coordinates.
func geocodePlace(ctx context.Context, name string) (weatherGeo, error) {
        key := strings.ToLower(strings.TrimSpace(name))
        geocodeCache.Lock()
        if g, ok := geocodeCache.entries[key]; ok && time.Since(g.at) < weatherGeoTTL {
                geocodeCache.Unlock()
                return g, nil
        }
        geocodeCache.Unlock()

        var req *http.Request
        var err error
        endpoint := fmt.Sprintf("%s?name=%s&count=1&language=en&format=json", openMeteoGeocodeURL, url.QueryEscape(key))
        if req, err = http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil); err != nil {
                return weatherGeo{}, fmt.Errorf("weather: build geocode request: %w", err)
        }
        req.Header.Set("Accept", "application/json")
        client := &http.Client{Timeout: 10 * time.Second}
        resp, err := client.Do(req)
        if err != nil {
                return weatherGeo{}, fmt.Errorf("weather: geocode: %w", err)
        }
        defer resp.Body.Close()
        body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
        if err != nil {
                return weatherGeo{}, fmt.Errorf("weather: read geocode body: %w", err)
        }
        if resp.StatusCode < 200 || resp.StatusCode >= 300 {
                return weatherGeo{}, fmt.Errorf("weather: geocoding API returned %d", resp.StatusCode)
        }
        var gr geocodeResponse
        if err := json.Unmarshal(body, &gr); err != nil {
                return weatherGeo{}, fmt.Errorf("weather: decode geocode: %w", err)
        }
        if len(gr.Results) == 0 {
                return weatherGeo{}, fmt.Errorf("weather: no place found for %q", name)
        }
        r := gr.Results[0]
        label := r.Name
        if r.Admin1 != "" && !strings.EqualFold(r.Admin1, r.Name) {
                label += ", " + r.Admin1
        }
        if r.Country != "" {
                label += ", " + r.Country
        }
        geo := weatherGeo{lat: r.Latitude, lon: r.Longitude, label: label, timezone: r.Timezone, at: time.Now()}
        geocodeCache.Lock()
        if len(geocodeCache.entries) >= weatherCacheCap {
                geocodeCache.entries = map[string]weatherGeo{}
        }
        geocodeCache.entries[key] = geo
        geocodeCache.Unlock()
        return geo, nil
}

// ipLocateResponse is the union of the two IP-geolocation providers'
// payloads. ipwho.is answers {success, latitude, longitude, city, country};
// ipapi.co answers {latitude, longitude, city, country_name} — one struct
// with both country spellings keeps the parsing branch-free.
type ipLocateResponse struct {
        Success      bool    `json:"success"`
        Latitude     float64 `json:"latitude"`
        Longitude    float64 `json:"longitude"`
        City         string  `json:"city"`
        Country      string  `json:"country"`
        CountryName  string  `json:"country_name"`
}

// locateIP resolves the requester's approximate location, trying each
// provider in order until one answers. Result is cached for weatherIPTTL.
func locateIP(ctx context.Context) (weatherGeo, error) {
        ipLocateCache.Lock()
        cached := ipLocateCache.geo
        cachedAt := ipLocateCache.at
        ipLocateCache.Unlock()
        if cached.label != "" && time.Since(cachedAt) < weatherIPTTL {
                return cached, nil
        }

        var lastErr error
        for _, endpoint := range ipGeoProviders {
                req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
                if err != nil {
                        lastErr = err
                        continue
                }
                req.Header.Set("Accept", "application/json")
                client := &http.Client{Timeout: 10 * time.Second}
                resp, err := client.Do(req)
                if err != nil {
                        lastErr = err
                        continue
                }
                body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
                resp.Body.Close()
                if resp.StatusCode < 200 || resp.StatusCode >= 300 {
                        lastErr = fmt.Errorf("%s returned %d", endpoint, resp.StatusCode)
                        continue
                }
                if readErr != nil {
                        lastErr = readErr
                        continue
                }
                var lr ipLocateResponse
                if err := json.Unmarshal(body, &lr); err != nil {
                        lastErr = err
                        continue
                }
                // ipwho.is reports success:false on failure; ipapi.co has no such
                // field (zero value true is fine there — a real answer has coords).
                if lr.Latitude == 0 && lr.Longitude == 0 {
                        lastErr = fmt.Errorf("%s returned no coordinates", endpoint)
                        continue
                }
                label := strings.TrimSpace(lr.City)
                country := strings.TrimSpace(lr.Country)
                if country == "" {
                        country = strings.TrimSpace(lr.CountryName)
                }
                if label != "" && country != "" {
                        label += ", " + country
                }
                if label == "" {
                        label = fmt.Sprintf("%.3f,%.3f", lr.Latitude, lr.Longitude)
                }
                geo := weatherGeo{lat: lr.Latitude, lon: lr.Longitude, label: label, at: time.Now()}
                ipLocateCache.Lock()
                ipLocateCache.geo = geo
                ipLocateCache.at = time.Now()
                ipLocateCache.Unlock()
                return geo, nil
        }
        return weatherGeo{}, fmt.Errorf("all providers failed: %w", lastErr)
}

// fetchWeatherForecast calls the Open-Meteo forecast endpoint for the
// coordinates. Mirrors the old weather package's request exactly (same
// daily parameters, timezone=auto, 7 days) so answers keep the same shape.
func fetchWeatherForecast(ctx context.Context, lat, lon float64) (*weatherForecast, error) {
        url := fmt.Sprintf(
                "%s?latitude=%.4f&longitude=%.4f&daily=temperature_2m_max,temperature_2m_min,weathercode,precipitation_sum,wind_speed_10m_max&timezone=auto&forecast_days=7",
                openMeteoForecastURL, lat, lon,
        )
        req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
        if err != nil {
                return nil, fmt.Errorf("weather: build request: %w", err)
        }
        req.Header.Set("Accept", "application/json")
        client := &http.Client{Timeout: 10 * time.Second}
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
        var fc weatherForecast
        if err := json.Unmarshal(body, &fc); err != nil {
                return nil, fmt.Errorf("weather: decode: %w", err)
        }
        if len(fc.Daily.Time) == 0 {
                return nil, fmt.Errorf("weather: open-meteo returned no daily data")
        }
        return &fc, nil
}

// renderWeather renders the forecast as one line per day — the same
// compact shape the old rider injection used, now served on demand.
func renderWeather(fc *weatherForecast, label string) string {
        var b strings.Builder
        b.WriteString("This week's weather forecast for ")
        b.WriteString(label)
        if fc.Timezone != "" {
                b.WriteString(" (")
                b.WriteString(fc.Timezone)
                b.WriteString(")")
        }
        b.WriteString(":\n")
        // Parallel arrays: cap at the shortest so a truncated response cannot
        // panic the renderer.
        days := len(fc.Daily.Time)
        for _, n := range []int{
                len(fc.Daily.Temperature2mMax), len(fc.Daily.Temperature2mMin),
                len(fc.Daily.Weathercode), len(fc.Daily.PrecipitationSum),
                len(fc.Daily.WindSpeed10mMax),
        } {
                if n < days {
                        days = n
                }
        }
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
                weekday := ""
                if t, err := time.Parse("2006-01-02", day); err == nil {
                        weekday = t.Format("Mon")
                }
                fmt.Fprintf(&b, "- %s %s: %s, high %.0f°C / low %.0f°C", day, weekday, weatherLabel(code), hi, lo)
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

// weatherLabel maps a WMO weather code to a short human-readable label.
// Reference: https://open-meteo.com/en/docs (WMO Weather interpretation
// codes). Compressed to one short phrase per code so the answer stays
// tight. Ported from the old internal/weather package verbatim.
func weatherLabel(code int) string {
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
