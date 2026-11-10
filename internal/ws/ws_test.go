package ws

import (
	"bufio"
	"context"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- a server-side websocket, for tests of the client ----

// serverWS is the server half of one connection: it completes the
// upgrade handshake and reads/writes raw frames. Its frame reader is
// deliberately independent of ws.go — it decodes headers per the RFC and
// unmasks with the frame's own key, which is exactly the oracle the
// differentials need (the wire verified against the spec, not against
// another run of the same code).
type serverWS struct {
	conn net.Conn
	br   *bufio.Reader
}

// upgrade completes the server side of the opening handshake on a
// hijacked connection. wrongAccept corrupts the accept key so the
// negative test can watch the client refuse it.
func upgrade(w http.ResponseWriter, r *http.Request, wrongAccept bool) (*serverWS, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("no Upgrade header")
	}
	if r.Header.Get("Sec-WebSocket-Key") == "" || r.Header.Get("Sec-WebSocket-Version") != "13" {
		return nil, errors.New("bad handshake headers")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("handler cannot hijack")
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + GUID))
	accept := base64.StdEncoding.EncodeToString(sum[:])
	if wrongAccept {
		accept = base64.StdEncoding.EncodeToString([]byte("definitely not it"))
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := conn.Write([]byte(resp)); err != nil {
		return nil, err
	}
	_ = brw.Flush()
	return &serverWS{conn: conn, br: bufio.NewReader(brw.Reader)}, nil
}

// srvFrame is one client frame as the server sees it: the parsed fields
// plus the raw 7-bit length byte, so tests can pin the minimal encoding
// the RFC requires.
type srvFrame struct {
	fin     bool
	opcode  byte
	masked  bool
	lenByte byte
	payload []byte
}

// readFrame reads one client frame: masked (it must be), unmasked here
// with the frame's own key.
func (s *serverWS) readFrame() (srvFrame, error) {
	var h [2]byte
	if _, err := io.ReadFull(s.br, h[:]); err != nil {
		return srvFrame{}, err
	}
	f := srvFrame{fin: h[0]&0x80 != 0, opcode: h[0] & 0x0F, masked: h[1]&0x80 != 0, lenByte: h[1] & 0x7F}
	length := int64(f.lenByte)
	switch f.lenByte {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(s.br, ext[:]); err != nil {
			return srvFrame{}, err
		}
		length = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(s.br, ext[:]); err != nil {
			return srvFrame{}, err
		}
		length = int64(binary.BigEndian.Uint64(ext[:]))
	}
	var key [4]byte
	if f.masked {
		if _, err := io.ReadFull(s.br, key[:]); err != nil {
			return srvFrame{}, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(s.br, payload); err != nil {
		return srvFrame{}, err
	}
	if f.masked {
		for i := range payload {
			payload[i] ^= key[i%4]
		}
	}
	f.payload = payload
	return f, nil
}

// writeFrame writes one unmasked server frame. fin and opcode are the
// caller's: tests use them to build fragments and control frames.
func (s *serverWS) writeFrame(fin bool, opcode byte, payload []byte) error {
	var hdr [10]byte
	hdr[0] = byte(opcode)
	if fin {
		hdr[0] |= 0x80
	}
	pos := 2
	switch {
	case len(payload) < 126:
		hdr[1] = byte(len(payload))
	case len(payload) <= 0xFFFF:
		hdr[1] = 126
		binary.BigEndian.PutUint16(hdr[2:4], uint16(len(payload)))
		pos = 4
	default:
		hdr[1] = 127
		binary.BigEndian.PutUint64(hdr[2:10], uint64(len(payload)))
		pos = 10
	}
	if _, err := s.conn.Write(hdr[:pos]); err != nil {
		return err
	}
	_, err := s.conn.Write(payload)
	return err
}

func (s *serverWS) writeText(b []byte) error { return s.writeFrame(true, opText, b) }

// writeMaskedViolently writes a MASKED server frame — a protocol
// violation the client must refuse, not parse.
func (s *serverWS) writeMaskedViolently(opcode byte, payload []byte) error {
	var hdr [6]byte
	hdr[0] = 0x80 | opcode
	hdr[1] = 0x80 | byte(len(payload))
	key := [4]byte{1, 2, 3, 4}
	copy(hdr[2:6], key[:])
	masked := make([]byte, len(payload))
	for i, b := range payload {
		masked[i] = b ^ key[i%4]
	}
	if _, err := s.conn.Write(hdr[:]); err != nil {
		return err
	}
	_, err := s.conn.Write(masked)
	return err
}

// wsServer runs one httptest server whose handler upgrades one
// connection and hands it to handle. The handler also gets a done
// channel, closed at cleanup, for "hold the connection open" shapes.
// Assertions inside handle run on the server's goroutine: use assert
// (t.Errorf is goroutine-safe), never require — FailNow from a non-test
// goroutine is a runtime.Goexit the test cannot see.
type wsServer struct {
	srv     *httptest.Server
	mu      sync.Mutex
	request *http.Request

	wrongAccept bool
	noUpgrade   bool
}

func newWSServer(t *testing.T, handle func(*serverWS, <-chan struct{})) *wsServer {
	t.Helper()
	done := make(chan struct{})
	w := &wsServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		w.request = r.Clone(r.Context())
		w.mu.Unlock()
		if w.noUpgrade {
			rw.WriteHeader(http.StatusNotFound)
			return
		}
		s, err := upgrade(rw, r, w.wrongAccept)
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
		handle(s, done)
		// The connection is the test's now; nothing more to serve.
	})
	w.srv = httptest.NewServer(mux)
	t.Cleanup(func() {
		w.srv.Close()
		close(done)
	})
	return w
}

func (w *wsServer) url() string { return "ws" + strings.TrimPrefix(w.srv.URL, "http") }

func (w *wsServer) lastRequest() *http.Request {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.request
}

// dial wires the standard client dial against the server.
func dial(t *testing.T, ctx context.Context, url string, opts *Options) *Conn {
	t.Helper()
	c, err := Dial(ctx, url, nil, opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// ---- the trio ----

// TestDialHandshakeAndEchoRoundTrip is the happy path through a real
// socket: upgrade, one message each way, and the handshake's own
// headers pinned on the request the server saw.
func TestDialHandshakeAndEchoRoundTrip(t *testing.T) {
	srv := newWSServer(t, func(s *serverWS, _ <-chan struct{}) {
		for {
			f, err := s.readFrame()
			if err != nil {
				return
			}
			if err := s.writeFrame(f.fin, f.opcode, f.payload); err != nil {
				return
			}
		}
	})
	ctx := context.Background()
	c := dial(t, ctx, srv.url(), nil)

	require.NoError(t, c.Send([]byte(`{"hello":"world"}`)))
	got, err := c.Receive(ctx)
	require.NoError(t, err)
	assert.Equal(t, `{"hello":"world"}`, string(got))

	req := srv.lastRequest()
	require.NotNil(t, req)
	assert.Equal(t, "/", req.URL.Path)
	assert.Equal(t, "websocket", strings.ToLower(req.Header.Get("Upgrade")))
	assert.Equal(t, "13", req.Header.Get("Sec-WebSocket-Version"))
}

// TestDialCarriesCustomHeaders pins that caller headers ride the
// handshake; the handshake's own keys stay ours.
func TestDialCarriesCustomHeaders(t *testing.T) {
	srv := newWSServer(t, func(s *serverWS, _ <-chan struct{}) { <-make(chan struct{}) })
	h := http.Header{}
	h.Set("Authorization", "Bearer tok-1")
	h.Set("X-Custom", "v")
	c, err := Dial(context.Background(), srv.url(), h, nil)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	req := srv.lastRequest()
	require.NotNil(t, req)
	assert.Equal(t, "Bearer tok-1", req.Header.Get("Authorization"))
	assert.Equal(t, "v", req.Header.Get("X-Custom"))
}

// wireVerdict is the server-side oracle's report on one client frame.
type wireVerdict struct {
	f   srvFrame
	err error
}

// TestSendMasksAndEncodesLengthsMinimal_Differential is the wire
// differential: every outgoing frame must be masked (RFC 6455 §5.3),
// unmask with its own key must be the identity (the server's reader is
// the independent oracle), and the length field must use the minimal
// encoding the RFC pins (<126 in the 7-bit field, ≤65535 in the 16-bit
// word, beyond in the 64-bit word) — with the echo proving the client
// parses the same band on the way back in.
func TestSendMasksAndEncodesLengthsMinimal_Differential(t *testing.T) {
	sizes := []int{0, 1, 5, 124, 125, 126, 127, 1024, 65534, 65535, 65536, 65537}
	verdicts := make(chan wireVerdict, len(sizes))
	srv := newWSServer(t, func(s *serverWS, _ <-chan struct{}) {
		for range sizes {
			f, err := s.readFrame()
			if err != nil {
				verdicts <- wireVerdict{err: err}
				return
			}
			verdicts <- wireVerdict{f: f}
			if err := s.writeFrame(f.fin, f.opcode, f.payload); err != nil {
				return
			}
		}
	})
	c := dial(t, context.Background(), srv.url(), nil)

	original := func(n int) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(i % 251) // deterministic, non-trivial bytes
		}
		return b
	}
	for _, n := range sizes {
		require.NoError(t, c.Send(original(n)), "send of %d bytes", n)

		v := <-verdicts
		require.NoError(t, v.err, "server read of a %d byte send", n)
		assert.True(t, v.f.masked, "frame of %d bytes must be masked", n)
		assert.True(t, v.f.fin, "data sends are never fragmented")
		assert.Equal(t, byte(opText), v.f.opcode)
		assert.Equal(t, original(n), v.f.payload, "unmask(mask(payload)) must be the identity for %d bytes", n)
		var wantLen byte
		switch {
		case n < 126:
			wantLen = byte(n)
		case n <= 0xFFFF:
			wantLen = 126
		default:
			wantLen = 127
		}
		assert.Equal(t, wantLen, v.f.lenByte, "length encoding for %d bytes", n)

		got, err := c.Receive(context.Background())
		require.NoError(t, err, "echo of %d bytes", n)
		assert.Equal(t, original(n), got, "echo round trip for %d bytes", n)
	}
}

// TestReceiveAssemblesFragmentedMessages pins the continuation path:
// one message split across frames arrives as one message.
func TestReceiveAssemblesFragmentedMessages(t *testing.T) {
	srv := newWSServer(t, func(s *serverWS, done <-chan struct{}) {
		assert.NoError(t, s.writeFrame(false, opText, []byte(`{"hello":`)))
		assert.NoError(t, s.writeFrame(false, opContinuation, []byte(`"wor`)))
		assert.NoError(t, s.writeFrame(true, opContinuation, []byte(`ld"}`)))
		<-done
	})
	c := dial(t, context.Background(), srv.url(), nil)

	got, err := c.Receive(context.Background())
	require.NoError(t, err)
	assert.Equal(t, `{"hello":"world"}`, string(got))
}

// TestReceiveAnswersPingWithPong pins the automatic pong: a ping never
// surfaces to the Receive caller, and the reply carries the payload.
func TestReceiveAnswersPingWithPong(t *testing.T) {
	pongs := make(chan []byte, 1)
	srv := newWSServer(t, func(s *serverWS, done <-chan struct{}) {
		assert.NoError(t, s.writeFrame(true, opPing, []byte("heartbeat")))
		f, err := s.readFrame()
		if err != nil {
			return
		}
		pongs <- f.payload
		assert.NoError(t, s.writeText([]byte("after")))
		<-done
	})
	c := dial(t, context.Background(), srv.url(), nil)

	got, err := c.Receive(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "after", string(got), "the ping must not surface as a message")

	select {
	case p := <-pongs:
		assert.Equal(t, []byte("heartbeat"), p, "pong payload must echo the ping's")
	case <-time.After(3 * time.Second):
		t.Error("no pong arrived")
	}
}

// TestServerCloseSurfacesCloseError pins the close handshake: a
// server-initiated close becomes a *CloseError with the code and reason,
// and terminal is sticky.
func TestServerCloseSurfacesCloseError(t *testing.T) {
	srv := newWSServer(t, func(s *serverWS, _ <-chan struct{}) {
		payload := make([]byte, 2+len("policy violation"))
		binary.BigEndian.PutUint16(payload[:2], 1008)
		copy(payload[2:], "policy violation")
		assert.NoError(t, s.writeFrame(true, opClose, payload))
		_, _ = s.readFrame() // the client's close reply
	})
	c := dial(t, context.Background(), srv.url(), nil)

	_, err := c.Receive(context.Background())
	require.Error(t, err)
	var ce *CloseError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, uint16(1008), ce.Code)
	assert.Equal(t, "policy violation", ce.Reason)

	_, err = c.Receive(context.Background())
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, uint16(1008), ce.Code)
}

// TestLocalCloseSendsCloseFrameAndStops pins Close: one masked close
// frame with 1000, then ErrClosed for everyone, idempotently.
func TestLocalCloseSendsCloseFrameAndStops(t *testing.T) {
	srv := newWSServer(t, func(s *serverWS, _ <-chan struct{}) {
		f, err := s.readFrame()
		if err != nil {
			return
		}
		assert.Equal(t, byte(opClose), f.opcode)
		assert.True(t, f.masked, "the client's close frame must be masked")
		require.GreaterOrEqual(t, len(f.payload), 2)
		assert.Equal(t, uint16(1000), binary.BigEndian.Uint16(f.payload[:2]))
	})
	c := dial(t, context.Background(), srv.url(), nil)

	require.NoError(t, c.Close())
	assert.ErrorIs(t, c.Send([]byte("x")), ErrClosed)
	_, err := c.Receive(context.Background())
	assert.ErrorIs(t, err, ErrClosed)
	require.NoError(t, c.Close())
}

// TestDialRejectsNon101Status pins the handshake negative: a plain HTTP
// answer is an error naming the status, never a half-open connection.
func TestDialRejectsNon101Status(t *testing.T) {
	srv := newWSServer(t, func(s *serverWS, _ <-chan struct{}) {})
	srv.noUpgrade = true
	_, err := Dial(context.Background(), srv.url(), nil, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "404")
}

// TestDialRejectsWrongAcceptKey pins the security check: a 101 with a
// wrong Sec-WebSocket-Accept is refused.
func TestDialRejectsWrongAcceptKey(t *testing.T) {
	srv := newWSServer(t, func(s *serverWS, _ <-chan struct{}) {})
	srv.wrongAccept = true
	_, err := Dial(context.Background(), srv.url(), nil, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, "Sec-WebSocket-Accept")
}

// TestDialRejectsNonWebsocketURL pins the URL contract.
func TestDialRejectsNonWebsocketURL(t *testing.T) {
	for _, raw := range []string{"http://example.invalid/", "https://example.invalid/", "ftp://example.invalid/"} {
		_, err := Dial(context.Background(), raw, nil, nil)
		require.Error(t, err, raw)
		assert.ErrorContains(t, err, "scheme")
	}
}

// TestReceiveContextCancelReturnsPromptly pins that Receive honors ctx,
// not just the socket.
func TestReceiveContextCancelReturnsPromptly(t *testing.T) {
	srv := newWSServer(t, func(s *serverWS, done <-chan struct{}) { <-done })
	c := dial(t, context.Background(), srv.url(), nil)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := c.Receive(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestIdleDeadlineTearsDownDeadPeers pins the liveness ruling: a silent
// server inside IdleTimeout kills the connection with an error.
func TestIdleDeadlineTearsDownDeadPeers(t *testing.T) {
	srv := newWSServer(t, func(s *serverWS, done <-chan struct{}) { <-done })
	c := dial(t, context.Background(), srv.url(), &Options{IdleTimeout: 250 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.Receive(ctx)
	require.Error(t, err)
	require.NotErrorIs(t, err, context.DeadlineExceeded, "the idle deadline must fire, not the test's ctx")
}

// TestOversizedMessagesRefused pins both size caps: one received
// message and one send, each beyond MaxMessageBytes, refuse loudly.
func TestOversizedMessagesRefused(t *testing.T) {
	srv := newWSServer(t, func(s *serverWS, done <-chan struct{}) {
		assert.NoError(t, s.writeText(make([]byte, 4096)))
		<-done
	})
	c := dial(t, context.Background(), srv.url(), &Options{MaxMessageBytes: 1024})

	_, err := c.Receive(context.Background())
	require.Error(t, err)
	assert.ErrorContains(t, err, "exceeds")

	err = c.Send(make([]byte, 2048))
	require.Error(t, err)
	assert.ErrorContains(t, err, "exceeds")
}

// TestProtocolViolationsRefused pins the three wire lies: masked server
// frames, RSV bits (no extension was negotiated), and binary data.
func TestProtocolViolationsRefused(t *testing.T) {
	t.Run("masked server frame", func(t *testing.T) {
		srv := newWSServer(t, func(s *serverWS, done <-chan struct{}) {
			assert.NoError(t, s.writeMaskedViolently(opText, []byte("sneaky")))
			<-done
		})
		c := dial(t, context.Background(), srv.url(), nil)
		_, err := c.Receive(context.Background())
		require.Error(t, err)
		assert.ErrorContains(t, err, "must not be masked")
	})
	t.Run("rsv bits", func(t *testing.T) {
		srv := newWSServer(t, func(s *serverWS, done <-chan struct{}) {
			// A text frame with RSV1 set, hand-rolled.
			_, err := s.conn.Write([]byte{0x80 | 0x40 | opText, 2, 'o', 'k'})
			assert.NoError(t, err)
			<-done
		})
		c := dial(t, context.Background(), srv.url(), nil)
		_, err := c.Receive(context.Background())
		require.Error(t, err)
		assert.ErrorContains(t, err, "rsv")
	})
	t.Run("binary frame", func(t *testing.T) {
		srv := newWSServer(t, func(s *serverWS, done <-chan struct{}) {
			assert.NoError(t, s.writeFrame(true, opBinary, []byte{0x1, 0x2}))
			<-done
		})
		c := dial(t, context.Background(), srv.url(), nil)
		_, err := c.Receive(context.Background())
		require.Error(t, err)
		assert.ErrorContains(t, err, "binary")
	})
}

// TestWSSDialOverTLS pins the wss:// path against a real TLS server.
func TestWSSDialOverTLS(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		s, err := upgrade(rw, r, false)
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
		for {
			f, err := s.readFrame()
			if err != nil {
				return
			}
			if err := s.writeFrame(f.fin, f.opcode, f.payload); err != nil {
				return
			}
		}
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	wsURL := "wss:" + strings.TrimPrefix(srv.URL, "https:")
	c, err := Dial(context.Background(), wsURL+"/events?version=1", nil,
		&Options{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}) //nolint:gosec // test-only trust of a throwaway certificate
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	require.NoError(t, c.Send([]byte("over tls")))
	got, err := c.Receive(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "over tls", string(got))
}

// TestConcurrentSendWhileReceiving pins wmu: sends from one goroutine
// while the automatic pong shares the writer — the race detector's case.
func TestConcurrentSendWhileReceiving(t *testing.T) {
	const n = 20
	srv := newWSServer(t, func(s *serverWS, _ <-chan struct{}) {
		// One ping up front; the echo loop keeps the reader honest while
		// the pong shares the write path with data sends. The loop runs
		// until the peer goes away — pongs read here are not echoes and
		// must not count against the echo budget.
		assert.NoError(t, s.writeFrame(true, opPing, []byte("p")))
		for {
			f, err := s.readFrame()
			if err != nil {
				return
			}
			if f.opcode == opText {
				if err := s.writeText(f.payload); err != nil {
					return
				}
			}
		}
	})
	c := dial(t, context.Background(), srv.url(), nil)

	go func() {
		for i := 0; i < n; i++ {
			if err := c.Send([]byte(strings.Repeat("m", i+1))); err != nil {
				return
			}
		}
	}()
	for i := 0; i < n; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		b, err := c.Receive(ctx)
		cancel()
		require.NoError(t, err)
		assert.Equal(t, strings.Repeat("m", i+1), string(b))
	}
}

// TestSlowConsumerTearsDown pins ErrSlowConsumer: a caller that stops
// receiving cannot stall the reader that answers pings.
func TestSlowConsumerTearsDown(t *testing.T) {
	const n = 70 // buffer is 64
	srv := newWSServer(t, func(s *serverWS, done <-chan struct{}) {
		for i := 0; i < n; i++ {
			if err := s.writeText([]byte{byte('a' + i%26)}); err != nil {
				return
			}
		}
		<-done
	})
	c := dial(t, context.Background(), srv.url(), nil)

	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, err := c.Receive(ctx)
		return errors.Is(err, ErrSlowConsumer)
	}, 3*time.Second, 25*time.Millisecond, "a full buffer must tear the connection down")
}
