package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math/rand"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// oracle: encoding/json round-trip plus a naive line-scanner reference — the
// stdio frame (payload + '\n') must decode, via a plain strings.Split of the
// stream, to exactly the hand-built request/notification value, and the
// payload must stay a single line so newline framing cannot split a frame.
func TestMarshalFramesVsJSONRoundTrip_Differential(t *testing.T) {
	payload, err := marshalRequest(42, "tools/call", map[string]any{
		"name":      "echo",
		"arguments": map[string]any{"msg": "hi\nthere\ttab"},
	})
	require.NoError(t, err)

	// Framing contract: no embedded newline (json escapes them); the trailing
	// '\n' added by the transport makes one frame per line.
	assert.NotContains(t, string(payload), "\n", "request payload must be single-line")
	frame := append(append([]byte(nil), payload...), '\n')

	var lines []string
	for _, ln := range strings.Split(strings.TrimSuffix(string(frame), "\n"), "\n") {
		if ln != "" {
			lines = append(lines, ln)
		}
	}
	require.Len(t, lines, 1, "one frame must yield one scanner line")

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &got))
	assert.Equal(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(42),
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "echo",
			"arguments": map[string]any{"msg": "hi\nthere\ttab"},
		},
	}, got, "decoded frame must equal the hand-built reference value")

	// Notifications carry no id at all (JSON-RPC 2.0).
	notif, err := marshalNotification("notifications/initialized", map[string]any{})
	require.NoError(t, err)
	var nmap map[string]any
	require.NoError(t, json.Unmarshal(notif, &nmap))
	assert.NotContains(t, nmap, "id", "notification must omit id")
	assert.Equal(t, "2.0", nmap["jsonrpc"])
	assert.Equal(t, "notifications/initialized", nmap["method"])
	assert.Equal(t, map[string]any{}, nmap["params"])

	// Request ids from a fresh counter are 1, 2, 3 — never reused on a stream.
	var counter atomic.Int64
	assert.Equal(t, int64(1), nextID(&counter))
	assert.Equal(t, int64(2), nextID(&counter))
	assert.Equal(t, int64(3), nextID(&counter))
}

// refExtractToolContent re-derives the tools/call content extraction with a
// generic map decode (the implementation uses a typed struct): concatenate
// text-typed parts, fall back to the raw body when nothing renders, and
// prefix "error: " when isError is set.
func refExtractToolContent(raw string) string {
	if raw == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return raw
	}
	var b strings.Builder
	if parts, ok := m["content"].([]any); ok {
		for _, part := range parts {
			pm, ok := part.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := pm["type"].(string)
			if typ == "text" || typ == "" {
				txt, _ := pm["text"].(string)
				b.WriteString(txt)
			}
		}
	}
	out := b.String()
	if out == "" {
		return raw
	}
	if isErr, _ := m["isError"].(bool); isErr {
		return "error: " + out
	}
	return out
}

// oracle: naive map-decode reference (above) over normal, edge, and hostile
// tools/call result bodies.
func TestExtractToolContentVsNaiveMapDecode_Differential(t *testing.T) {
	cases := []string{
		`{"content":[{"type":"text","text":"ok"}]}`,
		`{"content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}`,
		`{"content":[{"type":"image","data":"..."}]}`,
		`{"content":[{"type":"text","text":"x"},{"type":"image","data":"..."}]}`,
		`{"content":[{"text":"no type field"}]}`,
		`{"content":[]}`,
		`{"content":null}`,
		`{"isError":true,"content":[{"type":"text","text":"boom"}]}`,
		`{"isError":false,"content":[{"type":"text","text":"fine"}]}`,
		`{}`,
		`null`,
		`"scalar"`,
		`not json`,
		`{"content":"not an array"}`,
		`{"content":[{"type":"text","text":""}]}`,
		`  {"content":[{"type":"text","text":"padded"}]}  `,
	}
	for _, raw := range cases {
		assert.Equal(t, refExtractToolContent(raw), extractToolContent(json.RawMessage(raw)), "raw=%q", raw)
	}
	assert.Equal(t, "", refExtractToolContent(""), "empty raw")
	assert.Equal(t, "", extractToolContent(json.RawMessage("")), "empty raw")
}

// rpcCanonical is the comparison form for parse results: the pieces both the
// implementation and the reference must agree on, with RawMessage compacted
// so spacing differences cannot mask equality.
type rpcCanonical struct {
	ID           any
	Method       string
	Result       string
	HasError     bool
	ErrorCode    int
	ErrorMessage string
	ParseFailed  bool
}

func compactJSON(s string) string {
	if s == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(s)); err != nil {
		return s
	}
	return buf.String()
}

// refFrame is the reference's field probe for one JSON-RPC frame.
type refFrame struct {
	ID     any             `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (f refFrame) canonical() rpcCanonical {
	c := rpcCanonical{
		ID:       f.ID,
		Method:   f.Method,
		Result:   compactJSON(string(f.Result)),
		HasError: f.Error != nil,
	}
	if f.Error != nil {
		c.ErrorCode = f.Error.Code
		c.ErrorMessage = f.Error.Message
	}
	return c
}

// canonImpl converts the implementation's typed result for comparison.
func canonImpl(rpc jsonRPCResponse, parseErr error) rpcCanonical {
	c := rpcCanonical{
		ID:          rpc.ID,
		Method:      rpc.Method,
		Result:      compactJSON(string(rpc.Result)),
		HasError:    rpc.Error != nil,
		ParseFailed: parseErr != nil,
	}
	if rpc.Error != nil {
		c.ErrorCode = rpc.Error.Code
		c.ErrorMessage = rpc.Error.Message
	}
	return c
}

// refParseHTTPOrSSEBody re-derives the body parse independently of the
// implementation's strings.SplitSeq walk: a bufio.Scanner drives the line
// handling, and the frame-selection rule is re-stated from the documented
// contract — the first "data: " frame that has an id and no method wins;
// otherwise the first parseable data frame; otherwise the whole body parsed
// as plain JSON.
func refParseHTTPOrSSEBody(body []byte) rpcCanonical {
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var first rpcCanonical
	found := false
	for sc.Scan() {
		s := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(s, "data: ") {
			continue
		}
		var f refFrame
		if json.Unmarshal([]byte(s[len("data: "):]), &f) != nil {
			continue
		}
		cand := f.canonical()
		if !found {
			first, found = cand, true
		}
		if f.ID != nil && f.Method == "" {
			return cand
		}
	}
	if found {
		return first
	}
	var f refFrame
	if err := json.Unmarshal(body, &f); err != nil {
		return rpcCanonical{ParseFailed: true}
	}
	return f.canonical()
}

// oracle: naive scanner reference (above) over randomized SSE streams and
// hand-picked edge bodies — notification interleaving, garbage data lines,
// plain JSON, and unparseable input must all agree frame for frame.
func TestParseHTTPOrSSEBodyVsScannerReference_Differential(t *testing.T) {
	fixed := []string{
		// Plain JSON, no SSE at all.
		`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`,
		// Notification then response.
		"data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{}}\n\n" +
			"data: {\"jsonrpc\":\"2.0\",\"id\":3,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n\n",
		// Error response frame after a notification.
		"data: {\"method\":\"notifications/message\"}\n" +
			"data: {\"jsonrpc\":\"2.0\",\"id\":7,\"error\":{\"code\":-32601,\"message\":\"no such tool\"}}\n",
		// Garbage data line between frames must be skipped.
		"data: {{{not json\n" +
			"data: {\"jsonrpc\":\"2.0\",\"id\":9,\"result\":42}\n",
		// Only notifications: first frame kept.
		"data: {\"method\":\"a\"}\ndata: {\"method\":\"b\"}\n",
		// Non-object data payloads are not frames.
		"data: 42\ndata: \"str\"\ndata: null\n",
		// "data:" without the space is not an SSE data line.
		"data:{\"id\":5}\n",
		// Response before more notifications: first response wins immediately.
		"data: {\"id\":1,\"result\":\"first\"}\ndata: {\"method\":\"late\"}\n",
		// Whitespace-padded data lines still count.
		"event: message\n   data:   {\"id\":2,\"result\":\"padded\"}\n\n",
		// Unparseable everywhere.
		"event: message\ndata: not json\n",
		"",
		"\n\n\n",
	}

	rng := rand.New(rand.NewSource(7))
	blocks := []func() string{
		func() string { return "event: message\n" },
		func() string { return ": comment stream\n" },
		func() string {
			return "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{\"n\":1}}\n\n"
		},
		func() string {
			return "data: {\"jsonrpc\":\"2.0\",\"id\":11,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"hi\"}]}}\n\n"
		},
		func() string {
			return "data: {\"jsonrpc\":\"2.0\",\"id\":12,\"error\":{\"code\":-32000,\"message\":\"srv\"}}\n\n"
		},
		func() string { return "data: totally not json\n\n" },
		func() string { return "data: [1,2,3]\n\n" },
		func() string { return "garbage line\n" },
	}
	random := make([]string, 0, 120)
	for range 120 {
		var b strings.Builder
		for n := rng.Intn(5); n > 0; n-- {
			b.WriteString(blocks[rng.Intn(len(blocks))]())
		}
		b.WriteString(`{"jsonrpc":"2.0","id":99,"result":{"tools":[]}}`) // plain-JSON tail
		random = append(random, b.String())
	}

	for _, body := range append(fixed, random...) {
		got, err := parseHTTPOrSSEBody([]byte(body))
		assert.Equal(t, refParseHTTPOrSSEBody([]byte(body)), canonImpl(got, err), "body=%q", body)
	}
}

// oracle: stdlib regexp equivalence — sanitizeName must equal a single
// [^A-Za-z0-9_-] → "_" replacement pass, with an empty result mapped to
// "server".
func TestSanitizeNameVsRegexp_Differential(t *testing.T) {
	re := regexp.MustCompile(`[^A-Za-z0-9_-]`)
	ref := func(name string) string {
		out := re.ReplaceAllString(name, "_")
		if out == "" {
			return "server"
		}
		return out
	}

	fixed := []string{
		"", "alpha", "server-1_2", "a.b/c", "My Server!", "日本語",
		"with space", "tab\tname", "dots...", "a+b=c", "!---", "café",
	}
	alphabet := []string{"a", "Z", "9", "-", "_", ".", "/", " ", "!", "é", "日"}
	rng := rand.New(rand.NewSource(11))
	inputs := append([]string{}, fixed...)
	for range 200 {
		var b strings.Builder
		for n := rng.Intn(12); n > 0; n-- {
			b.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		inputs = append(inputs, b.String())
	}
	for _, name := range inputs {
		assert.Equal(t, ref(name), sanitizeName(name), "name=%q", name)
	}
}
