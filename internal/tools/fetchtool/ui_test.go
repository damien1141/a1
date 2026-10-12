package fetchtool_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/damien1141/a1/internal/tools/fetchtool"
)

func TestSummaryFormat(t *testing.T) {
	got := fetchtool.Summary("https://example.com/docs", 200, 43520, 1200*time.Millisecond)
	assert.Equal(t, "200 OK · 42.5 KB · 1.2s · example.com", got,
		"the row reads as one glanceable line, not a page of headers")
}

func TestSummaryMilliseconds(t *testing.T) {
	got := fetchtool.Summary("https://a.test/x", 200, 100, 250*time.Millisecond)
	assert.Contains(t, got, "250ms")
}

func TestSummaryNon200(t *testing.T) {
	got := fetchtool.Summary("https://a.test/x", 404, 12, 5*time.Millisecond)
	assert.Contains(t, got, "404 Not Found", "the status stays visible")
}

func TestSummaryNoResponse(t *testing.T) {
	got := fetchtool.Summary("https://a.test/x", 0, 0, 0)
	assert.Contains(t, got, "no response")
}

func TestHostOfStripsPathAndPort(t *testing.T) {
	assert.Equal(t, "example.com", fetchtool.HostOf("https://example.com/a/b?c=d"))
	assert.Equal(t, "example.com", fetchtool.HostOf("http://example.com:8080/x"))
	assert.Equal(t, "example.com", fetchtool.HostOf("example.com/x"))
	assert.Equal(t, "example.com", fetchtool.HostOf("https://user:pw@example.com/y"))
}

func TestRunningNamesHost(t *testing.T) {
	assert.Equal(t, "Fetching example.com…", fetchtool.Running("https://example.com/a"))
	assert.Equal(t, "Fetching…", fetchtool.Running(""))
}
