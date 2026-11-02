package fetchtool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/tools/tooldef"
)

// frankfurterEngine converts currency queries via the free
// https://api.frankfurter.app API. No API key, no captcha, instant
// response. The API is a thin wrapper around the ECB reference rates
// published every business day around 16:00 CET.
//
// Query shapes we accept:
//   - "100 usd to eur"
//   - "convert 50 gbp to jpy"
//   - "100 USD → EUR"
//   - "100 USD in EUR"
//
// The Matches() predicate is a tight regex; when it returns true we
// almost certainly have a real currency conversion request and the
// API will give us a definitive answer.
type frankfurterEngine struct{}

func newFrankfurterEngine() *frankfurterEngine { return &frankfurterEngine{} }

func (frankfurterEngine) Name() string { return "frankfurter" }

// currencyQueryRe captures the amount, source currency, and target
// currency from natural-language conversion queries. Case-insensitive
// via the (?i) flag. Currencies are 3-letter ISO 4217 codes (USD, EUR,
// GBP, JPY, CNY, etc.).
//
// Examples that match:
//   - "100 usd to eur"
//   - "Convert 50 GBP to JPY"
//   - "100 USD → EUR"
//   - "100 USD in EUR"
//   - "how much is 100 usd in eur"
//
// Examples that don't match (correctly):
//   - "what is the euro" (no amount + 3-letter code + target)
//   - "usd currency" (no amount)
//   - "100 dollars to euros" (uses names, not ISO codes — too loose)
var currencyQueryRe = regexp.MustCompile(`(?i)\b(\d+(?:\.\d+)?)\s+([A-Z]{3})\s*(?:to|in|→|->|=>)\s*([A-Z]{3})\b`)

func (frankfurterEngine) Matches(query string) bool {
	return currencyQueryRe.MatchString(query)
}

// frankfurterLatestResponse is the JSON shape of the
// https://api.frankfurter.app/latest?from=USD&to=EUR&amount=100
// endpoint. The "rates" map always contains exactly one entry (the
// target currency) keyed by ISO code.
type frankfurterLatestResponse struct {
	Amount   float64           `json:"amount"`
	Base     string            `json:"base"`
	Date     string            `json:"date"` // YYYY-MM-DD
	Rates    map[string]float64 `json:"rates"`
}

func (e *frankfurterEngine) Search(ctx context.Context, query string, _ searchOptions) (*SearchResponse, error) {
	matches := currencyQueryRe.FindStringSubmatch(query)
	if len(matches) < 4 {
		// Defensive — Matches() returned true but the regex didn't
		// actually capture. Should never happen, but degrade to
		// "no results" rather than panic.
		return &SearchResponse{Engine: e.Name(), Query: query}, nil
	}
	amountStr, fromCur, toCur := matches[1], strings.ToUpper(matches[2]), strings.ToUpper(matches[3])
	if fromCur == toCur {
		// "100 USD to USD" — the API would return 100. Save the call.
		return &SearchResponse{
			Engine: e.Name(),
			Query:  query,
			Answer: fmt.Sprintf("%s %s = %s %s (same currency)", amountStr, fromCur, amountStr, toCur),
		}, nil
	}
	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		return &SearchResponse{Engine: e.Name(), Query: query}, nil
	}

	// Build the Frankfurter API URL.
	endpoint := fmt.Sprintf("https://api.frankfurter.app/latest?from=%s&to=%s&amount=%s",
		fromCur, toCur, amountStr)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("frankfurter: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("frankfurter: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16)) // 64 KiB cap — response is tiny
	if err != nil {
		return nil, err
	}
	var latest frankfurterLatestResponse
	if err := json.Unmarshal(body, &latest); err != nil {
		return nil, fmt.Errorf("frankfurter: parse response: %w", err)
	}
	converted, ok := latest.Rates[toCur]
	if !ok {
		return nil, fmt.Errorf("frankfurter: no rate for %s in response", toCur)
	}
	rate := converted / amount // per-unit rate, for the answer line

	answer := fmt.Sprintf("%s %s = %.2f %s (rate: %.4f, as of %s)",
		amountStr, fromCur, converted, toCur, rate, latest.Date)

	// One source: the Frankfurter page for this conversion. Lets the
	// user click through to see the rate history + chart.
	sourceURL := fmt.Sprintf("https://www.frankfurter.app/%s..%s", fromCur, toCur)
	sources := []tooldef.Source{
		{
			Rank:       1,
			URL:        sourceURL,
			Title:      fmt.Sprintf("%s/%s exchange rate history", fromCur, toCur),
			Host:       "frankfurter.app",
			FaviconURL: formatFaviconURL("frankfurter.app"),
			Snippet:    fmt.Sprintf("1 %s = %.4f %s (latest ECB reference rate, %s)", fromCur, rate, toCur, latest.Date),
		},
	}

	return &SearchResponse{
		Engine:  e.Name(),
		Query:   query,
		Answer:  answer,
		Sources: sources,
	}, nil
}
